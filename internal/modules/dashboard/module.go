package dashboard

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/keel-framework/keel/internal/modules/dashboard",
		Name:  "dashboard",
		Label: "工作台",
		Desc:  "首页看板：统计卡片与趋势图",
		Menus: []modulekit.Menu{
			{ID: "m_dashboard", Name: "仪表盘", Type: model.MenuPage, Path: "/dashboard",
				Icon: "dashboard", Perm: "dashboard:view", Sort: 1,
				// 普通角色也有首页入口，否则登录后落在空白页。
				Roles: []string{"r_user"}},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	h := NewHandler(NewService(c.DB, c.Cache))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/dashboard/overview", middleware.RequirePerm("dashboard:view"), h.Overview)
	})
	return nil
}
