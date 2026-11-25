// Package cluster 提供多节点协同：节点登记与心跳、配置版本同步、运维指令下发。
//
// 设计取舍：节点之间不做 HTTP 直连，全部通过共享数据库协调。这样部署时
// 不需要为节点间通信开放额外端口，也不需要维护内部调用令牌；代价是必须
// 使用 MySQL / PostgreSQL 这类支持多写入的数据库，SQLite 部署会退化为单机。
package cluster

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/configcenter"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/pkg/errs"
)

// 支持的运维指令。
const (
	ActionRestart = "restart"
	ActionReload  = "reload-config"
)

// 节点与指令状态。
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusPending = "pending"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"
	StatusPartial = "partial"
)

// Service 集群协同服务。
type Service struct {
	db     *gorm.DB
	cfg    *config.Config
	log    *zap.Logger
	cfgSvc *configcenter.Service
	// restart 触发本进程原地重启；单测中可为空。
	restart func()

	nodeID    string
	nodeName  string
	host      string
	port      int
	startedAt time.Time

	mu       sync.Mutex
	lastCfg  int64
	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewService 创建集群服务。
func NewService(db *gorm.DB, cfg *config.Config, log *zap.Logger, cfgSvc *configcenter.Service, restart func()) *Service {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "tenon"
	}
	port := cfg.Server.Port
	if addr := strings.TrimSpace(cfg.Cluster.AdvertiseAddr); addr != "" {
		if h, p, ok := strings.Cut(addr, ":"); ok {
			host = h
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				port = n
			}
		} else {
			host = addr
		}
	}
	// 重启后必须保持同一身份，否则节点列表会不断堆积，历史指令也找不到归属。
	id := strings.TrimSpace(cfg.Runtime.InstanceID)
	if id == "" {
		id = fmt.Sprintf("%s:%d", host, port)
	}
	name := strings.TrimSpace(cfg.Cluster.Name)
	if name == "" {
		name = id
	}
	return &Service{
		db: db, cfg: cfg, log: log, cfgSvc: cfgSvc, restart: restart,
		nodeID: id, nodeName: name, host: host, port: port,
		startedAt: time.Now(), stopCh: make(chan struct{}),
	}
}

// NodeID 返回本节点标识。
func (s *Service) NodeID() string { return s.nodeID }

// Enabled 判断当前部署是否支持多节点协同。
func (s *Service) Enabled() bool { return s.cfg.Cluster.Enabled }

// Mode 返回 cluster / standalone，供页面提示。
func (s *Service) Mode() string {
	if !s.cfg.Cluster.Enabled {
		return "disabled"
	}
	if !s.cfg.UseCluster() {
		return "standalone"
	}
	return "cluster"
}

// Start 登记本节点并启动后台循环：心跳、指令领取、配置版本同步。
func (s *Service) Start(ctx context.Context) {
	if !s.cfg.Cluster.Enabled {
		s.log.Info("集群协同已关闭（cluster.enabled=false）")
		return
	}
	if !s.cfg.UseCluster() {
		s.log.Info("当前使用 SQLite，集群协同退化为单机模式")
	}
	if !s.db.Migrator().HasTable(&model.ClusterNode{}) {
		// migration.mode=off 且未执行建表时，后台循环只会反复报错。
		s.log.Warn("集群相关表不存在，已跳过节点登记；请先执行数据库迁移")
		return
	}
	if err := s.heartbeat(ctx); err != nil {
		s.log.Warn("节点登记失败", zap.Error(err))
	}
	s.lastCfg = s.cfgSvc.Version(ctx)

	interval := s.cfg.Cluster.Heartbeat
	if interval <= 0 {
		interval = 15 * time.Second
	}
	s.wg.Add(3)
	go s.loop(interval, s.heartbeat)
	go s.loop(3*time.Second, s.processOnce)
	go s.loop(10*time.Second, s.syncConfig)
	s.log.Info("集群协同已启动", zap.String("node", s.nodeID), zap.String("host", s.host),
		zap.Int("port", s.port), zap.String("mode", s.Mode()))
}

// Close 停止后台循环。
func (s *Service) Close() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *Service) loop(interval time.Duration, fn func(context.Context) error) {
	defer s.wg.Done()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := fn(ctx); err != nil {
				s.log.Debug("集群后台任务失败", zap.Error(err))
			}
			cancel()
		}
	}
}

// heartbeat 登记或刷新本节点。
func (s *Service) heartbeat(ctx context.Context) error {
	now := time.Now()
	rec := model.ClusterNode{
		ID: s.nodeID, Name: s.nodeName, Host: s.host, Port: s.port,
		Version: s.cfg.App.Version, ConfigVersion: s.cfgSvc.Version(ctx),
		Status: StatusOnline, StartedAt: s.startedAt, LastSeen: now,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "host", "port", "version", "config_version", "status", "started_at", "last_seen", "updated_at",
		}),
	}).Create(&rec).Error
}

