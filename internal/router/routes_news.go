package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// newsRoutes 资讯中心管理端接口。
//
// 数据源单独挂在 /news-sources，避免与 /news/:id 的路径参数混在一起。
func (d Deps) newsRoutes(g *gin.RouterGroup) {
	g.GET("/news/stats", middleware.RequirePerm("content:news:list"), d.News.Stats)
	g.GET("/news", middleware.RequirePerm("content:news:list"), d.News.List)
	g.GET("/news/:id", middleware.RequirePerm("content:news:view"), d.News.Detail)
	g.DELETE("/news/:id", middleware.RequirePerm("content:news:delete"), d.News.Delete)
	g.POST("/news/cleanup", middleware.RequirePerm("content:news:cleanup"), d.News.Cleanup)

	g.GET("/news-sources", middleware.RequirePerm("content:news:list"), d.News.Sources)
	g.POST("/news-sources", middleware.RequirePerm("content:news:source"), d.News.CreateSource)
	g.PUT("/news-sources/:id", middleware.RequirePerm("content:news:source"), d.News.UpdateSource)
	g.DELETE("/news-sources/:id", middleware.RequirePerm("content:news:delete"), d.News.DeleteSource)
}
