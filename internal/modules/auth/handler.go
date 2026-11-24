package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
	"github.com/keel-framework/keel/pkg/utils"
)

// Handler 认证接口。
type Handler struct {
	svc            *Service
	captchaEnabled bool
	trustProxy     bool
	refreshCookie  string
}

// NewHandler 创建认证处理器。
func NewHandler(svc *Service, captchaEnabled, trustProxy bool) *Handler {
	return &Handler{svc: svc, captchaEnabled: captchaEnabled, trustProxy: trustProxy, refreshCookie: "keel_refresh"}
}

func metaFrom(c *gin.Context, trustProxy bool) ClientMeta {
	ua := c.Request.UserAgent()
	browser, os := utils.ParseUA(ua)
	return ClientMeta{IP: utils.ClientIP(c, trustProxy), UA: ua, Browser: browser, OS: os}
}

// Captcha 获取图形验证码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Captcha
// @Tags 认证
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
// @Router /api/v1/auth/captcha [get]
// KE:SWAGGER-END
func (h *Handler) Captcha(c *gin.Context) {
	id, img, err := h.svc.captcha.Generate()
	if err != nil {
		response.Fail(c, errs.Internal("验证码生成失败"))
		return
	}
	response.OK(c, gin.H{"captchaId": id, "image": img, "enabled": h.captchaEnabled})
}

// Login 账号密码登录。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Login
// @Tags 认证
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
// @Router /api/v1/auth/login [post]
// KE:SWAGGER-END
func (h *Handler) Login(c *gin.Context) {
	var in LoginInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	pair, user, err := h.svc.Login(c.Request.Context(), &in, metaFrom(c, h.trustProxy))
	if err != nil {
		response.Fail(c, err)
		return
	}
	// 刷新令牌同时写入 HttpOnly Cookie，降低 XSS 窃取风险；前端仍可使用返回值。
	c.SetCookie(h.refreshCookie, pair.RefreshToken, int(h.svc.jwt.RefreshTTL().Seconds()), "/", "", c.Request.TLS != nil, true)
	middleware.AuditInfo(c, "auth", "login")
	response.OK(c, gin.H{"token": pair, "user": user})
}

// RefreshInput 刷新令牌入参。
type RefreshInput struct {
	RefreshToken string `json:"refreshToken"`
}

// Refresh 刷新访问令牌。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Refresh
// @Tags 认证
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
// @Router /api/v1/auth/refresh [post]
// KE:SWAGGER-END
func (h *Handler) Refresh(c *gin.Context) {
	var in RefreshInput
	_ = c.ShouldBindJSON(&in)
	if in.RefreshToken == "" {
		if ck, err := c.Cookie(h.refreshCookie); err == nil {
			in.RefreshToken = ck
		}
	}
	if in.RefreshToken == "" {
		response.Fail(c, errs.Unauthorized("缺少刷新令牌"))
		return
	}
	pair, err := h.svc.Refresh(c.Request.Context(), in.RefreshToken)
	if err != nil {
		c.SetCookie(h.refreshCookie, "", -1, "/", "", c.Request.TLS != nil, true)
		response.Fail(c, err)
		return
	}
	c.SetCookie(h.refreshCookie, pair.RefreshToken, int(h.svc.jwt.RefreshTTL().Seconds()), "/", "", c.Request.TLS != nil, true)
	response.OK(c, pair)
}

// Logout 登出。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Logout
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/logout [post]
// KE:SWAGGER-END
func (h *Handler) Logout(c *gin.Context) {
	id := middleware.Identity(c)
	if id != nil {
		_ = h.svc.Logout(c.Request.Context(), id.SessionID)
	}
	c.SetCookie(h.refreshCookie, "", -1, "/", "", c.Request.TLS != nil, true)
	response.OK(c, gin.H{"message": "已退出登录"})
}

// Profile 获取当前用户资料、菜单与权限。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Profile
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/profile [get]
// KE:SWAGGER-END
func (h *Handler) Profile(c *gin.Context) {
	id := middleware.Identity(c)
	if id == nil {
		response.Fail(c, errs.Unauthorized("请先登录"))
		return
	}
	p, err := h.svc.Profile(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

// UpdateProfile 更新个人资料。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary UpdateProfile
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/profile [put]
// KE:SWAGGER-END
func (h *Handler) UpdateProfile(c *gin.Context) {
	var in UpdateProfileInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.UpdateProfile(c.Request.Context(), middleware.UserID(c), &in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "资料已更新"})
}

// UpdatePreferences 保存语言与时区偏好。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary UpdatePreferences
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/preferences [put]
// KE:SWAGGER-END
func (h *Handler) UpdatePreferences(c *gin.Context) {
	var in UpdatePreferencesInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.UpdatePreferences(c.Request.Context(), middleware.UserID(c), &in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "偏好已更新"})
}

