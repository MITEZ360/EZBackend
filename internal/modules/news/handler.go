package news

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 管理端接口。
type Handler struct{ svc *Service }

// NewHandler 创建管理端处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 资讯分页。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
// @Tags 内容
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
// @Router /api/v1/news [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	opt := ListOptions{
		Category: c.Query("category"),
		SourceID: c.Query("sourceId"),
		From:     parseTime(c.Query("from")),
		To:       parseTime(c.Query("to")),
	}
	if v := c.Query("status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s := int8(n)
			opt.Status = &s
		}
	}
	list, total, err := h.svc.List(c.Request.Context(), p, opt)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Detail 资讯详情。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Detail
// @Tags 内容
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
// @Router /api/v1/news/{id} [get]
// KE:SWAGGER-END
func (h *Handler) Detail(c *gin.Context) {
	a, err := h.svc.Get(c.Request.Context(), c.Param("id"), false)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, a)
}

// Delete 删除资讯。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Delete
// @Tags 内容
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
// @Router /api/v1/news/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "删除资讯")
	response.OK(c, gin.H{"message": "已删除"})
}

// Stats 概览统计。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Stats
// @Tags 内容
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
// @Router /api/v1/news/stats [get]
// KE:SWAGGER-END
func (h *Handler) Stats(c *gin.Context) {
	out, err := h.svc.Stats(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"total": out.Total, "today": out.Today, "last24h": out.Last24h,
		"categories": out.Categories, "sources": out.Sources,
		"retentionDays": int(h.svc.Retention().Hours() / 24),
	})
}

// Categories 分类全量列表（含资讯条数）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Categories
// @Tags 内容
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
// @Router /api/v1/news-categories [get]
// KE:SWAGGER-END
func (h *Handler) Categories(c *gin.Context) {
	list, err := h.svc.ListCategories(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// CreateCategory 新增分类。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary CreateCategory
// @Tags 内容
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
// @Router /api/v1/news-categories [post]
// KE:SWAGGER-END
func (h *Handler) CreateCategory(c *gin.Context) {
	var in CategoryInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	cat, err := h.svc.CreateCategory(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "新增分类 "+cat.Code)
	response.OK(c, cat)
}

// UpdateCategory 编辑分类。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary UpdateCategory
// @Tags 内容
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
// @Router /api/v1/news-categories/{id} [put]
// KE:SWAGGER-END
func (h *Handler) UpdateCategory(c *gin.Context) {
	var in CategoryInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	cat, err := h.svc.UpdateCategory(c.Request.Context(), c.Param("id"), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "编辑分类 "+cat.Code)
	response.OK(c, cat)
}

// DeleteCategory 删除分类。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DeleteCategory
// @Tags 内容
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
// @Router /api/v1/news-categories/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) DeleteCategory(c *gin.Context) {
	if err := h.svc.DeleteCategory(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "删除分类")
	response.OK(c, gin.H{"message": "已删除"})
}

// Cleanup 手动触发过期清理。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Cleanup
// @Tags 内容
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
// @Router /api/v1/news/cleanup [post]
// KE:SWAGGER-END
func (h *Handler) Cleanup(c *gin.Context) {
	n, err := h.svc.Cleanup(c.Request.Context(), 0)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "清理过期资讯")
	response.OK(c, gin.H{"deleted": n})
}

// Sources 数据源分页。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Sources
// @Tags 内容
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
// @Router /api/v1/news-sources [get]
// KE:SWAGGER-END
func (h *Handler) Sources(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.ListSources(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// CreateSource 新增数据源。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary CreateSource
// @Tags 内容
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
// @Router /api/v1/news-sources [post]
// KE:SWAGGER-END
func (h *Handler) CreateSource(c *gin.Context) {
	var in SourceInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	src, err := h.svc.CreateSource(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "新增数据源 "+src.Code)
	response.OK(c, src)
}

// UpdateSource 编辑数据源。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary UpdateSource
// @Tags 内容
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
// @Router /api/v1/news-sources/{id} [put]
// KE:SWAGGER-END
func (h *Handler) UpdateSource(c *gin.Context) {
	var in SourceInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	src, err := h.svc.UpdateSource(c.Request.Context(), c.Param("id"), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "编辑数据源 "+src.Code)
	response.OK(c, src)
}

// DeleteSource 删除数据源。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary DeleteSource
// @Tags 内容
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
// @Router /api/v1/news-sources/{id} [delete]
// KE:SWAGGER-END
func (h *Handler) DeleteSource(c *gin.Context) {
	if err := h.svc.DeleteSource(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "资讯中心", "删除数据源")
	response.OK(c, gin.H{"message": "已删除"})
}

// parseTime 解析 YYYY-MM-DD 或 RFC3339，失败返回 nil（视为不限制）。
func parseTime(v string) *time.Time {
	if v == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}
