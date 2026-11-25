package configcenter

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 配置中心接口。
type Handler struct{ svc *Service }

// NewHandler 创建配置中心处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 配置项清单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
// @Tags 配置
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
// @Router /api/v1/ops/config/items [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	groups, deploy, err := h.svc.List(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, configErr(err, "读取配置失败"))
		return
	}
	response.OK(c, gin.H{"groups": groups, "deploy": deploy})
}

// Save 保存配置。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Save
// @Tags 配置
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
// @Router /api/v1/ops/config/items [put]
// KE:SWAGGER-END
func (h *Handler) Save(c *gin.Context) {
	var body struct {
		Items []Update `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.Validation("请求参数格式错误"))
		return
	}
	operator := c.GetString("username")
	if operator == "" {
		operator = c.GetString("user_id")
	}
	res, err := h.svc.Save(c.Request.Context(), body.Items, operator)
	if err != nil {
		c.Error(err)
		response.Fail(c, configErr(err, "保存配置失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "更新运行时配置")
	response.OK(c, res)
}

// Reload 重新加载配置。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Reload
// @Tags 配置
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
// @Router /api/v1/ops/config/reload [post]
// KE:SWAGGER-END
func (h *Handler) Reload(c *gin.Context) {
	res, err := h.svc.Reload(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, configErr(err, "重新加载配置失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "重新加载配置")
	response.OK(c, res)
}

func configErr(err error, msg string) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	return errs.Internal(msg).WithCause(err)
}
