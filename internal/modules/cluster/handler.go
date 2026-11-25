package cluster

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 集群管理接口。
type Handler struct{ svc *Service }

// NewHandler 创建集群管理处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Nodes 节点列表。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Nodes
// @Tags 运维
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
// @Security BearerAuth
// @Router /api/v1/ops/cluster/nodes [get]
// KE:SWAGGER-END
func (h *Handler) Nodes(c *gin.Context) {
	nodes, err := h.svc.Nodes(c.Request.Context())
	if err != nil {
		c.Error(err)
		response.Fail(c, clusterErr(err, "读取节点列表失败"))
		return
	}
	response.OK(c, gin.H{"mode": h.svc.Mode(), "nodeId": h.svc.NodeID(), "nodes": nodes})
}

// Dispatch 下发运维指令（重启 / 重新加载配置）。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Dispatch
// @Tags 运维
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
// @Security BearerAuth
// @Router /api/v1/ops/cluster/dispatch [post]
// KE:SWAGGER-END
func (h *Handler) Dispatch(c *gin.Context) {
	var body struct {
		Action  string   `json:"action" binding:"required,oneof=restart reload-config" label:"动作"`
		Scope   string   `json:"scope" binding:"oneof=all selected" label:"范围"`
		NodeIDs []string `json:"nodeIds" label:"节点"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.Validation("请求参数格式错误"))
		return
	}
	operator := c.GetString("username")
	res, err := h.svc.Dispatch(c.Request.Context(), body.Action, body.NodeIDs, body.Scope, operator)
	if err != nil {
		c.Error(err)
		response.Fail(c, clusterErr(err, "下发指令失败"))
		return
	}
	action := "重启节点"
	if body.Action == ActionReload {
		action = "下发配置重载"
	}
	if body.Scope == "all" {
		action += "（全部）"
	}
	middleware.AuditInfo(c, "集群", action)
	response.OK(c, res)
}

// Commands 指令记录。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Commands
// @Tags 运维
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
// @Security BearerAuth
// @Router /api/v1/ops/cluster/commands [get]
// KE:SWAGGER-END
func (h *Handler) Commands(c *gin.Context) {
	list, err := h.svc.Commands(c.Request.Context(), 20)
	if err != nil {
		c.Error(err)
		response.Fail(c, clusterErr(err, "读取指令记录失败"))
		return
	}
	response.OK(c, gin.H{"list": list})
}

func clusterErr(err error, msg string) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	return errs.Internal(msg).WithCause(err)
}
