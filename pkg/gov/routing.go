package gov

import (
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// CompiledRoutes 是路由规则的执行视图。
//
// 控制面负责编译（规范化、排序、冲突校验），数据面只做执行。这样网关
// 不必在请求路径上解析 JSON、比较优先级，匹配成本只与路径段数有关。
type CompiledRoutes struct {
	byPrefix map[string][]*Route
}

// SortRoutes 规范化并排序规则，返回可直接下发的副本。
//
// 控制面用它给编译预览排序，数据面用 Compile 装配，两边共用同一套规则，
// 预览里看到的顺序就是请求真正命中的顺序。ID 为空的规则允许存在（预览阶段
// 还没有落库主键），由 Compile 在下发前过滤。
func SortRoutes(routes []Route) []Route {
	items := make([]Route, 0, len(routes))
	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		r.Prefix = normPrefix(r.Prefix)
		if r.Prefix == "" {
			continue
		}
		if r.ID != "" {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
		}
		if r.Weight <= 0 {
			r.Weight = 100
		}
		items = append(items, r)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := &items[i], &items[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if ca, cb := specificity(a), specificity(b); ca != cb {
			return ca > cb
		}
		if len(a.Prefix) != len(b.Prefix) {
			return len(a.Prefix) > len(b.Prefix)
		}
		return a.ID < b.ID
	})
	return items
}

// Compile 编译规则集合。
//
// 排序必须完全确定，否则同一请求可能因注册顺序不同命中不同规则：
// 优先级降序 → 条件具体度降序 → 前缀长度降序 → ID 字典序。
func Compile(routes []Route) *CompiledRoutes {
	sorted := SortRoutes(routes)
	byPrefix := make(map[string][]*Route, len(sorted))
	for i := range sorted {
		// 数据面必须能按 ID 做失败摘除与灰度定位，没有 ID 的规则不参与转发。
		if sorted[i].ID == "" {
			continue
		}
		byPrefix[sorted[i].Prefix] = append(byPrefix[sorted[i].Prefix], &sorted[i])
	}
	return &CompiledRoutes{byPrefix: byPrefix}
}

// Match 返回第一条命中的规则；没有命中返回 nil。
//
// 前缀从最具体到最泛逐级尝试，命中前缀后再按编译顺序检查方法/Header/Query。
// 这保证 /api/v1/order/detail 优先于 /api/v1/order，而不会因为条件多就乱序。
func (c *CompiledRoutes) Match(method, path string, header http.Header, query url.Values) *Route {
	if c == nil {
		return nil
	}
	for _, prefix := range candidatePrefixes(path) {
		list := c.byPrefix[prefix]
		for _, r := range list {
			if r.match(method, header, query) {
				return r
			}
		}
	}
	return nil
}

// Len 返回规则总数，用于启动日志与监控。
func (c *CompiledRoutes) Len() int {
	if c == nil {
		return 0
	}
	n := 0
	for _, list := range c.byPrefix {
		n += len(list)
	}
	return n
}

func (r *Route) match(method string, header http.Header, query url.Values) bool {
	if len(r.Methods) > 0 {
		ok := false
		for _, m := range r.Methods {
			if strings.EqualFold(m, method) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for k, want := range r.Headers {
		if header.Get(k) != want {
			return false
		}
	}
	for k, want := range r.Query {
		if query.Get(k) != want {
			return false
		}
	}
	return true
}

func specificity(r *Route) int {
	n := len(r.Methods)
	if len(r.Headers) > 0 {
		n += len(r.Headers) * 2
	}
	if len(r.Query) > 0 {
		n += len(r.Query)
	}
	return n
}

// candidatePrefixes 生成从具体到泛化的前缀序列。
// /api/v1/user/1 → /api/v1/user/1, /api/v1/user, /api/v1, /
func candidatePrefixes(path string) []string {
	if path == "" {
		return []string{"/"}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	out := make([]string, 0, 8)
	cur := path
	for cur != "" && cur != "/" {
		out = append(out, cur)
		i := strings.LastIndex(cur, "/")
		if i <= 0 {
			break
		}
		cur = cur[:i]
	}
	out = append(out, "/")
	return out
}

func normPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}
