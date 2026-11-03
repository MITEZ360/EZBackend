package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// apiKeyRoutes 机器身份管理。
//
// 只有后台用户可以管理凭证；凭证本身反过来只能调用开放接口，
// 不能回到后台管理面，避免一条泄露的密钥接管整个系统。
func (d Deps) apiKeyRoutes(g *gin.RouterGroup) {
	g.GET("/api-keys", middleware.RequirePerm("system:apikey:list"), d.APIKey.List)
	g.GET("/api-keys/scopes", middleware.RequirePerm("system:apikey:list"), d.APIKey.Scopes)
	g.POST("/api-keys", middleware.RequirePerm("system:apikey:create"), d.APIKey.Create)
	g.PUT("/api-keys/:id", middleware.RequirePerm("system:apikey:update"), d.APIKey.Update)
	g.POST("/api-keys/:id/rotate", middleware.RequirePerm("system:apikey:rotate"), d.APIKey.Rotate)
	g.PATCH("/api-keys/:id/status", middleware.RequirePerm("system:apikey:update"), d.APIKey.SetStatus)
	g.DELETE("/api-keys/:id", middleware.RequirePerm("system:apikey:delete"), d.APIKey.Delete)
}
