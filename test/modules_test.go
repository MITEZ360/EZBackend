package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keel-framework/keel/internal/bootstrap"
)

func moduleTestConfig(dir string, modules string) string {
	return fmt.Sprintf(`
app:
  name: Keel 模块裁剪测试
  env: test
server:
  host: 127.0.0.1
  port: 18084
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
modules:
  enabled: [%s]
micro:
  mode: mono
`, dir, filepath.Join(dir, "keel.db"), dir, modules)
}

// TestModulePruning 验证按 modules.enabled 裁剪后：健康检查仍可用、被裁剪模块的路由消失。
func TestModulePruning(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// 只保留 dashboard：user/news/monitor 等应被裁剪
	if err := os.WriteFile(cfgPath, []byte(moduleTestConfig(dir, "dashboard")), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := bootstrap.New(cfgPath)
	if err != nil {
		t.Fatalf("裁剪装配失败: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})

	srv := httptest.NewServer(app.Engine)
	defer srv.Close()

	// 1. /healthz 必须在只挂少量模块时仍返回 200
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", resp.StatusCode)
	}

	// 2. 路由清单：保留的 dashboard 在、被裁剪的 user/news/monitor 不在
	routes := app.Engine.Routes()
	paths := make([]string, 0, len(routes))
	for _, r := range routes {
		paths = append(paths, r.Path)
	}
	joined := strings.Join(paths, "\n")
	has := func(frag string) bool { return strings.Contains(joined, frag) }
	if !has("/api/v1/dashboard/overview") {
		t.Error("dashboard 模块应保留路由")
	}
	for _, trimmed := range []string{"/api/v1/users", "/api/v1/news", "/api/v1/monitor"} {
		if has(trimmed) {
			t.Errorf("模块被裁剪但路由仍挂载: %s", trimmed)
		}
	}
	// 基础模块公开路由仍在
	if !has("/api/v1/auth/login") || !has("/api/v1/public/configs") {
		t.Error("基础模块（auth/config）公开路由必须常驻")
	}
}

// TestModulePruningUnknownFailFast 验证未知模块名在装配阶段即失败。
func TestModulePruningUnknownFailFast(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(moduleTestConfig(dir, "does-not-exist")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.New(cfgPath); err == nil {
		t.Fatal("未知模块应 fail-fast")
	}
}
