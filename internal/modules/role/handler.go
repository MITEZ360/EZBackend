package role

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 角色接口。
type Handler struct{ svc *Service }

// NewHandler 创建角色处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 角色列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
// @Tags 用户与权限
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
// @Router /api/v1/roles [get]
// KE:SWAGGER-END
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
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary All
// @Tags 用户与权限
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
// @Router /api/v1/options/roles [get]
// KE:SWAGGER-END
func (h *Handler) All(c *gin.Context) {
	list, err := h.svc.All(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Get 角色详情。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Get
// @Tags 用户与权限
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
// @Router /api/v1/roles/{id} [get]
// KE:SWAGGER-END
func (h *Handler) Get(c *gin.Context) {
	r, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, r)
}

// Create 创建角色。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Create
// @Tags 用户与权限
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
// @Router /api/v1/roles [post]
// KE:SWAGGER-END
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
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Update
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param id path string true "id"
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
// @Router /api/v1/roles/{id} [put]
// KE:SWAGGER-END
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
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Delete
// @Tags 用户与权限
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
// @Router /api/v1/roles/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "角色管理", "删除角色")
	response.OK(c, gin.H{"message": "已删除"})
}

// Menus 查询角色菜单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Menus
// @Tags 用户与权限
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
// @Router /api/v1/roles/{id}/menus [get]
// KE:SWAGGER-END
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
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary SetMenus
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param id path string true "id"
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
// @Router /api/v1/roles/{id}/menus [put]
// KE:SWAGGER-END
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
