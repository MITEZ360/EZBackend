package routemanage

import (
	"sort"
	"strings"
)

// Route 是运行时路由的最小快照，由调用方从 HTTP 引擎读取。
type Route struct {
	Method string
	Path   string
	Name   string
}

// Entry 是管理端列表的一行：运行时事实 + 目录元数据。
type Entry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Group  string `json:"group"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
	Perm   string `json:"perm"`
	Public bool   `json:"public"`
	Auth   string `json:"auth"`
	// Example 完整请求示例（curl，含认证头与请求体）。
	Example string `json:"example"`
	// Registered 为 false 表示目录里没有这条路由，说明需要补 catalog.yaml。
	Registered bool `json:"registered"`
}

// Query 列表筛选条件。
type Query struct {
	Page     int
	PageSize int
	Keyword  string
	Method   string
	Group    string
}

// GroupStat 分组数量统计，供前端筛选下拉展示。
type GroupStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Page 分页结果。
type Page struct {
	List     []Entry `json:"list"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
	// Registered/Unregistered 用于提示目录维护完整度。
	Registered   int         `json:"registered"`
	Unregistered int         `json:"unregistered"`
	Groups       []GroupStat `json:"groups"`
}

// Service 路由目录业务。
type Service struct {
	routes func() []Route
	metas  map[string]Meta
}

// NewService 创建服务。routes 延迟读取，保证 HTTP 引擎装配完成后才能拿到全量路由。
func NewService(routes func() []Route) *Service {
	if routes == nil {
		routes = func() []Route { return nil }
	}
	return &Service{routes: routes, metas: loadCatalog()}
}

// List 合并运行时路由与目录元数据后分页返回。
//
// 目录中存在但运行时未注册的条目不会出现：以进程实际挂载为准，
// 避免配置了模块裁剪却仍显示未生效的接口。
func (s *Service) List(q Query, baseURL string) Page {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}

	all := make([]Entry, 0, len(s.routes()))
	for _, r := range s.routes() {
		meta, ok := s.metas[key(r.Method, r.Path)]
		e := Entry{
			Method:     strings.ToUpper(r.Method),
			Path:       r.Path,
			Name:       r.Name,
			Registered: ok,
		}
		if ok {
			e.Group, e.Title, e.Desc = meta.Group, meta.Title, meta.Desc
			e.Perm, e.Public, e.Auth = meta.Perm, meta.Public, meta.Auth
			e.Example = buildExample(meta, baseURL)
		} else {
			e.Group = guessGroup(r.Path)
			e.Title = "未登记路由"
			e.Desc = "该路由没有在 catalog.yaml 中登记，请在 internal/modules/routemanage/catalog.yaml 补充作用、备注与示例"
			e.Example = buildExample(Meta{Method: r.Method, Path: r.Path}, baseURL)
		}
		all = append(all, e)
	}

	sortEntries(all)

	groupCount := map[string]int{}
	registered, unregistered := 0, 0
	for _, e := range all {
		groupCount[e.Group]++
		if e.Registered {
			registered++
		} else {
			unregistered++
		}
	}
	groups := make([]GroupStat, 0, len(groupCount))
	for name, n := range groupCount {
		groups = append(groups, GroupStat{Name: name, Count: n})
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })

	filtered := filter(all, q)

	total := len(filtered)
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	return Page{
		List:         filtered[start:end],
		Total:        total,
		Page:         q.Page,
		PageSize:     q.PageSize,
		Registered:   registered,
		Unregistered: unregistered,
		Groups:       groups,
	}
}

// Methods 返回实际存在的方法集合，前端筛选下拉使用。
func (s *Service) Methods() []string {
	seen := map[string]bool{}
	for _, r := range s.routes() {
		seen[strings.ToUpper(r.Method)] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func filter(all []Entry, q Query) []Entry {
	kw := strings.ToLower(strings.TrimSpace(q.Keyword))
	method := strings.ToUpper(strings.TrimSpace(q.Method))
	group := strings.TrimSpace(q.Group)

	out := make([]Entry, 0, len(all))
	for _, e := range all {
		if method != "" && e.Method != method {
			continue
		}
		if group != "" && e.Group != group {
			continue
		}
		if kw != "" && !matchKeyword(e, kw) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func matchKeyword(e Entry, kw string) bool {
	for _, s := range []string{e.Path, e.Method, e.Title, e.Desc, e.Perm, e.Group, e.Name} {
		if strings.Contains(strings.ToLower(s), kw) {
			return true
		}
	}
	return false
}

// guessGroup 给未登记路由一个可读的分组，至少不显示成空白。
func guessGroup(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/open/"):
		return "开放接口"
	case strings.HasPrefix(path, "/api/v1/"):
		return "未分类"
	case path == "/healthz" || path == "/readyz" || path == "/metrics":
		return "系统内置"
	case strings.HasPrefix(path, "/uploads") || strings.HasPrefix(path, "/assets") || strings.HasPrefix(path, "/docs"):
		return "系统内置"
	}
	return "其他"
}

func sortEntries(list []Entry) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return methodRank(a.Method) < methodRank(b.Method)
	})
}
