package registry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/config"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/gov.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&model.GovRevision{}, &model.GovService{}, &model.GovInstance{},
		&model.GovRoute{}, &model.GovRelease{},
		&model.GovConfigNamespace{}, &model.GovConfigItem{},
	); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if err := db.Create(&model.GovRevision{ID: 1, GlobalRev: 1}).Error; err != nil {
		t.Fatalf("初始化版本号失败: %v", err)
	}
	cfg := &config.Config{}
	cfg.App.Version = "1.0.0"
	cfg.Server.Port = 18090
	cfg.Gov.SelfService = "tenon-core"
	gs := govserver.New(db, "test-token", zap.NewNop())
	return NewService(db, zap.NewNop(), cfg, gs), db
}

func mustCreateService(t *testing.T, s *Service, name string) *model.GovService {
	t.Helper()
	svc, err := s.CreateService(context.Background(), ServiceInput{
		Name: name, Owner: "ops", Desc: "测试服务",
		CheckType: "none", CheckInterval: 10, CheckTimeout: 3, CheckFailThreshold: 3,
	})
	if err != nil {
		t.Fatalf("创建服务 %s 失败: %v", name, err)
	}
	return svc
}

// TestCreateServiceValidation 名称必填、唯一；重复名返回冲突而不是 500。
func TestCreateServiceValidation(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	if _, err := s.CreateService(ctx, ServiceInput{Name: "  "}); err == nil {
		t.Fatal("空名称应被拒绝")
	}
	mustCreateService(t, s, "order-svc")
	if _, err := s.CreateService(ctx, ServiceInput{Name: "order-svc"}); err == nil {
		t.Fatal("重复名称应被拒绝")
	} else if !strings.Contains(err.Error(), "已存在") && !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("重复名称的错误信息应说明冲突，实际: %v", err)
	}
}

// TestRegisterAutoCreatesServiceAndDefaults 实例上报时服务不存在应自动建档，
// instanceId 缺省为 host:port。
func TestRegisterAutoCreatesServiceAndDefaults(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	ins, err := s.Register(ctx, RegisterInput{
		Service: "auto-svc", Host: "10.0.0.7", Port: 8080, Version: "1.0.0", Weight: 100,
		Labels: map[string]string{"zone": "a"}, ConfigVer: 3,
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if ins.InstanceID != "10.0.0.7:8080" {
		t.Fatalf("instanceId 应为 host:port，实际 %q", ins.InstanceID)
	}
	if ins.Status != "online" {
		t.Fatalf("新实例应为 online，实际 %q", ins.Status)
	}
	if ins.LastSeen.IsZero() || ins.StartedAt.IsZero() {
		t.Fatal("应记录 lastSeen 与 startedAt")
	}

	var svc model.GovService
	if err := db.Where("name = ?", "auto-svc").First(&svc).Error; err != nil {
		t.Fatalf("服务未自动建档: %v", err)
	}
	if svc.Status != "enabled" || svc.CheckType != "none" {
		t.Fatalf("自动建档的默认值异常: %+v", svc)
	}

	// 缺字段必须拒绝，不能写出半条实例。
	if _, err := s.Register(ctx, RegisterInput{Host: "10.0.0.8", Port: 80}); err == nil {
		t.Fatal("缺服务名应被拒绝")
	}
	if _, err := s.Register(ctx, RegisterInput{Service: "x", Host: "10.0.0.8"}); err == nil {
		t.Fatal("缺端口应被拒绝")
	}
}

// TestRegisterIsUpsert 同一实例重复注册只更新可变字段，不产生第二行。
func TestRegisterIsUpsert(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	first, err := s.Register(ctx, RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 8000, Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	started := first.StartedAt

	second, err := s.Register(ctx, RegisterInput{
		Service: "svc", Host: "10.0.0.1", Port: 8000, Version: "2.0.0",
		Weight: 30, Status: "draining", ConfigVer: 9,
	})
	if err != nil {
		t.Fatalf("重复注册应作为更新: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("重复注册不应新建行: %s != %s", second.ID, first.ID)
	}
	if second.Version != "2.0.0" || second.Weight != 30 || second.Status != "draining" {
		t.Fatalf("可变字段未更新: %+v", second)
	}
	if !second.StartedAt.Equal(started) {
		t.Fatalf("startedAt 不应被覆盖: %v -> %v", started, second.StartedAt)
	}

	var count int64
	db.Model(&model.GovInstance{}).Where("service_id = ?", "svc").Count(&count)
	if count != 1 {
		t.Fatalf("应只有 1 条实例，实际 %d", count)
	}
}

// TestHeartbeat 未注册实例心跳报 NotFound；offline 实例心跳自动恢复 online。
func TestHeartbeat(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	if err := s.Heartbeat(ctx, RegisterInput{Service: "ghost", Host: "1.1.1.1", Port: 80}); err == nil {
		t.Fatal("未注册实例的心跳应报错")
	}

	if _, err := s.Register(ctx, RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 8000}); err != nil {
		t.Fatal(err)
	}
	// 先置为离线，模拟实例重启前被判定失联。
	if err := s.SetStatus(ctx, "10.0.0.1:8000", "offline"); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 8000, ConfigVer: 42}); err != nil {
		t.Fatalf("心跳应成功: %v", err)
	}
	var ins model.GovInstance
	db.Where("instance_id = ?", "10.0.0.1:8000").First(&ins)
	if ins.Status != "online" {
		t.Fatalf("心跳应把 offline 拉回 online，实际 %q", ins.Status)
	}
	if ins.ConfigVersion != 42 {
		t.Fatalf("心跳应更新配置版本，实际 %d", ins.ConfigVersion)
	}
}

// TestSetStatusStateMachine 状态白名单与不存在实例的处理。
func TestSetStatusStateMachine(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	ins, err := s.Register(ctx, RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 8000})
	if err != nil {
		t.Fatal(err)
	}

	for _, st := range []string{"online", "draining", "offline"} {
		if err := s.SetStatus(ctx, ins.ID, st); err != nil {
			t.Fatalf("设置为 %s 失败: %v", st, err)
		}
	}
	if err := s.SetStatus(ctx, ins.ID, "deleted"); err == nil {
		t.Fatal("非法状态应被拒绝")
	}
	if err := s.SetStatus(ctx, "not-exist", "offline"); err == nil {
		t.Fatal("不存在的实例应报 NotFound")
	}
}

