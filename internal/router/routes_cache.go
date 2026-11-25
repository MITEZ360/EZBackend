package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// cacheAdminRoutes 缓存管理：查看后端与键分布，删除单个键或按命名空间清空。
func (d Deps) cacheAdminRoutes(g *gin.RouterGroup) {
	g.GET("/cache/overview", middleware.RequirePerm("ops:cache:list"), d.CacheAdmin.Overview)
	g.GET("/cache/keys", middleware.RequirePerm("ops:cache:list"), d.CacheAdmin.Keys)
	g.DELETE("/cache/keys", middleware.RequirePerm("ops:cache:delete"), d.CacheAdmin.Delete)
	g.POST("/cache/flush", middleware.RequirePerm("ops:cache:flush"), d.CacheAdmin.Flush)
}
