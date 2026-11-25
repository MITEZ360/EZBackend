// Package job 基于 robfig/cron 的定时任务调度：支持动态增删、手动触发、执行日志与并发保护。
package job

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/internal/platform/idgen"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/utils"
)

// HandlerFunc 任务处理函数，返回执行摘要。
type HandlerFunc func(ctx context.Context, params map[string]any) (string, error)

// Service 调度服务。
type Service struct {
	db       *gorm.DB
	cache    cache.Cache
	log      *zap.Logger
	cron     *cron.Cron
	mu       sync.Mutex
	entries  map[string]cron.EntryID // jobID -> entry
	running  map[string]bool
	handlers map[string]HandlerFunc
}

// NewService 创建调度服务。
// c 为缓存实现：启用 Redis 时任务通过分布式锁抢占，多实例只会执行一份。
func NewService(db *gorm.DB, c cache.Cache, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	s := &Service{
		db: db, cache: c, log: log,
		cron:     cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger))),
		entries:  map[string]cron.EntryID{},
		running:  map[string]bool{},
		handlers: map[string]HandlerFunc{},
	}
	s.registerBuiltins()
	return s
}

// Register 注册任务处理器。各业务模块在装配时注册自己的任务实现，
// 调度器不关心任务来自哪个模块。
func (s *Service) Register(name string, fn HandlerFunc) { s.handlers[name] = fn }

// Handlers 返回已注册的处理器名称。
func (s *Service) Handlers() []string {
	out := make([]string, 0, len(s.handlers))
	for k := range s.handlers {
		out = append(out, k)
	}
	return out
}

// registerBuiltins 只保留一个示例实现，用于验证管理端的「手动执行」链路。
// 真实清理任务由各自模块注册：清除日志在审计模块、清理会话在审计模块
// （会话表由它维护清理逻辑）、清理消息在消息模块。调度器不认识这些模块。
func (s *Service) registerBuiltins() {
	s.Register("demo.hello", func(ctx context.Context, p map[string]any) (string, error) {
		msg := "Hello from Tenon"
		if v, ok := p["message"].(string); ok && v != "" {
			msg = v
		}
		return fmt.Sprintf("%s at %s", msg, time.Now().Format(time.RFC3339)), nil
	})
}

// Start 加载启用状态的任务并开始调度。
func (s *Service) Start(ctx context.Context) error {
	var list []model.Job
	if err := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled).Find(&list).Error; err != nil {
		return err
	}
	for i := range list {
		s.schedule(&list[i])
	}
	s.cron.Start()
	s.log.Info("定时任务调度器已启动", zap.Int("jobs", len(list)))
	return nil
}

// Stop 停止调度，等待运行中的任务结束。
func (s *Service) Stop() {
	if s.cron != nil {
		ctx := s.cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
			s.log.Warn("定时任务停止超时")
		}
	}
}

func (s *Service) schedule(j *model.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.entries[j.ID]; ok {
		s.cron.Remove(id)
		delete(s.entries, j.ID)
	}
	if j.Status != model.StatusEnabled {
		return
	}
	jobCopy := *j
	id, err := s.cron.AddFunc(j.Cron, func() {
		s.execute(&jobCopy, false)
	})
	if err != nil {
		s.log.Error("注册定时任务失败", zap.String("job", j.Name), zap.String("cron", j.Cron), zap.Error(err))
		return
	}
	s.entries[j.ID] = id
	next := s.cron.Entry(id).Next
	if !next.IsZero() {
		_ = s.db.Model(&model.Job{}).Where("id = ?", j.ID).Update("next_run_at", next).Error
	}
}

// ValidateCron 校验 cron 表达式。
func ValidateCron(expr string) error {
	_, err := cron.ParseStandard(expr)
	if err != nil {
		return errs.BadRequest("cron 表达式不合法，需为标准 5 段格式（分 时 日 月 周），如 */5 * * * *")
	}
	return nil
}

