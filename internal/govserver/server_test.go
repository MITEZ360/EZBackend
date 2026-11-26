package govserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/gov"
)

const testMasterKey = "govserver-test-key"

func newTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()
	t.Setenv("TENON_MASTER_KEY", testMasterKey)
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
		&model.GovCommand{}, &model.GovCommandTarget{},
	); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if err := db.Create(&model.GovRevision{ID: 1, GlobalRev: 1}).Error; err != nil {
		t.Fatalf("初始化版本号失败: %v", err)
	}
	return New(db, "test-token", zap.NewNop()), db
}

// assertChecksums 每个分区的校验和必须与载荷一致，否则数据面会整体拒收。
func assertChecksums(t *testing.T, snap *gov.Snapshot) {
	t.Helper()
	for key, p := range snap.Parts {
		if p.Checksum == "" {
			t.Fatalf("分区 %s 缺少校验和", key)
		}
		if got := gov.Checksum(p.Payload); got != p.Checksum {
			t.Fatalf("分区 %s 校验和不匹配", key)
		}
		// 分区版本可以小于全局版本：它是该域最后一次发布的编号，
		// 而全局号被任意一个域的发布推高。数据面只信分区号判断"这块要不要换"。
		if p.Rev <= 0 {
			t.Fatalf("分区 %s 缺少版本号", key)
		}
	}
}

// TestBuildEmptySnapshot 从未发布过路由时不下发 route 分区：
// 数据面据此拒绝切换、继续用自己的兜底，避免新部署全站 404。
func TestBuildEmptySnapshot(t *testing.T) {
	s, _ := newTestServer(t)
	snap, err := s.Build(context.Background(), "")
	if err != nil {
		t.Fatalf("空库构建快照失败: %v", err)
	}
	if snap.GlobalRev != 1 {
		t.Fatalf("全局版本应为 1，实际 %d", snap.GlobalRev)
	}
	assertChecksums(t, snap)
	if _, ok := snap.Parts[gov.PartRoute]; ok {
		t.Fatal("未发布过路由时不应下发 route 分区")
	}
	if _, ok := snap.Parts[gov.PartRegistry]; !ok {
		t.Fatal("应有 registry 分区")
	}
	if _, ok := snap.Routes(); ok {
		t.Fatal("没有 route 分区时解码应失败")
	}
}

// TestBuildRegistryFiltersOffline 注册表只下发 enabled 服务下 online 且心跳新鲜的实例。
func TestBuildRegistryFiltersOffline(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	db.Create(&model.GovService{Name: "live", Status: "enabled"})
	db.Create(&model.GovService{Name: "disabled", Status: "disabled"})
	db.Create(&model.GovService{Name: "empty", Status: "enabled"})

	db.Create(&model.GovInstance{ServiceID: "live", InstanceID: "a:1", Host: "10.0.0.1", Port: 8080,
		Status: "online", LastSeen: time.Now(), Version: "1.0.0", Weight: 100, Labels: `{"zone":"a"}`})
	db.Create(&model.GovInstance{ServiceID: "live", InstanceID: "b:2", Host: "10.0.0.2", Port: 8080,
		Status: "offline", LastSeen: time.Now()})
	db.Create(&model.GovInstance{ServiceID: "live", InstanceID: "c:3", Host: "10.0.0.3", Port: 8080,
		Status: "online", LastSeen: time.Now().Add(-10 * time.Minute)}) // 心跳过期
	db.Create(&model.GovInstance{ServiceID: "disabled", InstanceID: "d:4", Host: "10.0.0.4", Port: 8080,
		Status: "online", LastSeen: time.Now()})

	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	reg, ok := snap.Registry()
	if !ok {
		t.Fatal("registry 分区应可解码")
	}
	if _, found := reg.Services["disabled"]; found {
		t.Fatal("停用服务不应下发")
	}
	empty, found := reg.Services["empty"]
	if !found {
		t.Fatal("无实例的服务也应下发，供路由引用时立刻报无可用实例")
	}
	if empty.Instances == nil || len(empty.Instances) != 0 {
		t.Fatalf("无实例服务的实例列表应为空数组，实际 %+v", empty.Instances)
	}

	live := reg.Services["live"]
	if len(live.Instances) != 1 {
		t.Fatalf("只应下发 1 个健康实例，实际 %+v", live.Instances)
	}
	ins := live.Instances[0]
	if ins.ID != "a:1" || ins.Addr != "10.0.0.1:8080" || ins.Version != "1.0.0" {
		t.Fatalf("实例内容异常: %+v", ins)
	}
	if ins.Labels["zone"] != "a" {
		t.Fatalf("labels 应解码为 map，实际 %+v", ins.Labels)
	}
	assertChecksums(t, snap)
}

