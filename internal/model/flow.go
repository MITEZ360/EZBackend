package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/platform/idgen"
)

// 流程定义状态。
const (
	FlowDraft     int8 = 0 // 草稿，不可发起
	FlowPublished int8 = 1 // 已发布，可发起
)

// 流程实例状态。
const (
	FlowRunning   = "running"
	FlowApproved  = "approved"
	FlowRejected  = "rejected"
	FlowCancelled = "cancelled"
)

// 任务状态。
const (
	TaskPending     = "pending"
	TaskApproved    = "approved"
	TaskRejected    = "rejected"
	TaskTransferred = "transferred"
	TaskCancelled   = "cancelled"
)

// 任务动作。
const (
	TaskActApprove  = "approve"
	TaskActReject   = "reject"
	TaskActTransfer = "transfer"
	TaskActAddSign  = "addSign"
	TaskActRemove   = "removeSign"
	TaskActRollback = "rollback"
	TaskActCancel   = "cancel"
	TaskActTimeout  = "timeout"
)

// 审批人来源类型。
const (
	AssigneeUser           = "user"
	AssigneeRole           = "role"
	AssigneeDeptLeader     = "deptLeader"
	AssigneeInitiatorLead  = "initiatorLeader"
	AssigneeInitiatorSelf  = "initiatorSelf"
	AssigneeInitiatorInput = "initiatorInput"
)

// 聚合规则。
const (
	FlowModeAny = "any" // 或签：任一同意即通过
	FlowModeAll = "all" // 会签：全部同意才通过
)

// 驳回策略。
const (
	RejectBackToInitiator = "backToInitiator"
	RejectEnd             = "end"
	RejectPrevNode        = "prevNode"
)

// 超时动作。
const (
	TimeoutNotify  = "notify"
	TimeoutApprove = "approve"
	TimeoutReject  = "reject"
)

// FlowAssignee 是一个审批人来源声明，运行期由引擎解析为具体用户。
type FlowAssignee struct {
	Type  string `json:"type" binding:"required"`
	Value string `json:"value,omitempty"`
}

// FlowCondition 是结构化跳过条件。Op 支持 eq/ne/gt/gte/lt/lte/in；
// 多条件通过 Group 组合，Group 为空时按单个条件处理。
type FlowCondition struct {
	Field string `json:"field,omitempty"`
	Op    string `json:"op,omitempty"`
	Value any    `json:"value,omitempty"`
	All   []FlowCondition `json:"all,omitempty"`
	Any   []FlowCondition `json:"any,omitempty"`
}

// FlowNode 是一个流程节点，nodes 数组即流程定义。
type FlowNode struct {
	ID            string          `json:"id" binding:"required"`
	Name          string          `json:"name" binding:"required"`
	Type          string          `json:"type,omitempty"`
	Assignees     []FlowAssignee  `json:"assignees,omitempty"`
	Mode          string          `json:"mode,omitempty"`
	OnReject      string          `json:"onReject,omitempty"`
	Sequential    bool            `json:"sequential,omitempty"`
	SkipWhen      *FlowCondition  `json:"skipWhen,omitempty"`
	TimeoutHours  int             `json:"timeoutHours,omitempty"`
	TimeoutAction string          `json:"timeoutAction,omitempty"`
}

// Flow 流程定义模板。
type Flow struct {
	Base
	Key         string     `gorm:"size:64;uniqueIndex;not null" json:"key"`
	Name        string     `gorm:"size:64;not null" json:"name"`
	Description string     `gorm:"size:255" json:"description,omitempty"`
	Status      int8       `gorm:"index;default:0" json:"status"`
	Nodes       []FlowNode `gorm:"serializer:json;type:text" json:"nodes"`
	Version     int        `gorm:"default:1" json:"version"`
}

// TableName 指定表名。
func (Flow) TableName() string { return "wf_flow" }

