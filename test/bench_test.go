package e2e

import (
	"net/http"
	"testing"
)

// benchGet 复用一次装配，避免把启动开销计入基准。
// 并行客户端各自复用长连接，贴近真实网关/服务间调用场景。
func benchGet(b *testing.B, path string, auth bool) {
	b.Helper()
	e := newEnv(b)
	code, body := e.login("admin", "Admin@123456")
	if code != http.StatusOK {
		b.Fatalf("登录失败: %v", body)
	}
	token := dataOf(body)["token"].(map[string]any)
	access, _ := token["accessToken"].(string)

	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 256}}
	defer client.CloseIdleConnections()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
			if auth {
				req.Header.Set("Authorization", "Bearer "+access)
			}
			resp, err := client.Do(req)
			if err != nil {
				b.Errorf("请求失败: %v", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				b.Errorf("%s 返回 %d", path, resp.StatusCode)
				return
			}
		}
	})
}

// BenchmarkHealthz 衡量纯 HTTP 链路开销（无数据库、无鉴权）。
func BenchmarkHealthz(b *testing.B) { benchGet(b, "/healthz", false) }

// BenchmarkListUsers 衡量鉴权 + RBAC + 分页查询的典型列表接口。
func BenchmarkListUsers(b *testing.B) { benchGet(b, "/api/v1/users?page=1&page_size=20", true) }

// BenchmarkProfile 衡量会话校验 + 权限加载（缓存命中）路径。
func BenchmarkProfile(b *testing.B) { benchGet(b, "/api/v1/auth/profile", true) }
