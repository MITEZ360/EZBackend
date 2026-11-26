package registry

import (
	"crypto/subtle"
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 服务注册与发现接口。
type Handler struct {
	svc   *Service
	token string
}

// NewHandler 创建处理器。
func NewHandler(svc *Service, token string) *Handler {
	return &Handler{svc: svc, token: token}
}

// Services 服务目录。
func (h *Handler) Services(c *gin.Context) {
	list, err := h.svc.ListServices(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "读取服务目录失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// CreateService 新建服务。
func (h *Handler) CreateService(c *gin.Context) {
	var in ServiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	row, err := h.svc.CreateService(c.Request.Context(), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "创建服务失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "新增服务 "+row.Name)
	response.Created(c, row)
}

// UpdateService 更新服务。
func (h *Handler) UpdateService(c *gin.Context) {
	var in ServiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	row, err := h.svc.UpdateService(c.Request.Context(), c.Param("id"), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "更新服务失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "编辑服务 "+row.Name)
	response.OK(c, row)
}

// DeleteService 删除服务。
func (h *Handler) DeleteService(c *gin.Context) {
	if err := h.svc.DeleteService(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "删除服务失败"))
		return
	}
	middleware.AuditInfo(c, "服务治理", "删除服务")
	response.OK(c, gin.H{"deleted": true})
}

// Instances 实例列表。
func (h *Handler) Instances(c *gin.Context) {
	list, err := h.svc.ListInstances(c.Request.Context(), c.Query("service"), c.Query("status"))
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "读取实例列表失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// SetStatus 摘流 / 恢复。
func (h *Handler) SetStatus(c *gin.Context) {
	var body struct {
		Status string `json:"status" binding:"required,oneof=online draining offline"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.Validation("状态只能是 online / draining / offline"))
		return
	}
	if err := h.svc.SetStatus(c.Request.Context(), c.Param("id"), body.Status); err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "修改实例状态失败"))
		return
	}
	action := "实例摘流"
	if body.Status == "online" {
		action = "实例恢复"
	}
	middleware.AuditInfo(c, "服务治理", action)
	response.OK(c, gin.H{"status": body.Status})
}

// Versions 版本分布。
func (h *Handler) Versions(c *gin.Context) {
	list, err := h.svc.Versions(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "读取版本分布失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// Register 实例注册（内部令牌鉴权）。
func (h *Handler) Register(c *gin.Context) {
	var in RegisterInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	row, err := h.svc.Register(c.Request.Context(), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "注册实例失败"))
		return
	}
	response.OK(c, row)
}

// Heartbeat 实例心跳（内部令牌鉴权）。
func (h *Handler) Heartbeat(c *gin.Context) {
	var in RegisterInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	if err := h.svc.Heartbeat(c.Request.Context(), in); err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "心跳失败"))
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// Deregister 实例主动下线（内部令牌鉴权）。
func (h *Handler) Deregister(c *gin.Context) {
	instanceID := c.Param("id")
	if err := h.svc.DeregisterScoped(c.Request.Context(), c.Query("service"), instanceID); err != nil {
		c.Error(err)
		response.Fail(c, wrapErr(err, "注销实例失败"))
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// internalOnly 校验内部令牌。注册与心跳是给业务进程调的，不能走用户会话。
func (h *Handler) internalOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.token == "" {
			c.Next()
			return
		}
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.token)) != 1 {
			c.Abort()
			response.Fail(c, errs.Unauthorized("内部令牌无效"))
			return
		}
		c.Next()
	}
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
