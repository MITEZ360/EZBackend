package cacheadmin

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 缓存管理接口。
type Handler struct{ svc *Service }

// NewHandler 创建缓存管理处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Overview 缓存概况。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Overview
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
// @Router /api/v1/cache/overview [get]
// KE:SWAGGER-END
func (h *Handler) Overview(c *gin.Context) {
	out, err := h.svc.Overview(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, cacheErr(err, "读取缓存概况失败"))
		return
	}
	response.OK(c, out)
}

// Keys 缓存键列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Keys
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
// @Router /api/v1/cache/keys [get]
// KE:SWAGGER-END
func (h *Handler) Keys(c *gin.Context) {
	p := query.Parse(c)
	page, err := h.svc.Keys(c.Request.Context(), Query{
		Page:      p.Page,
		PageSize:  p.PageSize,
		Keyword:   p.Keyword,
		Namespace: c.Query("namespace"),
	})
	if err != nil {
		c.Error(err)
		response.Fail(c, cacheErr(err, "读取缓存键失败"))
		return
	}
	response.OK(c, page)
}

// Delete 删除缓存键。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Delete
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
// @Router /api/v1/cache/keys [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	var body struct {
		Keys []string `json:"keys"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	n, err := h.svc.Delete(c.Request.Context(), body.Keys)
	if err != nil {
		c.Error(err)
		response.Fail(c, cacheErr(err, "删除缓存键失败"))
		return
	}
	middleware.AuditInfo(c, "运维", "删除缓存键")
	response.OK(c, gin.H{"deleted": n, "message": "已删除缓存键"})
}

// Flush 清空缓存命名空间。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Flush
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
// @Security BearerAuth
// @Router /api/v1/cache/flush [post]
// KE:SWAGGER-END
func (h *Handler) Flush(c *gin.Context) {
	var body struct {
		Namespace string `json:"namespace"`
		Confirm   bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	if !body.Confirm {
		response.Fail(c, errs.BadRequest("清空缓存需要显式确认"))
		return
	}
	n, err := h.svc.Flush(c.Request.Context(), body.Namespace)
	if err != nil {
		c.Error(err)
		response.Fail(c, cacheErr(err, "清空缓存失败"))
		return
	}
	action := "清空全部缓存"
	if body.Namespace != "" {
		action = "清空缓存命名空间 " + body.Namespace
	}
	middleware.AuditInfo(c, "运维", action)
	response.OK(c, gin.H{"deleted": n, "message": "已清空缓存"})
}

// cacheErr 保留业务校验错误，其余统一包装为内部错误，避免把底层细节直接暴露给前端。
func cacheErr(err error, msg string) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	return errs.Internal(msg).WithCause(err)
}
