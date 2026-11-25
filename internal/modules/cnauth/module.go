package cnauth

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/auth"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
	"github.com/MITEZ360/tenon/internal/platform/sms"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/MITEZ360/tenon/internal/modules/cnauth",
		Name:      "cnauth",
		Label:     "自助注册与短信登录",
		Desc:      "手机号注册、短信验证码登录、找回密码；会话签发复用认证模块，不另立一套登录态",
		DependsOn: []string{"auth"},
		Models:    []any{&model.SMSCode{}},
		Setup:     setup,
	})
}

// setup 挂公开入口：注册、验证码、短信登录、重置密码。
// 都挂在 /api/v1 的公开分组下，走 IP 限流，不需要登录。
func setup(c *modulekit.Context) error {
	authSvc, err := modulekit.Need[*auth.Service](c, "auth")
	if err != nil {
		return err
	}
	// 未配置短信网关时 sender 为 nil，服务照常构造：注册与短信登录会在
	// 下发验证码这一步给出明确错误，而不是启动就失败。
	sender, err := sms.New(c.Config.SMS, c.Log)
	if err != nil {
		return fmt.Errorf("初始化短信网关失败: %w", err)
	}

	svc := NewService(c.DB, c.Config, authSvc, c.Authz, sender, c.Log)
	h := NewHandler(svc, c.Config.Server.TrustProxy)

	c.Public(func(g *gin.RouterGroup) {
		g.GET("/public/cn-config", h.PublicConfig)
		g.POST("/auth/register", h.Register)
		g.POST("/auth/sms/code", h.SendCode)
		g.POST("/auth/login/phone", h.LoginByPhone)
		g.POST("/auth/password/reset", h.ResetPassword)
	})
	return nil
}