// syncConfig 检测覆盖表版本变化并热加载，实现配置向所有节点自动下发。
func (s *Service) syncConfig(ctx context.Context) error {
	v := s.cfgSvc.Version(ctx)
	s.mu.Lock()
	last := s.lastCfg
	s.mu.Unlock()
	if v == last {
		return nil
	}
	if _, err := s.cfgSvc.Reload(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.lastCfg = v
	s.mu.Unlock()
	s.log.Info("检测到配置变更，已在本节点热加载", zap.Int64("config_version", v))
	// 同步刷新节点记录的配置版本，便于页面判断哪个节点还没跟上。
	return s.db.WithContext(ctx).Model(&model.ClusterNode{}).Where("id = ?", s.nodeID).
		Update("config_version", v).Error
}

// NodeView 是节点列表的一行。
type NodeView struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Version       string    `json:"version"`
	ConfigVersion int64     `json:"configVersion"`
	Status        string    `json:"status"`
	Online        bool      `json:"online"`
	Self          bool      `json:"self"`
	Leader        bool      `json:"leader"`
	Lagging       bool      `json:"lagging"`
	StartedAt     time.Time `json:"startedAt"`
	LastSeen      time.Time `json:"lastSeen"`
}

// Nodes 返回全部已登记节点及在线状态。
func (s *Service) Nodes(ctx context.Context) ([]NodeView, error) {
	var rows []model.ClusterNode
	if err := s.db.WithContext(ctx).Order("started_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	cfgVersion := s.cfgSvc.Version(ctx)
	offlineAfter := s.cfg.Cluster.OfflineAfter
	if offlineAfter <= 0 {
		offlineAfter = 45 * time.Second
	}
	now := time.Now()
	out := make([]NodeView, 0, len(rows))
	leaderAssigned := false
	for _, r := range rows {
		online := now.Sub(r.LastSeen) <= offlineAfter
		v := NodeView{
			ID: r.ID, Name: r.Name, Host: r.Host, Port: r.Port, Version: r.Version,
			ConfigVersion: r.ConfigVersion, Online: online, Self: r.ID == s.nodeID,
			Lagging:   online && cfgVersion > 0 && r.ConfigVersion < cfgVersion,
			StartedAt: r.StartedAt, LastSeen: r.LastSeen,
		}
		v.Status = StatusOffline
		if online {
			v.Status = StatusOnline
			if !leaderAssigned {
				// 最早启动的在线节点作为协调者，仅用于展示"谁在负责下发"。
				v.Leader = true
				leaderAssigned = true
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Dispatch 创建一次运维指令，由目标节点自行领取执行。
func (s *Service) Dispatch(ctx context.Context, action string, nodeIDs []string, scope, operator string) (*CommandView, error) {
	if action != ActionRestart && action != ActionReload {
		return nil, errs.BadRequest("不支持的运维动作")
	}
	if !s.cfg.Cluster.Enabled {
		return nil, errs.BadRequest("集群协同已关闭，请先在配置中心启用")
	}
	nodes, err := s.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	selected := make([]NodeView, 0, len(nodes))
	switch scope {
	case "all":
		for _, n := range nodes {
			if n.Online {
				selected = append(selected, n)
			}
		}
	case "", "selected":
		scope = "selected"
		want := make(map[string]bool, len(nodeIDs))
		for _, id := range nodeIDs {
			want[id] = true
		}
		for _, n := range nodes {
			if want[n.ID] {
				selected = append(selected, n)
			}
		}
	default:
		return nil, errs.BadRequest("非法的下发范围")
	}
	if len(selected) == 0 {
		return nil, errs.BadRequest("没有可下发的在线节点")
	}

	cmd := model.ClusterCommand{
		Action: action, Scope: scope, Status: StatusPending,
		Total: len(selected), Operator: operator,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&cmd).Error; err != nil {
			return err
		}
		for _, n := range selected {
			t := model.ClusterCommandTarget{
				CommandID: cmd.ID, NodeID: n.ID, NodeName: n.Name, Status: StatusPending,
			}
			if err := tx.Create(&t).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("已下发运维指令", zap.String("action", action),
		zap.Int("nodes", len(selected)), zap.String("operator", operator))
	return s.commandView(ctx, cmd.ID)
}

// processOnce 领取并执行属于本节点的待处理指令。
func (s *Service) processOnce(ctx context.Context) error {
	var targets []model.ClusterCommandTarget
	if err := s.db.WithContext(ctx).
		Where("node_id = ? AND status = ?", s.nodeID, StatusPending).
		Order("created_at ASC").Limit(5).Find(&targets).Error; err != nil {
		return err
	}
	for _, t := range targets {
		var cmd model.ClusterCommand
		if err := s.db.WithContext(ctx).First(&cmd, "id = ?", t.CommandID).Error; err != nil {
			continue
		}
		now := time.Now()
		s.db.WithContext(ctx).Model(&model.ClusterCommandTarget{}).Where("id = ?", t.ID).
			Updates(map[string]any{"status": StatusRunning, "started_at": now})

		msg, execErr := s.execute(ctx, cmd)
		status := StatusSuccess
		if execErr != nil {
			status = StatusFailed
			if msg == "" {
				msg = execErr.Error()
			}
		}
		done := time.Now()
		s.db.WithContext(ctx).Model(&model.ClusterCommandTarget{}).Where("id = ?", t.ID).
			Updates(map[string]any{"status": status, "message": msg, "finished_at": done})
		s.rollup(ctx, cmd.ID)

		if cmd.Action == ActionRestart && execErr == nil {
			// 先落库再重启：进程退出后不会再有回写机会，状态必须提前确定。
			s.log.Info("执行集群重启指令，准备原地重启")
			time.Sleep(500 * time.Millisecond)
			if s.restart != nil {
				s.restart()
			}
			return nil
		}
	}
	return nil
}

// execute 执行单条指令，返回给前端展示的结果说明。
func (s *Service) execute(ctx context.Context, cmd model.ClusterCommand) (string, error) {
	switch cmd.Action {
	case ActionRestart:
		return "节点已接受重启指令", nil
	case ActionReload:
		res, err := s.cfgSvc.Reload(ctx)
		if err != nil {
			return "配置加载失败", err
		}
		return fmt.Sprintf("已重新加载配置，热生效 %d 项", len(res.Applied)), nil
	default:
		return "", fmt.Errorf("未知指令: %s", cmd.Action)
	}
}

// rollup 重算指令汇总状态。
func (s *Service) rollup(ctx context.Context, commandID string) {
	var total, success, failed, running int64
	base := s.db.WithContext(ctx).Model(&model.ClusterCommandTarget{}).Where("command_id = ?", commandID)
	base.Count(&total)
	s.db.WithContext(ctx).Model(&model.ClusterCommandTarget{}).Where("command_id = ? AND status = ?", commandID, StatusSuccess).Count(&success)
	s.db.WithContext(ctx).Model(&model.ClusterCommandTarget{}).Where("command_id = ? AND status = ?", commandID, StatusFailed).Count(&failed)
	s.db.WithContext(ctx).Model(&model.ClusterCommandTarget{}).Where("command_id = ? AND status IN ?", commandID, []string{StatusPending, StatusRunning}).Count(&running)

	status := StatusRunning
	switch {
	case running > 0:
		status = StatusRunning
	case failed == 0:
		status = StatusSuccess
	case success == 0:
		status = StatusFailed
	default:
		status = StatusPartial
	}
	s.db.WithContext(ctx).Model(&model.ClusterCommand{}).Where("id = ?", commandID).
		Updates(map[string]any{"status": status, "total": total, "succeeded": success, "failed": failed})
}

// CommandView 是指令及其节点明细。
type CommandView struct {
	ID        string       `json:"id"`
	Action    string       `json:"action"`
	Scope     string       `json:"scope"`
	Status    string       `json:"status"`
	Total     int          `json:"total"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Operator  string       `json:"operator"`
	CreatedAt time.Time    `json:"createdAt"`
	Targets   []TargetView `json:"targets"`
}

// TargetView 是单个节点的执行明细。
type TargetView struct {
	NodeID     string     `json:"nodeId"`
	NodeName   string     `json:"nodeName"`
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

func (s *Service) commandView(ctx context.Context, id string) (*CommandView, error) {
	var cmd model.ClusterCommand
	if err := s.db.WithContext(ctx).First(&cmd, "id = ?", id).Error; err != nil {
		return nil, err
	}
	var targets []model.ClusterCommandTarget
	if err := s.db.WithContext(ctx).Where("command_id = ?", id).Order("created_at ASC").Find(&targets).Error; err != nil {
		return nil, err
	}
	out := &CommandView{
		ID: cmd.ID, Action: cmd.Action, Scope: cmd.Scope, Status: cmd.Status,
		Total: cmd.Total, Succeeded: cmd.Succeeded, Failed: cmd.Failed,
		Operator: cmd.Operator, CreatedAt: cmd.CreatedAt,
		Targets: make([]TargetView, 0, len(targets)),
	}
	for _, t := range targets {
		out.Targets = append(out.Targets, TargetView{
			NodeID: t.NodeID, NodeName: t.NodeName, Status: t.Status,
			Message: t.Message, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt,
		})
	}
	return out, nil
}

// Commands 返回最近的指令记录。
func (s *Service) Commands(ctx context.Context, limit int) ([]*CommandView, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []model.ClusterCommand
	if err := s.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*CommandView, 0, len(rows))
	for _, r := range rows {
		v, err := s.commandView(ctx, r.ID)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// Cleanup 删除超过保留期的指令记录，避免指令表无限增长。
func (s *Service) Cleanup(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var ids []string
	if err := s.db.WithContext(ctx).Model(&model.ClusterCommand{}).
		Where("created_at < ?", cutoff).Limit(500).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := s.db.WithContext(ctx).Where("command_id IN ?", ids).Delete(&model.ClusterCommandTarget{}).Error; err != nil {
		return 0, err
	}
	res := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&model.ClusterCommand{})
	return res.RowsAffected, res.Error
}
