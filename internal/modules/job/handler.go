package job

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 任务接口。
type Handler struct{ svc *Service }

// NewHandler 创建任务处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 任务列表。
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Handlers 可用执行器。
func (h *Handler) Handlers(c *gin.Context) {
	response.OK(c, h.svc.Handlers())
}

// Create 新增任务。
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	j, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "定时任务", "新增任务")
	response.OK(c, j)
}

// Update 更新任务。
func (h *Handler) Update(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "定时任务", "编辑任务")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除任务。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "定时任务", "删除任务")
	response.OK(c, gin.H{"message": "已删除"})
}

// StatusInput 状态入参。
type StatusInput struct {
	Status int8 `json:"status" binding:"oneof=0 1" label:"状态"`
}

// SetStatus 启停任务。
func (h *Handler) SetStatus(c *gin.Context) {
	var in StatusInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SetStatus(c.Request.Context(), c.Param("id"), in.Status); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "定时任务", "修改任务状态")
	response.OK(c, gin.H{"message": "状态已更新"})
}

// Run 立即执行。
func (h *Handler) Run(c *gin.Context) {
	entry, err := h.svc.RunNow(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "定时任务", "手动执行")
	response.OK(c, entry)
}

// Logs 执行日志。
func (h *Handler) Logs(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.Logs(c.Request.Context(), c.Query("jobId"), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}
