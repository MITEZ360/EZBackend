package routemanage

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 路由管理接口。
type Handler struct{ svc *Service }

// NewHandler 创建路由管理处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 路由目录分页。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
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
// @Security BearerAuth
// @Router /api/v1/routes [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	page := h.svc.List(Query{
		Page:     p.Page,
		PageSize: p.PageSize,
		Keyword:  p.Keyword,
		Method:   c.Query("method"),
		Group:    c.Query("group"),
	}, baseURL(c))
	response.OK(c, page)
}

// baseURL 用于拼接示例里的完整地址。优先采用反向代理传入的协议，
// 否则按是否 TLS 推断；路径不带前缀，示例直接拼 /api/v1/...
func baseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = strings.Split(proto, ",")[0]
	}
	host := c.Request.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}
