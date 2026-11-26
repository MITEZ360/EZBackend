package gov

import (
	"net/http"
	"net/url"
	"testing"
)

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

// TestCompileSortRules 编译顺序必须是完全确定的四段式排序。
func TestCompileSortRules(t *testing.T) {
	routes := []Route{
		{ID: "low", Prefix: "/api", Service: "a", Priority: 0},
		{ID: "high", Prefix: "/api", Service: "b", Priority: 10},
		{ID: "cond", Prefix: "/api", Service: "c", Priority: 10, Headers: map[string]string{"X-Tenant": "vip"}},
		{ID: "deep", Prefix: "/api/v1/order", Service: "d", Priority: 10, Headers: map[string]string{"X-Tenant": "vip"}},
		{ID: "aaa", Prefix: "/api/v1", Service: "e", Priority: 10},
	}
	c := Compile(routes)
	if c.Len() != 5 {
		t.Fatalf("规则数应为 5，实际 %d", c.Len())
	}

	cases := []struct {
		path, header, headerVal, want string
	}{
		// 同优先级同前缀下，条件更具体的优先。
		{"/api/v1/order/detail", "X-Tenant", "vip", "deep"},
		// 前缀更长但条件少，仍优先于前缀短的条件规则。
		{"/api/v1/user", "", "", "aaa"},
		// 不在更具体前缀下时，命中带 Header 条件的 cond，而不是 priority 更低的 low。
		{"/api/other", "X-Tenant", "vip", "cond"},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.header != "" {
			h.Set(tc.header, tc.headerVal)
		}
		got := c.Match(http.MethodGet, tc.path, h, url.Values{})
		if got == nil || got.ID != tc.want {
			t.Fatalf("Match(%s) 期望 %s，实际 %+v", tc.path, tc.want, got)
		}
	}
}

// TestCompileSamePriorityPrefix 同优先级、同条件、同前缀时按 ID 字典序，保证注册顺序无关。
func TestCompileSamePriorityPrefix(t *testing.T) {
	c := Compile([]Route{
		{ID: "b", Prefix: "/api", Service: "svc-b"},
		{ID: "a", Prefix: "/api", Service: "svc-a"},
	})
	got := c.Match(http.MethodGet, "/api/x", http.Header{}, url.Values{})
	if got == nil || got.ID != "a" {
		t.Fatalf("应按 ID 字典序选 a，实际 %+v", got)
	}
}

// TestCompileNormalizeAndSkip 前缀规范化与非法规则跳过。
func TestCompileNormalizeAndSkip(t *testing.T) {
	c := Compile([]Route{
		{ID: "trail", Prefix: "/api/v1/", Service: "a"},
		{ID: "nolead", Prefix: "api/v2", Service: "b"},
		{ID: "", Prefix: "/empty-id", Service: "c"},  // 无 ID，跳过
		{ID: "noprefix", Prefix: "  ", Service: "d"}, // 空前缀，跳过
		{ID: "dup", Prefix: "/dup", Service: "e"},    // 重复 ID，只留第一条
		{ID: "dup", Prefix: "/dup2", Service: "f"},
	})
	if c.Len() != 3 {
		t.Fatalf("有效规则应为 3 条，实际 %d", c.Len())
	}
	for _, p := range []string{"/api/v1/users", "/api/v2/users", "/dup/x"} {
		if c.Match(http.MethodGet, p, http.Header{}, url.Values{}) == nil {
			t.Fatalf("路径 %s 应命中", p)
		}
	}
	// 重复 ID 的第二条被丢弃，其前缀不再可用。
	if r := c.Match(http.MethodGet, "/dup2/x", http.Header{}, url.Values{}); r != nil {
		t.Fatalf("重复 ID 的第二条规则应被丢弃，实际 %+v", r)
	}
	// 末尾斜杠必须被吃掉，不会出现 /api/v1/ 这种键。
	got := c.Match(http.MethodGet, "/api/v1/", http.Header{}, url.Values{})
	if got == nil || got.ID != "trail" {
		t.Fatalf("=/api/v1/ 应命中 trail，实际 %+v", got)
	}
}

