package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/pkg/errs"
)

// Service 是服务注册与发现的领域实现。
type Service struct {
	db       *gorm.DB
	log      *zap.Logger
	cfg      *config.Config
	gov      *govserver.Server
	stop     chan struct{}
	stopOnce sync.Once

	// 健康检查的连续失败计数只放内存：探测是高频的，写库没有意义，
	// 进程重启后重新计数即可。
	mu       sync.Mutex
	failures map[string]int
}

// NewService 创建注册中心服务。
func NewService(db *gorm.DB, log *zap.Logger, cfg *config.Config, gs *govserver.Server) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{
		db: db, log: log, cfg: cfg, gov: gs,
		stop:     make(chan struct{}),
		failures: map[string]int{},
	}
}

// ServiceView 是服务目录的展示结构。
type ServiceView struct {
	model.GovService
	Instances int `json:"instances"`
	Online    int `json:"online"`
	Versions  int `json:"versions"`
}

// ListServices 返回服务目录与实例统计。
func (s *Service) ListServices(ctx context.Context) ([]ServiceView, error) {
	var rows []model.GovService
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	type stat struct {
		ServiceID string
		Total     int
		Online    int
		Versions  int
	}
	var stats []stat
	s.db.WithContext(ctx).Model(&model.GovInstance{}).
		Select("service_id, count(*) as total, sum(case when status = 'online' then 1 else 0 end) as online, count(distinct version) as versions").
		Group("service_id").Scan(&stats)
	index := make(map[string]stat, len(stats))
	for _, st := range stats {
		index[st.ServiceID] = st
	}
	out := make([]ServiceView, 0, len(rows))
	for _, r := range rows {
		st := index[r.Name]
		out = append(out, ServiceView{GovService: r, Instances: st.Total, Online: st.Online, Versions: st.Versions})
	}
	return out, nil
}

// ServiceInput 是创建/更新服务的入参。
type ServiceInput struct {
	Name               string `json:"name"`
	Owner              string `json:"owner"`
	Group              string `json:"group"`
	Desc               string `json:"desc"`
	Meta               string `json:"meta"`
	Status             string `json:"status"`
	CheckType          string `json:"checkType"`
	CheckPath          string `json:"checkPath"`
	CheckInterval      int    `json:"checkInterval"`
	CheckTimeout       int    `json:"checkTimeout"`
	CheckFailThreshold int    `json:"checkFailThreshold"`
}

// CreateService 新建服务；名称必须唯一。
func (s *Service) CreateService(ctx context.Context, in ServiceInput) (*model.GovService, error) {
	name := strings.TrimSpace(in.Name)
	if err := validateServiceName(name); err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.GovService{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, errs.Conflict("服务已存在: " + name)
	}
	row := model.GovService{
		Name: name, Owner: in.Owner, Group: in.Group, Desc: in.Desc, Meta: in.Meta,
		Status:    orDefault(in.Status, "enabled"),
		CheckType: orDefault(in.CheckType, "none"), CheckPath: in.CheckPath,
		CheckInterval: orDefaultInt(in.CheckInterval, 10), CheckTimeout: orDefaultInt(in.CheckTimeout, 3),
		CheckFailThreshold: orDefaultInt(in.CheckFailThreshold, 3),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// 上面的 Count 与这里的 Create 之间存在窗口：并发同名创建时，唯一索引
		// 会挡住后到的那个。这不是服务端故障，回查一次确认是重名就报冲突，
		// 否则调用方会看到一个莫名其妙的 500。
		var n int64
		if e := s.db.WithContext(ctx).Model(&model.GovService{}).Where("name = ?", name).Count(&n).Error; e == nil && n > 0 {
			return nil, errs.Conflict("服务已存在: " + name)
		}
		return nil, err
	}
	s.bump()
	return &row, nil
}

// UpdateService 更新服务元数据与检查策略。
func (s *Service) UpdateService(ctx context.Context, id string, in ServiceInput) (*model.GovService, error) {
	var row model.GovService
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("服务不存在")
		}
		return nil, err
	}
	updates := map[string]any{
		"owner": in.Owner, "group": in.Group, "desc": in.Desc, "meta": in.Meta,
		"status":     orDefault(in.Status, row.Status),
		"check_type": orDefault(in.CheckType, row.CheckType), "check_path": in.CheckPath,
		"check_interval":       orDefaultInt(in.CheckInterval, row.CheckInterval),
		"check_timeout":        orDefaultInt(in.CheckTimeout, row.CheckTimeout),
		"check_fail_threshold": orDefaultInt(in.CheckFailThreshold, row.CheckFailThreshold),
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	s.db.WithContext(ctx).Where("id = ?", id).First(&row)
	s.bump()
	return &row, nil
}

