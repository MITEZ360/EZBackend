package routemanage

import (
	"strings"
	"testing"
)

// 目录是手工维护的，重复键会让某条路由的说明被静默覆盖，必须挡住。
func TestCatalogNoDuplicate(t *testing.T) {
	list, err := parseCatalog(catalogRaw)
	if err != nil {
		t.Fatalf("解析目录失败: %v", err)
	}
	if len(list) < 100 {
		t.Fatalf("目录条目过少（%d），疑似文件损坏", len(list))
	}
	seen := map[string]bool{}
	for _, m := range list {
		k := key(m.Method, m.Path)
		if seen[k] {
			t.Errorf("重复条目: %s", k)
		}
		seen[k] = true
		if m.Title == "" {
			t.Errorf("%s 缺少作用说明", k)
		}
		if m.Group == "" {
			t.Errorf("%s 缺少分组", k)
		}
		if m.Public && m.Perm != "" {
			t.Errorf("%s 同时标记 public 与权限点", k)
		}
	}
}

func TestBuildExample(t *testing.T) {
	base := "http://localhost:9000"

	// 需要登录：带令牌头，路径参数替换为样例值
	got := buildExample(Meta{
		Method: "PUT",
		Path:   "/api/v1/users/:id",
		Body:   `{"nickname":"张三"}`,
	}, base)
	for _, want := range []string{
		"curl -X PUT",
		"http://localhost:9000/api/v1/users/9f1c2c4e-8f3a-4b6d-9c2e-6d5a1b7c8e90",
		"Authorization: Bearer <access_token>",
		"Content-Type: application/json",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("示例缺少 %q:\n%s", want, got)
		}
	}

	// 公开接口：不带任何认证头
	open := buildExample(Meta{Method: "GET", Path: "/healthz", Public: true}, base)
	if strings.Contains(open, "Authorization") {
		t.Errorf("公开接口不应带令牌头: %s", open)
	}

	// 开放接口：用 API 密钥而不是用户令牌
	apiKey := buildExample(Meta{Method: "GET", Path: "/api/open/v1/news", Auth: "api_key"}, base)
	if !strings.Contains(apiKey, "X-API-Key") || strings.Contains(apiKey, "Bearer") {
		t.Errorf("开放接口认证头不正确: %s", apiKey)
	}
}

func TestListPagingAndFilter(t *testing.T) {
	routes := []Route{
		{Method: "GET", Path: "/api/v1/users", Name: "List"},
		{Method: "POST", Path: "/api/v1/users", Name: "Create"},
		{Method: "GET", Path: "/api/v1/unknown"},
	}
	svc := NewService(func() []Route { return routes })

	all := svc.List(Query{}, "http://localhost:9000")
	if all.Total != 3 {
		t.Fatalf("total=%d, want 3", all.Total)
	}
	if all.Registered != 2 || all.Unregistered != 1 {
		t.Errorf("登记统计不符: registered=%d unregistered=%d", all.Registered, all.Unregistered)
	}
	if all.List[0].Group == "" || all.List[0].Title == "" {
		t.Error("未登记路由也应给出兜底分组与标题")
	}

	// 分页：第二页只应剩一条
	page2 := svc.List(Query{Page: 2, PageSize: 2}, "http://localhost:9000")
	if page2.Total != 3 || len(page2.List) != 1 {
		t.Fatalf("分页结果不符: total=%d len=%d", page2.Total, len(page2.List))
	}

	// 方法 + 关键字过滤同时生效
	filtered := svc.List(Query{Method: "post", Keyword: "users"}, "http://localhost:9000")
	if filtered.Total != 1 || filtered.List[0].Method != "POST" {
		t.Fatalf("过滤结果不符: %+v", filtered.List)
	}

	// 超范围页码返回空列表而不是 panic
	empty := svc.List(Query{Page: 99, PageSize: 20}, "http://localhost:9000")
	if len(empty.List) != 0 {
		t.Errorf("越界页码应返回空列表, got %d", len(empty.List))
	}
}
