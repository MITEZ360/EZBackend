package menu

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 菜单接口。
type Handler struct{ svc *Service }

// NewHandler 创建菜单处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 扁平列表。
func (h *Handler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Tree 菜单树。
func (h *Handler) Tree(c *gin.Context) {
	list, err := h.svc.Tree(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Create 新增菜单。
func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	m, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "菜单管理", "新增菜单")
	response.OK(c, m)
}

// Update 更新菜单。
func (h *Handler) Update(c *gin.Context) {
	var in CreateInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "菜单管理", "编辑菜单")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除菜单。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "菜单管理", "删除菜单")
	response.OK(c, gin.H{"message": "已删除"})
}