// FlowInstance 流程实例。NodesSnapshot 在发起时复制模板节点，
// 因此后续改模板不会影响已在跑的实例。
type FlowInstance struct {
	Base
	FlowKey       string         `gorm:"size:64;index;not null" json:"flowKey"`
	FlowName      string         `gorm:"size:64" json:"flowName,omitempty"`
	BusinessID    string         `gorm:"size:64;index" json:"businessId,omitempty"`
	BusinessType  string         `gorm:"size:64;index" json:"businessType,omitempty"`
	Title         string         `gorm:"size:128" json:"title,omitempty"`
	InitiatorID   string         `gorm:"size:32;index;not null" json:"initiatorId"`
	Status        string         `gorm:"size:16;index;not null" json:"status"`
	CurrentNode   string         `gorm:"size:64" json:"currentNode,omitempty"`
	NodesSnapshot []FlowNode     `gorm:"serializer:json;type:text" json:"nodesSnapshot,omitempty"`
	Variables     map[string]any `gorm:"serializer:json;type:text" json:"variables,omitempty"`
	Round         int            `gorm:"default:1" json:"round"`
	StartedAt     *time.Time     `json:"startedAt,omitempty"`
	FinishedAt    *time.Time     `json:"finishedAt,omitempty"`
}

// TableName 指定表名。
func (FlowInstance) TableName() string { return "wf_instance" }

// FlowTask 审批任务。每个审批人一条，同一节点的多条任务天然并行。
type FlowTask struct {
	Base
	InstanceID string     `gorm:"size:32;index;not null" json:"instanceId"`
	NodeID     string     `gorm:"size:64;index;not null" json:"nodeId"`
	NodeName   string     `gorm:"size:64" json:"nodeName,omitempty"`
	AssigneeID string     `gorm:"size:32;index;not null" json:"assigneeId"`
	Status     string     `gorm:"size:16;index;not null" json:"status"`
	Action     string     `gorm:"size:16" json:"action,omitempty"`
	Comment    string     `gorm:"size:512" json:"comment,omitempty"`
	IsAdded    bool       `gorm:"default:false" json:"isAdded,omitempty"`
	Round      int        `gorm:"default:1" json:"round"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// TableName 指定表名。
func (FlowTask) TableName() string { return "wf_task" }

// FlowRecord 流转轨迹，只追加不修改。
type FlowRecord struct {
	ID         string    `gorm:"primaryKey;size:32" json:"id"`
	InstanceID string    `gorm:"size:32;index;not null" json:"instanceId"`
	TaskID     string    `gorm:"size:32;index" json:"taskId,omitempty"`
	NodeID     string    `gorm:"size:64;index" json:"nodeId,omitempty"`
	NodeName   string    `gorm:"size:64" json:"nodeName,omitempty"`
	Action     string    `gorm:"size:16;not null" json:"action"`
	OperatorID string    `gorm:"size:32;index" json:"operatorId,omitempty"`
	Comment    string    `gorm:"size:512" json:"comment,omitempty"`
	FromUser   string    `gorm:"size:32" json:"fromUser,omitempty"`
	ToUser     string    `gorm:"size:32" json:"toUser,omitempty"`
	Round      int       `gorm:"default:1" json:"round"`
	CreatedAt  time.Time `json:"createdAt"`
}

// BeforeCreate 自动生成主键，与 Base 一致。
func (r *FlowRecord) BeforeCreate(*gorm.DB) error {
	if r.ID == "" {
		r.ID = idgen.Next()
	}
	return nil
}

// TableName 指定表名。
func (FlowRecord) TableName() string { return "wf_record" }

// FlowDelegate 委托代理，带生效时段。
type FlowDelegate struct {
	Base
	FromUserID string    `gorm:"size:32;index;not null" json:"fromUserId"`
	ToUserID   string    `gorm:"size:32;index;not null" json:"toUserId"`
	StartAt    time.Time `gorm:"index" json:"startAt"`
	EndAt      time.Time `gorm:"index" json:"endAt"`
	Enabled    bool      `gorm:"default:true;index" json:"enabled"`
	Remark     string    `gorm:"size:255" json:"remark,omitempty"`
}

// TableName 指定表名。
func (FlowDelegate) TableName() string { return "wf_delegate" }
