// Package e2e 通过真实装配（配置 -> 数据库 -> 路由）验证端到端行为。
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MITEZ360/tenon/internal/bootstrap"
)

type env struct {
	t      testing.TB
	app    *bootstrap.App
	server *httptest.Server
	token  string
}

func newEnv(t testing.TB) *env {
	t.Helper()
	return newEnvWithServer(t, "")
}

// newEnvWithServer 允许在 server 段追加配置行（已带缩进的 YAML），
// 用于验证 body 上限这类必须改启动参数的行为。
func newEnvWithServer(t testing.TB, extraServerLines string) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`
app:
  name: Tenon 测试
  env: test
server:
  host: 127.0.0.1
  port: 18080
  mode: release
%s
log:
  level: warn
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
  captcha_enabled: false
  max_login_fail: 5
  password_min_len: 8
  password_strength: 2
  internal_token: "test-internal-token"
rate_limit:
  enabled: false
metrics:
  enabled: true
storage:
  local_dir: %s/uploads
  max_size: 1048576
micro:
  mode: mono
`, extraServerLines, dir, filepath.Join(dir, "tenon.db"), dir)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	app, err := bootstrap.New(cfgPath)
	if err != nil {
		t.Fatalf("装配应用失败: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})

	e := &env{t: t, app: app, server: httptest.NewServer(app.Engine)}
	t.Cleanup(e.server.Close)
	return e
}

// call 发起请求并返回状态码与解析后的响应体。
func (e *env) call(method, path string, body any, token string) (int, map[string]any) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("序列化请求失败: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		e.t.Fatalf("构造请求失败: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("请求 %s %s 失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// login 登录并缓存令牌。
func (e *env) login(username, password string) (int, map[string]any) {
	code, body := e.call(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username, "password": password,
	}, "")
	return code, body
}

func (e *env) mustLogin(username, password string) {
	e.t.Helper()
	code, body := e.login(username, password)
	if code != http.StatusOK {
		e.t.Fatalf("登录失败: %d %v", code, body)
	}
	data, _ := body["data"].(map[string]any)
	token, _ := data["token"].(map[string]any)
	e.token, _ = token["accessToken"].(string)
	if e.token == "" {
		e.t.Fatalf("未获取到访问令牌: %v", body)
	}
}

func dataOf(body map[string]any) map[string]any {
	d, _ := body["data"].(map[string]any)
	return d
}

func TestHealth(t *testing.T) {
	e := newEnv(t)
	code, body := e.call(http.MethodGet, "/healthz", nil, "")
	if code != http.StatusOK || body["status"] != "up" {
		t.Fatalf("健康检查异常: %d %v", code, body)
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["database"] != "up" || checks["cache"] != "up" {
		t.Fatalf("依赖状态异常: %v", checks)
	}
}

func TestLoginFlow(t *testing.T) {
	e := newEnv(t)

	// 错误密码
	if code, body := e.login("admin", "wrong-password"); code != http.StatusUnauthorized {
		t.Fatalf("错误密码应返回 401，实际 %d %v", code, body)
	}

	e.mustLogin("admin", "Admin@123456")

	// 个人资料与权限
	code, body := e.call(http.MethodGet, "/api/v1/auth/profile", nil, e.token)
	if code != http.StatusOK {
		t.Fatalf("获取资料失败: %d %v", code, body)
	}
	data := dataOf(body)
	user, _ := data["user"].(map[string]any)
	if user["username"] != "admin" {
		t.Fatalf("用户信息异常: %v", user)
	}
	perms, _ := user["perms"].([]any)
	if len(perms) == 0 {
		t.Fatalf("权限点为空: %v", user)
	}

}

func TestRefreshToken(t *testing.T) {
	e := newEnv(t)
	code, body := e.login("admin", "Admin@123456")
	if code != http.StatusOK {
		t.Fatalf("登录失败: %v", body)
	}
	token := dataOf(body)["token"].(map[string]any)
	refresh, _ := token["refreshToken"].(string)

	code, body = e.call(http.MethodPost, "/api/v1/auth/refresh", map[string]string{"refreshToken": refresh}, "")
	if code != http.StatusOK {
		t.Fatalf("刷新失败: %d %v", code, body)
	}
	refreshed := dataOf(body)
	if refreshed["accessToken"] == nil || refreshed["accessToken"] == "" {
		t.Fatalf("刷新未返回新令牌: %v", body)
	}
	if refreshed["refreshToken"] == refresh {
		t.Fatal("刷新令牌应轮换")
	}
	// 新令牌应可直接访问受保护接口
	newAccess, _ := refreshed["accessToken"].(string)
	if code, _ := e.call(http.MethodGet, "/api/v1/auth/profile", nil, newAccess); code != http.StatusOK {
		t.Fatalf("刷新后的令牌不可用，实际 %d", code)
	}
}

func TestUnauthorized(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/v1/users", "/api/v1/roles", "/api/v1/dashboard/overview"} {
		if code, _ := e.call(http.MethodGet, path, nil, ""); code != http.StatusUnauthorized {
			t.Fatalf("%s 未登录应返回 401，实际 %d", path, code)
		}
		if code, _ := e.call(http.MethodGet, path, nil, "invalid-token"); code != http.StatusUnauthorized {
			t.Fatalf("%s 非法令牌应返回 401，实际 %d", path, code)
		}
	}
}

func TestUserCRUD(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")

	// 创建
	code, body := e.call(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "zhangsan", "nickname": "张三", "password": "Zhang@123456",
		"phone": "13700137000", "email": "zhangsan@example.com", "deptId": "d_tech",
		"status": 1, "roleIds": []string{"r_user"},
	}, e.token)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("创建用户失败: %d %v", code, body)
	}
	if dataOf(body)["id"] == nil {
		t.Fatalf("创建用户未返回 ID: %v", body)
	}

	// 列表（手机号应脱敏）
	code, body = e.call(http.MethodGet, "/api/v1/users?page=1&page_size=10&keyword=zhangsan", nil, e.token)
	if code != http.StatusOK {
		t.Fatalf("查询用户列表失败: %d %v", code, body)
	}
	list, _ := dataOf(body)["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("期望命中 1 个用户，实际 %d", len(list))
	}
	row, _ := list[0].(map[string]any)
	if row["phone"] != "137****7000" {
		t.Fatalf("手机号未脱敏: %v", row["phone"])
	}
	uid, _ := row["id"].(string)

	// 更新
	code, body = e.call(http.MethodPut, "/api/v1/users/"+uid, map[string]any{
		"nickname": "张三丰", "phone": "13700137001", "status": 1, "deptId": "d_tech",
	}, e.token)
	if code != http.StatusOK {
		t.Fatalf("更新用户失败: %d %v", code, body)
	}

	// 详情
	code, body = e.call(http.MethodGet, "/api/v1/users/"+uid, nil, e.token)
	if code != http.StatusOK || dataOf(body)["nickname"] != "张三丰" {
		t.Fatalf("用户详情异常: %d %v", code, body)
	}

	// 重置密码
	code, body = e.call(http.MethodPut, "/api/v1/users/"+uid+"/password", map[string]string{"password": "NewPass@123456"}, e.token)
	if code != http.StatusOK {
		t.Fatalf("重置密码失败: %d %v", code, body)
	}

	// 重复用户名冲突
	code, body = e.call(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "zhangsan", "password": "Zhang@123456", "nickname": "张三", "status": 1,
	}, e.token)
	if code != http.StatusConflict {
		t.Fatalf("重复用户名应返回 409，实际 %d %v", code, body)
	}

	// 删除
	if code, body := e.call(http.MethodDelete, "/api/v1/users/"+uid, nil, e.token); code != http.StatusOK {
		t.Fatalf("删除用户失败: %d %v", code, body)
	}
	if code, _ := e.call(http.MethodGet, "/api/v1/users/"+uid, nil, e.token); code != http.StatusNotFound {
		t.Fatalf("删除后详情应 404，实际 %d", code)
	}
}