// DeleteService 删除服务及其实例登记。
//
// 物理删除：name 与 (service_id, instance_id) 都是唯一索引，软删的墓碑
// 会让同名服务再也建不回来。删除动作已进审计日志。
func (s *Service) DeleteService(ctx context.Context, id string) error {
	var row model.GovService
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.NotFound("服务不存在")
		}
		return err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("service_id = ?", row.Name).Delete(&model.GovInstance{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&row).Error
	})
	if err == nil {
		s.bump()
	}
	return err
}

// ListInstances 返回实例列表，可按服务与状态过滤。
func (s *Service) ListInstances(ctx context.Context, service, status string) ([]model.GovInstance, error) {
	q := s.db.WithContext(ctx).Model(&model.GovInstance{})
	if service != "" {
		q = q.Where("service_id = ?", service)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []model.GovInstance
	if err := q.Order("service_id ASC, instance_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// RegisterInput 是实例注册/心跳的入参。
type RegisterInput struct {
	Service   string            `json:"service"`
	Instance  string            `json:"instanceId"`
	Name      string            `json:"name"`
	Host      string            `json:"host"`
	Port      int               `json:"port"`
	Version   string            `json:"version"`
	Zone      string            `json:"zone"`
	Weight    int               `json:"weight"`
	Labels    map[string]string `json:"labels"`
	Status    string            `json:"status"`
	ConfigVer int64             `json:"configVersion"`
}

// Register 注册或续约一个实例。
//
// 服务不存在时自动建档：接入方只需要上报自己的名字，不应先来后台点一遍。
func (s *Service) Register(ctx context.Context, in RegisterInput) (*model.GovInstance, error) {
	service := strings.TrimSpace(in.Service)
	host := strings.TrimSpace(in.Host)
	if err := validateServiceName(service); err != nil {
		return nil, err
	}
	if err := validateHost(host); err != nil {
		return nil, err
	}
	// 端口必须落在有效区间：0 或 70000 这类值能存进库，还会被原样下发给网关，
	// 每次转发都拨号失败，而服务目录里看着一切正常。
	if in.Port < 1 || in.Port > 65535 {
		return nil, errs.Validation(fmt.Sprintf("端口非法: %d（应在 1~65535）", in.Port))
	}
	if in.Weight < 0 || in.Weight > 10000 {
		return nil, errs.Validation("权重必须在 0~10000 之间")
	}
	instanceID := strings.TrimSpace(in.Instance)
	if instanceID == "" {
		instanceID = fmt.Sprintf("%s:%d", host, in.Port)
	}
	if len(instanceID) > 128 {
		return nil, errs.Validation("实例标识过长（上限 128 字符）")
	}
	var svc model.GovService
	err := s.db.WithContext(ctx).Where("name = ?", service).First(&svc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		svc = model.GovService{Name: service, Status: "enabled", CheckType: "none",
			CheckInterval: 10, CheckTimeout: 3, CheckFailThreshold: 3}
		if err := s.db.WithContext(ctx).Create(&svc).Error; err != nil {
			// 并发首次注册：另一个 goroutine 抢先建档，唯一索引拒绝本次插入。
			// 这不是失败——服务已经存在，继续注册实例即可。若不回查，扩容时
			// 会有一部分实例白白注册失败。
			var existing model.GovService
			if e := s.db.WithContext(ctx).Where("name = ?", service).First(&existing).Error; e != nil {
				return nil, err
			}
		}
	} else if err != nil {
		return nil, err
	}
	labels := "{}"
	if len(in.Labels) > 0 {
		if b, err := json.Marshal(in.Labels); err == nil {
			labels = string(b)
		}
	}
	status := orDefault(in.Status, "online")
	row := model.GovInstance{
		ServiceID: service, InstanceID: instanceID, Name: in.Name,
		Host: host, Port: in.Port, Version: in.Version, Zone: in.Zone,
		Weight: orDefaultInt(in.Weight, 100), Labels: labels, Status: status,
		ConfigVersion: in.ConfigVer, LastSeen: time.Now(), StartedAt: time.Now(),
	}
	// upsert：新实例插入，老实例只更新可变字段，不覆盖创建时间与主键。
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "service_id"}, {Name: "instance_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "host", "port", "version", "zone", "weight", "labels", "status", "last_seen", "config_version", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return nil, err
	}
	// 回读用独立变量：row 已经被 Create 填上本次生成的 ID，若直接 First(&row)，
	// GORM 会把这个非零主键当作查询条件，upsert 命中已有行时反而查不到，
	// 调用方就会拿到一个库里并不存在的 ID。
	var saved model.GovInstance
	if err := s.db.WithContext(ctx).
		Where("service_id = ? AND instance_id = ?", service, instanceID).
		First(&saved).Error; err != nil {
		return nil, err
	}
	s.bump()
	return &saved, nil
}

// Heartbeat 只续约 last_seen，并在实例从 offline 恢复时改回 online。
func (s *Service) Heartbeat(ctx context.Context, in RegisterInput) error {
	instanceID := strings.TrimSpace(in.Instance)
	if instanceID == "" {
		in.Instance = fmt.Sprintf("%s:%d", in.Host, in.Port)
		instanceID = in.Instance
	}
	q := s.db.WithContext(ctx).Model(&model.GovInstance{}).Where("instance_id = ?", instanceID)
	if in.Service != "" {
		q = q.Where("service_id = ?", in.Service)
	}
	res := q.Updates(map[string]any{
		"last_seen":      time.Now(),
		"config_version": in.ConfigVer,
		"status":         gorm.Expr("CASE WHEN status = 'offline' THEN 'online' ELSE status END"),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("实例未注册")
	}
	s.mu.Lock()
	delete(s.failures, instanceID)
	s.mu.Unlock()
	return nil
}

// Deregister 把实例置为离线。不限定服务，兼容只传 instanceId 的旧调用。
func (s *Service) Deregister(ctx context.Context, instanceID string) error {
	return s.DeregisterScoped(ctx, "", instanceID)
}

// DeregisterScoped 按服务 + 实例注销；service 为空时不限定服务。
//
// instanceId 缺省是 host:port，两个服务监听同一地址（sidecar、共享端口）
// 时会撞号。不按服务限定时，注销一个会把另一个也摘掉，属于静默误伤。
func (s *Service) DeregisterScoped(ctx context.Context, service, instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return errs.Validation("缺少实例标识")
	}
	q := s.db.WithContext(ctx).Model(&model.GovInstance{}).Where("instance_id = ?", instanceID)
	if service = strings.TrimSpace(service); service != "" {
		q = q.Where("service_id = ?", service)
	}
	res := q.Update("status", "offline")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("实例不存在")
	}
	s.bump()
	return nil
}

// SetStatus 修改实例状态（online / draining / offline）。
func (s *Service) SetStatus(ctx context.Context, id, status string) error {
	switch status {
	case "online", "draining", "offline":
	default:
		return errs.Validation("状态只能是 online / draining / offline")
	}
	res := s.db.WithContext(ctx).Model(&model.GovInstance{}).Where("id = ? OR instance_id = ?", id, id).
		Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("实例不存在")
	}
	s.bump()
	return nil
}

