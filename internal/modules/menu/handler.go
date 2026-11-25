package menu

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 菜单接口。
type Handler struct{ svc *Service }

// NewHandler 创建菜单处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 扁平列表。
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
// @Router /api/v1/menus [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Tree 菜单树。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Tree
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
// @Router /api/v1/menus/tree [get]
// KE:SWAGGER-END
func (h *Handler) Tree(c *gin.Context) {
	list, err := h.svc.Tree(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Create 新增菜单。
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
// @Router /api/v1/menus [post]
// KE:SWAGGER-END
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
// @Router /api/v1/menus/{id} [put]
// KE:SWAGGER-END
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
// @Router /api/v1/menus/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "菜单管理", "删除菜单")
	response.OK(c, gin.H{"message": "已删除"})
}
