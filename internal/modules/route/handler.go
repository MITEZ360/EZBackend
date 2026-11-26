package route

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 动态路由接口。
type Handler struct{ svc *Service }

// NewHandler 创建处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 规则列表。
func (h *Handler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "读取路由规则失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// Create 新建规则（草稿）。
func (h *Handler) Create(c *gin.Context) {
	var in RouteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	row, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "创建路由规则失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "新增路由 "+row.Prefix)
	response.Created(c, row)
}

// Update 编辑规则。
func (h *Handler) Update(c *gin.Context) {
	var in RouteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	row, err := h.svc.Update(c.Request.Context(), c.Param("id"), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "更新路由规则失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "编辑路由 "+row.Prefix)
	response.OK(c, row)
}

// Delete 删除规则。
func (h *Handler) Delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "删除路由规则失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "删除路由")
	response.OK(c, gin.H{"deleted": true})
}

// Compile 校验并预览编译结果，不发布。
func (h *Handler) Compile(c *gin.Context) {
	var in []RouteInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	out, err := h.svc.Compile(c.Request.Context(), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "规则校验失败"))
		return
	}
	response.OK(c, gin.H{"routes": out, "count": len(out)})
}

// Publish 发布当前规则集。
func (h *Handler) Publish(c *gin.Context) {
	var in PublishInput
	// 允许空 body：不带灰度名单就是全量发布。
	_ = c.ShouldBindJSON(&in)
	in.Operator = c.GetString("username")
	rel, err := h.svc.Publish(c.Request.Context(), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "发布路由规则失败"))
		return
	}
	action := "发布路由规则 r" + itoa(rel.Revision)
	if rel.Stage == "canary" {
		action += "（灰度）"
	}
	middleware.AuditInfo(c, "服务治理", action)
	response.OK(c, rel)
}

// Releases 发布历史。
func (h *Handler) Releases(c *gin.Context) {
	list, err := h.svc.Releases(c.Request.Context(), c.Query("domain"))
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "读取发布记录失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// Promote 灰度转全量。
func (h *Handler) Promote(c *gin.Context) {
	rel, err := h.svc.Promote(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "转全量失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "灰度转全量 r"+itoa(rel.Revision))
	response.OK(c, rel)
}

// Rollback 回滚到指定发布。
func (h *Handler) Rollback(c *gin.Context) {
	rel, err := h.svc.Rollback(c.Request.Context(), c.Param("id"), c.GetString("username"))
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "回滚失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "回滚路由到 r"+itoa(rel.Revision))
	response.OK(c, rel)
}

func wrapErr(err error, msg string) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.NotFound("记录不存在")
	}
	return errs.Internal(msg).WithCause(err)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