// TestDeregister 摘除即置离线，幂等。
func TestDeregister(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	if _, err := s.Register(ctx, RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 8000}); err != nil {
		t.Fatal(err)
	}
	if err := s.Deregister(ctx, "10.0.0.1:8000"); err != nil {
		t.Fatal(err)
	}
	var ins model.GovInstance
	db.Where("instance_id = ?", "10.0.0.1:8000").First(&ins)
	if ins.Status != "offline" {
		t.Fatalf("摘除后应为 offline，实际 %q", ins.Status)
	}
	// 重复摘除不应报错（调度重试的场景）。
	if err := s.Deregister(ctx, "10.0.0.1:8000"); err != nil {
		t.Fatalf("重复摘除应幂等: %v", err)
	}
}

// TestListServicesStats 服务目录统计：实例总数、在线数、版本数。
func TestListServicesStats(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	mustCreateService(t, s, "svc-a")

	for i, ver := range []string{"1.0.0", "1.0.0", "2.0.0"} {
		host := "10.0.0." + string(rune('1'+i))
		if _, err := s.Register(ctx, RegisterInput{Service: "svc-a", Host: host, Port: 8000, Version: ver}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetStatus(ctx, "10.0.0.3:8000", "offline"); err != nil {
		t.Fatal(err)
	}

	views, err := s.ListServices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("应只有 1 个服务，实际 %d", len(views))
	}
	v := views[0]
	if v.Instances != 3 || v.Online != 2 || v.Versions != 2 {
		t.Fatalf("统计异常: total=%d online=%d versions=%d", v.Instances, v.Online, v.Versions)
	}

	// 版本分布只统计在线实例。
	versions, err := s.Versions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, vv := range versions {
		got[vv.Version] = vv.Count
	}
	if got["1.0.0"] != 2 || got["2.0.0"] != 0 {
		t.Fatalf("在线版本分布异常: %v", got)
	}
}

// TestDeleteServiceCascadesAndAllowsRecreate 删除服务应级联删实例；
// 同名服务必须能重新创建（回归：软删除墓碑曾占住唯一索引）。
func TestDeleteServiceCascadesAndAllowsRecreate(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	svc := mustCreateService(t, s, "temp-svc")
	if _, err := s.Register(ctx, RegisterInput{Service: "temp-svc", Host: "10.0.0.1", Port: 8000}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteService(ctx, svc.ID); err != nil {
		t.Fatalf("删除服务失败: %v", err)
	}
	var count int64
	db.Model(&model.GovInstance{}).Where("service_id = ?", "temp-svc").Count(&count)
	if count != 0 {
		t.Fatalf("实例应被级联删除，实际剩 %d 条", count)
	}
	// 物理删除：表里不能留墓碑。
	db.Unscoped().Model(&model.GovService{}).Where("name = ?", "temp-svc").Count(&count)
	if count != 0 {
		t.Fatalf("服务应被物理删除，实际剩 %d 条", count)
	}

	if _, err := s.CreateService(ctx, ServiceInput{Name: "temp-svc"}); err != nil {
		t.Fatalf("同名服务应能重建: %v", err)
	}
	if err := s.DeleteService(ctx, "not-exist"); err == nil {
		t.Fatal("删除不存在的服务应报错")
	}
}

// TestSelfServiceName 控制面自注册名由配置决定，缺省回落 tenon-core。
func TestSelfServiceName(t *testing.T) {
	s, _ := newTestService(t)
	if got := s.selfService(); got != "tenon-core" {
		t.Fatalf("默认自注册名应为 tenon-core，实际 %q", got)
	}
	s.cfg.Gov.SelfService = "tenon-gov"
	if got := s.selfService(); got != "tenon-gov" {
		t.Fatalf("应读取配置，实际 %q", got)
	}
}

// TestProbeAddressIPv6 健康探针地址必须用 JoinHostPort，IPv6 不能拼坏。
func TestProbeAddressIPv6(t *testing.T) {
	s, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	svc := model.GovService{CheckType: "tcp", CheckTimeout: 1}
	ins := model.GovInstance{Host: "::1", Port: 1}
	// 只验证不 panic 且能返回 false（端口 1 无人监听）；地址拼错会立刻返回 false，
	// 但 IPv6 字面量拼接错误会得到 "::1:1" 且解析失败——两种都返回 false，
	// 因此这里直接断言 JoinHostPort 的结果形态。
	if got := probeAddr(ins); got != "[::1]:1" {
		t.Fatalf("IPv6 地址应带方括号，实际 %q", got)
	}
	_ = s.probe(ctx, svc, ins)
}
