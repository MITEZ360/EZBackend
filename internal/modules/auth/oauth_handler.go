package auth

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
)

// OAuthProviders 登录页拉取可用渠道（公开，不含凭据）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OAuthProviders
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
// @Router /api/v1/oauth/providers [get]
// KE:SWAGGER-END
func (h *Handler) OAuthProviders(c *gin.Context) {
	providers := h.svc.oauth.Providers()
	response.OK(c, gin.H{"enabled": len(providers) > 0, "providers": providers})
}

// oauthRedirectURI 计算回调地址：优先使用配置的 callback_base（部署在网关后必填），
// 否则按当前请求推导，本地开发开箱可用。
func (h *Handler) oauthRedirectURI(c *gin.Context, name string) string {
	base := strings.TrimRight(strings.TrimSpace(h.svc.cfg.OAuth.CallbackBase), "/")
	if base == "" {
		scheme := "http"
		if c.Request.TLS != nil ||
			(h.trustProxy && strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")) {
			scheme = "https"
		}
		base = scheme + "://" + c.Request.Host
	}
	return base + "/api/v1/oauth/callback/" + name
}

// OAuthAuthorize 发起登录授权。返回 JSON 供 SPA 跳转；?redirect=1 时直接 302。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OAuthAuthorize
// @Tags 认证
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/oauth/authorize/{name} [get]
// KE:SWAGGER-END
func (h *Handler) OAuthAuthorize(c *gin.Context) {
	name := c.Param("name")
	addr, state, err := h.svc.oauth.Authorize(c.Request.Context(), name, "login", "",
		h.oauthRedirectURI(c, name))
	if err != nil {
		response.Fail(c, err)
		return
	}
	if c.Query("redirect") == "1" {
		c.Redirect(http.StatusFound, addr)
		return
	}
	response.OK(c, gin.H{"url": addr, "state": state})
}

// OAuthCallback 第三方回调：换取令牌并签发给前端。
// 令牌放在 URL fragment，不会出现在服务端访问日志与 Referer 里。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OAuthCallback
// @Tags 认证
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/oauth/callback/{name} [get]
// KE:SWAGGER-END
func (h *Handler) OAuthCallback(c *gin.Context) {
	name := c.Param("name")
	frontend := h.svc.cfg.OAuth.FrontendPath
	if frontend == "" {
		frontend = "/oauth/callback"
	}
	fail := func(err error) {
		msg := errs.From(err).Message
		if e := c.Query("error"); e != "" {
			msg = "第三方授权被拒绝：" + e
		}
		c.Redirect(http.StatusFound, "/login?oauth_error="+url.QueryEscape(msg))
	}
	if e := c.Query("error"); e != "" {
		fail(errs.BadRequest(e))
		return
	}
	code, state := c.Query("code"), c.Query("state")
	if code == "" || state == "" {
		fail(errs.BadRequest("缺少授权码或 state"))
		return
	}
	res, err := h.svc.oauth.Callback(c.Request.Context(), name, code, state,
		h.oauthRedirectURI(c, name), metaFrom(c, h.trustProxy))
	if err != nil {
		fail(err)
		return
	}
	if res.Mode == "bind" {
		c.Redirect(http.StatusFound, frontend+"?oauth_bound="+url.QueryEscape(res.Provider))
		return
	}
	q := url.Values{
		"access_token":  {res.Token.AccessToken},
		"refresh_token": {res.Token.RefreshToken},
		"token_type":    {res.Token.TokenType},
		"expires_in":    {strconv.FormatInt(res.Token.ExpiresIn, 10)},
		"provider":      {res.Provider},
	}
	c.SetCookie(h.refreshCookie, res.Token.RefreshToken,
		int(h.svc.jwt.RefreshTTL().Seconds()), "/", "", c.Request.TLS != nil, true)
	c.Redirect(http.StatusFound, frontend+"#"+q.Encode())
}

// OAuthBind 已登录用户发起账号绑定。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OAuthBind
// @Tags 认证
// @Accept json
// @Produce json
// @Param name path string true "name"
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
// @Router /api/v1/oauth/bind/{name} [post]
// KE:SWAGGER-END
func (h *Handler) OAuthBind(c *gin.Context) {
	id := middleware.Identity(c)
	if id == nil {
		response.Fail(c, errs.Unauthorized("请先登录"))
		return
	}
	name := c.Param("name")
	addr, state, err := h.svc.oauth.Authorize(c.Request.Context(), name, "bind", id.UserID,
		h.oauthRedirectURI(c, name))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"url": addr, "state": state})
}

// OAuthBindings 我的第三方绑定。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OAuthBindings
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
// @Router /api/v1/oauth/bindings [get]
// KE:SWAGGER-END
func (h *Handler) OAuthBindings(c *gin.Context) {
	id := middleware.Identity(c)
	if id == nil {
		response.Fail(c, errs.Unauthorized("请先登录"))
		return
	}
	list, err := h.svc.oauth.Bindings(c.Request.Context(), id.UserID)
	if err != nil {
		response.Fail(c, errs.Internal("读取绑定关系失败"))
		return
	}
	providers := map[string]OAuthProviderView{}
	for _, p := range h.svc.oauth.Providers() {
		providers[p.Name] = p
	}
	out := make([]gin.H, 0, len(list))
	for _, b := range list {
		label := b.Provider
		if p, ok := providers[b.Provider]; ok {
			label = p.Label
		}
		out = append(out, gin.H{
			"id": b.ID, "provider": b.Provider, "label": label,
			"username": b.Username, "email": b.Email, "avatar": b.Avatar,
			"createdAt": b.CreatedAt, "lastLoginAt": b.LastLoginAt,
		})
	}
	response.OK(c, out)
}

// OAuthUnbind 解绑第三方账号。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OAuthUnbind
// @Tags 认证
// @Accept json
// @Produce json
// @Param name path string true "name"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/oauth/bindings/{name} [delete]
// KE:SWAGGER-END
func (h *Handler) OAuthUnbind(c *gin.Context) {
	id := middleware.Identity(c)
	if id == nil {
		response.Fail(c, errs.Unauthorized("请先登录"))
		return
	}
	if err := h.svc.oauth.Unbind(c.Request.Context(), id.UserID, c.Param("name")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "账号安全", "解绑第三方账号")
	response.OK(c, gin.H{"message": "已解绑"})
}
