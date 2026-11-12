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
	g.POST("/auth/totp/recovery", d.Auth.RegenerateRecoveryCodes)
	g.GET("/auth/sessions", d.Auth.Sessions)
	g.DELETE("/auth/sessions/:id", d.Auth.RevokeSession)

	// 第三方账号绑定（需登录）。静态段与 :name 分在不同层级，避免 Gin 通配冲突
	g.GET("/oauth/bindings", d.Auth.OAuthBindings)
	g.POST("/oauth/bind/:name", d.Auth.OAuthBind)
	g.DELETE("/oauth/bindings/:name", d.Auth.OAuthUnbind)
}
