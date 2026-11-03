package router

import (
	"github.com/gin-gonic/gin"
)

// authRoutes 注册认证与个人中心相关接口（始终启用）。
func (d Deps) authRoutes(g *gin.RouterGroup) {
	g.POST("/auth/logout", d.Auth.Logout)
	g.GET("/auth/profile", d.Auth.Profile)
	g.PUT("/auth/profile", d.Auth.UpdateProfile)
	g.PUT("/auth/password", d.Auth.ChangePassword)
	g.POST("/auth/totp/setup", d.Auth.SetupTOTP)
	g.POST("/auth/totp/enable", d.Auth.EnableTOTP)
	g.POST("/auth/totp/disable", d.Auth.DisableTOTP)
	g.GET("/auth/sessions", d.Auth.Sessions)
	g.DELETE("/auth/sessions/:id", d.Auth.RevokeSession)
}
