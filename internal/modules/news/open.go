package news

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/response"
)

// OpenHandler 开放接口，供爬虫与客户端调用。
//
// 与后台接口的关键差异：读接口只返回已发布内容、强制保留窗口、走游标分页，
// 写接口只接受批量推送，且幂等（重复推送不会产生重复数据）。
type OpenHandler struct{ svc *Service }

// NewOpenHandler 创建开放接口处理器。
func NewOpenHandler(svc *Service) *OpenHandler { return &OpenHandler{svc: svc} }

// Ingest 批量推送资讯。scope: news:ingest
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Ingest
// @Tags 开放接口
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
// @Security ApiKeyAuth
// @Router /api/open/v1/news/batch [post]
// KE:SWAGGER-END
func (h *OpenHandler) Ingest(c *gin.Context) {
	var in IngestRequest
	if !httpx.BindJSON(c, &in) {
		return
	}
	res, err := h.svc.Ingest(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// List 资讯列表。scope: news:read
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
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
// @Router /api/open/v1/news [get]
// KE:SWAGGER-END
func (h *OpenHandler) List(c *gin.Context) {
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	page, err := h.svc.CursorList(c.Request.Context(), CursorQuery{
		Cursor:   c.Query("cursor"),
		Limit:    limit,
		Category: c.Query("category"),
		Source:   c.Query("source"),
		Keyword:  c.Query("keyword"),
		From:     parseTime(c.Query("from")),
		To:       parseTime(c.Query("to")),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, page)
}

// Detail 资讯详情。scope: news:read
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Detail
// @Tags 开放接口
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
// @Security ApiKeyAuth
// @Router /api/open/v1/news/{id} [get]
// KE:SWAGGER-END
func (h *OpenHandler) Detail(c *gin.Context) {
	a, err := h.svc.Get(c.Request.Context(), c.Param("id"), true)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, a)
}

// Meta 筛选元数据：来源、分类与保留窗口。
//
// 来源与分类都是后台配置好的白名单，这里全量下发（含图标与颜色），
// 客户端启动时拉一次并缓存，即可渲染筛选面板与来源标识。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Meta
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
// @Router /api/open/v1/news/meta [get]
// KE:SWAGGER-END
func (h *OpenHandler) Meta(c *gin.Context) {
	meta, err := h.svc.BuildMeta(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, meta)
}
