package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MITEZ360/tenon/internal/bootstrap"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// callRaw 发起请求并返回原始响应，用于需要自定义 Header（内部令牌、灰度实例）的场景。
func (e *env) callRaw(method, path string, body any, headers map[string]string) (int, []byte) {
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("请求 %s %s 失败: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (e *env) jsonCall(method, path string, body any, headers map[string]string) (int, map[string]any) {
	e.t.Helper()
	code, raw := e.callRaw(method, path, body, headers)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return code, out
}

// internalHeaders 内部接口的调用凭据；newEnv 配置里写的是 test-internal-token。
func internalHeaders() map[string]string {
	return map[string]string{"X-Internal-Token": "test-internal-token"}
}

func (e *env) authHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer " + e.token}
}

// mustOK 断言请求成功。创建类接口返回 201，其余返回 200，两者都算成功。
func mustOK(t *testing.T, code int, body map[string]any, what string) map[string]any {
	t.Helper()
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("%s 失败: %d %v", what, code, body)
	}
	return dataOf(body)
}

// TestGovernanceControlPlane 走完整治理链路：服务 → 实例 → 路由 → 发布 → 快照 → 灰度 → 配置。
func TestGovernanceControlPlane(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()

	// 1. 内部接口必须带令牌，匿名访问要被拒绝。
	if code, _ := e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("匿名访问快照接口应 401，实际 %d", code)
	}

	// 2. 建服务。
	code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{
		"name": "order-svc", "owner": "ops", "desc": "端到端测试",
		"checkType": "none", "checkInterval": 10, "checkTimeout": 3, "checkFailThreshold": 3,
	}, auth)
	svc := mustOK(t, code, body, "创建服务")

	// 3. 注册两个实例（一个用于灰度）。
	for _, id := range []string{"inst-a", "inst-b"} {
		code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/instances/register", map[string]any{
			"service": "order-svc", "instanceId": id, "host": "10.0.0.9", "port": 8080,
			"version": "1.0.0", "weight": 100, "labels": map[string]string{"zone": "a"},
		}, internalHeaders())
		mustOK(t, code, body, "注册实例 "+id)
	}

	code, body = e.jsonCall(http.MethodGet, "/api/v1/gov/instances?service=order-svc", nil, auth)
	list, _ := mustOK(t, code, body, "实例列表")["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("应有 2 个实例，实际 %d", len(list))
	}

	// 4. 未发布路由时快照里没有 route 分区，数据面应继续用自己的兜底。
	code, raw := e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, internalHeaders())
	if code != http.StatusOK {
		t.Fatalf("拉快照失败: %d %s", code, raw)
	}
	var snap gov.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("快照不可解析: %v", err)
	}
	if _, ok := snap.Parts[gov.PartRoute]; ok {
		t.Fatal("未发布路由时不应下发 route 分区")
	}
	if _, ok := snap.Parts[gov.PartRegistry]; !ok {
		t.Fatal("应下发 registry 分区")
	}
	firstRev := snap.GlobalRev
	if firstRev != 0 {
		t.Fatalf("还没发布过任何东西时全局版本应为 0，实际 %d", firstRev)
	}

	// 5. 建路由并发布。
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/routes", map[string]any{
		"name": "订单入口", "prefix": "/api/v1/order/", "methods": []string{"get"},
		"headers": map[string]string{}, "query": map[string]string{},
		"service": "order-svc", "weight": 100, "strip": true, "timeoutMs": 1000, "retries": 1,
		"status": "draft",
	}, auth)
	route := mustOK(t, code, body, "创建路由")
	if route["prefix"] != "/api/v1/order" {
		t.Fatalf("前缀应被规范化，实际 %v", route["prefix"])
	}

	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish", map[string]any{
		"targets": []string{}, "note": "端到端首发",
	}, auth)
	rel := mustOK(t, code, body, "发布路由")
	pubRev, _ := rel["revision"].(float64)
	if pubRev <= float64(firstRev) {
		t.Fatalf("发布应推进版本号: %v -> %v", firstRev, pubRev)
	}

	// 6. 快照里应出现 route 分区，且校验和与载荷一致。
	code, raw = e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, internalHeaders())
	var snap2 gov.Snapshot
	json.Unmarshal(raw, &snap2)
	part, ok := snap2.Parts[gov.PartRoute]
	if !ok {
		t.Fatal("发布后应下发 route 分区")
	}
	if got := gov.Checksum(part.Payload); got != part.Checksum {
		t.Fatalf("校验和不匹配: %s != %s", got, part.Checksum)
	}
	rp, ok := snap2.Routes()
	if !ok || len(rp.Routes) != 1 {
		t.Fatalf("路由分区内容异常: %+v", rp.Routes)
	}
	if rp.Routes[0].Service != "order-svc" || !rp.Routes[0].Strip {
		t.Fatalf("路由字段异常: %+v", rp.Routes[0])
	}

	// 7. 内容没变时带指纹请求应 304 短路。
	code, _ = e.callRaw(http.MethodGet,
		"/internal/gov/snapshot?fp="+url.QueryEscape(snap2.Fingerprint()), nil, internalHeaders())
	if code != http.StatusNotModified {
		t.Fatalf("内容未变时应 304，实际 %d", code)
	}

	// 8. 灰度发布：名单内拿新版，名单外拿上一版。
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/routes", map[string]any{
		"name": "灰度入口", "prefix": "/api/v1/canary", "methods": []string{},
		"headers": map[string]string{}, "query": map[string]string{},
		"service": "order-svc", "weight": 100, "timeoutMs": 0, "retries": 0,
	}, auth)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("创建灰度路由失败: %d %v", code, body)
	}
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish", map[string]any{
		"targets": []string{"inst-a"}, "note": "灰度",
	}, auth)
	canary := mustOK(t, code, body, "灰度发布")
	if canary["stage"] != "canary" {
		t.Fatalf("应发布为 canary，实际 %v", canary["stage"])
	}

	canaryRoutes := func(instance string) []gov.Route {
		h := internalHeaders()
		h["X-Instance-Id"] = instance
		code, raw := e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, h)
		if code != http.StatusOK {
			t.Fatalf("instance=%s 拉快照失败: %d", instance, code)
		}
		var s gov.Snapshot
		json.Unmarshal(raw, &s)
		rp, _ := s.Routes()
		return rp.Routes
	}
	if got := canaryRoutes("inst-a"); len(got) != 2 {
		t.Fatalf("灰度名单内应看到 2 条规则，实际 %d", len(got))
	}
	if got := canaryRoutes("inst-b"); len(got) != 1 {
		t.Fatalf("灰度名单外应看到上一版 1 条规则，实际 %d", len(got))
	}
	if got := canaryRoutes(""); len(got) != 1 {
		t.Fatalf("未声明身份的数据面应按名单外处理，实际 %d", len(got))
	}

	// 9. 提升为全量后名单外也能看到新规则。
	relID, _ := canary["id"].(string)
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/releases/"+relID+"/promote", nil, auth)
	promoted := mustOK(t, code, body, "提升灰度")
	if promoted["stage"] != "full" {
		t.Fatalf("提升后应为 full，实际 %v", promoted["stage"])
	}
	if got := canaryRoutes("inst-b"); len(got) != 2 {
		t.Fatalf("提升后名单外应看到新规则，实际 %d", len(got))
	}

	// 10. 回滚到首发，产生更大版本号。
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/releases/"+rel["id"].(string)+"/rollback", nil, auth)
	rolled := mustOK(t, code, body, "回滚路由")
	if rolled["revision"].(float64) <= promoted["revision"].(float64) {
		t.Fatalf("回滚应分配更大版本号: %v", rolled["revision"])
	}
	if got := canaryRoutes("inst-b"); len(got) != 1 {
		t.Fatalf("回滚后应只剩首发规则，实际 %d", len(got))
	}

	// 11. 清理：删除服务应级联删实例，同名可重建。
	id, _ := svc["id"].(string)
	if code, body := e.jsonCall(http.MethodDelete, "/api/v1/gov/services/"+id, nil, auth); code != http.StatusOK {
		t.Fatalf("删除服务失败: %d %v", code, body)
	}
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{"name": "order-svc"}, auth)
	mustOK(t, code, body, "重建同名服务")
}

