package route

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/MITEZ360/tenon/internal/modules/route",
		Name:      "route",
		Label:     "动态路由",
		Desc:      "转发规则、编译校验、灰度发布与回滚；改动不重启网关",
		DependsOn: []string{"registry"},
		Models:    []any{&model.GovRoute{}, &model.GovRelease{}},
		Menus: []modulekit.Menu{
			modulekit.DirGov,
			{ID: "m_gov_routes", Parent: "m_gov", Name: "动态路由", Type: model.MenuPage,
				Path: "/gov/routes", Icon: "route", Perm: "route:rule:list", Sort: 2},
			{ID: "m_gov_route_create", Parent: "m_gov_routes", Name: "新增规则", Type: model.MenuButton, Perm: "route:rule:create"},
			{ID: "m_gov_route_update", Parent: "m_gov_routes", Name: "编辑规则", Type: model.MenuButton, Perm: "route:rule:update"},
			{ID: "m_gov_route_delete", Parent: "m_gov_routes", Name: "删除规则", Type: model.MenuButton, Perm: "route:rule:delete"},
			{ID: "m_gov_route_publish", Parent: "m_gov_routes", Name: "发布规则", Type: model.MenuButton, Perm: "route:rule:publish"},
			{ID: "m_gov_releases", Parent: "m_gov", Name: "发布记录", Type: model.MenuPage,
				Path: "/gov/releases", Icon: "history", Perm: "route:release:list", Sort: 3},
			{ID: "m_gov_release_rollback", Parent: "m_gov_releases", Name: "回滚", Type: model.MenuButton, Perm: "route:release:rollback"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	govSrv, _ := modulekit.Need[*govserver.Server](c, "govserver")
	svc := NewService(c.DB, c.Log, govSrv)
	h := NewHandler(svc)
	c.Provide("route", svc)

	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/gov/routes", middleware.RequirePerm("route:rule:list"), h.List)
		g.POST("/gov/routes", middleware.RequirePerm("route:rule:create"), h.Create)
		g.PUT("/gov/routes/:id", middleware.RequirePerm("route:rule:update"), h.Update)
		g.DELETE("/gov/routes/:id", middleware.RequirePerm("route:rule:delete"), h.Delete)
		g.POST("/gov/routes/compile", middleware.RequirePerm("route:rule:list"), h.Compile)
		g.POST("/gov/routes/publish", middleware.RequirePerm("route:rule:publish"), h.Publish)
		g.GET("/gov/releases", middleware.RequirePerm("route:release:list"), h.Releases)
		g.POST("/gov/releases/:id/promote", middleware.RequirePerm("route:rule:publish"), h.Promote)
		g.POST("/gov/releases/:id/rollback", middleware.RequirePerm("route:release:rollback"), h.Rollback)
	})
	return nil
}
