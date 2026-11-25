package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// routeManageRoutes 路由目录：运行时路由 + 作用/备注/示例元数据。
//
// 与 /monitor/routes 的区别：后者只给监控页做数量核对，这里面向排障与联调，
// 提供分页、筛选和可直接复制的请求示例。
func (d Deps) routeManageRoutes(g *gin.RouterGroup) {
	g.GET("/routes", middleware.RequirePerm("ops:route:list"), d.Route.List)
}