// TestGovernanceConfigLifecycle 配置中心：加密落库、发布载荷保持密文、下发前解密、回滚回写。
func TestGovernanceConfigLifecycle(t *testing.T) {
	t.Setenv("TENON_MASTER_KEY", "e2e-master-key")
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()

	code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/config/namespaces", map[string]any{
		"namespace": "e2e", "env": "test", "desc": "端到端",
	}, auth)
	mustOK(t, code, body, "创建命名空间")

	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/config/items", map[string]any{
		"namespace": "e2e",
		"items": []map[string]any{
			{"key": "db.password", "value": "S3cret-2026", "type": "string", "encrypted": true, "scope": "global"},
			{"key": "feature.flag", "value": "on", "type": "string", "encrypted": false, "scope": "global"},
		},
	}, auth)
	mustOK(t, code, body, "保存配置")

	// 接口只回显掩码。
	code, body = e.jsonCall(http.MethodGet, "/api/v1/gov/config/items?namespace=e2e", nil, auth)
	items, _ := mustOK(t, code, body, "读取配置")["list"].([]any)
	values := map[string]string{}
	for _, it := range items {
		m := it.(map[string]any)
		values[m["key"].(string)] = m["value"].(string)
	}
	if values["db.password"] != "******" || values["feature.flag"] != "on" {
		t.Fatalf("配置回显异常: %v", values)
	}

	// 发布两次，验证版本推进与载荷密文。
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/config/publish", map[string]any{
		"namespace": "e2e", "note": "v1",
	}, auth)
	rel1 := mustOK(t, code, body, "发布配置")
	if strings.Contains(string(mustMarshal(t, rel1)), "S3cret-2026") {
		t.Fatal("发布响应不该出现明文密钥")
	}

	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/config/items", map[string]any{
		"namespace": "e2e",
		"items":     []map[string]any{{"key": "feature.flag", "value": "off", "type": "string", "scope": "global"}},
	}, auth)
	mustOK(t, code, body, "修改配置")
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/config/publish", map[string]any{
		"namespace": "e2e", "note": "v2",
	}, auth)
	rel2 := mustOK(t, code, body, "二次发布")
	if rel2["revision"].(float64) <= rel1["revision"].(float64) {
		t.Fatalf("版本应递增: %v", rel2["revision"])
	}

	// 快照里应是明文，且保留加密标记。
	code, raw := e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, internalHeaders())
	if code != http.StatusOK {
		t.Fatalf("拉快照失败: %d", code)
	}
	var snap gov.Snapshot
	json.Unmarshal(raw, &snap)
	cfg, ok := snap.Config("e2e")
	if !ok {
		t.Fatal("快照应包含 config:e2e 分区")
	}
	if cfg.Items["db.password"].Value != "S3cret-2026" {
		t.Fatalf("下发值应为明文，实际 %q", cfg.Items["db.password"].Value)
	}
	if !cfg.Items["db.password"].Encrypted {
		t.Fatal("应保留加密标记")
	}
	if cfg.Items["feature.flag"].Value != "off" {
		t.Fatalf("应下发最新值 off，实际 %q", cfg.Items["feature.flag"].Value)
	}
	part, _ := snap.Parts[gov.PartConfigPrefix+"e2e"]
	if gov.Checksum(part.Payload) != part.Checksum {
		t.Fatal("解密后应重算校验和")
	}

	// 回滚到 v1：配置项本身也要回到 on。
	code, body = e.jsonCall(http.MethodPost,
		"/api/v1/gov/config/releases/"+rel1["id"].(string)+"/rollback", nil, auth)
	rolled := mustOK(t, code, body, "回滚配置")
	if rolled["revision"].(float64) <= rel2["revision"].(float64) {
		t.Fatalf("回滚应分配更大版本号: %v", rolled["revision"])
	}
	code, body = e.jsonCall(http.MethodGet, "/api/v1/gov/config/items?namespace=e2e", nil, auth)
	items, _ = mustOK(t, code, body, "回滚后读取配置")["list"].([]any)
	for _, it := range items {
		m := it.(map[string]any)
		if m["key"] == "feature.flag" && m["value"] != "on" {
			t.Fatalf("回滚后配置项应回到 on，实际 %v", m["value"])
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestGovernanceMenuAndPermissions 治理菜单与权限点必须真的串起来：
// 菜单挂权限点 → 角色分到菜单 → 用户通过角色拿到权限 → 接口按权限点放行或拒绝。
// 只检查 perms 数组等于 ["*"] 是没有意义的，超管恒定通配，验证不到任何链路。
func TestGovernanceMenuAndPermissions(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	admin := e.authHeaders()

	// 1. 菜单树里要有治理目录，四个页面各自带自己的权限点。
	code, body := e.jsonCall(http.MethodGet, "/api/v1/menus/tree", nil, admin)
	tree := mustOKList(t, code, body, "菜单树")

	govDir := findMenu(tree, "/gov")
	if govDir == nil {
		t.Fatal("菜单树里缺少服务治理目录 /gov")
	}
	wantPerm := map[string]string{
		"/gov/services": "registry:service:list",
		"/gov/routes":   "route:rule:list",
		"/gov/releases": "route:release:list",
		"/gov/config":   "configcenter:config:list",
	}
	children, _ := govDir["children"].([]any)
	got := map[string]string{}
	for _, c := range children {
		node := c.(map[string]any)
		if p, ok := node["path"].(string); ok {
			got[p] = node["perm"].(string)
		}
	}
	for path, perm := range wantPerm {
		if got[path] != perm {
			t.Fatalf("菜单 %s 的权限点应为 %s，实际 %q（全部 %v）", path, perm, got[path], got)
		}
	}

	// 2. 超管的权限点是通配符，这时候任何接口都该放行。
	code, body = e.jsonCall(http.MethodGet, "/api/v1/auth/profile", nil, admin)
	data := mustOK(t, code, body, "读取资料")
	user, _ := data["user"].(map[string]any)
	if perms, _ := user["perms"].([]any); len(perms) != 1 || perms[0] != "*" {
		t.Fatalf("超管权限点应为 [*]，实际 %v", perms)
	}

	// 3. 建一个只分到"服务目录-查看"的角色，验证权限点是真生效的。
	govDirID, _ := govDir["id"].(string)
	servicesID := findMenuID(children, "/gov/services")
	if govDirID == "" || servicesID == "" {
		t.Fatalf("没取到治理目录或服务目录的菜单 ID: dir=%q services=%q", govDirID, servicesID)
	}
	code, body = e.jsonCall(http.MethodPost, "/api/v1/roles", map[string]any{
		"name": "治理观察员", "code": "gov_viewer", "dataScope": 1, "status": 1, "sort": 50,
	}, admin)
	role := mustOK(t, code, body, "创建角色")
	roleID, _ := role["id"].(string)
	code, body = e.jsonCall(http.MethodPut, "/api/v1/roles/"+roleID+"/menus", map[string]any{
		"menuIds": []string{govDirID, servicesID},
	}, admin)
	mustOK(t, code, body, "分配菜单")

	code, body = e.jsonCall(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "govviewer", "nickname": "治理观察员", "password": "Viewer@123456",
		"status": 1, "roleIds": []string{roleID},
	}, admin)
	mustOK(t, code, body, "创建用户")

	viewer := &env{t: t, app: e.app, server: e.server}
	viewer.mustLogin("govviewer", "Viewer@123456")
	vh := viewer.authHeaders()

	// 有 registry:service:list，能看；没有 create，不能建；没有 route 的权限点，看不到路由。
	if code, body := e.jsonCall(http.MethodGet, "/api/v1/gov/services", nil, vh); code != http.StatusOK {
		t.Fatalf("观察员应能查看服务目录，实际 %d %v", code, body)
	}
	if code, _ := e.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{"name": "x-svc"}, vh); code != http.StatusForbidden {
		t.Fatalf("观察员创建服务应 403，实际 %d", code)
	}
	if code, _ := e.jsonCall(http.MethodGet, "/api/v1/gov/routes", nil, vh); code != http.StatusForbidden {
		t.Fatalf("观察员访问路由应 403，实际 %d", code)
	}
	if code, _ := e.jsonCall(http.MethodGet, "/api/v1/gov/config/namespaces", nil, vh); code != http.StatusForbidden {
		t.Fatalf("观察员访问配置中心应 403，实际 %d", code)
	}

	// 4. 观察员看到的菜单里只有治理目录与服务目录，不该出现路由与配置。
	code, body = e.jsonCall(http.MethodGet, "/api/v1/auth/profile", nil, vh)
	viewerProfile := mustOK(t, code, body, "观察员资料")
	vmenus, _ := viewerProfile["menus"].([]any)
	if dir := findMenu(vmenus, "/gov"); dir == nil {
		t.Fatal("观察员菜单里应有 /gov 目录")
	} else if ch, _ := dir["children"].([]any); len(ch) != 1 || findMenu(ch, "/gov/services") == nil {
		t.Fatalf("观察员菜单里只应有服务目录，实际 %v", pathsOf(ch))
	}
}

