package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
)

// 引擎错误。service 层用 errors.Is 判断。
var (
	ErrFlowNotFound     = errors.New("流程定义不存在")
	ErrFlowNotPublished = errors.New("流程未发布")
	ErrFlowNoNodes      = errors.New("流程没有节点")
	ErrInstanceNotFound = errors.New("流程实例不存在")
	ErrTaskNotFound     = errors.New("任务不存在")
	ErrTaskNotPending   = errors.New("任务不是待处理状态")
	ErrInstanceDone     = errors.New("流程已结束")
	ErrNodeNotFound     = errors.New("节点不存在")
	ErrNoAssignee       = errors.New("节点无审批人")
)

// StartInput 发起流程的参数。
type StartInput struct {
	FlowKey      string
	BusinessID   string
	BusinessType string
	Title        string
	InitiatorID  string
	Variables    map[string]any
}

// TaskInput 处理任务的参数。
type TaskInput struct {
	Action   string
	Comment  string
	Operator string
	TargetID string // transfer 的目标用户；addSign 的新增审批人
}

// Engine 是流程推进核心，只依赖 GORM 与平台日志，不认识 HTTP 层。
type Engine struct {
	db  *gorm.DB
	log *zap.Logger
	// Notify 超时提醒钩子，由 service 注入；nil 表示只记轨迹不发通知。
	Notify func(ctx context.Context, instanceID, assigneeID, nodeName string)
}

func NewEngine(db *gorm.DB, log *zap.Logger) *Engine {
	if log == nil {
		log = zap.NewNop()
	}
	return &Engine{db: db, log: log}
}

// Start 创建实例并进入第一个节点。
func (e *Engine) Start(ctx context.Context, in StartInput) (*model.FlowInstance, error) {
	var flow model.Flow
	if err := e.db.WithContext(ctx).Where("key = ? AND status = ?", in.FlowKey, model.FlowPublished).
		First(&flow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var exists model.Flow
			if e.db.WithContext(ctx).Where("key = ?", in.FlowKey).First(&exists).Error == nil {
				return nil, ErrFlowNotPublished
			}
			return nil, ErrFlowNotFound
		}
		return nil, err
	}
	if len(flow.Nodes) == 0 {
		return nil, ErrFlowNoNodes
	}

	now := time.Now()
	inst := &model.FlowInstance{
		FlowKey:       flow.Key,
		FlowName:      flow.Name,
		BusinessID:    in.BusinessID,
		BusinessType:  in.BusinessType,
		Title:         in.Title,
		InitiatorID:   in.InitiatorID,
		Status:        model.FlowRunning,
		NodesSnapshot: append([]model.FlowNode(nil), flow.Nodes...),
		Variables:     in.Variables,
		Round:         1,
		StartedAt:     &now,
	}
	if err := e.db.WithContext(ctx).Create(inst).Error; err != nil {
		return nil, err
	}
	if err := e.advance(ctx, e.db, inst, 0); err != nil {
		return nil, err
	}
	return inst, nil
}

// advance 从 index 开始顺序推进，自动跳过命中 skipWhen 的节点与抄送节点。
// db 可以是事务句柄，保证整段推进在同一事务内。
func (e *Engine) advance(ctx context.Context, db *gorm.DB, inst *model.FlowInstance, index int) error {
	nodes := inst.NodesSnapshot
	for index < len(nodes) {
		node := nodes[index]
		if matchCondition(node.SkipWhen, inst.Variables) {
			if err := e.recordTx(db, inst, "", node, "skip", "", "", "", ""); err != nil {
				return err
			}
			index++
			continue
		}
		if node.Type == "cc" {
			if err := e.recordTx(db, inst, "", node, "cc", "", "", "", ""); err != nil {
				return err
			}
			index++
			continue
		}

		assignees, err := e.resolveAssignees(ctx, inst, node)
		if err != nil {
			return err
		}
		if len(assignees) == 0 {
			if err := e.recordTx(db, inst, "", node, "noAssignee", "", "", "", ""); err != nil {
				return err
			}
			// 无审批人视为自动通过，避免流程卡死；轨迹留痕供排查。
			index++
			continue
		}

		if err := db.WithContext(ctx).Model(inst).Updates(map[string]any{
			"current_node": node.ID,
			"status":       model.FlowRunning,
		}).Error; err != nil {
			return err
		}
		inst.CurrentNode = node.ID

		// 顺序会签只展开第一个任务，其余在前一条通过后逐个创建。
		pending := assignees
		if node.Sequential {
			pending = assignees[:1]
		}
		for _, uid := range pending {
			task := &model.FlowTask{
				InstanceID: inst.ID,
				NodeID:     node.ID,
				NodeName:   node.Name,
				AssigneeID: uid,
				Status:     model.TaskPending,
				Round:      inst.Round,
			}
			if err := db.WithContext(ctx).Create(task).Error; err != nil {
				return err
			}
		}
		return nil
	}
	return e.finishTx(db, inst, model.FlowApproved, "approve", "")
}

