package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/pkg/response"
	"github.com/MITEZ360/tenon/pkg/utils"
)

// APIKeyAuthenticator 校验机器身份并判断配额，由 apikey.Service 实现。
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, plain, ip string) (*model.APIKey, error)
	Allow(ctx context.Context, k *model.APIKey) (bool, error)
}

// APIKeyAuth 校验 API 密钥并注入与登录用户同构的身份。
//
// scopes 非空时要求凭证至少具备其中之一；后续仍可用 RequirePerm 二次校验，
// 因此业务代码不需要关心调用方是人还是程序。
func APIKeyAuth(a APIKeyAuthenticator, trustProxy bool, scopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		plain := extractAPIKey(c)
		if plain == "" {
			response.FailMsg(c, http.StatusUnauthorized, 40107, "缺少 API 密钥")
			return
		}
		ip := utils.ClientIP(c, trustProxy)
		k, err := a.Authenticate(c.Request.Context(), plain, ip)
		if err != nil {
			response.Fail(c, err)
			return
		}
		ok, err := a.Allow(c.Request.Context(), k)
		if err != nil {
			// 配额统计故障不拦截业务，与限流中间件保持一致的降级策略
			ok = true
		}
		if !ok {
			c.Header("Retry-After", "1")
			response.FailMsg(c, http.StatusTooManyRequests, 42900, "调用配额已用完，请稍后再试")
			return
		}

		id := &authn.Identity{
			UserID:   "ak:" + k.ID,
			Username: k.Name,
			Nickname: k.Name,
			Kind:     authn.KindService,
			APIKeyID: k.ID,
			Perms:    make(map[string]struct{}, len(k.Scopes)),
		}
		for _, sc := range k.Scopes {
			id.Perms[sc] = struct{}{}
		}
		c.Set("identity", id)
		c.Set("user_id", id.UserID)
		c.Set("username", id.Username)
		c.Set("api_key_id", k.ID)

		for _, sc := range scopes {
			if !id.Has(sc) {
				response.FailMsg(c, http.StatusForbidden, 40300, "该密钥无权访问此接口")
				return
			}
		}
		c.Next()
	}
}

// extractAPIKey 从 X-API-Key 或 Authorization: ApiKey <key> 中取值。
// 不复用 JWT 的 extractToken：两者混用会让「拿用户令牌冒充机器身份」成为可能。
func extractAPIKey(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-API-Key")); v != "" {
		return v
	}
	h := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(h) > 6 && strings.EqualFold(h[:6], "apikey") {
		return strings.TrimSpace(h[6:])
	}
	if len(h) > 7 && strings.EqualFold(h[:7], "api-key") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
