package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/platform/config"
)

// dialectRow 是跨方言契约测试用的最小模型，字段覆盖本框架最易出方言差异的几类：
//  - EpochMs int64：重现「PG 上 INTEGER 装不下 epoch 毫秒、SQLite 却正常」的坑
//  - Meta JSON：JSON/文本序列化
//  - Enabled bool：布尔列
//  - Code 唯一索引：唯一约束与冲突行为
type dialectRow struct {
	ID      uint64 `gorm:"primaryKey;autoIncrement"`
	Code    string `gorm:"size:64;not null;uniqueIndex"`
	EpochMs int64  `gorm:"not null;index"`
	Meta    string `gorm:"type:text"`
	Enabled bool   `gorm:"not null"`
}

type dialectCase struct {
	name string
	dsn  string
	open func(string) (gorm.Dialector, error)
}

func dialectTargets() []dialectCase {
	out := []dialectCase{}
	if dsn := os.Getenv("KEEL_TEST_MYSQL_DSN"); dsn != "" {
		out = append(out, dialectCase{"mysql", dsn, func(s string) (gorm.Dialector, error) {
			return mysql.Open(s + "?parseTime=True&charset=utf8mb4"), nil
		}})
	}
	if dsn := os.Getenv("KEEL_TEST_PG_DSN"); dsn != "" {
		out = append(out, dialectCase{"postgres", dsn, func(s string) (gorm.Dialector, error) {
			return postgres.Open(s), nil
		}})
	}
	return out
}

// TestDialectContract 在 MySQL / PostgreSQL 上跑同一套契约断言。
// 未设置对应 DSN 时自动跳过，因此本地 `go test ./...` 不受影响。
func TestDialectContract(t *testing.T) {
	targets := dialectTargets()
	if len(targets) == 0 {
		t.Skip("未设置 KEEL_TEST_MYSQL_DSN / KEEL_TEST_PG_DSN，跳过跨方言契约测试")
	}
	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			dial, err := tc.open(tc.dsn)
			if err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(dial, &gorm.Config{})
			if err != nil {
				t.Fatalf("连接失败: %v", err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := sqlDB.PingContext(ctx); err != nil {
				t.Fatalf("Ping 失败: %v", err)
			}
			// 用唯一表名避免并发/重跑互相污染；每次操作新建句柄，避免 GORM
			// Statement 复用导致上一次失败 SQL 污染下一次查询。
			table := fmt.Sprintf("keel_dialect_%d", time.Now().UnixNano())
			q := func() *gorm.DB { return db.Table(table) }
			if err := q().AutoMigrate(&dialectRow{}); err != nil {
				t.Fatalf("AutoMigrate 失败: %v", err)
			}
			t.Cleanup(func() { _ = db.Migrator().DropTable(table) })

			epoch := time.Now().UnixMilli() // 13 位，超过 PG INTEGER 上限，验证列类型必须用 BIGINT
			meta, _ := json.Marshal(map[string]any{"title": "中文标题", "n": 42})
			rec := dialectRow{Code: "A-001", EpochMs: epoch, Meta: string(meta), Enabled: true}
			if err := q().Create(&rec).Error; err != nil {
				t.Fatalf("写入失败: %v", err)
			}

			// 1. 时间列：epoch 毫秒往返不丢失
			var got dialectRow
			if err := q().Where("code = ?", "A-001").First(&got).Error; err != nil {
				t.Fatalf("查询失败: %v", err)
			}
			if got.EpochMs != epoch {
				t.Fatalf("epoch 毫秒不一致: got %d want %d", got.EpochMs, epoch)
			}

			// 2. JSON 列往返
			var parsed map[string]any
			if err := json.Unmarshal([]byte(got.Meta), &parsed); err != nil {
				t.Fatalf("JSON 解析失败: %v", err)
			}
			if parsed["title"] != "中文标题" {
				t.Fatalf("JSON 内容不一致: %v", parsed)
			}

			// 3. 布尔列往返
			if !got.Enabled {
				t.Fatal("布尔列 true 未正确往返")
			}

			// 4. 唯一索引：重复 Code 必须冲突
			dup := dialectRow{Code: "A-001", EpochMs: epoch, Enabled: false}
			if err := q().Create(&dup).Error; err == nil {
				t.Fatal("唯一索引未生效：重复 Code 竟然写入成功")
			}

			// 5. 模糊查询：中文关键词 + LIKE
			var likeCount int64
			if err := q().Where("meta LIKE ?", "%中文%").Count(&likeCount).Error; err != nil {
				t.Fatalf("模糊查询失败: %v", err)
			}
			if likeCount != 1 {
				t.Fatalf("模糊查询结果数错误: %d", likeCount)
			}
			t.Logf("方言契约通过：epoch=%d, json/布尔/唯一索引/LIKE 均正常", epoch)
		})
	}
}

// TestDialectDriverResolution 验证配置层驱动推断在三方言下一致（不依赖真实数据库）。
func TestDialectDriverResolution(t *testing.T) {
	cases := []struct {
		driver string
		dsn    string
		want   string
	}{
		{"", "data/keel.db", "sqlite"},
		{"mysql", "user:pass@tcp(127.0.0.1:3306)/keel", "mysql"},
		{"postgres", "postgres://u:p@127.0.0.1:5432/keel", "postgres"},
	}
	for _, c := range cases {
		cfg := config.DB{Driver: c.driver, DSN: c.dsn}
		if got := cfg.ResolveDriver(); got != c.want {
			t.Errorf("ResolveDriver(%q) = %q, want %q", c.driver, got, c.want)
		}
	}
}