// VersionView 是版本分布的一项。
type VersionView struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Count   int    `json:"count"`
}

// Versions 返回在线实例的版本分布，用于灰度期间观察占比。
func (s *Service) Versions(ctx context.Context) ([]VersionView, error) {
	var rows []VersionView
	err := s.db.WithContext(ctx).Model(&model.GovInstance{}).
		Select("service_id as service, version, count(*) as count").
		Where("status = ?", "online").Group("service_id, version").
		Order("service_id ASC, count DESC").Scan(&rows).Error
	return rows, err
}

// Start 启动心跳与健康检查循环。
func (s *Service) Start(ctx context.Context) {
	s.syncSelf(ctx)
	go s.loop(ctx, 30*time.Second, s.heartbeatSelf)
	go s.loop(ctx, 10*time.Second, s.checkHealth)
}

// Close 停止后台循环。
func (s *Service) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func (s *Service) loop(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}

// syncSelf 把当前进程登记为控制面实例，单实例部署也能看到自己。
//
// 服务名取自 gov.self_service：独立部署治理面时它与内核不是同一个服务，
// 硬编码 tenon-core 会让服务目录出现一个名不副实的条目。
func (s *Service) syncSelf(ctx context.Context) {
	host := s.advertiseHost()
	port := s.cfg.Server.Port
	if port == 0 {
		return
	}
	_, err := s.Register(ctx, RegisterInput{
		Service: s.selfService(), Host: host, Port: port,
		Version: s.cfg.App.Version, Zone: s.cfg.Cluster.Name,
		Weight: 100, Labels: map[string]string{"role": "core"},
		ConfigVer: s.selfRev(ctx),
	})
	if err != nil {
		s.log.Debug("登记本实例失败", zap.Error(err))
	}
}

func (s *Service) selfService() string {
	if name := strings.TrimSpace(s.cfg.Gov.SelfService); name != "" {
		return name
	}
	return "tenon-core"
}

// selfRev 取当前全局版本。控制面本身就是版本源头，让它显示成"落后"
// 只会制造噪音，所以直接回报最新版本。
func (s *Service) selfRev(ctx context.Context) int64 {
	if s.gov == nil {
		return 0
	}
	return s.gov.CurrentRev(ctx)
}

