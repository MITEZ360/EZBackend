package cnauth

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/modules/auth"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/response"
	"github.com/keel-framework/keel/pkg/utils"
)

// Handler CN 认证接口。
type Handler struct {
	svc        *Service
	trustProxy bool
}

// NewHandler 创建处理器。
func NewHandler(svc *Service, trustProxy bool) *Handler {
	return &Handler{svc: svc, trustProxy: trustProxy}
}

func (h *Handler) meta(c *gin.Context) auth.ClientMeta {
	ua := c.Request.UserAgent()
	browser, os := utils.ParseUA(ua)
	return auth.ClientMeta{IP: utils.ClientIP(c, h.trustProxy), UA: ua, Browser: browser, OS: os}
}

func (h *Handler) setRefreshCookie(c *gin.Context, token string) {
	c.SetCookie("keel_refresh", token, int(h.svc.authSvc.RefreshTTL().Seconds()), "/", "", c.Request.TLS != nil, true)
}

// PublicConfig 返回注册/短信登录开关，供前端登录页决定展示入口。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary PublicConfig
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/public/cn-config [get]
// KE:SWAGGER-END
func (h *Handler) PublicConfig(c *gin.Context) {
	response.OK(c, h.svc.PublicConfig())
}

// SendCode 发送短信验证码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary SendCode
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/auth/sms/code [post]
// KE:SWAGGER-END
func (h *Handler) SendCode(c *gin.Context) {
	var in SMSCodeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SendCode(c.Request.Context(), &in, utils.ClientIP(c, h.trustProxy)); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "auth", "sms.send")
	response.OK(c, gin.H{"sent": true})
}

// Register 用户自助注册，成功后直接签发登录态。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Register
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/auth/register [post]
// KE:SWAGGER-END
func (h *Handler) Register(c *gin.Context) {
	var in RegisterInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	pair, user, err := h.svc.Register(c.Request.Context(), &in, h.meta(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.setRefreshCookie(c, pair.RefreshToken)
	middleware.AuditInfo(c, "auth", "register")
	response.Created(c, gin.H{"token": pair, "user": user})
}

// LoginByPhone 短信验证码登录。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary LoginByPhone
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/auth/login/phone [post]
// KE:SWAGGER-END
func (h *Handler) LoginByPhone(c *gin.Context) {
	var in PhoneLoginInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	pair, user, err := h.svc.LoginByPhone(c.Request.Context(), &in, h.meta(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	h.setRefreshCookie(c, pair.RefreshToken)
	middleware.AuditInfo(c, "auth", "login.phone")
	response.OK(c, gin.H{"token": pair, "user": user})
}

// ResetPasswordInput 找回密码入参。
type ResetPasswordInput struct {
	Phone       string `json:"phone" binding:"required,mobile" label:"手机号"`
	Code        string `json:"code" binding:"required,len=6" label:"短信验证码"`
	NewPassword string `json:"newPassword" binding:"required,max=64" label:"新密码"`
}

// ResetPassword 短信验证码重置密码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ResetPassword
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/auth/password/reset [post]
// KE:SWAGGER-END
func (h *Handler) ResetPassword(c *gin.Context) {
	var in ResetPasswordInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), in.Phone, in.Code, in.NewPassword); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "auth", "password.reset")
	response.OK(c, gin.H{"message": "密码已重置"})
}
