// Package routemanage 提供路由目录：把进程实际注册的路由与人工维护的
// 元数据（作用、备注、权限、请求示例）合并成分页可查的管理端列表。
//
// 元数据放在 catalog.yaml，按 method + path 与运行时路由对应；运行时存在
// 但目录未登记的路由仍会列出，只是标记为「未登记」，避免清单失真。
package routemanage

import (
	_ "embed"
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed catalog.yaml
var catalogRaw []byte

// Meta 单条路由的元数据。
type Meta struct {
	Method string `yaml:"method" json:"-"`
	Path   string `yaml:"path" json:"-"`
	Group  string `yaml:"group" json:"group"`
	Title  string `yaml:"title" json:"title"`
	Desc   string `yaml:"desc" json:"desc"`
	Perm   string `yaml:"perm" json:"perm"`
	Public bool   `yaml:"public" json:"public"`
	// Auth 认证方式：空表示用户令牌，api_key 表示开放接口。
	Auth  string `yaml:"auth" json:"auth"`
	Query string `yaml:"query" json:"-"`
	Body  string `yaml:"body" json:"-"`
}

// key 用于把运行时路由与元数据对齐。
func key(method, path string) string { return strings.ToUpper(method) + " " + path }

// parseCatalog 解析原始 YAML，供加载与单测共用。
func parseCatalog(raw []byte) ([]Meta, error) {
	var list []Meta
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// loadCatalog 解析内置目录。解析失败属于打包错误，启动期直接 panic，
// 避免管理端静默展示一份空目录。
func loadCatalog() map[string]Meta {
	list, err := parseCatalog(catalogRaw)
	if err != nil {
		panic(fmt.Sprintf("解析路由目录失败: %v", err))
	}
	out := make(map[string]Meta, len(list))
	for _, m := range list {
		if m.Method == "" || m.Path == "" {
			continue
		}
		out[key(m.Method, m.Path)] = m
	}
	return out
}

// buildExample 生成可直接复制执行的 curl 示例。
//
// 路径参数按语义替换为样例值，查询串与请求体取自目录；认证方式决定请求头，
// 用占位符而不是真实凭据，避免示例过期或泄露。
func buildExample(m Meta, baseURL string) string {
	path := fillPathParams(m.Path)
	url := strings.TrimRight(baseURL, "/") + path
	if m.Query != "" {
		url += "?" + m.Query
	}

	var b strings.Builder
	b.WriteString("curl -X ")
	b.WriteString(strings.ToUpper(m.Method))
	b.WriteString(" '")
	b.WriteString(url)
	b.WriteString("'")

	if m.Auth == "api_key" {
		b.WriteString(" \\\n  -H 'X-API-Key: <api_key>'")
	} else if !m.Public {
		b.WriteString(" \\\n  -H 'Authorization: Bearer <access_token>'")
	}
	if m.Body != "" {
		b.WriteString(" \\\n  -H 'Content-Type: application/json'")
		b.WriteString(" \\\n  -d '")
		b.WriteString(strings.TrimSpace(m.Body))
		b.WriteString("'")
	}
	return b.String()
}

// fillPathParams 把 Gin 的路径参数替换成有意义的样例值。
func fillPathParams(path string) string {
	repl := strings.NewReplacer(
		":id", "9f1c2c4e-8f3a-4b6d-9c2e-6d5a1b7c8e90",
		":name", "wechat",
		":code", "user_status",
		":filepath", "logo.png",
		":any", "index.html",
		"*filepath", "logo.png",
		"*any", "index.html",
		"*path", "demo",
	)
	return repl.Replace(path)
}

// methodRank 让同一路径下的方法按「读→写→删」排列，符合阅读习惯。
func methodRank(m string) int {
	switch strings.ToUpper(m) {
	case http.MethodGet:
		return 0
	case http.MethodHead:
		return 1
	case http.MethodPost:
		return 2
	case http.MethodPut:
		return 3
	case http.MethodPatch:
		return 4
	case http.MethodDelete:
		return 5
	}
	return 9
}
