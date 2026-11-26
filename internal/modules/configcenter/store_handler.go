package configcenter

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/response"
)

// StoreHandler 业务配置中心接口。
type StoreHandler struct{ svc *ConfigStore }

// NewStoreHandler 创建处理器。
func NewStoreHandler(svc *ConfigStore) *StoreHandler { return &StoreHandler{svc: svc} }

// Namespaces 命名空间列表。
func (h *StoreHandler) Namespaces(c *gin.Context) {
	list, err := h.svc.Namespaces(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "读取命名空间失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// CreateNamespace 新建命名空间。
func (h *StoreHandler) CreateNamespace(c *gin.Context) {
	var in NamespaceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	row, err := h.svc.CreateNamespace(c.Request.Context(), in)
	if err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "创建命名空间失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "新增命名空间 "+row.Namespace)
	response.Created(c, row)
}

// DeleteNamespace 删除命名空间。
func (h *StoreHandler) DeleteNamespace(c *gin.Context) {
	if err := h.svc.DeleteNamespace(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "删除命名空间失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "删除命名空间")
	response.OK(c, gin.H{"deleted": true})
}

// Items 配置项列表。
func (h *StoreHandler) Items(c *gin.Context) {
	list, err := h.svc.Items(c.Request.Context(), c.Query("namespace"))
	if err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "读取配置项失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// SaveItems 批量保存配置项。
func (h *StoreHandler) SaveItems(c *gin.Context) {
	var body struct {
		Namespace string      `json:"namespace" binding:"required"`
		Items     []ItemInput `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	if err := h.svc.SaveItems(c.Request.Context(), body.Namespace, body.Items, c.GetString("username")); err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "保存配置失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "保存配置 "+body.Namespace)
	response.OK(c, gin.H{"saved": len(body.Items)})
}

// DeleteItem 删除配置项。
func (h *StoreHandler) DeleteItem(c *gin.Context) {
	if err := h.svc.DeleteItem(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "删除配置项失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "删除配置项")
	response.OK(c, gin.H{"deleted": true})
}

// Publish 发布命名空间。
func (h *StoreHandler) Publish(c *gin.Context) {
	var body struct {
		Namespace string `json:"namespace" binding:"required"`
		Note      string `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	rel, err := h.svc.Publish(c.Request.Context(), body.Namespace, c.GetString("username"), body.Note)
	if err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "发布配置失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "发布 "+body.Namespace+" r"+itoa(rel.Revision))
	response.OK(c, rel)
}

// Releases 发布历史。
func (h *StoreHandler) Releases(c *gin.Context) {
	list, err := h.svc.Releases(c.Request.Context(), c.Query("namespace"))
	if err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "读取发布记录失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

// Rollback 回滚配置。
func (h *StoreHandler) Rollback(c *gin.Context) {
	rel, err := h.svc.Rollback(c.Request.Context(), c.Param("id"), c.GetString("username"))
	if err != nil {
		c.Error(err)
		response.Fail(c, storeErr(err, "回滚配置失败"))
		return
	}
	middleware.AuditInfo(c, "配置中心", "回滚配置 r"+itoa(rel.Revision))
	response.OK(c, rel)
}

func storeErr(err error, msg string) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.NotFound("记录不存在")
	}
	return errs.Internal(msg).WithCause(err)
}

func itoa(v int64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = digits[v%10]
		v /= 10
	}
	return string(buf[i:])
}
