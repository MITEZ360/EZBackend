package role

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/keel-framework/keel/internal/modules/role",
		Name:      "role",
		Label:     "角色权限",
		Desc:      "角色与菜单授权关系",
		DependsOn: []string{"menu"},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_role", Parent: "m_system", Name: "角色管理", Type: model.MenuPage, Path: "/system/role",
				Icon: "shield", Perm: "system:role:list", Sort: 2},
			{ID: "m_role_add", Parent: "m_role", Name: "新增角色", Type: model.MenuButton, Perm: "system:role:create"},
			{ID: "m_role_edit", Parent: "m_role", Name: "编辑角色", Type: model.MenuButton, Perm: "system:role:update"},
			{ID: "m_role_del", Parent: "m_role", Name: "删除角色", Type: model.MenuButton, Perm: "system:role:delete"},
			{ID: "m_role_perm", Parent: "m_role", Name: "分配权限", Type: model.MenuButton, Perm: "system:role:assign"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	h := NewHandler(NewService(c.DB, c.Authz))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/roles", middleware.RequirePerm("system:role:list"), h.List)
		g.POST("/roles", middleware.RequirePerm("system:role:create"), h.Create)
		g.GET("/roles/:id", middleware.RequirePerm("system:role:list"), h.Get)
		g.PUT("/roles/:id", middleware.RequirePerm("system:role:update"), h.Update)
		g.DELETE("/roles/:id", middleware.RequirePerm("system:role:delete"), h.Delete)
		g.GET("/roles/:id/menus", middleware.RequirePerm("system:role:list"), h.Menus)
		g.PUT("/roles/:id/menus", middleware.RequirePerm("system:role:assign"), h.SetMenus)
		g.GET("/options/roles", h.All)
	})
	return nil
}
