package system

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// DictHandler 字典接口。
type DictHandler struct{ svc *DictService }

// NewDictHandler 创建字典处理器。
func NewDictHandler(svc *DictService) *DictHandler { return &DictHandler{svc: svc} }

// ListTypes 字典类型列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary ListTypes
// @Tags 配置
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
// @Router /api/v1/dicts/types [get]
// KE:SWAGGER-END
func (h *DictHandler) ListTypes(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.ListTypes(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// CreateType 新增字典类型。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary CreateType
// @Tags 配置
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
// @Router /api/v1/dicts/types [post]
// KE:SWAGGER-END
func (h *DictHandler) CreateType(c *gin.Context) {
	var in TypeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	t, err := h.svc.CreateType(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "字典管理", "新增字典")
	response.OK(c, t)
}

// UpdateType 更新字典类型。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary UpdateType
// @Tags 配置
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
// @Router /api/v1/dicts/types/{id} [put]
// KE:SWAGGER-END
func (h *DictHandler) UpdateType(c *gin.Context) {
	var in TypeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.UpdateType(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "字典管理", "编辑字典")
	response.OK(c, gin.H{"message": "已保存"})
}

// DeleteType 删除字典类型。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DeleteType
// @Tags 配置
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
// @Router /api/v1/dicts/types/{id} [delete]
// KE:SWAGGER-END
func (h *DictHandler) DeleteType(c *gin.Context) {
	if err := h.svc.DeleteType(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "字典管理", "删除字典")
	response.OK(c, gin.H{"message": "已删除"})
}

// Items 字典项列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Items
// @Tags 配置
// @Accept json
// @Produce json
// @Param code path string true "code"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/dicts/types/{code}/items [get]
// KE:SWAGGER-END
func (h *DictHandler) Items(c *gin.Context) {
	list, err := h.svc.Items(c.Request.Context(), c.Param("code"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// CreateItem 新增字典项。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary CreateItem
// @Tags 配置
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
// @Router /api/v1/dicts/items [post]
// KE:SWAGGER-END
func (h *DictHandler) CreateItem(c *gin.Context) {
	var in ItemInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	it, err := h.svc.CreateItem(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "字典管理", "新增字典项")
	response.OK(c, it)
}

// UpdateItem 更新字典项。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary UpdateItem
// @Tags 配置
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
// @Router /api/v1/dicts/items/{id} [put]
// KE:SWAGGER-END
func (h *DictHandler) UpdateItem(c *gin.Context) {
	var in ItemInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.UpdateItem(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "字典管理", "编辑字典项")
	response.OK(c, gin.H{"message": "已保存"})
}

// DeleteItem 删除字典项。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DeleteItem
// @Tags 配置
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
// @Router /api/v1/dicts/items/{id} [delete]
// KE:SWAGGER-END
func (h *DictHandler) DeleteItem(c *gin.Context) {
	if err := h.svc.DeleteItem(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "字典管理", "删除字典项")
	response.OK(c, gin.H{"message": "已删除"})
}

// Data 字典数据（前端下拉）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Data
// @Tags 配置
// @Accept json
// @Produce json
// @Param code path string true "code"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/public/dicts/{code} [get]
// KE:SWAGGER-END
func (h *DictHandler) Data(c *gin.Context) {
	list, err := h.svc.Data(c.Request.Context(), c.Param("code"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// ConfigHandler 参数配置接口。
type ConfigHandler struct{ svc *ConfigService }

// NewConfigHandler 创建参数处理器。
func NewConfigHandler(svc *ConfigService) *ConfigHandler { return &ConfigHandler{svc: svc} }

// List 参数列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
// @Tags 配置
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
// @Router /api/v1/configs [get]
// KE:SWAGGER-END
func (h *ConfigHandler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Public 公开配置。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Public
// @Tags 配置
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
// @Router /api/v1/public/configs [get]
// KE:SWAGGER-END
func (h *ConfigHandler) Public(c *gin.Context) {
	m, err := h.svc.Public(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, m)
}

// Create 新增参数。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Create
// @Tags 配置
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
// @Router /api/v1/configs [post]
// KE:SWAGGER-END
func (h *ConfigHandler) Create(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	cfg, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "参数配置", "新增参数")
	response.OK(c, cfg)
}

// Update 更新参数。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Update
// @Tags 配置
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
// @Router /api/v1/configs/{id} [put]
// KE:SWAGGER-END
func (h *ConfigHandler) Update(c *gin.Context) {
	var in Input
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "参数配置", "修改参数")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除参数。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Delete
// @Tags 配置
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
// @Router /api/v1/configs/{id} [delete]
// KE:SWAGGER-END
func (h *ConfigHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "参数配置", "删除参数")
	response.OK(c, gin.H{"message": "已删除"})
}

// NoticeHandler 公告接口。
type NoticeHandler struct{ svc *NoticeService }

// NewNoticeHandler 创建公告处理器。
func NewNoticeHandler(svc *NoticeService) *NoticeHandler { return &NoticeHandler{svc: svc} }

// List 公告列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
// @Tags 配置
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
// @Router /api/v1/notices [get]
// KE:SWAGGER-END
func (h *NoticeHandler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Get 公告详情。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Get
// @Tags 配置
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
// @Router /api/v1/notices/{id} [get]
// KE:SWAGGER-END
func (h *NoticeHandler) Get(c *gin.Context) {
	n, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, n)
}

// Create 发布公告。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Create
// @Tags 配置
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
// @Router /api/v1/notices [post]
// KE:SWAGGER-END
func (h *NoticeHandler) Create(c *gin.Context) {
	var in NoticeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	in.Publisher = middleware.UserID(c)
	in.PublisherName = operatorName(c)
	n, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "通知公告", "发布公告")
	response.OK(c, n)
}

// Update 编辑公告。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Update
// @Tags 配置
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
// @Router /api/v1/notices/{id} [put]
// KE:SWAGGER-END
func (h *NoticeHandler) Update(c *gin.Context) {
	var in NoticeInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	in.Publisher = middleware.UserID(c)
	in.PublisherName = operatorName(c)
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "通知公告", "编辑公告")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除公告。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Delete
// @Tags 配置
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
// @Router /api/v1/notices/{id} [delete]
// KE:SWAGGER-END
func (h *NoticeHandler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "通知公告", "删除公告")
	response.OK(c, gin.H{"message": "已删除"})
}

// operatorName 取当前操作人展示名，用于站内信发送人。
func operatorName(c *gin.Context) string {
	if id := middleware.Identity(c); id != nil {
		if id.Nickname != "" {
			return id.Nickname
		}
		return id.Username
	}
	return ""
}

// Latest 最新公告。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Latest
// @Tags 配置
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
// @Router /api/v1/public/notices [get]
// KE:SWAGGER-END
func (h *NoticeHandler) Latest(c *gin.Context) {
	list, err := h.svc.Latest(c.Request.Context(), httpx.QueryInt(c, "limit", 5))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}
