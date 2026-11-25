package flow

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Handler 流程接口。
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ---- 流程定义 ----

func (h *Handler) ListFlows(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.ListFlows(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

func (h *Handler) GetFlow(c *gin.Context) {
	f, err := h.svc.GetFlow(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, f)
}

func (h *Handler) CreateFlow(c *gin.Context) {
	var in model.Flow
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.CreateFlow(c.Request.Context(), &in); err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, in)
}

func (h *Handler) UpdateFlow(c *gin.Context) {
	var in model.Flow
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.UpdateFlow(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	f, _ := h.svc.GetFlow(c.Request.Context(), c.Param("id"))
	response.OK(c, f)
}

func (h *Handler) SetFlowStatus(c *gin.Context) {
	var in struct {
		Status int8 `json:"status" binding:"required"`
	}
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SetFlowStatus(c.Request.Context(), c.Param("id"), in.Status); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "状态已更新")
}

func (h *Handler) DeleteFlow(c *gin.Context) {
	if err := h.svc.DeleteFlow(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "已删除")
}

// ---- 流程实例 ----

func (h *Handler) Start(c *gin.Context) {
	var in struct {
		FlowKey      string         `json:"flowKey" binding:"required"`
		BusinessID   string         `json:"businessId"`
		BusinessType string         `json:"businessType"`
		Title        string         `json:"title"`
		Variables    map[string]any `json:"variables"`
	}
	if !httpx.BindJSON(c, &in) {
		return
	}
	inst, err := h.svc.Start(c.Request.Context(), StartInput{
		FlowKey: in.FlowKey, BusinessID: in.BusinessID, BusinessType: in.BusinessType,
		Title: in.Title, InitiatorID: middleware.UserID(c), Variables: in.Variables,
	}, middleware.UserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, inst)
}

func (h *Handler) ListInstances(c *gin.Context) {
	p := query.Parse(c)
	mine := c.Query("mine") == "1" || c.Query("mine") == "true"
	list, total, err := h.svc.ListInstances(c.Request.Context(), p, middleware.UserID(c), mine)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

func (h *Handler) InstanceDetail(c *gin.Context) {
	d, err := h.svc.InstanceDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

func (h *Handler) CancelInstance(c *gin.Context) {
	if err := h.svc.CancelInstance(c.Request.Context(), c.Param("id"), middleware.UserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "已撤销")
}

func (h *Handler) Rollback(c *gin.Context) {
	var in struct {
		NodeID string `json:"nodeId" binding:"required"`
		Reason string `json:"reason"`
	}
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.Rollback(c.Request.Context(), c.Param("id"), in.NodeID, in.Reason, middleware.UserID(c)); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "已回退")
}

// ---- 任务 ----

func (h *Handler) ListTasks(c *gin.Context) {
	p := query.Parse(c)
	status := c.Query("status") // todo / done / 具体状态
	list, total, err := h.svc.ListTasks(c.Request.Context(), p, middleware.UserID(c), status)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

func (h *Handler) GetTask(c *gin.Context) {
	d, err := h.svc.TaskDetail(c.Request.Context(), c.Param("id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, d)
}

func (h *Handler) handle(c *gin.Context, action string) {
	var in struct {
		Comment  string `json:"comment"`
		TargetID string `json:"targetId"`
	}
	_ = c.ShouldBindJSON(&in)
	if err := h.svc.HandleTask(c.Request.Context(), c.Param("id"), middleware.UserID(c),
		TaskInput{Action: action, Comment: in.Comment, TargetID: in.TargetID}); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "已处理")
}

func (h *Handler) Approve(c *gin.Context)      { h.handle(c, model.TaskActApprove) }
func (h *Handler) Reject(c *gin.Context)       { h.handle(c, model.TaskActReject) }
func (h *Handler) Transfer(c *gin.Context)     { h.handle(c, model.TaskActTransfer) }
func (h *Handler) AddSign(c *gin.Context)      { h.handle(c, model.TaskActAddSign) }
func (h *Handler) RemoveSign(c *gin.Context)   { h.handle(c, model.TaskActRemove) }

// ---- 委托 ----

func (h *Handler) ListDelegates(c *gin.Context) {
	p := query.Parse(c)
	all := c.Query("all") == "1" || c.Query("all") == "true"
	uid := middleware.UserID(c)
	if all {
		uid = ""
	}
	list, total, err := h.svc.ListDelegates(c.Request.Context(), p, uid)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

func (h *Handler) CreateDelegate(c *gin.Context) {
	var in model.FlowDelegate
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.CreateDelegate(c.Request.Context(), &in); err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, in)
}

func (h *Handler) UpdateDelegate(c *gin.Context) {
	var in model.FlowDelegate
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.UpdateDelegate(c.Request.Context(), c.Param("id"), &in); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "已更新")
}

func (h *Handler) DeleteDelegate(c *gin.Context) {
	if err := h.svc.DeleteDelegate(c.Request.Context(), c.Param("id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OKMsg(c, nil, "已删除")
}

// TimeoutScan 手动触发超时扫描，供运维或测试使用。
func (h *Handler) TimeoutScan(c *gin.Context) {
	n, err := h.svc.Engine().ScanTimeout(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, map[string]any{"handled": n})
}
