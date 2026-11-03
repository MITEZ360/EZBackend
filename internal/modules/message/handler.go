package message

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 消息中心接口。
type Handler struct{ svc *Service }

// NewHandler 创建消息处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Inbox 我的消息。
func (h *Handler) Inbox(c *gin.Context) {
	q := &InboxQuery{
		Page:     httpx.QueryInt(c, "page", 1),
		PageSize: httpx.QueryInt(c, "page_size", 20),
		Status:   c.Query("status"),
		Type:     c.Query("type"),
		Keyword:  c.Query("keyword"),
	}
	list, total, err := h.svc.Inbox(c.Request.Context(), middleware.UserID(c), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, q.Page, q.PageSize)
}

// UnreadCount 未读数。
func (h *Handler) UnreadCount(c *gin.Context) {
	n, err := h.svc.UnreadCount(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"count": n})
}

// Recent 最近消息（铃铛预览）。
func (h *Handler) Recent(c *gin.Context) {
	list, err := h.svc.Recent(c.Request.Context(), middleware.UserID(c), httpx.QueryInt(c, "limit", 5))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Detail 消息详情。
func (h *Handler) Detail(c *gin.Context) {
	d, err := h.svc.Get(c.Request.Context(), middleware.UserID(c), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

// MarkRead 标记单条已读。
func (h *Handler) MarkRead(c *gin.Context) {
	if err := h.svc.MarkRead(c.Request.Context(), middleware.UserID(c), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已读"})
}

// MarkAllRead 全部已读，可按类型过滤。
func (h *Handler) MarkAllRead(c *gin.Context) {
	n, err := h.svc.MarkAllRead(c.Request.Context(), middleware.UserID(c), c.Query("type"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"updated": n})
}

// Delete 删除单条消息。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), middleware.UserID(c), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "已删除"})
}

// BatchDelete 批量删除消息。
func (h *Handler) BatchDelete(c *gin.Context) {
	var in struct {
		IDs []string `json:"ids" binding:"required,min=1,max=200" label:"消息"`
	}
	if !httpx.BindJSON(c, &in) {
		return
	}
	n, err := h.svc.BatchDelete(c.Request.Context(), middleware.UserID(c), in.IDs)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": n})
}

// DeleteRead 清空已读消息。
func (h *Handler) DeleteRead(c *gin.Context) {
	n, err := h.svc.DeleteRead(c.Request.Context(), middleware.UserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": n})
}

// Sent 发送记录。
func (h *Handler) Sent(c *gin.Context) {
	q := &SentQuery{
		Page:     httpx.QueryInt(c, "page", 1),
		PageSize: httpx.QueryInt(c, "page_size", 20),
		Keyword:  c.Query("keyword"),
		Type:     c.Query("type"),
		Status:   c.Query("status"),
	}
	list, total, err := h.svc.Sent(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, q.Page, q.PageSize)
}

// Send 发送消息。
func (h *Handler) Send(c *gin.Context) {
	var in SendInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	id := middleware.Identity(c)
	if id == nil {
		response.Fail(c, errs.Unauthorized("请先登录"))
		return
	}
	name := id.Nickname
	if name == "" {
		name = id.Username
	}
	res, err := h.svc.Send(c.Request.Context(), &in, id.UserID, name)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "消息中心", "发送消息")
	response.OK(c, res)
}

// Revoke 撤回消息。
func (h *Handler) Revoke(c *gin.Context) {
	if err := h.svc.Revoke(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "消息中心", "撤回消息")
	response.OK(c, gin.H{"message": "已撤回"})
}
