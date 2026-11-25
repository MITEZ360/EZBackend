package bootstrap

import (
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
)

// 本文件把「模块开关」的影响范围从路由扩展到菜单、数据表、定时任务。
//
// 设计取舍：归属关系集中声明在这里，而不是散落到每个模块包里让模块自报。
// 原因是模块之间的装配顺序、菜单父子关系、表依赖都只在这张全局视图里看得清；
// 集中一张表也便于审计「关掉某模块到底会少什么」。
//
// 未在表中出现的菜单一律视为常驻（登录、RBAC、字典、公告等底座能力）。

// menuModules 声明页面级菜单归属的模块。
// 只标页面（MenuPage）即可：按钮节点随父页面一起被裁掉，目录节点在
// 子项全部被裁后自动消失，无需逐条标注。
var menuModules = map[string]string{
	"m_dashboard":     "dashboard",
	"m_news":          "news",
	"m_news_source":   "news",
	"m_news_category": "news",

	"m_user":       "user",
	"m_role":       "role",
	"m_menu":       "menu",
	"m_dept":       "dept",
	"m_dict":       "dict",
	"m_config":     "config",
	"m_notice":     "notice",
	"m_message":    "message",
	"m_membership": "membership",
	"m_apikey":     "apikey",

	"m_job":         "job",
	"m_file":        "file",
	"m_loginlog":    "audit",
	"m_oplog":       "audit",
	"m_online":      "auth",
	"m_monitor":     "monitor",
	"m_maintenance": "maintenance",
	"m_route":       "routemanage",
	"m_cache":       "cacheadmin",
	"m_ops_config":  "configcenter",
	"m_cluster":     "cluster",
	// 危险操作依赖集群指令下发，节点模块关闭后单独留着没有意义。
	"m_danger": "cluster",
}

// moduleModels 声明可选模块独占的数据表。
//
// 这里只列「关掉模块就该不建」的表；RBAC、会话、字典、公告等底座表
// 无论开关如何都必须存在，统一放在 baseModels 里。
var moduleModels = map[string][]any{
	"message":    {&model.Message{}, &model.MessageRecipient{}},
	"membership": {&model.MembershipLevel{}},
	"apikey":     {&model.APIKey{}},
	"job":        {&model.Job{}, &model.JobLog{}},
	"file":       {&model.File{}},
	"audit":      {&model.OperationLog{}},
	"news":       {&model.NewsSource{}, &model.NewsCategory{}, &model.NewsArticle{}},
	"cluster": {
		&model.ClusterNode{}, &model.ClusterCommand{}, &model.ClusterCommandTarget{},
	},
}

// baseModels 是任何发行版都必须存在的表：登录会话、RBAC、字典、公告、登录日志。
// LoginLog 归在底座而不是 audit：安全审计日志是登录链路的产物，不能因为
// 关掉操作日志模块就失去登录记录。
//
// RuntimeConfig（管理端配置覆盖表）也必须常驻：modules.enabled 本身就存在这张表里，
// 如果它跟着 configcenter 模块一起被裁掉，关掉配置中心的发行版重启后会读不到
// 白名单，模块静默恢复成全开——这是最难排查的一类回退。
var baseModels = []any{
	&model.User{}, &model.UserRole{}, &model.UserRecoveryCode{},
	&model.Role{}, &model.RoleMenu{}, &model.Menu{},
	&model.Dept{}, &model.Session{},
	&model.DictType{}, &model.DictItem{}, &model.SysConfig{},
	&model.Notice{}, &model.LoginLog{},
	&model.RuntimeConfig{},
}

// scopeModels 按启用模块汇总需要建的表，顺序稳定（先底座，再按声明顺序）。
func (r *ModuleRegistry) scopeModels() []any {
	out := append([]any(nil), baseModels...)
	for _, name := range r.Ordered {
		if extra, ok := moduleModels[name]; ok {
			out = append(out, extra...)
		}
	}
	return out
}

// filterMenus 递归裁剪菜单树：模块被关闭的页面整棵移除，
// 目录节点在子项清空后一并移除，避免侧边栏出现空分组。
func filterMenus(items []menuSeed, enabled map[string]bool) []menuSeed {
	if len(items) == 0 {
		return nil
	}
	out := make([]menuSeed, 0, len(items))
	for _, it := range items {
		if mod, ok := menuModules[it.ID]; ok && mod != "" && !enabled[mod] {
			continue
		}
		cp := it
		cp.Children = filterMenus(it.Children, enabled)
		// 目录本身没有页面与权限点，子项被裁光后留着只是空壳。
		if it.Type == model.MenuDir && len(it.Children) > 0 && len(cp.Children) == 0 {
			continue
		}
		out = append(out, cp)
	}
	return out
}

// On 判断模块是否启用，供 seed 与定时任务使用。
func (r *ModuleRegistry) On(name string) bool {
	if r == nil || len(r.Enabled) == 0 {
		return true
	}
	return r.Enabled[name]
}

// missingModuleModels 列出「模块已启用、但表尚未建立」的模型。
// 只覆盖可选模块；底座表由迁移保证，不需要在这里兜底。
func missingModuleModels(db *gorm.DB, reg *ModuleRegistry) []any {
	var out []any
	for _, name := range reg.Ordered {
		extra, ok := moduleModels[name]
		if !ok {
			continue
		}
		for _, m := range extra {
			if !db.Migrator().HasTable(m) {
				out = append(out, m)
			}
		}
	}
	return out
}
