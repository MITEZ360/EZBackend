// Package flow 提供轻量工作流引擎：流程定义、发起、审批、轨迹与委托。
package flow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/query"
)

// Service 流程服务，承载 CRUD 与查询，核心推进委托给 Engine。
type Service struct {
	db     *gorm.DB
	log    *zap.Logger
	engine *Engine
}

func NewService(db *gorm.DB, log *zap.Logger) *Service {
	e := NewEngine(db, log)
	return &Service{db: db, log: log, engine: e}
}

func (s *Service) Engine() *Engine { return s.engine }

// ---- 流程定义 ----

func (s *Service) ListFlows(ctx context.Context, p *query.Params) ([]model.Flow, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.Flow{})
	if p.Keyword != "" {
		db = query.Search(db, p.Keyword, "key", "name")
	}
	db = p.Filter(db, map[string]string{"key": "key", "name": "name", "status": "status"})
	var list []model.Flow
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := p.Order(map[string]string{"key": "key", "name": "name", "status": "status", "createdAt": "created_at", "updatedAt": "updated_at"}, "created_at DESC")
	if err := db.Order(order).Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *Service) GetFlow(ctx context.Context, id string) (*model.Flow, error) {
	var f model.Flow
	if err := s.db.WithContext(ctx).First(&f, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("流程定义不存在")
		}
		return nil, err
	}
	return &f, nil
}

func (s *Service) CreateFlow(ctx context.Context, f *model.Flow) error {
	if f.Key == "" || f.Name == "" {
		return errs.Validation("key 与 name 不能为空")
	}
	if len(f.Nodes) == 0 {
		return errs.Validation("至少需要配置一个节点")
	}
	if err := s.validateNodes(f.Nodes); err != nil {
		return err
	}
	f.Status = model.FlowDraft
	f.Version = 1
	return s.db.WithContext(ctx).Create(f).Error
}

func (s *Service) UpdateFlow(ctx context.Context, id string, in *model.Flow) error {
	var old model.Flow
	if err := s.db.WithContext(ctx).First(&old, "id = ?", id).Error; err != nil {
		return errs.NotFound("流程定义不存在")
	}
	if in.Key != "" && in.Key != old.Key {
		var cnt int64
		s.db.WithContext(ctx).Model(&model.Flow{}).Where("key = ? AND id <> ?", in.Key, id).Count(&cnt)
		if cnt > 0 {
			return errs.Conflict("流程 key 已存在")
		}
	}
	if len(in.Nodes) > 0 {
		if err := s.validateNodes(in.Nodes); err != nil {
			return err
		}
		in.Version = old.Version + 1
	} else {
		in.Nodes = old.Nodes
	}
	in.ID = id
	in.CreatedAt = old.CreatedAt
	// 用 Select 明确字段，避免空值把 name/status 覆盖成零。
	return s.db.WithContext(ctx).Model(&model.Flow{}).Where("id = ?", id).
		Select("key", "name", "description", "status", "nodes", "version").Updates(in).Error
}

