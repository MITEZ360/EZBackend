package micro

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

func init() { gin.SetMode(gin.TestMode) }

func newGW(t *testing.T, cfg config.Micro) *Gateway {
	t.Helper()
	gw, err := New(cfg, nil, "internal-token", "tenon-gateway", nil)
	if err != nil {
		t.Fatalf("创建网关失败: %v", err)
	}
	return gw
}

// do 通过真实 HTTP 服务发起请求：ReverseProxy 需要底层 Writer 支持 CloseNotify。
func do(t *testing.T, gw *Gateway, path string) (*http.Response, string) {
	t.Helper()
	e := gin.New()
	e.Any("/api/*path", gw.Handler())
	e.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })
	srv := httptest.NewServer(e)
	defer srv.Close()
	resp, err := http.Get(srv.URL + path) //nolint:noctx // 测试代码
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestRouteMatchLongestPrefix(t *testing.T) {
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{
		{Prefix: "/api/v1", Service: "core"},
		{Prefix: "/api/v1/auth", Service: "auth"},
		{Prefix: "/api/v1/auth/login", Service: "sso"},
	}})
	cases := map[string]string{
		"/api/v1/users":      "core",
		"/api/v1/auth/login": "sso",
		"/api/v1/auth/token": "auth",
		"/api/v2/users":      "",
	}
	for path, want := range cases {
		rt := gw.match(path)
		got := ""
		if rt != nil {
			got = rt.Service
		}
		if got != want {
			t.Fatalf("路径 %s 期望命中 %q，实际 %q", path, want, got)
		}
	}
}

func TestGatewayConfigValidation(t *testing.T) {
	// 空规则集是合法的：纯治理模式下路由全部来自控制面快照。
	// "两样都没有"由装配层拒绝，见 bootstrap.newGateway。
	if _, err := New(config.Micro{}, nil, "", "", nil); err != nil {
		t.Fatalf("空静态规则应被允许: %v", err)
	}
	if _, err := New(config.Micro{Routes: []config.ProxyRoute{{Prefix: "/a", Service: "x"}, {Prefix: "/a", Service: "y"}}}, nil, "", "", nil); err == nil {
		t.Fatal("重复前缀应报错")
	}
	if _, err := New(config.Micro{Routes: []config.ProxyRoute{{Prefix: "/a"}}}, nil, "", "", nil); err == nil {
		t.Fatal("缺少 service/targets 应报错")
	}
}

func TestGatewayProxyAndStrip(t *testing.T) {
	var gotPath, gotToken, gotCaller string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Internal-Token")
		gotCaller = r.Header.Get("X-Caller-Service")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": "ok"})
	}))
	defer backend.Close()

	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{
		{Prefix: "/api/v1", Targets: []string{backend.URL}, Strip: true},
	}})
	resp, body := do(t, gw, "/api/v1/users?page=1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body=%s", resp.StatusCode, body)
	}
	if gotPath != "/users" {
		t.Fatalf("前缀未剥离，实际路径 %s", gotPath)
	}
	if gotToken != "internal-token" {
		t.Fatalf("内部令牌未注入: %q", gotToken)
	}
	if gotCaller != "tenon-gateway" {
		t.Fatalf("调用方标识未注入: %q", gotCaller)
	}
}

func TestGatewayFailoverRetry(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer ok.Close()

	gw := newGW(t, config.Micro{
		Timeout: time.Second, Retries: 2,
		Routes: []config.ProxyRoute{{Prefix: "/api/v1", Targets: []string{"127.0.0.1:1", ok.URL}}},
	})
	resp, body := do(t, gw, "/api/v1/ping")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应通过重试落到健康实例，实际 %d，body=%s", resp.StatusCode, body)
	}
}

func TestGatewayAllDown(t *testing.T) {
	gw := newGW(t, config.Micro{
		Timeout: 300 * time.Millisecond, Retries: 0,
		Routes: []config.ProxyRoute{{Prefix: "/api/v1", Targets: []string{"127.0.0.1:1"}}},
	})
	resp, body := do(t, gw, "/api/v1/ping")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("后端全挂时应返回 503，实际 %d", resp.StatusCode)
	}
	if !json.Valid([]byte(body)) {
		t.Fatalf("应返回统一 JSON 错误体: %s", body)
	}
}

func TestGatewayUnknownPath(t *testing.T) {
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{{Prefix: "/api/v1", Targets: []string{"127.0.0.1:1"}}}})
	resp, _ := do(t, gw, "/other")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未知路径应返回 404，实际 %d", resp.StatusCode)
	}
}