// ChangePassword 修改密码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ChangePassword
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/password [put]
// KE:SWAGGER-END
func (h *Handler) ChangePassword(c *gin.Context) {
	var in ChangePasswordInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	id := middleware.Identity(c)
	if id == nil {
		response.Fail(c, errs.Unauthorized("请先登录"))
		return
	}
	if err := h.svc.ChangePassword(c.Request.Context(), id.UserID, id.SessionID, &in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "密码已修改，其它设备已退出登录"})
}

// SetupTOTP 生成动态口令密钥与二维码（尚未启用）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary SetupTOTP
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/totp/setup [post]
// KE:SWAGGER-END
func (h *Handler) SetupTOTP(c *gin.Context) {
	out, err := h.svc.SetupTOTP(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "账号安全", "生成两步验证密钥")
	response.OK(c, out)
}

// TOTPCodeInput 动态口令验证码入参。
// code 既接受 6 位动态码，也接受恢复码（关闭两步验证时可用）。
type TOTPCodeInput struct {
	Code string `json:"code" binding:"required,max=32" label:"动态验证码或恢复码"`
}

// EnableTOTP 启用动态口令，返回一次性恢复码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary EnableTOTP
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/totp/enable [post]
// KE:SWAGGER-END
func (h *Handler) EnableTOTP(c *gin.Context) {
	var in TOTPCodeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.EnableTOTP(c.Request.Context(), middleware.UserID(c), in.Code)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "账号安全", "开启两步验证")
	response.OK(c, gin.H{"message": "两步验证已开启", "recoveryCodes": out.RecoveryCodes})
}

// DisableTOTP 关闭动态口令。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DisableTOTP
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/totp/disable [post]
// KE:SWAGGER-END
func (h *Handler) DisableTOTP(c *gin.Context) {
	var in TOTPCodeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.DisableTOTP(c.Request.Context(), middleware.UserID(c), in.Code); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "账号安全", "关闭两步验证")
	response.OK(c, gin.H{"message": "两步验证已关闭"})
}

// RegenerateRecoveryCodes 重新生成恢复码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary RegenerateRecoveryCodes
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/totp/recovery [post]
// KE:SWAGGER-END
func (h *Handler) RegenerateRecoveryCodes(c *gin.Context) {
	var in TOTPCodeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.RegenerateRecoveryCodes(c.Request.Context(), middleware.UserID(c), in.Code)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "账号安全", "重新生成恢复码")
	response.OK(c, gin.H{"recoveryCodes": out.RecoveryCodes})
}

// Sessions 查看我的登录设备。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Sessions
// @Tags 认证
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
// @Security BearerAuth
// @Router /api/v1/auth/sessions [get]
// KE:SWAGGER-END
func (h *Handler) Sessions(c *gin.Context) {
	id := middleware.Identity(c)
	list, err := h.svc.Sessions(c.Request.Context(), id.UserID, id.SessionID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// RevokeSession 注销指定设备。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary RevokeSession
// @Tags 认证
// @Accept json
// @Produce json
// @Param id path string true "id"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/auth/sessions/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) RevokeSession(c *gin.Context) {
	if err := h.svc.RevokeSession(c.Request.Context(), middleware.UserID(c), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已注销该设备"})
}

// PublicConfig 返回前端可公开使用的配置（站点名、验证码开关等）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary PublicConfig
// @Tags 认证
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
// @Router /api/v1/public/config [get]
// KE:SWAGGER-END
func (h *Handler) PublicConfig(c *gin.Context) {
	response.OK(c, gin.H{
		"siteName":        h.svc.cfg.App.Name,
		"version":         h.svc.cfg.App.Version,
		"env":             h.svc.cfg.App.Env,
		"captchaEnabled":  h.svc.cfg.Security.CaptchaEnabled,
		"registerEnabled": false,
	})
}

var _ = http.StatusOK
var _ = strings.TrimSpace

// ValidateSession 供认证中间件校验会话有效性。
func (h *Handler) ValidateSession(ctx context.Context, sessionID, userID string) (bool, error) {
	return h.svc.ValidateSession(ctx, sessionID, userID)
}