// resolveAssignees 把节点声明的来源解析为具体用户，并套用委托代理。
// 结果去重且过滤停用用户。
func (e *Engine) resolveAssignees(ctx context.Context, inst *model.FlowInstance, node model.FlowNode) ([]string, error) {
	now := time.Now()
	var out []string
	seen := map[string]bool{}
	add := func(uid string) {
		if uid == "" || seen[uid] {
			return
		}
		seen[uid] = true
		out = append(out, uid)
	}

	for _, a := range node.Assignees {
		var ids []string
		switch a.Type {
		case model.AssigneeUser:
			ids = []string{a.Value}
		case model.AssigneeRole:
			_ = e.db.WithContext(ctx).Model(&model.UserRole{}).
				Where("role_id = ?", a.Value).Pluck("user_id", &ids).Error
		case model.AssigneeInitiatorSelf:
			ids = []string{inst.InitiatorID}
		case model.AssigneeInitiatorInput:
			if v, ok := inst.Variables[a.Value]; ok {
				switch x := v.(type) {
				case string:
					ids = []string{x}
				case []any:
					for _, one := range x {
						if s, ok := one.(string); ok {
							ids = append(ids, s)
						}
					}
				}
			}
		case model.AssigneeDeptLeader, model.AssigneeInitiatorLead:
			var uid string
			if a.Type == model.AssigneeInitiatorLead {
				var user model.User
				if e.db.WithContext(ctx).Select("dept_id").Where("id = ?", inst.InitiatorID).First(&user).Error == nil {
					uid = e.deptLeader(ctx, user.DeptID)
				}
			} else {
				uid = e.deptLeader(ctx, a.Value)
			}
			ids = []string{uid}
		default:
			continue
		}

		for _, uid := range ids {
			var delegates []model.FlowDelegate
			_ = e.db.WithContext(ctx).Where("from_user_id = ? AND enabled = ? AND start_at <= ? AND end_at >= ?",
				uid, true, now, now).Find(&delegates).Error
			if len(delegates) > 0 {
				uid = delegates[0].ToUserID
			}
			var cnt int64
			e.db.WithContext(ctx).Model(&model.User{}).
				Where("id = ? AND status = ?", uid, model.StatusEnabled).Count(&cnt)
			if cnt > 0 {
				add(uid)
			}
		}
	}
	return out, nil
}

// deptLeader 解析部门负责人。Leader 字段历史存姓名，优先按 real_name 匹配；
// 若将来扩展 leader_user_id，这里按 ID 命中会更准确。
func (e *Engine) deptLeader(ctx context.Context, deptID string) string {
	if deptID == "" {
		return ""
	}
	var dept model.Dept
	if e.db.WithContext(ctx).Where("id = ?", deptID).First(&dept).Error != nil || dept.Leader == "" {
		return ""
	}
	var user model.User
	if e.db.WithContext(ctx).Where("real_name = ?", dept.Leader).First(&user).Error == nil {
		return user.ID
	}
	return ""
}

