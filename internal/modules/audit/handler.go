package audit

import (
	"github.com/gin-gonic/gin"
	"github.com/keel-framework/keel/internal/platform/httpx"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 审计接口。
type Handler struct{ svc *Service }

// NewHandler 创建审计处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// LoginLogs 登录日志列表。
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
func (h *Handler) OperationLogDetail(c *gin.Context) {
	l, err := h.svc.GetOperationLog(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, l)
}

// ClearLoginLogs 清理登录日志。
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
func (h *Handler) Kick(c *gin.Context) {
	if err := h.svc.Kick(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "在线用户", "强制下线")
	response.OK(c, gin.H{"message": "已强制下线"})
}