func TestValidationMessage(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	code, body := e.call(http.MethodPost, "/api/v1/users", map[string]any{"username": "ab", "password": "Abcd@1234", "nickname": "测试"}, e.token)
	if code != http.StatusBadRequest {
		t.Fatalf("参数校验应返回 400，实际 %d %v", code, body)
	}
	msg, _ := body["message"].(string)
	if msg == "" || !bytes.Contains([]byte(msg), []byte("用户名")) {
		t.Fatalf("校验提示应为中文且指明字段: %q", msg)
	}
}

func TestRBACPermission(t *testing.T) {
	e := newEnv(t)
	// demo 账号只有普通用户角色（数据范围=仅本人，无用户管理权限）
	e.mustLogin("demo", "Demo@123456")
	code, body := e.call(http.MethodGet, "/api/v1/users", nil, e.token)
	if code != http.StatusForbidden {
		t.Fatalf("普通用户访问用户管理应 403，实际 %d %v", code, body)
	}
	// 但可以访问自己的工作台
	if code, _ := e.call(http.MethodGet, "/api/v1/dashboard/overview", nil, e.token); code != http.StatusOK {
		t.Fatalf("普通用户访问工作台应 200，实际 %d", code)
	}
}

func TestRoleAndMenu(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")

	code, body := e.call(http.MethodGet, "/api/v1/menus/tree", nil, e.token)
	if code != http.StatusOK {
		t.Fatalf("菜单树获取失败: %d %v", code, body)
	}

	code, body = e.call(http.MethodPost, "/api/v1/roles", map[string]any{
		"name": "运维", "code": "ops", "dataScope": 2, "status": 1, "sort": 10,
	}, e.token)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("创建角色失败: %d %v", code, body)
	}

	code, body = e.call(http.MethodGet, "/api/v1/roles?page=1&page_size=10", nil, e.token)
	if code != http.StatusOK {
		t.Fatalf("角色列表失败: %d %v", code, body)
	}
	if len(dataOf(body)["list"].([]any)) < 3 {
		t.Fatalf("角色数量异常: %v", body)
	}
}

