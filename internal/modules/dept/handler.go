package dept

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 部门接口。
type Handler struct{ svc *Service }

// NewHandler 创建部门处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Tree 部门树。
func (h *Handler) Tree(c *gin.Context) {
	list, err := h.svc.Tree(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Create 新增部门。
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	d, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "部门管理", "新增部门")
	response.OK(c, d)
}

// Update 更新部门。
func (h *Handler) Update(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "部门管理", "编辑部门")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除部门。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "部门管理", "删除部门")
	response.OK(c, gin.H{"message": "已删除"})
}