// HandleTask 处理任务：同意、驳回、移交、加签、减签。
func (e *Engine) HandleTask(ctx context.Context, taskID string, in TaskInput) error {
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.FlowTask
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTaskNotFound
			}
			return err
		}
		if task.Status != model.TaskPending {
			return ErrTaskNotPending
		}
		var inst model.FlowInstance
		if err := tx.First(&inst, "id = ?", task.InstanceID).Error; err != nil {
			return err
		}
		if inst.Status != model.FlowRunning {
			return ErrInstanceDone
		}
		node, idx := findNode(inst.NodesSnapshot, task.NodeID)
		if idx < 0 {
			return ErrNodeNotFound
		}

		now := time.Now()
		switch in.Action {
		case model.TaskActTransfer:
			if in.TargetID == "" {
				return errors.New("移交必须指定目标用户")
			}
			// 原任务终结、同节点新建任务；round 不变，聚合计数不变。
			if err := tx.Model(&task).Updates(map[string]any{
				"status": model.TaskTransferred, "action": model.TaskActTransfer,
				"comment": in.Comment, "finished_at": now,
			}).Error; err != nil {
				return err
			}
			next := model.FlowTask{InstanceID: inst.ID, NodeID: node.ID, NodeName: node.Name,
				AssigneeID: in.TargetID, Status: model.TaskPending, Round: inst.Round}
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			return e.recordTx(tx, &inst, task.ID, node, model.TaskActTransfer, in.Operator, in.Comment, task.AssigneeID, in.TargetID)

		case model.TaskActAddSign:
			if in.TargetID == "" {
				return errors.New("加签必须指定审批人")
			}
			next := model.FlowTask{InstanceID: inst.ID, NodeID: node.ID, NodeName: node.Name,
				AssigneeID: in.TargetID, Status: model.TaskPending, IsAdded: true, Round: inst.Round}
			if err := tx.Create(&next).Error; err != nil {
				return err
			}
			return e.recordTx(tx, &inst, task.ID, node, model.TaskActAddSign, in.Operator, in.Comment, "", in.TargetID)

		case model.TaskActRemove:
			// 减签只能取消待处理任务，且节点至少保留一条任务避免卡死。
			var cnt int64
			tx.Model(&model.FlowTask{}).Where("instance_id = ? AND node_id = ? AND round = ? AND status = ?",
				inst.ID, node.ID, inst.Round, model.TaskPending).Count(&cnt)
			if cnt <= 1 {
				return errors.New("节点至少保留一条待处理任务")
			}
			if err := tx.Model(&task).Updates(map[string]any{
				"status": model.TaskCancelled, "action": model.TaskActRemove,
				"comment": in.Comment, "finished_at": now,
			}).Error; err != nil {
				return err
			}
			e.afterTaskDone(ctx, &inst, node, idx, taskID, tx)
			return e.recordTx(tx, &inst, task.ID, node, model.TaskActRemove, in.Operator, in.Comment, "", task.AssigneeID)

		case model.TaskActApprove, model.TaskActReject:
			action := model.TaskApproved
			if in.Action == model.TaskActReject {
				action = model.TaskRejected
			}
			// 条件更新保证幂等：并发下只有一个请求能改动这条任务。
			res := tx.Model(&model.FlowTask{}).Where("id = ? AND status = ?", taskID, model.TaskPending).
				Updates(map[string]any{"status": action, "action": in.Action,
					"comment": in.Comment, "finished_at": now})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrTaskNotPending
			}
			if err := e.recordTx(tx, &inst, task.ID, node, in.Action, in.Operator, in.Comment, "", ""); err != nil {
				return err
			}
			if action == model.TaskRejected {
				return e.handleReject(ctx, tx, &inst, node, idx, in.Operator, in.Comment)
			}
			return e.afterTaskDone(ctx, &inst, node, idx, taskID, tx)

		default:
			return fmt.Errorf("未知任务动作: %s", in.Action)
		}
	})
}

// afterTaskDone 在一条任务通过后判断节点是否完成；完成则推进下一节点。
func (e *Engine) afterTaskDone(ctx context.Context, inst *model.FlowInstance, node model.FlowNode, idx int, doneTaskID string, tx *gorm.DB) error {
	var tasks []model.FlowTask
	if err := tx.Where("instance_id = ? AND node_id = ? AND round = ? AND status <> ?",
		inst.ID, node.ID, inst.Round, model.TaskCancelled).Find(&tasks).Error; err != nil {
		return err
	}

	// 顺序会签：还有未展开的审批人时，创建下一条任务后返回。
	if node.Sequential {
		for _, a := range node.Assignees {
			var cnt int64
			tx.Model(&model.FlowTask{}).Where("instance_id = ? AND node_id = ? AND round = ? AND assignee_id = ?",
				inst.ID, node.ID, inst.Round, a.Value).Count(&cnt)
			if cnt == 0 && a.Type == model.AssigneeUser {
				next := model.FlowTask{InstanceID: inst.ID, NodeID: node.ID, NodeName: node.Name,
					AssigneeID: a.Value, Status: model.TaskPending, Round: inst.Round}
				return tx.Create(&next).Error
			}
		}
	}

	allApproved := true
	anyApproved := false
	for _, t := range tasks {
		switch t.Status {
		case model.TaskApproved:
			anyApproved = true
		case model.TaskPending:
			allApproved = false
		case model.TaskRejected:
			allApproved = false
		}
	}
	passed := (node.Mode == model.FlowModeAll && allApproved) ||
		(node.Mode != model.FlowModeAll && anyApproved)
	if !passed {
		return nil
	}
	// 或签通过后取消其余待办，避免重复处理。
	if node.Mode != model.FlowModeAll {
		if err := tx.Model(&model.FlowTask{}).
			Where("instance_id = ? AND node_id = ? AND round = ? AND status = ?",
				inst.ID, node.ID, inst.Round, model.TaskPending).
			Update("status", model.TaskCancelled).Error; err != nil {
			return err
		}
	}
	// 用新事务上下文推进；外层事务仍包裹，GORM 的嵌套事务即 savepoint。
	return e.advance(ctx, tx, inst, idx+1)
}

