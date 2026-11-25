package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MITEZ360/tenon/internal/bootstrap"
)

// TestBootSelfCheckHealthy 验证正常装配时自检报告全绿，且关键项均为 OK。
func TestBootSelfCheckHealthy(t *testing.T) {
	dir := t.TempDir()
	cfg := fmt.Sprintf(`
app:
  name: Tenon 自检测试
  env: test
server:
  host: 127.0.0.1
  port: 18082
  mode: release
log:
  level: error
  format: json
  dir: %s/logs
  console: false
db:
  driver: sqlite
  dsn: %s
  auto_migrate: true
  seed: true
redis:
  enabled: false
security:
  internal_token: "test-internal-token"
rate_limit:
  enabled: false
storage:
  local_dir: %s/uploads
micro:
  mode: mono
`, dir, filepath.Join(dir, "tenon.db"), dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := bootstrap.New(cfgPath)
	if err != nil {
		t.Fatalf("正常装配不应失败: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})

	report := app.SelfCheck
	if report == nil {
		t.Fatal("自检报告不应为空")
	}
	if !report.OK {
		t.Fatalf("自检报告应为 OK: %+v", report.Items)
	}
	want := map[string]bool{"database": true, "cache": true, "storage": true}
	seen := map[string]bool{}
	for _, item := range report.Items {
		if want[item.Name] {
			seen[item.Name] = true
			if !item.OK || !item.Critical {
				t.Errorf("关键项 %s 应为 OK 且 Critical: %+v", item.Name, item)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("缺少自检项 %s", name)
		}
	}
}

// TestBootSelfCheckFailFast 验证关键依赖不可用（Redis 配置了但连不上）时拒绝启动。
func TestBootSelfCheckFailFast(t *testing.T) {
	dir := t.TempDir()
	cfg := fmt.Sprintf(`
app:
  name: Tenon 自检失败测试
  env: test
server:
  host: 127.0.0.1
  port: 18083
  mode: release
log:
  level: error
  format: json
  dir: %s/logs
  console: false
db:
  driver: sqlite
  dsn: %s
  auto_migrate: true
  seed: false
redis:
  enabled: true
  addr: 127.0.0.1:1
  dial_timeout: 200ms
security:
  internal_token: "test-internal-token"
rate_limit:
  enabled: false
storage:
  local_dir: %s/uploads
micro:
  mode: mono
`, dir, filepath.Join(dir, "tenon.db"), dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := bootstrap.New(cfgPath)
	if err == nil {
		t.Fatal("缓存不可达时应 fail-fast 返回错误")
	}
}
