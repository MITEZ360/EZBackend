package audit

import (
	"github.com/gin-gonic/gin"
	"github.com/MITEZ360/tenon/internal/platform/httpx"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 审计接口。
type Handler struct{ svc *Service }

// NewHandler 创建审计处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// LoginLogs 登录日志列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary LoginLogs
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
// @Router /api/v1/logs/login [get]
// KE:SWAGGER-END
func (h *Handler) LoginLogs(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.LoginLogs(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// OperationLogs 操作日志列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OperationLogs
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
// @Router /api/v1/logs/operation [get]
// KE:SWAGGER-END
func (h *Handler) OperationLogs(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.OperationLogs(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// OperationLogDetail 操作日志详情。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary OperationLogDetail
// @Tags 运维
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
// @Router /api/v1/logs/operation/{id} [get]
// KE:SWAGGER-END
func (h *Handler) OperationLogDetail(c *gin.Context) {
	l, err := h.svc.GetOperationLog(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, l)
}

// ClearLoginLogs 清理登录日志。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ClearLoginLogs
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
// @Router /api/v1/logs/login [delete]
// KE:SWAGGER-END
func (h *Handler) ClearLoginLogs(c *gin.Context) {
	n, err := h.svc.ClearLoginLogs(c.Request.Context(), httpx.QueryInt(c, "days", 30))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "登录日志", "清理日志")
	response.OK(c, gin.H{"deleted": n})
}

// ClearOperationLogs 清理操作日志。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ClearOperationLogs
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
// @Router /api/v1/logs/operation [delete]
// KE:SWAGGER-END
func (h *Handler) ClearOperationLogs(c *gin.Context) {
	n, err := h.svc.ClearOperationLogs(c.Request.Context(), httpx.QueryInt(c, "days", 30))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "操作日志", "清理日志")
	response.OK(c, gin.H{"deleted": n})
}

// Online 在线用户。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Online
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
// @Router /api/v1/online/sessions [get]
// KE:SWAGGER-END
func (h *Handler) Online(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.Online(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Kick 强制下线。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Kick
// @Tags 运维
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
// @Router /api/v1/online/sessions/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Kick(c *gin.Context) {
	if err := h.svc.Kick(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "在线用户", "强制下线")
	response.OK(c, gin.H{"message": "已强制下线"})
}