func (s *Service) heartbeatSelf(ctx context.Context) {
	in := RegisterInput{
		Service: s.selfService(), Host: s.advertiseHost(), Port: s.cfg.Server.Port,
		ConfigVer: s.selfRev(ctx),
	}
	_ = s.Heartbeat(ctx, in)
}

func (s *Service) advertiseHost() string {
	if h := strings.TrimSpace(s.cfg.Cluster.AdvertiseAddr); h != "" {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return h
	}
	return "127.0.0.1"
}

// checkHealth 对声明了检查策略的服务做主动探测。
//
// 探测集中在控制面做，数据面只消费结果：如果每个网关都去探一遍，100 个实例
// 会放大成 100×M 次请求，得不偿失。
func (s *Service) checkHealth(ctx context.Context) {
	var services []model.GovService
	if err := s.db.WithContext(ctx).Where("status = ? AND check_type <> ?", "enabled", "none").
		Find(&services).Error; err != nil {
		return
	}
	for _, svc := range services {
		var instances []model.GovInstance
		if err := s.db.WithContext(ctx).Where("service_id = ? AND status <> ?", svc.Name, "draining").
			Find(&instances).Error; err != nil {
			continue
		}
		for _, ins := range instances {
			ok := s.probe(ctx, svc, ins)
			key := ins.ServiceID + "/" + ins.InstanceID
			if ok {
				s.mu.Lock()
				delete(s.failures, key)
				s.mu.Unlock()
				if ins.Status == "offline" {
					s.db.WithContext(ctx).Model(&model.GovInstance{}).Where("id = ?", ins.ID).Update("status", "online")
					s.bump()
				}
				continue
			}
			s.mu.Lock()
			s.failures[key]++
			n := s.failures[key]
			s.mu.Unlock()
			if n >= svc.CheckFailThreshold && ins.Status != "offline" {
				s.db.WithContext(ctx).Model(&model.GovInstance{}).Where("id = ?", ins.ID).Update("status", "offline")
				s.log.Warn("实例健康检查连续失败，已摘除",
					zap.String("service", svc.Name), zap.String("instance", ins.InstanceID), zap.Int("failures", n))
				s.bump()
			}
		}
	}
}

// probeAddr 拼接探测目标地址。IPv6 字面量必须带方括号，因此不能用 "%s:%d"。
func probeAddr(ins model.GovInstance) string {
	return net.JoinHostPort(ins.Host, strconv.Itoa(ins.Port))
}

func (s *Service) probe(ctx context.Context, svc model.GovService, ins model.GovInstance) bool {
	timeout := time.Duration(orDefaultInt(svc.CheckTimeout, 3)) * time.Second
	// 用 JoinHostPort 而不是 "%s:%d"：IPv6 地址要加方括号，直拼会得到
	// 2001:db8::1:8080 这种无法拨号的字符串。
	addr := probeAddr(ins)
	switch svc.CheckType {
	case "http":
		path := svc.CheckPath
		if path == "" {
			path = "/healthz"
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, "http://"+addr+path, nil)
		if err != nil {
			return false
		}
		resp, err := (&http.Client{Timeout: timeout}).Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode < 500
	case "tcp":
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	default:
		return true
	}
}

func (s *Service) bump() {
	if s.gov != nil {
		s.gov.Bump()
	}
}

// serviceNameRe 服务名只能是标识符形态。
//
// 服务名同时是路由目标（route.service）与实例登记的外键，允许空格、斜杠
// 会在路由前缀、日志、URL 里到处需要转义；限制成标识符是最省事的做法。
// 中划线、下划线、点都放行，覆盖 k8s / 传统命名习惯。
var serviceNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const maxServiceNameLen = 64

func validateServiceName(name string) error {
	if name == "" {
		return errs.Validation("服务名不能为空")
	}
	if len(name) > maxServiceNameLen {
		return errs.Validation(fmt.Sprintf("服务名过长（上限 %d 字符）", maxServiceNameLen))
	}
	if !serviceNameRe.MatchString(name) {
		return errs.Validation("服务名只能包含字母、数字、中划线、下划线与点，且需以字母或数字开头")
	}
	return nil
}

// validateHost 主机名或 IP。不允许空白与斜杠：它们拼进 addr 后无法拨号，
// 只会在转发时变成一连串 connect 失败。
func validateHost(host string) error {
	if host == "" {
		return errs.Validation("主机不能为空")
	}
	if len(host) > 255 {
		return errs.Validation("主机名过长（上限 255 字符）")
	}
	if strings.ContainsAny(host, " \t\r\n/\\") {
		return errs.Validation("主机不能包含空白或斜杠: " + host)
	}
	return nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func orDefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
