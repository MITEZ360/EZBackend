package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/pkg/response"
)

// SessionValidator 校验会话是否仍然有效（支持登出、异地登录踢下线）。
type SessionValidator interface {
	ValidateSession(ctx context.Context, sessionID, userID string) (bool, error)
}

// AuthDeps 认证中间件依赖。
type AuthDeps struct {
	JWT      *authn.Manager
	Authz    *authz.Loader
	Sessions SessionValidator
}

// Auth 解析访问令牌并加载身份与权限。令牌可来自 Authorization 头、token 查询参数或 Cookie。
func Auth(d AuthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			response.FailMsg(c, http.StatusUnauthorized, 40100, "请先登录")
			return
		}
		claims, err := d.JWT.Parse(token)
		if err != nil {
			if err == authn.ErrTokenExpired {
				response.FailMsg(c, http.StatusUnauthorized, 40101, "登录已过期，请重新登录")
				return
			}
			response.FailMsg(c, http.StatusUnauthorized, 40102, "登录凭证无效")
			return
		}
		if d.Sessions != nil && claims.SessionID != "" {
			ok, err := d.Sessions.ValidateSession(c.Request.Context(), claims.SessionID, claims.UserID)
			if err != nil {
				response.FailMsg(c, http.StatusInternalServerError, 50000, "会话校验失败")
				return
			}
			if !ok {
				response.FailMsg(c, http.StatusUnauthorized, 40102, "登录状态已失效，请重新登录")
				return
			}
		}
		identity, err := d.Authz.Load(c.Request.Context(), claims.UserID)
		if err != nil {
			response.FailMsg(c, http.StatusUnauthorized, 40102, err.Error())
			return
		}
		identity.SessionID = claims.SessionID
		c.Set("identity", identity)
		c.Set("user_id", identity.UserID)
		c.Set("username", identity.Username)
		c.Next()
	}
}

// OptionalAuth 解析令牌但不强制登录（公开接口可获取登录态）。
func OptionalAuth(d AuthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			c.Next()
			return
		}
		claims, err := d.JWT.Parse(token)
		if err != nil {
			c.Next()
			return
		}
		identity, err := d.Authz.Load(c.Request.Context(), claims.UserID)
		if err == nil {
			identity.SessionID = claims.SessionID
			c.Set("identity", identity)
			c.Set("user_id", identity.UserID)
		}
		c.Next()
	}
}

func extractToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return strings.TrimSpace(h)
	}
	if t := c.Query("token"); t != "" {
		return t
	}
	if ck, err := c.Cookie("keel_token"); err == nil && ck != "" {
		return ck
	}
	return ""
}

// Identity 返回当前登录主体，未登录返回 nil。
func Identity(c *gin.Context) *authn.Identity {
	if v, ok := c.Get("identity"); ok {
		if id, ok := v.(*authn.Identity); ok {
			return id
		}
	}
	return nil
}

// UserID 返回当前用户 ID。
func UserID(c *gin.Context) string {
	if id := Identity(c); id != nil {
		return id.UserID
	}
	return ""
}

// Username 返回当前用户名。
func Username(c *gin.Context) string {
	if id := Identity(c); id != nil {
		return id.Username
	}
	return ""
}

// RequirePerm 要求具备指定权限点之一（超级管理员直接放行）。
func RequirePerm(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := Identity(c)
		if id == nil {
			response.FailMsg(c, http.StatusUnauthorized, 40100, "请先登录")
			return
		}
		if id.Super {
			c.Next()
			return
		}
		for _, p := range perms {
			if id.Has(p) {
				c.Next()
				return
			}
		}
		response.FailMsg(c, http.StatusForbidden, 40300, "无权访问该资源")
	}
}

// RequireSuper 要求超级管理员。
func RequireSuper() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := Identity(c)
		if id == nil || !id.Super {
			response.FailMsg(c, http.StatusForbidden, 40300, "仅超级管理员可操作")
			return
		}
		c.Next()
	}
}