// TestRouteTableAloneIsNotEnough 规则表里有 published 行但没有发布记录时，
// 依然不下发路由：数据面只认"发布"这个动作，不认表里的中间状态。
func TestRouteTableAloneIsNotEnough(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	db.Create(&model.GovRoute{Name: "live", Prefix: "/api", Service: "svc", Status: "published", Weight: 100})

	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.Parts[gov.PartRoute]; ok {
		t.Fatal("只有规则行、没有发布记录时不应下发 route 分区")
	}
}

// TestPublishEmptyRoutesStillAuthoritative 发布空规则集也要生效：
// 那是运维显式下线的动作，数据面必须拿到空表而不是继续用旧视图。
func TestPublishEmptyRoutesStillAuthoritative(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	empty, _ := json.Marshal(gov.RoutePayload{Rev: 2, Routes: []gov.Route{}})
	db.Create(&model.GovRelease{Domain: "route", Revision: 2, Payload: empty,
		Checksum: gov.Checksum(empty), Stage: "full", Status: "active"})
	db.Model(&model.GovRevision{}).Where("id = 1").Update("global_rev", 2)

	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	rp, ok := snap.Routes()
	if !ok {
		t.Fatal("发布过空规则集时仍应有 route 分区")
	}
	if len(rp.Routes) != 0 {
		t.Fatalf("路由列表应为空，实际 %+v", rp.Routes)
	}
}

// TestReleasePrefersSnapshotOncePublished 一旦发布过，以发布载荷为准（支持灰度与回滚），
// 而不是直接读规则表。
func TestReleasePrefersSnapshotOncePublished(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	db.Create(&model.GovRoute{Name: "table", Prefix: "/from-table", Service: "svc", Status: "published", Weight: 100})

	payload, _ := json.Marshal(gov.RoutePayload{Rev: 2, Routes: []gov.Route{
		{ID: "released", Name: "from-release", Prefix: "/from-release", Service: "svc", Weight: 100},
	}})
	db.Create(&model.GovRelease{Domain: "route", Revision: 2, Payload: payload,
		Checksum: gov.Checksum(payload), Stage: "full", Status: "active"})
	db.Model(&model.GovRevision{}).Where("id = 1").Update("global_rev", 2)

	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	rp, _ := snap.Routes()
	if len(rp.Routes) != 1 || rp.Routes[0].Name != "from-release" {
		t.Fatalf("应使用发布载荷，实际 %+v", rp.Routes)
	}
}

// TestCanaryRouting 灰度名单内用新载荷，名单外继续用上一版；空 instanceID 视为名单外。
func TestCanaryRouting(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()

	oldPayload, _ := json.Marshal(gov.RoutePayload{Rev: 1, Routes: []gov.Route{
		{ID: "old", Name: "old", Prefix: "/api", Service: "svc", Weight: 100},
	}})
	db.Create(&model.GovRelease{Domain: "route", Revision: 1, Payload: oldPayload,
		Checksum: gov.Checksum(oldPayload), Stage: "full", Status: "done"})

	newPayload, _ := json.Marshal(gov.RoutePayload{Rev: 2, Routes: []gov.Route{
		{ID: "new", Name: "new", Prefix: "/api", Service: "svc", Weight: 100},
	}})
	db.Create(&model.GovRelease{Domain: "route", Revision: 2, Payload: newPayload,
		Checksum: gov.Checksum(newPayload), Targets: `["gw-1"]`, Stage: "canary", Status: "active"})
	db.Model(&model.GovRevision{}).Where("id = 1").Update("global_rev", 2)

	for _, tc := range []struct{ instance, want string }{
		{"gw-1", "new"},
		{"gw-2", "old"},
		{"", "old"}, // 未声明身份的数据面按名单外处理，宁可晚升级不可早升级
	} {
		snap, err := s.Build(ctx, tc.instance)
		if err != nil {
			t.Fatalf("instance=%s 构建失败: %v", tc.instance, err)
		}
		rp, _ := snap.Routes()
		if len(rp.Routes) != 1 || rp.Routes[0].Name != tc.want {
			t.Fatalf("instance=%s 应拿到 %s，实际 %+v", tc.instance, tc.want, rp.Routes)
		}
	}
}