func TestAuditLogRecorded(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	_, _ = e.call(http.MethodPost, "/api/v1/depts", map[string]any{
		"name": "测试部门", "parentId": "d_root", "sort": 99, "status": 1,
	}, e.token)

	// 审计日志异步落库，最多等待 3 秒
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		code, body := e.call(http.MethodGet, "/api/v1/logs/operation?page=1&page_size=20", nil, e.token)
		if code == http.StatusOK {
			if list, ok := dataOf(body)["list"].([]any); ok && len(list) > 0 {
				row, _ := list[0].(map[string]any)
				if row["module"] == "部门管理" {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("操作日志未在预期时间内落库")
}

func TestExportExcel(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")

	req, _ := http.NewRequest(http.MethodGet, e.server.URL+"/api/v1/exports/users", nil)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("导出请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("导出失败: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !bytes.Contains([]byte(ct), []byte("spreadsheet")) {
		t.Fatalf("导出类型错误: %s", ct)
	}
	head := make([]byte, 2)
	_, _ = io.ReadFull(resp.Body, head)
	if string(head) != "PK" {
		t.Fatalf("导出内容不是合法 xlsx（应为 zip 头）: %q", head)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	if code, _ := e.call(http.MethodGet, "/api/v1/auth/profile", nil, e.token); code != http.StatusOK {
		t.Fatal("登出前应可访问")
	}
	if code, body := e.call(http.MethodPost, "/api/v1/auth/logout", nil, e.token); code != http.StatusOK {
		t.Fatalf("登出失败: %d %v", code, body)
	}
	if code, _ := e.call(http.MethodGet, "/api/v1/auth/profile", nil, e.token); code != http.StatusUnauthorized {
		t.Fatalf("登出后应返回 401，实际 %d", code)
	}
}