// findMenu 在菜单树里按路径找节点（只查一层 children，够用）。
func findMenu(nodes []any, path string) map[string]any {
	for _, n := range nodes {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		if node["path"] == path {
			return node
		}
	}
	return nil
}

func findMenuID(nodes []any, path string) string {
	if node := findMenu(nodes, path); node != nil {
		id, _ := node["id"].(string)
		return id
	}
	return ""
}

func pathsOf(nodes []any) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if node, ok := n.(map[string]any); ok {
			if p, ok := node["path"].(string); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

// mustOKList 解出 data 里的 list 数组，元素保留为 map
func mustOKList(t *testing.T, code int, body map[string]any, what string) []any {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("%s 失败: %d %v", what, code, body)
	}
	raw, _ := body["data"].([]any)
	return raw
}

// gatewayConfig 生成一个纯治理模式的网关配置：没有静态路由，全部靠控制面快照。
func gatewayConfig(dir, controlURL string) string {
	return fmt.Sprintf(`
app:
  name: tenon-gateway-e2e
  env: test
server:
  host: 127.0.0.1
  port: 18097
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
  enabled: false
security:
  captcha_enabled: false
  internal_token: "test-internal-token"
rate_limit:
  enabled: false
storage:
  local_dir: %s/uploads
micro:
  mode: gateway
  routes: []
gov:
  enabled: true
  control_url: "%s"
  poll_interval: 200ms
  snapshot_file: %s
  service_name: tenon-gateway-e2e
  announce: false
`, dir, filepath.Join(dir, "gw.db"), dir, controlURL, filepath.Join(dir, "gw-snapshot.json"))
}

