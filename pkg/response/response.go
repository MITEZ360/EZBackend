// Package response 提供统一的 HTTP 响应封装，保证所有接口返回结构一致。
package response

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/pkg/errs"
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

// OK 输出成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Body{Code: errs.CodeOK, Message: "success", Data: data, TraceID: TraceID(c)})
}

// OKMsg 输出带自定义提示语的成功响应。
func OKMsg(c *gin.Context, data any, msg string) {
	c.JSON(http.StatusOK, Body{Code: errs.CodeOK, Message: msg, Data: data, TraceID: TraceID(c)})
}

// Created 输出 201。
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Body{Code: errs.CodeOK, Message: "created", Data: data, TraceID: TraceID(c)})
}

// PageOK 输出分页响应。
func PageOK(c *gin.Context, list any, total int64, page, pageSize int) {
	OK(c, Page(list, total, page, pageSize))
}

// Fail 输出错误响应；HTTP 状态与业务码均可被前端与网关识别。
func Fail(c *gin.Context, err error) {
	e := errs.From(err)
	c.AbortWithStatusJSON(e.Status, Body{Code: e.Code, Message: e.Message, Data: e.Details, TraceID: TraceID(c)})
}

// FailMsg 输出指定业务码的错误。
func FailMsg(c *gin.Context, status, code int, msg string) {
	c.AbortWithStatusJSON(status, Body{Code: code, Message: msg, TraceID: TraceID(c)})
}

// NoContent 输出 204。
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }

// Download 输出附件下载响应头。
func Download(c *gin.Context, filename string) {
	c.Header("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	c.Header("Content-Type", "application/octet-stream")
}
