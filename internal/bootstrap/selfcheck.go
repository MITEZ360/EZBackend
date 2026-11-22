package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/migrate"
	"github.com/keel-framework/keel/internal/platform/storage"
)

// SelfCheckItem 单个启动自检项。
type SelfCheckItem struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
	Critical bool   `json:"critical"` // 关键项失败时拒绝启动
}

// SelfCheckReport 启动自检报告，挂在 App 上供日志与测试读取。
type SelfCheckReport struct {
	OK        bool            `json:"ok"`
	CheckedAt string          `json:"checkedAt"`
	Items     []SelfCheckItem `json:"items"`
}

// selfCheckDeps 自检所需的运行时依赖，由 New 在装配完成后填充。
type selfCheckDeps struct {
	DB              *gorm.DB
	Cache           cache.Cache
	Storage         *storage.Manager
	MigrationRunner *migrate.Runner
	MigrationMode   string
	SeedEnabled     bool
}

// runSelfChecks 执行启动自检。关键依赖（数据库/缓存/存储）失败即返回错误（fail-fast），
// 非关键项（迁移版本落后、种子状态）只记录到报告并打日志，不阻断启动。
func runSelfChecks(ctx context.Context, deps selfCheckDeps, lg *zap.Logger) (*SelfCheckReport, error) {
	if lg == nil {
		lg = zap.NewNop()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	report := &SelfCheckReport{CheckedAt: time.Now().UTC().Format(time.RFC3339)}

	add := func(name string, critical bool, err error, okDetail string) {
		item := SelfCheckItem{Name: name, Critical: critical, OK: err == nil, Detail: okDetail}
		if err != nil {
			item.Detail = err.Error()
		}
		report.Items = append(report.Items, item)
	}

	// 1. 数据库连通性（关键）
	if deps.DB != nil {
		var one int
		err := deps.DB.WithContext(ctx).Raw("SELECT 1").Scan(&one).Error
		add("database", true, err, "SELECT 1 正常")
	} else {
		add("database", true, fmt.Errorf("数据库未初始化"), "")
	}

	// 2. 缓存可用性（关键）：写入-读取-删除探针
	if deps.Cache != nil {
		err := probeCache(ctx, deps.Cache)
		add("cache", true, err, "kind="+deps.Cache.Kind())
	} else {
		add("cache", true, fmt.Errorf("缓存未初始化"), "")
	}

	// 3. 存储可用性（关键）：Health 探测
	if deps.Storage != nil {
		err := deps.Storage.Health(ctx)
		add("storage", true, err, "kind="+deps.Storage.Kind())
	} else {
		add("storage", true, fmt.Errorf("存储未初始化"), "")
	}

	// 4. 迁移版本（非关键）：落后时告警，不阻断启动
	if deps.MigrationRunner != nil {
		st, err := deps.MigrationRunner.Status(ctx)
		if err != nil {
			add("migration", false, err, "")
		} else {
			detail := fmt.Sprintf("current=%d latest=%d pending=%d", st.Current, st.Latest, st.Pending)
			var mErr error
			if st.Pending > 0 {
				mErr = fmt.Errorf("数据库版本落后：%d 个迁移待应用", st.Pending)
			}
			add("migration", false, mErr, detail)
		}
	} else {
		add("migration", false, nil, "mode="+deps.MigrationMode+"（未启用版本化迁移）")
	}

	// 5. 种子状态（非关键）：仅提示，未开种子时不检查
	if deps.SeedEnabled && deps.DB != nil {
		var users int64
		err := deps.DB.WithContext(ctx).Table("sys_user").Count(&users).Error
		detail := fmt.Sprintf("用户 %d", users)
		var sErr error
		if err == nil && users == 0 {
			sErr = fmt.Errorf("已开启种子但 sys_user 为空，请检查 seed 执行结果")
		}
		add("seed", false, errOr(sErr, err), detail)
	} else {
		add("seed", false, nil, "未开启种子，跳过")
	}

	// 汇总：关键项全部通过即 OK
	report.OK = true
	var criticalFailures []string
	for _, item := range report.Items {
		if !item.OK {
			lg.Warn("启动自检未通过", zap.String("item", item.Name), zap.String("detail", item.Detail))
			if item.Critical {
				criticalFailures = append(criticalFailures, item.Name+": "+item.Detail)
			}
		}
	}
	if len(criticalFailures) > 0 {
		report.OK = false
		return report, fmt.Errorf("启动自检失败（关键依赖不可用）：%s", strings.Join(criticalFailures, "；"))
	}
	lg.Info("启动自检完成",
		zap.Int("items", len(report.Items)),
		zap.Bool("ok", report.OK),
	)
	return report, nil
}

// probeCache 用独立的探针键做一次写-读-删，避免污染业务数据。
func probeCache(ctx context.Context, c cache.Cache) error {
	const key = "keel:boot:probe"
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.Set(probeCtx, key, []byte("1"), 10*time.Second); err != nil {
		return fmt.Errorf("缓存写入失败: %w", err)
	}
	if _, err := c.Get(probeCtx, key); err != nil {
		_ = c.Delete(probeCtx, key)
		return fmt.Errorf("缓存读取失败: %w", err)
	}
	if err := c.Delete(probeCtx, key); err != nil {
		return fmt.Errorf("缓存删除失败: %w", err)
	}
	return nil
}

func errOr(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