func (s *Service) SetFlowStatus(ctx context.Context, id string, status int8) error {
	if status != model.FlowDraft && status != model.FlowPublished {
		return errs.Validation("状态只能是草稿或已发布")
	}
	var f model.Flow
	if err := s.db.WithContext(ctx).First(&f, "id = ?", id).Error; err != nil {
		return errs.NotFound("流程定义不存在")
	}
	if status == model.FlowPublished {
		if len(f.Nodes) == 0 {
			return errs.Validation("没有节点不能发布")
		}
		if err := s.validateNodes(f.Nodes); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Model(&f).Update("status", status).Error
}

func (s *Service) DeleteFlow(ctx context.Context, id string) error {
	var cnt int64
	s.db.WithContext(ctx).Model(&model.FlowInstance{}).Where("flow_key IN (SELECT key FROM wf_flow WHERE id = ?)", id).Count(&cnt)
	if cnt > 0 {
		return errs.Conflict("已有流程实例引用该定义，不能删除")
	}
	return s.db.WithContext(ctx).Delete(&model.Flow{}, "id = ?", id).Error
}

func (s *Service) validateNodes(nodes []model.FlowNode) error {
	ids := map[string]bool{}
	for _, n := range nodes {
		if n.ID == "" || n.Name == "" {
			return errs.Validation("节点 id 与 name 不能为空")
		}
		if ids[n.ID] {
			return errs.Validation(fmt.Sprintf("节点 id 重复: %s", n.ID))
		}
		ids[n.ID] = true
		if n.Mode != "" && n.Mode != model.FlowModeAny && n.Mode != model.FlowModeAll {
			return errs.Validation(fmt.Sprintf("节点 %s 的 mode 非法", n.ID))
		}
		for _, a := range n.Assignees {
			switch a.Type {
			case model.AssigneeUser, model.AssigneeRole, model.AssigneeDeptLeader,
				model.AssigneeInitiatorLead, model.AssigneeInitiatorSelf, model.AssigneeInitiatorInput:
			default:
				return errs.Validation(fmt.Sprintf("节点 %s 的审批人来源非法: %s", n.ID, a.Type))
			}
		}
	}
	return nil
}

// ---- 流程实例 ----

func (s *Service) Start(ctx context.Context, in StartInput, operator string) (*model.FlowInstance, error) {
	if in.InitiatorID == "" {
		in.InitiatorID = operator
	}
	inst, err := s.engine.Start(ctx, in)
	if err != nil {
		return nil, toHTTPError(err)
	}
	return inst, nil
}

func (s *Service) ListInstances(ctx context.Context, p *query.Params, initiatorID string, mineOnly bool) ([]model.FlowInstance, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.FlowInstance{})
	if mineOnly && initiatorID != "" {
		db = db.Where("initiator_id = ?", initiatorID)
	}
	if p.Keyword != "" {
		db = query.Search(db, p.Keyword, "title", "flow_key", "business_id")
	}
	db = p.Filter(db, map[string]string{
		"flowKey": "flow_key", "status": "status", "businessId": "business_id",
		"businessType": "business_type", "initiatorId": "initiator_id",
	})
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := p.Order(map[string]string{
		"createdAt": "created_at", "startedAt": "started_at", "status": "status",
	}, "created_at DESC")
	var list []model.FlowInstance
	if err := db.Order(order).Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// InstanceDetail 实例详情，含任务与轨迹。
type InstanceDetail struct {
	Instance *model.FlowInstance `json:"instance"`
	Tasks    []model.FlowTask    `json:"tasks"`
	Records  []model.FlowRecord  `json:"records"`
}

func (s *Service) InstanceDetail(ctx context.Context, id string) (*InstanceDetail, error) {
	var inst model.FlowInstance
	if err := s.db.WithContext(ctx).First(&inst, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("流程实例不存在")
	}
	var tasks []model.FlowTask
	s.db.WithContext(ctx).Where("instance_id = ?", id).Order("created_at, id").Find(&tasks)
	var records []model.FlowRecord
	s.db.WithContext(ctx).Where("instance_id = ?", id).Order("created_at, id").Find(&records)
	return &InstanceDetail{Instance: &inst, Tasks: tasks, Records: records}, nil
}

func (s *Service) CancelInstance(ctx context.Context, id, operator string) error {
	var inst model.FlowInstance
	if err := s.db.WithContext(ctx).First(&inst, "id = ?", id).Error; err != nil {
		return errs.NotFound("流程实例不存在")
	}
	if inst.InitiatorID != operator {
		return errs.Forbidden("只有发起人可以撤销")
	}
	if err := s.engine.Cancel(ctx, id, operator); err != nil {
		return toHTTPError(err)
	}
	return nil
}

func (s *Service) Rollback(ctx context.Context, id, targetNodeID, reason, operator string) error {
	if err := s.engine.Rollback(ctx, id, targetNodeID, reason, operator); err != nil {
		return toHTTPError(err)
	}
	return nil
}

// ---- 任务 ----

func (s *Service) ListTasks(ctx context.Context, p *query.Params, userID, status string) ([]model.FlowTask, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.FlowTask{}).Where("assignee_id = ?", userID)
	if status == "todo" {
		db = db.Where("status = ?", model.TaskPending)
	} else if status == "done" {
		db = db.Where("status IN ?", []string{model.TaskApproved, model.TaskRejected, model.TaskTransferred, model.TaskCancelled})
	} else if status != "" {
		db = db.Where("status = ?", status)
	}
	if p.Keyword != "" {
		db = query.Search(db, p.Keyword, "node_name")
	}
	// status 已在上面按 todo/done/具体值处理，不再作为 filter 重复追加，
	// 否则 status=todo 会被当成字面量值追加成 status='todo'，导致查询为空。
	db = p.Filter(db, map[string]string{"nodeId": "node_id", "instanceId": "instance_id"})
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := p.Order(map[string]string{"createdAt": "created_at", "finishedAt": "finished_at", "status": "status"}, "created_at DESC")
	var list []model.FlowTask
	if err := db.Order(order).Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *Service) GetTask(ctx context.Context, id string) (*model.FlowTask, error) {
	var t model.FlowTask
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("任务不存在")
	}
	return &t, nil
}

