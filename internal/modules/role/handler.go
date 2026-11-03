package role

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 角色接口。
type Handler struct{ svc *Service }

// NewHandler 创建角色处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 角色列表。
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// All 全量启用角色（下拉用）。
func (h *Handler) All(c *gin.Context) {
	list, err := h.svc.All(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get 角色详情。
func (h *Handler) Get(c *gin.Context) {
	r, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, r)
}

// Create 创建角色。
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	r, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "角色管理", "新增角色")
	response.OK(c, r)
}

// Update 更新角色。
func (h *Handler) Update(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "角色管理", "编辑角色")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除角色。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "角色管理", "删除角色")
	response.OK(c, gin.H{"message": "已删除"})
}

// Menus 查询角色菜单。
func (h *Handler) Menus(c *gin.Context) {
	ids, err := h.svc.Menus(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, ids)
}

// MenuInput 菜单分配入参。
type MenuInput struct {
	MenuIDs []string `json:"menuIds"`
}

// SetMenus 分配菜单权限。
func (h *Handler) SetMenus(c *gin.Context) {
	var in MenuInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SetMenus(c.Request.Context(), c.Param("id"), in.MenuIDs); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "角色管理", "分配权限")
	response.OK(c, gin.H{"message": "权限已更新"})
}
