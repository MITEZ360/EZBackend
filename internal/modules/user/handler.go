package user

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 用户接口。
type Handler struct {
	svc *Service
}

// NewHandler 创建用户处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 用户列表。
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
// @Router /api/v1/users [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	sc := h.svc.ResolveScope(c.Request.Context(), middleware.Identity(c))
	list, total, err := h.svc.List(c.Request.Context(), p, sc, h.deptFilter(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// deptFilter 解析部门筛选参数，返回该部门及全部下级。
func (h *Handler) deptFilter(c *gin.Context) []string {
	id := c.Query("deptId")
	if id == "" {
		id = c.Query("dept_id")
	}
	if id == "" {
		return nil
	}
	return h.svc.deptSubtree(c.Request.Context(), id)
}

// Get 用户详情。
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
// @Router /api/v1/users/{id} [get]
// KE:SWAGGER-END
func (h *Handler) Get(c *gin.Context) {
	u, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, u)
}

// Create 创建用户。
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
// @Router /api/v1/users [post]
// KE:SWAGGER-END
func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	u, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "新增用户")
	response.OK(c, u)
}

// Update 更新用户。
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
// @Router /api/v1/users/{id} [put]
// KE:SWAGGER-END
func (h *Handler) Update(c *gin.Context) {
	var in UpdateInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "编辑用户")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除用户。
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
// @Router /api/v1/users/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id"), middleware.UserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "删除用户")
	response.OK(c, gin.H{"message": "已删除"})
}

// StatusInput 状态入参。
type StatusInput struct {
	Status int8 `json:"status" binding:"oneof=0 1" label:"状态"`
}

// SetStatus 启用/禁用。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary SetStatus
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
// @Router /api/v1/users/{id}/status [put]
// KE:SWAGGER-END
func (h *Handler) SetStatus(c *gin.Context) {
	var in StatusInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SetStatus(c.Request.Context(), c.Param("id"), in.Status, middleware.UserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "修改用户状态")
	response.OK(c, gin.H{"message": "状态已更新"})
}

// ResetPasswordInput 重置密码入参。
type ResetPasswordInput struct {
	Password string `json:"password" binding:"required,min=8,max=64" label:"新密码"`
}

// ResetPassword 重置密码。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ResetPassword
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
// @Router /api/v1/users/{id}/password [put]
// KE:SWAGGER-END
func (h *Handler) ResetPassword(c *gin.Context) {
	var in ResetPasswordInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), c.Param("id"), in.Password); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "重置密码")
	response.OK(c, gin.H{"message": "密码已重置"})
}

// ResetTOTP 重置指定用户的两步验证。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ResetTOTP
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
// @Router /api/v1/users/{id}/totp [delete]
// KE:SWAGGER-END
func (h *Handler) ResetTOTP(c *gin.Context) {
	if err := h.svc.ResetTOTP(c.Request.Context(), c.Param("id"), middleware.UserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "重置两步验证")
	response.OK(c, gin.H{"message": "两步验证已重置"})
}

// RolesInput 角色分配入参。
type RolesInput struct {
	RoleIDs []string `json:"roleIds"`
}

// SetRoles 分配角色。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary SetRoles
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
// @Router /api/v1/users/{id}/roles [put]
// KE:SWAGGER-END
func (h *Handler) SetRoles(c *gin.Context) {
	var in RolesInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SetRoles(c.Request.Context(), c.Param("id"), in.RoleIDs); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "分配角色")
	response.OK(c, gin.H{"message": "角色已更新"})
}

// Options 用户下拉选项。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Options
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
// @Router /api/v1/options/users [get]
// KE:SWAGGER-END
func (h *Handler) Options(c *gin.Context) {
	list, err := h.svc.Options(c.Request.Context(), c.Query("keyword"), httpx.QueryInt(c, "limit", 50))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Export 导出 Excel。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Export
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
// @Router /api/v1/exports/users [get]
// KE:SWAGGER-END
func (h *Handler) Export(c *gin.Context) {
	sc := h.svc.ResolveScope(c.Request.Context(), middleware.Identity(c))
	f, err := h.svc.Export(c.Request.Context(), query.Parse(c), sc, h.deptFilter(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer f.Close()
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="users.xlsx"`)
	c.Status(http.StatusOK)
	if err := f.Write(c.Writer); err != nil {
		_ = c.Error(errs.Internal("导出失败"))
	}
	middleware.AuditInfo(c, "用户管理", "导出用户")
}

// ImportTemplate 下载导入模板。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ImportTemplate
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
// @Router /api/v1/imports/users/template [get]
// KE:SWAGGER-END
func (h *Handler) ImportTemplate(c *gin.Context) {
	f, err := h.svc.ImportTemplate()
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer f.Close()
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", `attachment; filename="user-import-template.xlsx"`)
	c.Status(http.StatusOK)
	if err := f.Write(c.Writer); err != nil {
		_ = c.Error(errs.Internal("生成模板失败"))
	}
}

// Import 从 Excel 批量导入用户。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Import
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
// @Router /api/v1/imports/users [post]
// KE:SWAGGER-END
func (h *Handler) Import(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, errs.BadRequest("请选择要导入的 Excel 文件"))
		return
	}
	src, err := fh.Open()
	if err != nil {
		response.Fail(c, errs.BadRequest("文件读取失败"))
		return
	}
	defer src.Close()

	res, err := h.svc.Import(c.Request.Context(), src, fh.Size)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "用户管理", "导入用户")
	response.OK(c, res)
}

// BatchDelete 批量删除用户。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary BatchDelete
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
// @Router /api/v1/users/batch-delete [post]
// KE:SWAGGER-END
func (h *Handler) BatchDelete(c *gin.Context) {
	var in struct {
		IDs []string `json:"ids" binding:"required,min=1,max=200"`
	}
	if !httpx.BindJSON(c, &in) {
		return
	}
	n, skipped := h.svc.BatchDelete(c.Request.Context(), in.IDs, middleware.UserID(c))
	middleware.AuditInfo(c, "用户管理", "批量删除用户")
	response.OK(c, gin.H{"deleted": n, "skipped": skipped})
}
