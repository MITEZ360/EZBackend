package membership

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
)

// RequireBenefit 要求当前用户拥有指定权益。
//
// 用于衍生业务的接口保护，例如：
//
//	grp.POST("/reports/export", svc.RequireBenefit("export.excel"), handler.Export)
//
// 权益值为空、false、0、off、no 时视为未开通；非会员一律拒绝。
// 超管不豁免——会员权益是业务能力，不应因后台身份而获得。
func (s *Service) RequireBenefit(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := s.HasBenefit(c.Request.Context(), middleware.UserID(c), key)
		if err != nil {
			response.Fail(c, err)
			c.Abort()
			return
		}
		if !ok {
			response.Fail(c, errs.Forbidden("该操作需要会员权益："+key))
			c.Abort()
			return
		}
		c.Next()
	}
}