// handleReject 按节点 onReject 策略处理驳回。
func (e *Engine) handleReject(ctx context.Context, tx *gorm.DB, inst *model.FlowInstance, node model.FlowNode, idx int, operator, comment string) error {
	policy := node.OnReject
	if policy == "" {
		policy = model.RejectBackToInitiator
	}
	switch policy {
	case model.RejectEnd:
		return e.finishTx(tx, inst, model.FlowRejected, model.TaskActReject, comment)
	case model.RejectPrevNode, model.RejectBackToInitiator:
		target := idx - 1
		if policy == model.RejectBackToInitiator {
			target = -1
		}
		if target < 0 {
			return e.finishTx(tx, inst, model.FlowRejected, model.TaskActReject, comment)
		}
		if err := tx.Model(&model.FlowTask{}).
			Where("instance_id = ? AND status = ?", inst.ID, model.TaskPending).
			Update("status", model.TaskCancelled).Error; err != nil {
			return err
		}
		round := inst.Round + 1
		if err := tx.Model(inst).Update("round", round).Error; err != nil {
			return err
		}
		inst.Round = round
		return e.advance(ctx, tx, inst, target)
	default:
		return e.finishTx(tx, inst, model.FlowRejected, model.TaskActReject, comment)
	}
}

// Rollback 把流程退回指定节点重走，旧任务作废并递增 round。
func (e *Engine) Rollback(ctx context.Context, instanceID, targetNodeID, reason, operator string) error {
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inst model.FlowInstance
		if err := tx.First(&inst, "id = ?", instanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInstanceNotFound
			}
			return err
		}
		if inst.Status != model.FlowRunning {
			return ErrInstanceDone
		}
		_, idx := findNode(inst.NodesSnapshot, targetNodeID)
		if idx < 0 {
			return ErrNodeNotFound
		}
		if err := tx.Model(&model.FlowTask{}).
			Where("instance_id = ? AND status = ?", inst.ID, model.TaskPending).
			Update("status", model.TaskCancelled).Error; err != nil {
			return err
		}
		round := inst.Round + 1
		if err := tx.Model(&inst).Update("round", round).Error; err != nil {
			return err
		}
		inst.Round = round
		if err := e.recordTx(tx, &inst, "", inst.NodesSnapshot[idx], model.TaskActRollback, operator, reason, "", ""); err != nil {
			return err
		}
		return e.advance(ctx, tx, &inst, idx)
	})
}

// Cancel 发起人撤销。
func (e *Engine) Cancel(ctx context.Context, instanceID, operator string) error {
	return e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inst model.FlowInstance
		if err := tx.First(&inst, "id = ?", instanceID).Error; err != nil {
			return err
		}
		if inst.Status != model.FlowRunning {
			return ErrInstanceDone
		}
		if err := tx.Model(&model.FlowTask{}).Where("instance_id = ? AND status = ?",
			inst.ID, model.TaskPending).Update("status", model.TaskCancelled).Error; err != nil {
			return err
		}
		return e.finishTx(tx, &inst, model.FlowCancelled, model.TaskActCancel, "")
	})
}