// TestMatchMethodAndConditions 方法、Header、Query 三类条件的组合过滤。
func TestMatchMethodAndConditions(t *testing.T) {
	c := Compile([]Route{
		{ID: "get-only", Prefix: "/api", Service: "a", Methods: []string{"GET"}},
		{ID: "post-vip", Prefix: "/api", Service: "b", Priority: 5,
			Methods: []string{"POST"}, Headers: map[string]string{"X-Tenant": "vip"},
			Query: map[string]string{"debug": "1"}},
	})

	if r := c.Match(http.MethodGet, "/api/x", http.Header{}, url.Values{}); r == nil || r.ID != "get-only" {
		t.Fatalf("GET 应命中 get-only: %+v", r)
	}
	// DELETE 不匹配任何规则。
	if r := c.Match(http.MethodDelete, "/api/x", http.Header{}, url.Values{}); r != nil {
		t.Fatalf("DELETE 不应命中: %+v", r)
	}
	// 方法大小写不敏感。
	if r := c.Match("get", "/api/x", http.Header{}, url.Values{}); r == nil || r.ID != "get-only" {
		t.Fatalf("小写方法名应命中: %+v", r)
	}
	// 三条件齐全才命中。
	q := url.Values{"debug": {"1"}}
	if r := c.Match(http.MethodPost, "/api/x", hdr("X-Tenant", "vip"), q); r == nil || r.ID != "post-vip" {
		t.Fatalf("三条件齐全应命中 post-vip: %+v", r)
	}
	// 缺 Query 时降级到 get-only？不，方法不匹配，应无命中。
	if r := c.Match(http.MethodPost, "/api/x", hdr("X-Tenant", "vip"), url.Values{}); r != nil {
		t.Fatalf("缺 Query 条件不应命中: %+v", r)
	}
}

// TestMatchLongestPrefixGeneralization 前缀逐级泛化：未命中更具体前缀时回退到更短前缀。
func TestMatchLongestPrefixGeneralization(t *testing.T) {
	c := Compile([]Route{
		{ID: "root", Prefix: "/", Service: "root"},
		{ID: "v1", Prefix: "/api/v1", Service: "v1"},
		{ID: "order", Prefix: "/api/v1/order", Service: "order"},
	})
	cases := map[string]string{
		"/api/v1/order/123": "order",
		"/api/v1/order":     "order",
		"/api/v1/user":      "v1",
		"/api/v2/user":      "root",
		"/healthz":          "root",
		"/":                 "root",
	}
	for path, want := range cases {
		got := c.Match(http.MethodGet, path, http.Header{}, url.Values{})
		if got == nil || got.ID != want {
			t.Fatalf("Match(%s) 期望 %s，实际 %+v", path, want, got)
		}
	}
	// 没有根规则时未知路径必须返回 nil——网关据此回 404 而不是瞎转发。
	c2 := Compile([]Route{{ID: "v1", Prefix: "/api/v1", Service: "v1"}})
	if r := c2.Match(http.MethodGet, "/unknown", http.Header{}, url.Values{}); r != nil {
		t.Fatalf("未命中应返回 nil，实际 %+v", r)
	}
}

// TestMatchDefaultWeight 权重缺省为 100，负数也归一到 100。
func TestMatchDefaultWeight(t *testing.T) {
	c := Compile([]Route{
		{ID: "zero", Prefix: "/a", Service: "s"},
		{ID: "neg", Prefix: "/b", Service: "s", Weight: -5},
		{ID: "ok", Prefix: "/c", Service: "s", Weight: 30},
	})
	for path, want := range map[string]int{"/a/x": 100, "/b/x": 100, "/c/x": 30} {
		if got := c.Match(http.MethodGet, path, http.Header{}, url.Values{}); got == nil || got.Weight != want {
			t.Fatalf("%s 权重应为 %d，实际 %+v", path, want, got)
		}
	}
}

// TestMatchNilSafe 零值/ nil 编译结果不应 panic。
func TestMatchNilSafe(t *testing.T) {
	var c *CompiledRoutes
	if r := c.Match(http.MethodGet, "/x", http.Header{}, url.Values{}); r != nil {
		t.Fatal("nil CompiledRoutes 应返回 nil")
	}
	if c.Len() != 0 {
		t.Fatal("nil CompiledRoutes 长度应为 0")
	}
	if r := Compile(nil).Match(http.MethodGet, "/x", nil, nil); r != nil {
		t.Fatal("空规则集应返回 nil")
	}
}

// TestCandidatePrefixes 前缀候选序列从具体到泛化，且覆盖根路径。
func TestCandidatePrefixes(t *testing.T) {
	got := candidatePrefixes("/api/v1/user/1")
	want := []string{"/api/v1/user/1", "/api/v1/user", "/api/v1", "/api", "/"}
	if len(got) != len(want) {
		t.Fatalf("前缀序列长度应为 %d，实际 %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("前缀序列应为 %v，实际 %v", want, got)
		}
	}
	// 不以 / 开头时补齐；空路径回根。
	if got := candidatePrefixes("api"); got[0] != "/api" {
		t.Fatalf("应补齐前导斜杠: %v", got)
	}
	if got := candidatePrefixes(""); len(got) != 1 || got[0] != "/" {
		t.Fatalf("空路径应回根: %v", got)
	}
}