// TestCanaryTargetWildcard 通配目标表示全量，避免运维写 ["*"] 却只推给一台。
func TestCanaryTargetWildcard(t *testing.T) {
	for _, targets := range []string{`["*"]`, `["all"]`, `[]`, `null`, ``} {
		if !matchTargets(targets, "any-instance") {
			t.Fatalf("targets=%q 应视为全量", targets)
		}
	}
	if matchTargets(`["gw-1"]`, "gw-2") {
		t.Fatal("名单外实例不应命中")
	}
	if !matchTargets(`["gw-1","gw-2"]`, "gw-2") {
		t.Fatal("名单内实例应命中")
	}
	// 解析失败按全量处理：宁可多推不可漏推。
	if !matchTargets(`{broken`, "x") {
		t.Fatal("损坏的名单应按全量处理")
	}
}

// TestConfigDecryptedOnDelivery 配置在库里与发布记录里都是密文，只有快照里是明文。
func TestConfigDecryptedOnDelivery(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()

	enc, err := gov.Encrypt("top-secret", testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	db.Create(&model.GovConfigNamespace{Namespace: "app", Env: "dev"})
	db.Create(&model.GovConfigItem{Namespace: "app", Key: "db.password", Value: enc, Type: "string", Encrypted: true})
	db.Create(&model.GovConfigItem{Namespace: "app", Key: "flag", Value: "on", Type: "string"})

	// 未发布时走实时兜底，同样要解密。
	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := snap.Config("app")
	if !ok {
		t.Fatal("应下发 config:app 分区")
	}
	if cfg.Items["db.password"].Value != "top-secret" {
		t.Fatalf("下发值应为明文，实际 %q", cfg.Items["db.password"].Value)
	}
	if !cfg.Items["db.password"].Encrypted {
		t.Fatal("加密标记应保留，供客户端判断敏感级别")
	}
	if cfg.Items["flag"].Value != "on" {
		t.Fatalf("明文项异常: %+v", cfg.Items["flag"])
	}
	assertChecksums(t, snap)
}

// TestConfigReleaseDecrypted 走发布记录时也要解密，且解密后重算校验和。
func TestConfigReleaseDecrypted(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	enc, err := gov.Encrypt("released-secret", testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	// 发布载荷保持密文。
	payload, _ := json.Marshal(gov.ConfigPayload{Rev: 2, Namespace: "app", Items: map[string]gov.ConfigItem{
		"db.password": {Value: enc, Type: "string", Encrypted: true},
	}})
	db.Create(&model.GovRelease{Domain: "config:app", Revision: 2, Payload: payload,
		Checksum: gov.Checksum(payload), Stage: "full", Status: "active"})
	db.Create(&model.GovConfigNamespace{Namespace: "app", Env: "dev"})

	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := snap.Config("app")
	if !ok {
		t.Fatal("应下发 config:app 分区")
	}
	if cfg.Items["db.password"].Value != "released-secret" {
		t.Fatalf("发布载荷也应解密后下发，实际 %q", cfg.Items["db.password"].Value)
	}
	assertChecksums(t, snap)
}

// TestBuildCacheInvalidatedByBump 快照缓存 1 秒；Bump 后必须立即看到新数据。
func TestBuildCacheInvalidatedByBump(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	db.Create(&model.GovService{Name: "svc", Status: "enabled"})

	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := snap.Registry()
	if len(reg.Services) != 1 {
		t.Fatalf("初始应有 1 个服务，实际 %d", len(reg.Services))
	}

	db.Create(&model.GovService{Name: "svc2", Status: "enabled"})
	// 缓存未失效时仍是旧快照（这是热路径的预期行为，不是 bug）。
	cached, _ := s.Build(ctx, "")
	if reg, _ := cached.Registry(); len(reg.Services) != 1 {
		t.Fatalf("1 秒内应命中缓存，实际 %d 个服务", len(reg.Services))
	}

	s.Bump()
	fresh, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if reg, _ := fresh.Registry(); len(reg.Services) != 2 {
		t.Fatalf("Bump 后应看到 2 个服务，实际 %d", len(reg.Services))
	}
}

// TestCurrentRev 控制面自报的版本就是快照里的全局版本。
func TestCurrentRev(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	if got := s.CurrentRev(ctx); got != 1 {
		t.Fatalf("初始版本应为 1，实际 %d", got)
	}
	db.Model(&model.GovRevision{}).Where("id = 1").Update("global_rev", 42)
	if got := s.CurrentRev(ctx); got != 42 {
		t.Fatalf("应读到 42，实际 %d", got)
	}
	// 版本行缺失时回落到 0，而不是 panic（迁移前的老库）。
	db.Delete(&model.GovRevision{}, "id = 1")
	if got := s.CurrentRev(ctx); got != 0 {
		t.Fatalf("缺版本行应返回 0，实际 %d", got)
	}
}
