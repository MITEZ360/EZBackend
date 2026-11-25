// Package modulekit 声明全部业务模块、依赖关系与管理端展示文案。
//
// 放在 platform 层而不是 bootstrap，是因为这张清单有两个消费者：
// bootstrap 用它解析启停、决定路由与建表；配置中心用它渲染管理端的模块开关。
// 各写一份必然漂移——关掉一个模块后管理端还列着它，或者列出一个并不存在的名字
// 导致下次启动 fail-fast 起不来。单一事实来源在这里。
package modulekit

import "sort"

// Spec 是一个业务模块的声明。
type Spec struct {
	// Name 模块标识，与 router 的 d.on(name) 一致，也是管理端保存的值。
	Name string
	// Label 管理端展示名。
	Label string
	// Desc 管理端说明：这个模块负责什么，关掉会少什么。
	Desc string
	// DependsOn 依赖的模块标识；启用本模块时会自动补齐依赖。
	DependsOn []string
	// AlwaysOn 基础模块：公开接口（登录、公开配置、字典、公告）所依赖，不能裁剪。
	AlwaysOn bool
}

// Specs 是全部模块，声明顺序即装配与日志输出顺序。
var Specs = []Spec{
	{Name: "auth", Label: "登录认证", AlwaysOn: true,
		Desc: "登录、令牌刷新与在线会话，公开接口依赖，不可关闭"},
	{Name: "config", Label: "公开配置", AlwaysOn: true,
		Desc: "前端启动时读取的站点配置与开关，不可关闭"},
	{Name: "dict", Label: "数据字典", AlwaysOn: true,
		Desc: "下拉选项等字典数据，多处表单依赖，不可关闭"},
	{Name: "notice", Label: "站内公告", AlwaysOn: true,
		Desc: "公告读取接口，消息模块依赖，不可关闭"},

	{Name: "dashboard", Label: "工作台",
		Desc: "首页看板：统计卡片与趋势图"},
	{Name: "user", Label: "用户管理", DependsOn: []string{"role", "dept"},
		Desc: "用户增删改查、角色分配、会话与重置密码"},
	{Name: "membership", Label: "会员等级", DependsOn: []string{"user"},
		Desc: "演示业务：会员等级与用户等级关联"},
	{Name: "apikey", Label: "开放密钥",
		Desc: "第三方调用所需的 API Key 管理（资讯模块依赖它）"},
	{Name: "news", Label: "资讯聚合", DependsOn: []string{"apikey"},
		Desc: "演示业务：资讯源、分类、文章抓取与清理"},
	{Name: "role", Label: "角色权限", DependsOn: []string{"menu"},
		Desc: "角色与菜单授权关系"},
	{Name: "menu", Label: "菜单管理",
		Desc: "菜单树与按钮权限点；关闭后侧边栏与鉴权都无从渲染"},
	{Name: "dept", Label: "组织架构",
		Desc: "部门树与用户归属"},
	{Name: "message", Label: "站内消息", DependsOn: []string{"notice"},
		Desc: "演示业务：消息发送与收件箱"},
	{Name: "audit", Label: "审计日志",
		Desc: "操作日志与登录日志查询"},
	{Name: "job", Label: "定时任务",
		Desc: "任务配置、手动触发与执行记录；关闭后不再启动调度器"},
	{Name: "file", Label: "文件管理",
		Desc: "上传记录浏览与清理"},
	{Name: "monitor", Label: "系统监控",
		Desc: "运行时指标与健康检查面板"},
	{Name: "routemanage", Label: "路由管理",
		Desc: "已注册路由清单，用于核对权限点是否漏配"},
	{Name: "cacheadmin", Label: "缓存管理",
		Desc: "缓存键浏览与按前缀清理"},
	{Name: "maintenance", Label: "系统维护",
		Desc: "服务信息、自检与基础运维动作"},
	{Name: "configcenter", Label: "配置中心",
		Desc: "运行时配置的管理端入口。关闭后本页消失，只能改配置文件或直接改数据库里的覆盖表"},
	// 集群的「配置下发」指令直接调用配置中心的热加载，缺了它指令无法执行。
	{Name: "cluster", Label: "集群协同", DependsOn: []string{"configcenter"},
		Desc: "节点登记、心跳、配置下发与重启指令；关闭后退化为单机"},
}

var index = func() map[string]Spec {
	m := make(map[string]Spec, len(Specs))
	for _, s := range Specs {
		m[s.Name] = s
	}
	return m
}()

// Find 按标识查找模块。
func Find(name string) (Spec, bool) {
	s, ok := index[name]
	return s, ok
}

// Names 返回全部模块标识（排序后），用于错误提示。
func Names() []string {
	out := make([]string, 0, len(Specs))
	for _, s := range Specs {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// AlwaysOnNames 返回不可裁剪的基础模块标识。
func AlwaysOnNames() []string {
	var out []string
	for _, s := range Specs {
		if s.AlwaysOn {
			out = append(out, s.Name)
		}
	}
	return out
}
