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
func (h *Handler) Captcha(c *gin.Context) {
	id, img, err := h.svc.captcha.Generate()
	if err != nil {
		response.Fail(c, errs.Internal("验证码生成失败"))
		return
	}
	response.OK(c, gin.H{"captchaId": id, "image": img, "enabled": h.captchaEnabled})
}

// Login 账号密码登录。
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
func (h *Handler) Logout(c *gin.Context) {
	id := middleware.Identity(c)
	if id != nil {
		_ = h.svc.Logout(c.Request.Context(), id.SessionID)
	}
	c.SetCookie(h.refreshCookie, "", -1, "/", "", c.Request.TLS != nil, true)
	response.OK(c, gin.H{"message": "已退出登录"})
}

// Profile 获取当前用户资料、菜单与权限。
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

// ChangePassword 修改密码。
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

// SetupTOTP 生成动态口令密钥。
func (h *Handler) SetupTOTP(c *gin.Context) {
	out, err := h.svc.SetupTOTP(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// TOTPCodeInput 动态口令验证码入参。
type TOTPCodeInput struct {
	Code string `json:"code" binding:"required,len=6" label:"动态验证码"`
}

// EnableTOTP 启用动态口令。
func (h *Handler) EnableTOTP(c *gin.Context) {
	var in TOTPCodeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.EnableTOTP(c.Request.Context(), middleware.UserID(c), in.Code); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已启用动态口令"})
}

// DisableTOTP 关闭动态口令。
func (h *Handler) DisableTOTP(c *gin.Context) {
	var in TOTPCodeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.DisableTOTP(c.Request.Context(), middleware.UserID(c), in.Code); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已关闭动态口令"})
}

// Sessions 查看我的登录设备。
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
func (h *Handler) RevokeSession(c *gin.Context) {
	if err := h.svc.RevokeSession(c.Request.Context(), middleware.UserID(c), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已注销该设备"})
}

// PublicConfig 返回前端可公开使用的配置（站点名、验证码开关等）。
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
