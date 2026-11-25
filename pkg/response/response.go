// Package response 提供统一的 HTTP 响应封装，保证所有接口返回结构一致。
package response

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
)

// Body 是统一响应体。
type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	TraceID string `json:"traceId,omitempty"`
}

// PageData 是分页数据的标准结构。
type PageData struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
	Pages    int   `json:"pages"`
}

// Page 构造分页数据。
func Page(list any, total int64, page, pageSize int) *PageData {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if list == nil {
		list = []any{}
	}
	return &PageData{List: list, Total: total, Page: page, PageSize: pageSize, Pages: pages}
}

// TraceID 读取当前请求的链路 ID。
func TraceID(c *gin.Context) string {
	if v, ok := c.Get("trace_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return c.Writer.Header().Get("X-Trace-Id")
}

// translateDataMessage 翻译成功响应 data 中的 message 字段。
// 大量 handler 把「已保存」等提示语放在 gin.H{"message": ...} 中返回；
// 顶层 Message 固定为 success，若不处理这里，前端拿到的提示语仍是中文。
// 只处理 map[string]any 的顶层字段，避免递归改写业务对象。
func translateDataMessage(c *gin.Context, data any) any {
	var m map[string]any
	switch v := data.(type) {
	case gin.H:
		m = map[string]any(v)
	case map[string]any:
		m = v
	default:
		return data
	}
	msg, ok := m["message"].(string)
	if !ok {
		return data
	}
	translated := i18n.T(c, msg)
	if translated == msg {
		return data
	}
	copied := make(map[string]any, len(m))
	for k, v := range m {
		copied[k] = v
	}
	copied["message"] = translated
	return copied
}

// OK 输出成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{
		Code: errs.CodeOK, Message: i18n.T(c, "success"), Data: translateDataMessage(c, data), TraceID: TraceID(c),
	})
}

// OKMsg 输出带自定义提示语的成功响应。
func OKMsg(c *gin.Context, data any, msg string) {
	c.JSON(http.StatusOK, Body{
		Code: errs.CodeOK, Message: i18n.T(c, msg), Data: translateDataMessage(c, data), TraceID: TraceID(c),
	})
}

// Created 输出 201。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{
		Code: errs.CodeOK, Message: i18n.T(c, "created"), Data: translateDataMessage(c, data), TraceID: TraceID(c),
	})
}

// PageOK 输出分页响应。
func PageOK(c *gin.Context, list any, total int64, page, pageSize int) {
	OK(c, Page(list, total, page, pageSize))
}

// Fail 输出错误响应；HTTP 状态与业务码均可被前端与网关识别。
// 文案按请求语言翻译；未收录的译文原样返回中文，不影响存量接口。
func Fail(c *gin.Context, err error) {
	e := errs.From(err)
	c.AbortWithStatusJSON(e.Status, Body{
		Code: e.Code, Message: i18n.T(c, e.Message), Data: e.Details, TraceID: TraceID(c),
	})
}

// FailMsg 输出指定业务码的错误。
func FailMsg(c *gin.Context, status, code int, msg string) {
	c.AbortWithStatusJSON(status, Body{Code: code, Message: i18n.T(c, msg), TraceID: TraceID(c)})
}

// NoContent 输出 204。
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }

// Download 输出附件下载响应头。
func Download(c *gin.Context, filename string) {
	c.Header("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	c.Header("Content-Type", "application/octet-stream")
}