// List 任务分页。
func (s *Service) List(ctx context.Context, p *query.Params) ([]*model.Job, int64, error) {
	allowed := map[string]string{"name": "name", "handler": "handler", "status": "status", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.Job{}), allowed)
	db = query.Search(db, p.Keyword, "name", "handler", "remark")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.Job
	err := db.Order(p.Order(map[string]string{"created_at": "created_at"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// Input 任务入参。
type Input struct {
	Name    string `json:"name" binding:"required,max=64" label:"任务名称"`
	Cron    string `json:"cron" binding:"required,max=64" label:"cron 表达式"`
	Handler string `json:"handler" binding:"required,max=64" label:"执行器"`
	Params  string `json:"params" binding:"max=512" label:"参数"`
	Status  *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Remark  string `json:"remark" binding:"max=255" label:"备注"`
}

// Create 新增任务。
func (s *Service) Create(ctx context.Context, in *Input) (*model.Job, error) {
	if err := ValidateCron(in.Cron); err != nil {
		return nil, err
	}
	if _, ok := s.handlers[in.Handler]; !ok {
		return nil, errs.BadRequest(i18n.Tf(ctx, "执行器不存在：{0}", in.Handler))
	}
	if in.Params != "" && !json.Valid([]byte(in.Params)) {
		return nil, errs.BadRequest("参数必须是合法 JSON")
	}
	j := &model.Job{Name: in.Name, Cron: in.Cron, Handler: in.Handler,
		Params: in.Params, Remark: in.Remark,
		Status: utils.Int8Or(in.Status, model.StatusEnabled)}
	if err := s.db.WithContext(ctx).Select("*").Create(j).Error; err != nil {
		return nil, err
	}
	s.schedule(j)
	return j, nil
}

// Update 更新任务。
func (s *Service) Update(ctx context.Context, id string, in *Input) error {
	if err := ValidateCron(in.Cron); err != nil {
		return err
	}
	if _, ok := s.handlers[in.Handler]; !ok {
		return errs.BadRequest(i18n.Tf(ctx, "执行器不存在：{0}", in.Handler))
	}
	var j model.Job
	if err := s.db.WithContext(ctx).First(&j, "id = ?", id).Error; err != nil {
		return errs.NotFound("任务不存在")
	}
	if err := s.db.WithContext(ctx).Model(&model.Job{}).Where("id = ?", id).Updates(map[string]any{
		"name": in.Name, "cron": in.Cron, "handler": in.Handler,
		"params": in.Params, "status": utils.Int8Or(in.Status, model.StatusEnabled),
		"remark": in.Remark,
	}).Error; err != nil {
		return err
	}
	j.Name, j.Cron, j.Handler, j.Params, j.Remark = in.Name, in.Cron, in.Handler, in.Params, in.Remark
	j.Status = utils.Int8Or(in.Status, model.StatusEnabled)
	s.schedule(&j)
	return nil
}

// Delete 删除任务。
func (s *Service) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	if eid, ok := s.entries[id]; ok {
		s.cron.Remove(eid)
		delete(s.entries, id)
	}
	s.mu.Unlock()
	res := s.db.WithContext(ctx).Delete(&model.Job{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("任务不存在")
	}
	return nil
}

// SetStatus 启停任务。
func (s *Service) SetStatus(ctx context.Context, id string, status int8) error {
	if status != 0 && status != 1 {
		return errs.BadRequest("状态值非法")
	}
	var j model.Job
	if err := s.db.WithContext(ctx).First(&j, "id = ?", id).Error; err != nil {
		return errs.NotFound("任务不存在")
	}
	if err := s.db.WithContext(ctx).Model(&model.Job{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		return err
	}
	j.Status = status
	s.schedule(&j)
	return nil
}

// RunNow 立即执行一次。
func (s *Service) RunNow(ctx context.Context, id string) (*model.JobLog, error) {
	var j model.Job
	if err := s.db.WithContext(ctx).First(&j, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("任务不存在")
	}
	return s.execute(&j, true), nil
}

func (s *Service) execute(j *model.Job, manual bool) *model.JobLog {
	// 跨实例互斥：抢不到锁说明别的实例正在跑同一个任务。
	// 锁 TTL 覆盖单次执行上限（10 分钟）并留出余量，实例宕机后自动释放。
	if s.cache != nil {
		release, ok := cache.Lock(context.Background(), s.cache, "job:run:"+j.ID, 30*time.Minute)
		if !ok {
			s.log.Info("任务已在其它实例执行，本次跳过", zap.String("job", j.Name))
			return &model.JobLog{ID: idgen.Next(), JobID: j.ID, JobName: j.Name, Status: 0,
				Output: "任务已在其它实例执行中，本次跳过", StartedAt: time.Now()}
		}
		defer release()
	}

	s.mu.Lock()
	if s.running[j.ID] {
		s.mu.Unlock()
		out := "任务正在执行中，本次跳过"
		return &model.JobLog{ID: idgen.Next(), JobID: j.ID, JobName: j.Name, Status: 0,
			Output: out, StartedAt: time.Now()}
	}
	s.running[j.ID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, j.ID)
		s.mu.Unlock()
	}()

	start := time.Now()
	handler, ok := s.handlers[j.Handler]
	var output string
	var err error
	if !ok {
		err = fmt.Errorf("执行器不存在: %s", j.Handler)
		output = err.Error()
	} else {
		var params map[string]any
		if j.Params != "" {
			_ = json.Unmarshal([]byte(j.Params), &params)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		output, err = handler(ctx, params)
	}
	status := int8(1)
	if err != nil {
		status = 0
		output = err.Error()
	}
	entry := &model.JobLog{
		ID: idgen.Next(), JobID: j.ID, JobName: j.Name, Status: status,
		Output: truncate(output, 4000), CostMS: time.Since(start).Milliseconds(), StartedAt: start,
	}
	if e := s.db.Create(entry).Error; e != nil {
		s.log.Warn("写入任务日志失败", zap.Error(e))
	}
	updates := map[string]any{
		"last_run_at": start, "run_count": gorm.Expr("run_count + 1"),
		"last_result": map[bool]string{true: "success", false: "failed"}[err == nil],
	}
	if err != nil {
		updates["fail_count"] = gorm.Expr("fail_count + 1")
	}
	if !manual {
		s.mu.Lock()
		if eid, ok := s.entries[j.ID]; ok {
			if next := s.cron.Entry(eid).Next; !next.IsZero() {
				updates["next_run_at"] = next
			}
		}
		s.mu.Unlock()
	}
	s.db.Model(&model.Job{}).Where("id = ?", j.ID).Updates(updates)
	s.log.Info("定时任务执行完成",
		zap.String("job", j.Name), zap.Bool("success", err == nil),
		zap.Int64("cost_ms", entry.CostMS))
	return entry
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(截断)"
}

// Logs 任务日志分页。
func (s *Service) Logs(ctx context.Context, jobID string, p *query.Params) ([]*model.JobLog, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.JobLog{})
	if jobID != "" {
		db = db.Where("job_id = ?", jobID)
	}
	if p.Filters["status"] != "" {
		db = db.Where("status = ?", p.Filters["status"])
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.JobLog
	err := db.Order("started_at DESC").Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}