// TaskDetail 任务详情，带所属实例摘要，便于审批页展示业务信息。
type TaskDetail struct {
	Task     *model.FlowTask     `json:"task"`
	Instance *model.FlowInstance `json:"instance"`
}

func (s *Service) TaskDetail(ctx context.Context, id string) (*TaskDetail, error) {
	var t model.FlowTask
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("任务不存在")
	}
	var inst model.FlowInstance
	s.db.WithContext(ctx).First(&inst, "id = ?", t.InstanceID)
	return &TaskDetail{Task: &t, Instance: &inst}, nil
}

func (s *Service) HandleTask(ctx context.Context, id, operator string, in TaskInput) error {
	if in.Operator == "" {
		in.Operator = operator
	}
	if err := s.engine.HandleTask(ctx, id, in); err != nil {
		return toHTTPError(err)
	}
	return nil
}

// ---- 委托 ----

func (s *Service) ListDelegates(ctx context.Context, p *query.Params, userID string) ([]model.FlowDelegate, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.FlowDelegate{})
	if userID != "" {
		db = db.Where("from_user_id = ? OR to_user_id = ?", userID, userID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.FlowDelegate
	if err := db.Order("created_at DESC").Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *Service) CreateDelegate(ctx context.Context, d *model.FlowDelegate) error {
	if d.FromUserID == "" || d.ToUserID == "" {
		return errs.Validation("委托人与被委托人不能为空")
	}
	if d.FromUserID == d.ToUserID {
		return errs.Validation("不能委托给自己")
	}
	if d.StartAt.IsZero() {
		d.StartAt = time.Now()
	}
	if d.EndAt.IsZero() {
		return errs.Validation("结束时间不能为空")
	}
	if !d.EndAt.After(d.StartAt) {
		return errs.Validation("结束时间必须晚于开始时间")
	}
	return s.db.WithContext(ctx).Create(d).Error
}

func (s *Service) UpdateDelegate(ctx context.Context, id string, in *model.FlowDelegate) error {
	var old model.FlowDelegate
	if err := s.db.WithContext(ctx).First(&old, "id = ?", id).Error; err != nil {
		return errs.NotFound("委托不存在")
	}
	in.ID = id
	in.CreatedAt = old.CreatedAt
	return s.db.WithContext(ctx).Model(&old).Updates(in).Error
}

func (s *Service) DeleteDelegate(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&model.FlowDelegate{}, "id = ?", id).Error
}

func toHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var he *errs.Error
	if errors.As(err, &he) {
		return he
	}
	switch {
	case errors.Is(err, ErrFlowNotFound), errors.Is(err, ErrInstanceNotFound), errors.Is(err, ErrTaskNotFound), errors.Is(err, ErrNodeNotFound):
		return errs.NotFound(err.Error())
	case errors.Is(err, ErrFlowNotPublished), errors.Is(err, ErrTaskNotPending), errors.Is(err, ErrInstanceDone), errors.Is(err, ErrFlowNoNodes):
		return errs.Conflict(err.Error())
	default:
		return errs.Internal(err.Error())
	}
}

var _ = zap.NewNop
