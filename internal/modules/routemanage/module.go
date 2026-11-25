package routemanage

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/routemanage",
		Name:  "routemanage",
		Label: "路由管理",
		Desc:  "已注册路由清单，用于核对权限点是否漏配",
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_route", Parent: "m_ops", Name: "路由管理", Type: model.MenuPage, Path: "/ops/routes",
				Icon: "route", Perm: "ops:route:list", Sort: 8},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	routes := func() []Route {
		if c.Routes == nil {
			return nil
		}
		src := c.Routes()
		out := make([]Route, 0, len(src))
		for _, r := range src {
			// gin 的 Handler 形如 pkg.(*Handler).List-fm，去掉包路径与编译后缀。
			name := r.Name
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i+1:]
			}
			if i := strings.Index(name, "-"); i >= 0 {
				name = name[:i]
			}
			out = append(out, Route{Method: r.Method, Path: r.Path, Name: name})
		}
		return out
	}
	h := NewHandler(NewService(routes))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/routes", middleware.RequirePerm("ops:route:list"), h.List)
	})
	return nil
}
