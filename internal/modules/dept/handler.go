package dept

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 部门接口。
type Handler struct{ svc *Service }

// NewHandler 创建部门处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Tree 部门树。
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
// @Router /api/v1/depts/tree [get]
// KE:SWAGGER-END
func (h *Handler) Tree(c *gin.Context) {
	list, err := h.svc.Tree(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Create 新增部门。
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
// @Router /api/v1/depts [post]
// KE:SWAGGER-END
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
// @Router /api/v1/depts/{id} [put]
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
	middleware.AuditInfo(c, "部门管理", "编辑部门")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除部门。
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
// @Router /api/v1/depts/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "部门管理", "删除部门")
	response.OK(c, gin.H{"message": "已删除"})
}
