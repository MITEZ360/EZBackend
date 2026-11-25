package monitor

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/keel-framework/keel/internal/modules/monitor",
		Name:  "monitor",
		Label: "系统监控",
		Desc:  "运行时指标与健康检查面板",
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_monitor", Parent: "m_ops", Name: "服务监控", Type: model.MenuPage, Path: "/ops/monitor",
				Icon: "activity", Perm: "ops:monitor:view", Sort: 6},
			{ID: "m_monitor_restart", Parent: "m_monitor", Name: "重启服务", Type: model.MenuButton, Perm: "ops:monitor:restart"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	routes := func() []RouteInfo {
		if c.Routes == nil {
			return nil
		}
		src := c.Routes()
		out := make([]RouteInfo, 0, len(src))
		for _, r := range src {
			out = append(out, RouteInfo{Method: r.Method, Path: r.Path})
		}
		return out
	}
	h := NewHandler(NewService(c.DB, c.Cache, c.Config, routes, c.Restart))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/monitor/system", middleware.RequirePerm("ops:monitor:view"), h.System)
		g.GET("/monitor/routes", middleware.RequirePerm("ops:monitor:view"), h.Routes)
		g.POST("/monitor/restart", middleware.RequirePerm("ops:monitor:restart"), h.Restart)
	})
	return nil
}
