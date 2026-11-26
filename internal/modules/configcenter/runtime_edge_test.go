package configcenter

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/model"
	platformconfig "github.com/MITEZ360/tenon/internal/platform/config"
)

func newRuntimeService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	// 运行期配置服务依赖全局配置持有者：Save 内部会调用 config.Apply。
	cfg, err := platformconfig.Load("")
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/rt.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.RuntimeConfig{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return NewService(db, cfg, zap.NewNop()), db
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// TestSaveRuntimeConfigRejectsInvalidValue 越界、类型错误、白名单外的键
// 都必须被拒绝，且不能污染已经生效的配置。
func TestSaveRuntimeConfigRejectsInvalidValue(t *testing.T) {
	s, db := newRuntimeService(t)
	ctx := context.Background()

	res, err := s.Save(ctx, []Update{{Key: "security.password_min_len", Value: "12"}}, "tester")
	if err != nil {
		t.Fatalf("保存合法值失败: %v", err)
	}
	if res.ConfigVersion < 1 {
		t.Fatalf("配置版本应 >= 1，实际 %d", res.ConfigVersion)
	}
	if !contains(res.Applied, "security.password_min_len") {
		t.Fatalf("热更新项应出现在 applied 里: %+v", res)
	}

	bad := []struct {
		name string
		up   Update
	}{
		{"小于下限", Update{Key: "security.password_min_len", Value: "5"}},
		{"大于上限", Update{Key: "security.password_min_len", Value: "65"}},
		{"非数字", Update{Key: "security.password_min_len", Value: "abc"}},
		{"布尔值非法", Update{Key: "security.captcha_enabled", Value: "maybe"}},
		{"时长为零", Update{Key: "security.lock_duration", Value: "0s"}},
		{"时长格式错误", Update{Key: "security.lock_duration", Value: "forever"}},
		{"不在候选值内", Update{Key: "security.password_strength", Value: "9"}},
		{"小数代替整数", Update{Key: "security.password_strength", Value: "2.5"}},
		{"白名单外的键", Update{Key: "app.env", Value: "prod"}},
		{"部署类键", Update{Key: "server.port", Value: "1"}},
		{"键为空", Update{Key: "   ", Value: "x"}},
	}
	for _, tc := range bad {
		if _, err := s.Save(ctx, []Update{tc.up}, "tester"); err == nil {
			t.Fatalf("%s 应被拒绝", tc.name)
		}
	}

	var rows []model.RuntimeConfig
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("非法保存不应写库，实际 %d 行", len(rows))
	}
	if rows[0].Value != "12" {
		t.Fatalf("原值被改动: %q", rows[0].Value)
	}
}

// TestSaveRuntimeConfigEmptyAndMasked 空更新与"只提交掩码"不应产生写操作。
func TestSaveRuntimeConfigEmptyAndMasked(t *testing.T) {
	s, db := newRuntimeService(t)
	ctx := context.Background()

	if _, err := s.Save(ctx, nil, "tester"); err == nil {
		t.Fatal("空更新应被拒绝")
	}
	res, err := s.Save(ctx, []Update{{Key: "mail.password", Value: "******"}}, "tester")
	if err != nil {
		t.Fatalf("掩码提交不应报错: %v", err)
	}
	if res.Message != "没有实际变更" {
		t.Fatalf("掩码提交应报告没有变化，实际 %q", res.Message)
	}
	var count int64
	db.Model(&model.RuntimeConfig{}).Count(&count)
	if count != 0 {
		t.Fatalf("掩码提交不应写库，实际 %d 行", count)
	}
}

// TestSaveRuntimeConfigRestartItem 冷配置只落库、提示重启，不进 applied。
func TestSaveRuntimeConfigRestartItem(t *testing.T) {
	s, db := newRuntimeService(t)
	ctx := context.Background()

	res, err := s.Save(ctx, []Update{{Key: "app.name", Value: "治理面测试"}}, "tester")
	if err != nil {
		t.Fatalf("保存冷配置失败: %v", err)
	}
	if len(res.Applied) != 0 {
		t.Fatalf("冷配置不应立即生效: %v", res.Applied)
	}
	if !contains(res.NeedRestart, "app.name") {
		t.Fatalf("应提示需要重启: %+v", res)
	}
	var row model.RuntimeConfig
	if err := db.Where("config_key = ?", "app.name").First(&row).Error; err != nil {
		t.Fatalf("冷配置应落库: %v", err)
	}
	if row.Value != "治理面测试" {
		t.Fatalf("落库值不对: %q", row.Value)
	}
}

// TestConcurrentSaveRuntimeConfig 并发保存不同键：全部成功，版本号单调唯一。
func TestConcurrentSaveRuntimeConfig(t *testing.T) {
	s, db := newRuntimeService(t)
	ctx := context.Background()

	keys := []Update{
		{Key: "security.password_min_len", Value: "10"},
		{Key: "security.max_login_fail", Value: "7"},
		{Key: "security.captcha_enabled", Value: "true"},
		{Key: "security.session_single", Value: "true"},
		{Key: "security.password_strength", Value: "3"},
		{Key: "security.lock_duration", Value: "30m"},
	}
	var wg sync.WaitGroup
	errsOf := make([]error, len(keys))
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errsOf[i] = s.Save(ctx, []Update{keys[i]}, "tester")
		}(i)
	}
	wg.Wait()
	for i, err := range errsOf {
		if err != nil {
			t.Fatalf("第 %d 个并发保存失败: %v", i, err)
		}
	}

	var rows []model.RuntimeConfig
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(keys) {
		t.Fatalf("应有 %d 行，实际 %d", len(keys), len(rows))
	}
	seen := map[int64]bool{}
	for _, r := range rows {
		if seen[r.Version] {
			t.Fatalf("并发保存产生了重复版本号: %d", r.Version)
		}
		seen[r.Version] = true
		if r.Version < 1 {
			t.Fatalf("版本号应 >= 1，实际 %d", r.Version)
		}
	}
	// 版本号必须连续（每次保存 +1），否则"节点按版本判断要不要重载"会漏。
	for v := int64(1); v <= int64(len(keys)); v++ {
		if !seen[v] {
			t.Fatalf("版本号不连续，缺 %d（实际 %v）", v, seen)
		}
	}
	_ = fmt.Sprint()
}