// ScanTimeout 扫描超时待办，按节点 timeoutAction 处理。
// 由 job 模块定时调用；notify 通过 e.Notify 钩子发站内信。
func (e *Engine) ScanTimeout(ctx context.Context) (int, error) {
	var tasks []model.FlowTask
	now := time.Now()
	if err := e.db.WithContext(ctx).Where("status = ?", model.TaskPending).Find(&tasks).Error; err != nil {
		return 0, err
	}
	handled := 0
	for _, task := range tasks {
		var inst model.FlowInstance
		if e.db.WithContext(ctx).First(&inst, "id = ?", task.InstanceID).Error != nil || inst.Status != model.FlowRunning {
			continue
		}
		node, idx := findNode(inst.NodesSnapshot, task.NodeID)
		if idx < 0 || node.TimeoutHours <= 0 ||
			now.Sub(task.CreatedAt) < time.Duration(node.TimeoutHours)*time.Hour {
			continue
		}
		action := node.TimeoutAction
		if action == "" {
			action = model.TimeoutNotify
		}
		if action == model.TimeoutNotify {
			if e.Notify != nil {
				e.Notify(ctx, inst.ID, task.AssigneeID, node.Name)
			}
			if err := e.record(ctx, &inst, task.ID, node, "timeoutNotify", "", "", "", ""); err != nil {
				return handled, err
			}
			handled++
			continue
		}
		act := model.TaskActApprove
		if action == model.TimeoutReject {
			act = model.TaskActReject
		}
		if err := e.HandleTask(ctx, task.ID, TaskInput{Action: act, Comment: "超时自动处理", Operator: "system"}); err != nil {
			e.log.Warn("超时自动处理失败", zap.String("task", task.ID), zap.Error(err))
			continue
		}
		handled++
	}
	return handled, nil
}

func (e *Engine) finish(ctx context.Context, inst *model.FlowInstance, status, action, comment string) error {
	return e.finishTx(e.db.WithContext(ctx), inst, status, action, comment)
}

func (e *Engine) finishTx(tx *gorm.DB, inst *model.FlowInstance, status, action, comment string) error {
	now := time.Now()
	if err := tx.Model(inst).Updates(map[string]any{
		"status": status, "finished_at": now, "current_node": "",
	}).Error; err != nil {
		return err
	}
	inst.Status = status
	return e.recordTx(tx, inst, "", model.FlowNode{ID: "", Name: inst.FlowName}, action, "", comment, "", "")
}

func (e *Engine) record(ctx context.Context, inst *model.FlowInstance, taskID string, node model.FlowNode, action, operator, comment, from, to string) error {
	return e.recordTx(e.db.WithContext(ctx), inst, taskID, node, action, operator, comment, from, to)
}

func (e *Engine) recordTx(tx *gorm.DB, inst *model.FlowInstance, taskID string, node model.FlowNode, action, operator, comment, from, to string) error {
	return tx.Create(&model.FlowRecord{
		InstanceID: inst.ID, TaskID: taskID, NodeID: node.ID, NodeName: node.Name,
		Action: action, OperatorID: operator, Comment: comment,
		FromUser: from, ToUser: to, Round: inst.Round,
	}).Error
}

// findNode 按 ID 找节点及其下标。
func findNode(nodes []model.FlowNode, id string) (model.FlowNode, int) {
	for i, n := range nodes {
		if n.ID == id {
			return n, i
		}
	}
	return model.FlowNode{}, -1
}

// matchCondition 求结构化条件。nil 或空条件恒为 false（不跳过）。
func matchCondition(c *model.FlowCondition, vars map[string]any) bool {
	if c == nil {
		return false
	}
	if len(c.All) > 0 {
		for _, sub := range c.All {
			if !matchCondition(&sub, vars) {
				return false
			}
		}
		return true
	}
	if len(c.Any) > 0 {
		for _, sub := range c.Any {
			if matchCondition(&sub, vars) {
				return true
			}
		}
		return false
	}
	if c.Field == "" {
		return false
	}
	left, ok := vars[c.Field]
	if !ok {
		return false
	}
	switch c.Op {
	case "eq":
		return same(left, c.Value)
	case "ne":
		return !same(left, c.Value)
	case "in":
		arr, ok := c.Value.([]any)
		if !ok {
			return false
		}
		for _, one := range arr {
			if same(left, one) {
				return true
			}
		}
		return false
	default:
		l, err1 := toFloat(left)
		r, err2 := toFloat(c.Value)
		if err1 != nil || err2 != nil {
			return false
		}
		switch c.Op {
		case "gt":
			return l > r
		case "gte":
			return l >= r
		case "lt":
			return l < r
		case "lte":
			return l <= r
		}
		return false
	}
}

func same(a, b any) bool {
	if af, e1 := toFloat(a); e1 == nil {
		if bf, e2 := toFloat(b); e2 == nil {
			return af == bf
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case json.Number:
		return x.Float64()
	case string:
		return strconv.ParseFloat(x, 64)
	default:
		return 0, fmt.Errorf("非数值: %T", v)
	}
}
