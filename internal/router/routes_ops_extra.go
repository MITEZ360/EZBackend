package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// configCenterRoutes 运行时配置中心。
//
// 与 /configs（业务参数，如站点名称）刻意分开：这里的每一项都会直接
// 改写进程配置，权限也更严格。
func (d Deps) configCenterRoutes(g *gin.RouterGroup) {
	g.GET("/ops/config/items", middleware.RequirePerm("ops:config:view"), d.ConfigCenter.List)
	g.PUT("/ops/config/items", middleware.RequirePerm("ops:config:update"), d.ConfigCenter.Save)
	g.POST("/ops/config/reload", middleware.RequirePerm("ops:config:reload"), d.ConfigCenter.Reload)
}

// clusterRoutes 多节点协同。
func (d Deps) clusterRoutes(g *gin.RouterGroup) {
	g.GET("/ops/cluster/nodes", middleware.RequirePerm("ops:cluster:view"), d.Cluster.Nodes)
	g.POST("/ops/cluster/dispatch", middleware.RequirePerm("ops:cluster:operate"), d.Cluster.Dispatch)
	g.GET("/ops/cluster/commands", middleware.RequirePerm("ops:cluster:view"), d.Cluster.Commands)
}
