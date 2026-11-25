package bootstrap

import (
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

// 本文件只剩「底座表」：任何发行版都必须存在的表。
// 各业务模块的表跟着模块走，声明在各自 Spec.Models 里——加模块不用改这里，
// 裁模块也不会在这里留下无主的表。

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

// missingModuleModels 列出「模块已启用、但表尚未建立」的模型。
// 只覆盖可选模块；底座表由迁移保证，不需要在这里兜底。
func missingModuleModels(db *gorm.DB, reg *ModuleRegistry) []any {
	var out []any
	for _, spec := range reg.specs {
		for _, m := range spec.Models {
			if !db.Migrator().HasTable(m) {
				out = append(out, m)
			}
		}
	}
	return out
}

// allModels 返回底座表 + 全部已注册模块的表，供 CLI 迁移命令使用。
//
// CLI 是数据库管理工具，不按 modules.enabled 裁剪：迁移脚本引用的表必须
// 存在，否则关闭某模块时 `migrate up` 会因目标表缺失而失败。
func allModels() []any {
	return append(append([]any{}, baseModels...), modulekit.ModelsOf()...)
}
