package membership

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/httpx"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/response"
)

// Handler 会员接口。
type Handler struct{ svc *Service }

// NewHandler 创建会员处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 会员等级分页。
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Catalog 启用中的等级列表，登录用户即可读取（个人中心做权益对比）。
func (h *Handler) Catalog(c *gin.Context) {
	list, err := h.svc.Catalog(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, list)
}

// Create 新增等级。
func (h *Handler) Create(c *gin.Context) {
	var in LevelInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	lv, err := h.svc.Create(c.Request.Context(), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "会员管理", "新增会员等级")
	response.OK(c, lv)
}

// Update 更新等级。
func (h *Handler) Update(c *gin.Context) {
	var in LevelInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "会员管理", "编辑会员等级")
	response.OK(c, gin.H{"message": "已保存"})
}

// Delete 删除等级。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "会员管理", "删除会员等级")
	response.OK(c, gin.H{"message": "已删除"})
}

// Grant 开通或续费会员。
func (h *Handler) Grant(c *gin.Context) {
	var in GrantInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	snap, err := h.svc.Grant(c.Request.Context(), c.Param("id"), &in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "会员管理", "开通会员")
	response.OK(c, snap)
}

// Revoke 取消会员。
func (h *Handler) Revoke(c *gin.Context) {
	if err := h.svc.Revoke(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "会员管理", "取消会员")
	response.OK(c, gin.H{"message": "已取消会员"})
}

// Me 当前用户的会员状态与可对比的等级列表。
func (h *Handler) Me(c *gin.Context) {
	userID := middleware.UserID(c)
	snap, err := h.svc.OfUser(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	levels, err := h.svc.Catalog(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{
		"member": snap.Member, "state": snap.State, "level": snap.Level, "levelId": snap.LevelID,
		"startAt": snap.StartAt, "expireAt": snap.ExpireAt, "daysLeft": snap.DaysLeft,
		"benefits": snap.Benefits, "levels": levels,
	})
}