// startGateway 起一个纯治理模式的网关，指向给定的控制面地址。
//
// 它用的是真实的 bootstrap 装配而不是直接 New 一个 Gateway：订阅、轮询、SSE、
// 自注册这些才是"能不能跟上控制面"的关键，绕过它们测出来的收敛是假的。
func startGateway(t *testing.T, controlURL string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gateway.yaml")
	if err := os.WriteFile(cfgPath, []byte(gatewayConfig(dir, controlURL)), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := bootstrap.New(cfgPath)
	if err != nil {
		t.Fatalf("装配网关失败: %v", err)
	}
	t.Cleanup(func() {
		// 清理顺序：先关 HTTP 服务再停 App，最后才是控制面的 httptest。
		// Shutdown 会取消订阅上下文，SSE 长连接跟着断，控制面那边才不会卡在 Close。
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	srv := httptest.NewServer(app.Engine)
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestGatewayFollowsSnapshot 控制面发布路由后，网关应在秒级内按快照转发；
// 未发布前保持 404（纯治理模式没有静态兜底）。
func TestGatewayFollowsSnapshot(t *testing.T) {
	cp := newEnv(t)
	cp.mustLogin("admin", "Admin@123456")
	auth := cp.authHeaders()

	upstream := newEchoUpstream(t)
	host, port := splitAddr(t, upstream.URL)

	gwSrv := startGateway(t, cp.server.URL)
	client := &http.Client{Timeout: 2 * time.Second}

	// 发布前：没有快照、没有静态规则 → 404。
	if code := statusOf(t, client, gwSrv+"/api/hello"); code != http.StatusNotFound {
		t.Fatalf("发布前应 404，实际 %d", code)
	}

	// 建服务、注册上游实例。
	code, body := cp.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{"name": "echo-svc"}, auth)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("创建服务失败: %d %v", code, body)
	}
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/instances/register", map[string]any{
		"service": "echo-svc", "instanceId": "echo-1", "host": host, "port": port,
		"version": "1.0.0", "weight": 100,
	}, internalHeaders())
	if code != http.StatusOK {
		t.Fatalf("注册实例失败: %d %v", code, body)
	}

	// 发布路由。
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/routes", map[string]any{
		"name": "echo", "prefix": "/api", "service": "echo-svc", "weight": 100,
	}, auth)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("创建路由失败: %d %v", code, body)
	}
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish", map[string]any{"note": "e2e"}, auth)
	if code != http.StatusOK {
		t.Fatalf("发布失败: %d %v", code, body)
	}

	// 网关应在 3 秒内收敛（SSE 立即、轮询 200ms 兜底）。
	deadline := time.Now().Add(3 * time.Second)
	var lastCode int
	for time.Now().Before(deadline) {
		lastCode = statusOf(t, client, gwSrv+"/api/hello")
		if lastCode == http.StatusOK {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastCode != http.StatusOK {
		t.Fatalf("发布后网关未收敛，最后一次状态 %d", lastCode)
	}
	if got := upstream.paths.Load(); got != "/api/hello" {
		t.Fatalf("上游应收到原始路径（strip=false），实际 %v", got)
	}

	// 删除规则后重新发布空集合是禁止的，改为直接修改路由目标：把前缀换成 /other。
	routes := mustList(t, cp, "/api/v1/gov/routes", auth)
	routeID := routes[0]["id"].(string)
	code, body = cp.jsonCall(http.MethodPut, "/api/v1/gov/routes/"+routeID, map[string]any{
		"name": "echo", "prefix": "/other", "service": "echo-svc", "weight": 100,
	}, auth)
	if code != http.StatusOK {
		t.Fatalf("更新路由失败: %d %v", code, body)
	}
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish", map[string]any{"note": "move"}, auth)
	if code != http.StatusOK {
		t.Fatalf("二次发布失败: %d %v", code, body)
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if statusOf(t, client, gwSrv+"/api/hello") == http.StatusNotFound &&
			statusOf(t, client, gwSrv+"/other/hello") == http.StatusOK {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("路由变更未收敛：/api=%d /other=%d",
		statusOf(t, client, gwSrv+"/api/hello"), statusOf(t, client, gwSrv+"/other/hello"))
}

// TestGatewaySeesNewInstanceWithoutPublish 扩容不需要发布路由，网关就该把新实例纳入负载均衡。
//
// 这是"用内容指纹而不是全局版本号判断变没变"的回归测试。注册实例不推进版本号，
// 用版本号去重的数据面会一直拿到 304，永远停在旧的实例表上——流量全打到老实例，
// 新扩容的机器一台都不接，而且从控制面看一切正常。
//
// 路由前缀必须以 /api 开头：网关形态只接管 /api/*，其余路径留给控制台单页，
// 写到别的路径上会被前端兜底接走，返回 200 的 HTML，看起来"通"其实没转发。
func TestGatewaySeesNewInstanceWithoutPublish(t *testing.T) {
	cp := newEnv(t)
	cp.mustLogin("admin", "Admin@123456")
	auth := cp.authHeaders()

	first := newEchoUpstream(t)
	second := newEchoUpstream(t)
	h1, p1 := splitAddr(t, first.URL)
	h2, p2 := splitAddr(t, second.URL)

	gwSrv := startGateway(t, cp.server.URL)
	client := &http.Client{Timeout: 2 * time.Second}

	code, body := cp.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{"name": "scale-svc"}, auth)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("创建服务失败: %d %v", code, body)
	}
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/routes", map[string]any{
		"name": "scale", "prefix": "/api/scale", "service": "scale-svc", "weight": 100,
	}, auth)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("创建路由失败: %d %v", code, body)
	}
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/instances/register", map[string]any{
		"service": "scale-svc", "instanceId": "s-1", "host": h1, "port": p1,
		"version": "1.0.0", "weight": 100,
	}, internalHeaders())
	if code != http.StatusOK {
		t.Fatalf("注册实例失败: %d %v", code, body)
	}
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish", map[string]any{"note": "首发"}, auth)
	if code != http.StatusOK {
		t.Fatalf("发布失败: %d %v", code, body)
	}

	waitFor(t, 3*time.Second, "首发路由生效", func() bool {
		return statusOf(t, client, gwSrv+"/api/scale/ping") == http.StatusOK
	})
	if first.hits.Load() == 0 {
		t.Fatal("首个实例应收到请求")
	}

	// 只注册第二个实例，不发布任何东西。
	code, body = cp.jsonCall(http.MethodPost, "/api/v1/gov/instances/register", map[string]any{
		"service": "scale-svc", "instanceId": "s-2", "host": h2, "port": p2,
		"version": "1.0.0", "weight": 100,
	}, internalHeaders())
	if code != http.StatusOK {
		t.Fatalf("注册第二个实例失败: %d %v", code, body)
	}

	// 均衡是按请求轮转的，多发几次两台都该命中。
	waitFor(t, 3*time.Second, "新实例进入负载均衡", func() bool {
		for i := 0; i < 4; i++ {
			_ = statusOf(t, client, gwSrv+"/api/scale/ping")
		}
		return second.hits.Load() > 0
	})
}

// waitFor 轮询直到条件成立，超时即失败。
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s：%s 内未达成", what, timeout)
}

// echoUpstream 是回显路径的上游，记录最后一次收到的路径与命中次数。
type echoUpstream struct {
	srv   *httptest.Server
	URL   string
	hits  atomic.Int64
	paths atomic.Value
}

func newEchoUpstream(t *testing.T) *echoUpstream {
	t.Helper()
	u := &echoUpstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.paths.Store(r.URL.Path)
		_, _ = io.WriteString(w, "echo:"+r.URL.Path)
	}))
	t.Cleanup(u.srv.Close)
	u.URL = u.srv.URL
	return u
}

func splitAddr(t *testing.T, raw string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

func statusOf(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func mustList(t *testing.T, e *env, path string, headers map[string]string) []map[string]any {
	t.Helper()
	code, body := e.jsonCall(http.MethodGet, path, nil, headers)
	data := mustOK(t, code, body, "列表 "+path)
	raw, _ := data["list"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}
