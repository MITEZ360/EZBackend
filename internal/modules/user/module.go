package user

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/keel-framework/keel/internal/modules/user",
		Name:      "user",
		Label:     "用户管理",
		Desc:      "用户增删改查、角色分配、会话与重置密码",
		DependsOn: []string{"role", "dept"},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_user", Parent: "m_system", Name: "用户管理", Type: model.MenuPage, Path: "/system/user",
				Icon: "users", Perm: "system:user:list", Sort: 1},
			{ID: "m_user_add", Parent: "m_user", Name: "新增用户", Type: model.MenuButton, Perm: "system:user:create"},
			{ID: "m_user_edit", Parent: "m_user", Name: "编辑用户", Type: model.MenuButton, Perm: "system:user:update"},
			{ID: "m_user_del", Parent: "m_user", Name: "删除用户", Type: model.MenuButton, Perm: "system:user:delete"},
			{ID: "m_user_reset", Parent: "m_user", Name: "重置密码", Type: model.MenuButton, Perm: "system:user:reset"},
			{ID: "m_user_totp", Parent: "m_user", Name: "重置两步验证", Type: model.MenuButton, Perm: "system:user:reset-totp"},
			{ID: "m_user_member", Parent: "m_user", Name: "设置会员", Type: model.MenuButton, Perm: "system:user:grant-membership"},
			{ID: "m_user_export", Parent: "m_user", Name: "导出用户", Type: model.MenuButton, Perm: "system:user:export"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Config, c.Log, c.Authz, nil)
	// 会员模块的装配顺序不在本模块的控制范围内，等全部模块就绪后再接进来。
	// 未启用会员模块时保持 nil，用户列表不展示等级，其余功能不受影响。
	c.After(func(c *modulekit.Context) error {
		if mem, ok := c.Lookup("membership").(MembershipFiller); ok {
			svc.SetMembershipFiller(mem)
		}
		return nil
	})

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/users", middleware.RequirePerm("system:user:list"), h.List)
		g.POST("/users", middleware.RequirePerm("system:user:create"), h.Create)
		g.GET("/users/:id", middleware.RequirePerm("system:user:list"), h.Get)
		g.PUT("/users/:id", middleware.RequirePerm("system:user:update"), h.Update)
		g.DELETE("/users/:id", middleware.RequirePerm("system:user:delete"), h.Delete)
		g.PUT("/users/:id/status", middleware.RequirePerm("system:user:update"), h.SetStatus)
		g.PUT("/users/:id/password", middleware.RequirePerm("system:user:reset"), h.ResetPassword)
		g.DELETE("/users/:id/totp", middleware.RequirePerm("system:user:reset-totp"), h.ResetTOTP)
		g.PUT("/users/:id/roles", middleware.RequirePerm("system:user:update"), h.SetRoles)
		g.GET("/options/users", middleware.RequirePerm("system:user:list"), h.Options)
		g.GET("/exports/users", middleware.RequirePerm("system:user:export"), h.Export)
		g.POST("/users/batch-delete", middleware.RequirePerm("system:user:delete"), h.BatchDelete)
		g.GET("/imports/users/template", middleware.RequirePerm("system:user:create"), h.ImportTemplate)
		g.POST("/imports/users", middleware.RequirePerm("system:user:create"), h.Import)
	})
	return nil
}
