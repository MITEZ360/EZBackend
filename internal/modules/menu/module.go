package menu

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/keel-framework/keel/internal/modules/menu",
		Name:  "menu",
		Label: "菜单管理",
		Desc:  "菜单树与按钮权限点；关闭后侧边栏与鉴权都无从渲染",
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_menu", Parent: "m_system", Name: "菜单管理", Type: model.MenuPage, Path: "/system/menu",
				Icon: "menu", Perm: "system:menu:list", Sort: 3},
			{ID: "m_menu_add", Parent: "m_menu", Name: "新增菜单", Type: model.MenuButton, Perm: "system:menu:create"},
			{ID: "m_menu_edit", Parent: "m_menu", Name: "编辑菜单", Type: model.MenuButton, Perm: "system:menu:update"},
			{ID: "m_menu_del", Parent: "m_menu", Name: "删除菜单", Type: model.MenuButton, Perm: "system:menu:delete"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Authz)
	// 认证模块需要菜单服务来渲染身份对应的侧边栏与权限点。
	c.Provide("menu", svc)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/menus", middleware.RequirePerm("system:menu:list"), h.List)
		g.GET("/menus/tree", middleware.RequirePerm("system:menu:list"), h.Tree)
		g.POST("/menus", middleware.RequirePerm("system:menu:create"), h.Create)
		g.PUT("/menus/:id", middleware.RequirePerm("system:menu:update"), h.Update)
		g.DELETE("/menus/:id", middleware.RequirePerm("system:menu:delete"), h.Delete)
	})
	return nil
}
