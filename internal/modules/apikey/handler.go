package apikey

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 凭证接口。
type Handler struct{ svc *Service }

// NewHandler 创建凭证处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// KeyView 列表视图：附带当日用量，便于判断配额是否该调整。
type KeyView struct {
	*model.APIKey
	TodayUsage int64 `json:"todayUsage"`
}

// List 凭证分页。
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
// @Router /api/v1/api-keys [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	ids := make([]string, 0, len(list))
	for _, k := range list {
		ids = append(ids, k.ID)
	}
	usage := h.svc.Usage(c.Request.Context(), ids)
	views := make([]KeyView, 0, len(list))
	for _, k := range list {
		views = append(views, KeyView{APIKey: k, TodayUsage: usage[k.ID]})
	}
	response.PageOK(c, views, total, p.Page, p.PageSize)
}

// Create 签发凭证，明文只在此响应中出现一次。
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
// @Router /api/v1/api-keys [post]
// KE:SWAGGER-END
func (h *Handler) Create(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	out, err := h.svc.Create(c.Request.Context(), &in, middleware.Identity(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "API 密钥", "签发 "+out.Key.Name)
	response.OK(c, out)
}

// Update 编辑凭证。
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
// @Router /api/v1/api-keys/{id} [put]
// KE:SWAGGER-END
func (h *Handler) Update(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	k, err := h.svc.Update(c.Request.Context(), c.Param("id"), &in, middleware.Identity(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "API 密钥", "编辑 "+k.Name)
	response.OK(c, k)
}

// Rotate 轮换密钥，旧密钥立即失效。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Rotate
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
// @Router /api/v1/api-keys/{id}/rotate [post]
// KE:SWAGGER-END
func (h *Handler) Rotate(c *gin.Context) {
	out, err := h.svc.Rotate(c.Request.Context(), c.Param("id"), middleware.Identity(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "API 密钥", "轮换 "+out.Key.Name)
	response.OK(c, out)
}

// SetStatus 启用或停用。
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
// @Router /api/v1/api-keys/{id}/status [patch]
// KE:SWAGGER-END
func (h *Handler) SetStatus(c *gin.Context) {
	var in struct {
		Status int8 `json:"status" binding:"oneof=0 1" label:"状态"`
	}
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SetStatus(c.Request.Context(), c.Param("id"), in.Status); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "API 密钥", "变更状态")
	response.OK(c, gin.H{"message": "已更新"})
}

// Delete 删除凭证。
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
// @Router /api/v1/api-keys/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "API 密钥", "删除")
	response.OK(c, gin.H{"message": "已删除"})
}

// Scopes 可授予的权限点清单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Scopes
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
// @Router /api/v1/api-keys/scopes [get]
// KE:SWAGGER-END
func (h *Handler) Scopes(c *gin.Context) {
	list, err := h.svc.Scopes(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Whoami 开放接口的自检端点：调用方用它确认密钥有效、范围正确、配额还剩多少。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Whoami
// @Tags 开放接口
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
// @Security ApiKeyAuth
// @Router /api/open/v1/whoami [get]
// KE:SWAGGER-END
func (h *Handler) Whoami(c *gin.Context) {
	id := middleware.Identity(c)
	if id == nil || id.APIKeyID == "" {
		response.Fail(c, errs.Unauthorized("缺少机器身份"))
		return
	}
	k, err := h.svc.Get(c.Request.Context(), id.APIKeyID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	usage := h.svc.Usage(c.Request.Context(), []string{k.ID})
	response.OK(c, gin.H{
		"keyId":      k.ID,
		"name":       k.Name,
		"kind":       k.Kind,
		"scopes":     k.Scopes,
		"dailyQuota": k.DailyQuota,
		"todayUsage": usage[k.ID],
		"expireAt":   k.ExpireAt,
	})
}
