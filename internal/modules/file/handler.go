package file

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 文件接口。
type Handler struct{ svc *Service }

// NewHandler 创建文件处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Upload 上传文件。
func (h *Handler) Upload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		response.Fail(c, errs.BadRequest("请选择要上传的文件"))
		return
	}
	f, err := h.svc.Upload(c.Request.Context(), fh, middleware.UserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "文件管理", "上传文件")
	response.OK(c, f)
}

// List 文件列表。
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Delete 删除文件。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "文件管理", "删除文件")
	response.OK(c, gin.H{"message": "已删除"})
}

// Download 下载文件。
func (h *Handler) Download(c *gin.Context) {
	fh, f, err := h.svc.Open(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	defer fh.Close()
	ct := f.Mime
	if ct == "" {
		ct = mime.TypeByExtension(filepath.Ext(f.Name))
	}
	c.Header("Content-Type", ct)
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlEncode(f.Name))
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, fh)
}

func urlEncode(s string) string {
	const hexChars = "0123456789ABCDEF"
	var b []byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b = append(b, ch)
			continue
		}
		b = append(b, '%', hexChars[ch>>4], hexChars[ch&0x0f])
	}
	return string(b)
}
