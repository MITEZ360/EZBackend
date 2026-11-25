package auth

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/menu"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/auth",
		Name:  "auth",
		Label: "登录认证",
		Desc:  "登录、令牌刷新与在线会话，公开接口依赖，不可关闭",
		// 登录成功后要渲染身份对应的菜单与权限点，因此依赖菜单模块。
		DependsOn: []string{"menu"},
		AlwaysOn:  true,
		// 第三方账号绑定关系跟着认证模块走。
		Models: []any{&model.UserOAuthBinding{}},
		Setup:  setup,
	})
}

func setup(c *modulekit.Context) error {
	menuSvc, err := modulekit.Need[*menu.Service](c, "menu")
	if err != nil {
		return err
	}
	svc := NewService(c.DB, c.Cache, c.JWT, c.Captcha, c.Config, c.Log, c.Authz, menuSvc)
	h := NewHandler(svc, c.Config.Security.CaptchaEnabled, c.Config.Server.TrustProxy)
	// 鉴权中间件用它校验会话是否已被踢下线。
	c.Sessions = h
	// 自助注册/短信登录（cnauth）要在凭据校验后签发同一套会话与登录日志，
	// 供给服务实例而不是复制一份令牌逻辑。
	c.Provide("auth", svc)

	c.Public(func(g *gin.RouterGroup) {
		g.GET("/auth/captcha", h.Captcha)
		g.POST("/auth/login", h.Login)
		g.POST("/auth/refresh", h.Refresh)
		g.GET("/public/config", h.PublicConfig)

		// 配置化第三方登录：渠道列表、发起授权与回调均无需登录。
		g.GET("/oauth/providers", h.OAuthProviders)
		g.GET("/oauth/authorize/:name", h.OAuthAuthorize)
		g.GET("/oauth/callback/:name", h.OAuthCallback)
	})

	c.Authed(func(g *gin.RouterGroup) {
		g.POST("/auth/logout", h.Logout)
		g.GET("/auth/profile", h.Profile)
		g.PUT("/auth/profile", h.UpdateProfile)
		g.PUT("/auth/preferences", h.UpdatePreferences)
		g.PUT("/auth/password", h.ChangePassword)
		g.POST("/auth/totp/setup", h.SetupTOTP)
		g.POST("/auth/totp/enable", h.EnableTOTP)
		g.POST("/auth/totp/disable", h.DisableTOTP)
		g.POST("/auth/totp/recovery", h.RegenerateRecoveryCodes)
		g.GET("/auth/sessions", h.Sessions)
		g.DELETE("/auth/sessions/:id", h.RevokeSession)

		// 第三方账号绑定（需登录）。静态段与 :name 分在不同层级，避免 Gin 通配冲突。
		g.GET("/oauth/bindings", h.OAuthBindings)
		g.POST("/oauth/bind/:name", h.OAuthBind)
		g.DELETE("/oauth/bindings/:name", h.OAuthUnbind)
	})
	return nil
}
