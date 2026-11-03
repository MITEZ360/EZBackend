package middleware

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/pkg/utils"
)

// Auditor 接收操作日志，由审计模块异步落库。
type Auditor interface {
	Push(entry *model.OperationLog)
}

// Publisher 是事件总线的最小接口（*event.Bus 直接满足）。
type Publisher interface {
	Publish(ctx context.Context, topic string, data any)
}

// OperationEvent 是写操作完成后对外发布的事件，供指标、通知、外部同步等订阅。
type OperationEvent struct {
	Module   string `json:"module"`
	Action   string `json:"action"`
	Username string `json:"username"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	CostMS   int64  `json:"costMs"`
	TraceID  string `json:"traceId"`
}

// Audit 记录写操作审计日志（含请求/响应摘要），异步写入不阻塞请求；
// 同时向事件总线发布 audit.operation 事件（pub 可为 nil）。
func Audit(a Auditor, pub Publisher) gin.HandlerFunc {
	if a == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == "GET" || method == "HEAD" || method == "OPTIONS" {
			c.Next()
			return
		}
		if strings.Contains(c.Request.URL.Path, "/auth/login") ||
			strings.Contains(c.Request.URL.Path, "/auth/refresh") {
			c.Next()
			return
		}

		var reqBody []byte
		// 文件上传的 multipart 体不能被预读：Gin 的 FormFile 依赖完整未消费的 Body，
		// 按 4KB 截断后重建会导致解析失败（boundary 结尾丢失）。
		if c.Request.Body != nil {
			if strings.HasPrefix(c.ContentType(), "multipart/") {
				reqBody = []byte("[multipart/form-data 内容不记录]")
			} else {
				reqBody, _ = io.ReadAll(io.LimitReader(c.Request.Body, 4<<10))
				c.Request.Body = io.NopCloser(bytes.NewReader(reqBody))
			}
		}

		capture := &bodyCapture{ResponseWriter: c.Writer, buf: bytes.NewBuffer(nil), limit: 4 << 10}
		c.Writer = capture
		start := time.Now()
		c.Next()
		c.Writer = capture.ResponseWriter

		entry := &model.OperationLog{
			ID:        idgen.Next(),
			UserID:    c.GetString("user_id"),
			Username:  c.GetString("username"),
			Module:    c.GetString("audit_module"),
			Action:    c.GetString("audit_action"),
			Method:    method,
			Path:      c.Request.URL.Path,
			IP:        utils.ClientIP(c, true),
			Request:   sanitizeBody(reqBody),
			Response:  sanitizeBody(capture.buf.Bytes()),
			Status:    c.Writer.Status(),
			CostMS:    time.Since(start).Milliseconds(),
			TraceID:   c.GetString("trace_id"),
			CreatedAt: time.Now(),
		}
		if entry.Module == "" {
			entry.Module = guessModule(c.FullPath())
		}
		if entry.Action == "" {
			entry.Action = methodName(method)
		}
		if len(c.Errors) > 0 {
			entry.Error = c.Errors.String()
		}
		a.Push(entry)
		if pub != nil {
			pub.Publish(c.Request.Context(), "audit.operation", OperationEvent{
				Module: entry.Module, Action: entry.Action, Username: entry.Username,
				Path: entry.Path, Status: entry.Status, CostMS: entry.CostMS, TraceID: entry.TraceID,
			})
		}
	}
}

// AuditInfo 由业务处理器声明本次操作的模块与动作，供审计日志使用。
func AuditInfo(c *gin.Context, module, action string) {
	c.Set("audit_module", module)
	c.Set("audit_action", action)
}

type bodyCapture struct {
	gin.ResponseWriter
	buf   *bytes.Buffer
	limit int
}

func (w *bodyCapture) Write(b []byte) (int, error) {
	if w.buf.Len() < w.limit {
		remain := w.limit - w.buf.Len()
		if len(b) <= remain {
			w.buf.Write(b)
		} else {
			w.buf.Write(b[:remain])
		}
	}
	return w.ResponseWriter.Write(b)
}

var sensitiveRe = regexp.MustCompile(`(?i)"(password|oldPassword|newPassword|secret|token|refreshToken|accessToken|totpSecret)"\s*:\s*"[^"]*"`)

func sanitizeBody(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	s := sensitiveRe.ReplaceAllString(string(b), `"$1":"***"`)
	if len(s) > 2048 {
		return s[:2048] + "...(截断)"
	}
	return s
}

func guessModule(route string) string {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "system"
}

func methodName(m string) string {
	switch m {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	}
	return strings.ToLower(m)
}
