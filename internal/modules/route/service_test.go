package route

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/gov"
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
	// 路由目标服务必须存在，否则 build 会拒绝。
	if err := db.Create(&model.GovService{Name: "order-svc", Status: "enabled"}).Error; err != nil {
		t.Fatalf("创建目标服务失败: %v", err)
	}
	return NewService(db, zap.NewNop(), govserver.New(db, "test-token", zap.NewNop())), db
}

func baseInput() RouteInput {
	return RouteInput{
		Name: "订单入口", Prefix: "/api/v1/order", Service: "order-svc",
		Priority: 100, Weight: 100, TimeoutMs: 1000, Retries: 1,
	}
}

// TestCreateValidation 前缀、目标服务、超时、重试、方法都必须校验。
func TestCreateValidation(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*RouteInput)
	}{
		{"空前缀", func(in *RouteInput) { in.Prefix = "   " }},
		{"目标服务为空", func(in *RouteInput) { in.Service = "" }},
		{"目标服务不存在", func(in *RouteInput) { in.Service = "ghost-svc" }},
		{"超时为负", func(in *RouteInput) { in.TimeoutMs = -1 }},
		{"超时过大", func(in *RouteInput) { in.TimeoutMs = 60001 }},
		{"重试为负", func(in *RouteInput) { in.Retries = -1 }},
		{"重试过多", func(in *RouteInput) { in.Retries = 4 }},
		{"方法非法", func(in *RouteInput) { in.Methods = []string{"GET", "TRACE"} }},
		{"优先级过大", func(in *RouteInput) { in.Priority = 10001 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			tc.mutate(&in)
			if _, err := s.Create(ctx, in); err == nil {
				t.Fatalf("%s 应被拒绝", tc.name)
			}
		})
	}
}

// TestCreateDefaultsAndNormalize 前缀规范化、优先级/权重/状态缺省值。
func TestCreateDefaultsAndNormalize(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	row, err := s.Create(ctx, RouteInput{
		Prefix: "api/v1/order/", Service: "order-svc",
		Methods: []string{"get", " post "}, Headers: map[string]string{"X-Tenant": "vip"},
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if row.Prefix != "/api/v1/order" {
		t.Fatalf("前缀应规范化为 /api/v1/order，实际 %q", row.Prefix)
	}
	if row.Methods != "GET,POST" {
		t.Fatalf("方法应大写去空格，实际 %q", row.Methods)
	}
	if row.Priority != 100 || row.Weight != 100 {
		t.Fatalf("默认优先级/权重应为 100，实际 %d/%d", row.Priority, row.Weight)
	}
	if row.Status != "draft" {
		t.Fatalf("新规则应为 draft，实际 %q", row.Status)
	}
	if row.Name != "/api/v1/order" {
		t.Fatalf("未命名时应回落到前缀，实际 %q", row.Name)
	}
	if !strings.Contains(row.Headers, "vip") {
		t.Fatalf("Header 条件未保存: %q", row.Headers)
	}
}

// TestUpdateResetsPublishedToDraft 已发布规则被编辑后必须回到草稿。
func TestUpdateResetsPublishedToDraft(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	row, err := s.Create(ctx, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(ctx, PublishInput{Operator: "tester"}); err != nil {
		t.Fatal(err)
	}
	db.Where("id = ?", row.ID).First(row)
	if row.Status != "published" {
		t.Fatalf("发布后应为 published，实际 %q", row.Status)
	}

	in := baseInput()
	in.Prefix = "/api/v1/order/v2"
	updated, err := s.Update(ctx, row.ID, in)
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.Status != "draft" {
		t.Fatalf("编辑后应回到 draft，实际 %q", updated.Status)
	}

	if _, err := s.Update(ctx, "not-exist", in); err == nil {
		t.Fatal("更新不存在的规则应报错")
	}
}

// TestCompilePreview 预览编译按真实匹配顺序返回，且不落库。
func TestCompilePreview(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	out, err := s.Compile(ctx, []RouteInput{
		{Name: "wide", Prefix: "/api", Service: "order-svc"},
		{Name: "narrow", Prefix: "/api/v1/order", Service: "order-svc", Priority: 100},
		{Name: "vip", Prefix: "/api/v1/order", Service: "order-svc", Priority: 200,
			Headers: map[string]string{"X-Tenant": "vip"}},
	})
	if err != nil {
		t.Fatalf("预览编译失败: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("应编译出 3 条，实际 %d", len(out))
	}
	// 编译顺序：优先级降序。
	if out[0].Priority != 200 || out[2].Priority != 100 {
		t.Fatalf("编译顺序应按优先级降序: %+v", out)
	}
	var count int64
	db.Model(&model.GovRoute{}).Count(&count)
	if count != 0 {
		t.Fatalf("预览不应落库，实际 %d 条", count)
	}
}

// TestPublishFlow 发布：版本递增、草稿转已发布、旧 active 收口。
func TestPublishFlow(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	if _, err := s.Publish(ctx, PublishInput{}); err == nil {
		t.Fatal("没有规则时应拒绝发布")
	}

	for _, prefix := range []string{"/api/v1/order", "/api/v1/pay"} {
		in := baseInput()
		in.Prefix = prefix
		if _, err := s.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}

	first, err := s.Publish(ctx, PublishInput{Operator: "alice", Note: "首次"})
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if first.Revision < 2 {
		t.Fatalf("首次发布应分配新版本号，实际 %d", first.Revision)
	}
	if first.Stage != "full" || first.Status != "active" || first.Operator != "alice" {
		t.Fatalf("发布记录字段异常: %+v", first)
	}
	if first.Checksum != gov.Checksum(first.Payload) {
		t.Fatal("校验和与载荷不一致")
	}
	var payload gov.RoutePayload
	if err := json.Unmarshal(first.Payload, &payload); err != nil {
		t.Fatalf("载荷不可解析: %v", err)
	}
	if payload.Rev != first.Revision || len(payload.Routes) != 2 {
		t.Fatalf("载荷内容异常: rev=%d routes=%d", payload.Rev, len(payload.Routes))
	}

	var drafts int64
	db.Model(&model.GovRoute{}).Where("status = ?", "draft").Count(&drafts)
	if drafts != 0 {
		t.Fatalf("发布后不应残留草稿，实际 %d", drafts)
	}

	second, err := s.Publish(ctx, PublishInput{Operator: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision <= first.Revision {
		t.Fatalf("版本号必须单调递增: %d -> %d", first.Revision, second.Revision)
	}
	var old model.GovRelease
	db.Where("id = ?", first.ID).First(&old)
	if old.Status != "done" {
		t.Fatalf("旧发布应收口为 done，实际 %q", old.Status)
	}
}

// TestPublishCanaryAndPromote 灰度发布只对名单生效，提升后变全量。
func TestPublishCanaryAndPromote(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, baseInput()); err != nil {
		t.Fatal(err)
	}

	canary, err := s.Publish(ctx, PublishInput{Targets: []string{"gw-1"}, Note: "灰度"})
	if err != nil {
		t.Fatal(err)
	}
	if canary.Stage != "canary" {
		t.Fatalf("带 targets 应发布为 canary，实际 %q", canary.Stage)
	}
	if !strings.Contains(canary.Targets, "gw-1") {
		t.Fatalf("灰度名单未保存: %q", canary.Targets)
	}

	promoted, err := s.Promote(ctx, canary.ID)
	if err != nil {
		t.Fatalf("提升失败: %v", err)
	}
	if promoted.Stage != "full" || promoted.Status != "active" {
		t.Fatalf("提升后应为 full/active，实际 %+v", promoted)
	}
	if promoted.Targets != "[]" {
		t.Fatalf("提升后应清空灰度名单，实际 %q", promoted.Targets)
	}
	if _, err := s.Promote(ctx, canary.ID); err == nil {
		t.Fatal("对全量发布再提升应报错")
	}
	// 库内只能有一条 active。
	var active int64
	db.Model(&model.GovRelease{}).Where("status = ?", "active").Count(&active)
	if active != 1 {
		t.Fatalf("同一域只能有一条 active，实际 %d", active)
	}
}

// TestRollbackCreatesNewRevision 回滚生成更大的版本号，载荷回到目标版本。
func TestRollbackCreatesNewRevision(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, baseInput()); err != nil {
		t.Fatal(err)
	}
	r1, err := s.Publish(ctx, PublishInput{Note: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	// 改一条规则再发布，形成 v2。
	rows, _ := s.List(ctx)
	in := baseInput()
	in.Prefix = "/api/v1/order/v2"
	if _, err := s.Update(ctx, rows[0].ID, in); err != nil {
		t.Fatal(err)
	}
	r2, err := s.Publish(ctx, PublishInput{Note: "v2"})
	if err != nil {
		t.Fatal(err)
	}

	rolled, err := s.Rollback(ctx, r1.ID, "carol")
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if rolled.Revision <= r2.Revision {
		t.Fatalf("回滚必须分配更大的版本号: %d <= %d", rolled.Revision, r2.Revision)
	}
	if !strings.Contains(rolled.Note, "回滚到 r") {
		t.Fatalf("回滚备注应可追溯，实际 %q", rolled.Note)
	}
	var back, before gov.RoutePayload
	json.Unmarshal(rolled.Payload, &back)
	json.Unmarshal(r1.Payload, &before)
	if len(back.Routes) != len(before.Routes) || back.Routes[0].Prefix != before.Routes[0].Prefix {
		t.Fatalf("回滚载荷应回到 r%d: %+v", r1.Revision, back.Routes)
	}
	if back.Rev != rolled.Revision {
		t.Fatalf("载荷内版本应同步更新: %d != %d", back.Rev, rolled.Revision)
	}
}

// TestPublishConflict 同前缀同方法的两条规则不能同时发布。
func TestPublishConflict(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	// 绕过 Create 的校验直接落两条冲突规则，模拟历史脏数据。
	for _, name := range []string{"a", "b"} {
		if err := db.Create(&model.GovRoute{
			Name: name, Prefix: "/dup", Methods: "GET", Service: "order-svc", Status: "draft", Weight: 100,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Publish(ctx, PublishInput{}); err == nil {
		t.Fatal("冲突规则应拒绝发布")
	} else if !strings.Contains(err.Error(), "重复") {
		t.Fatalf("错误信息应指出重复，实际: %v", err)
	}
}

// TestReleasesOrdering 发布历史按版本倒序。
func TestReleasesOrdering(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, baseInput()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Publish(ctx, PublishInput{Note: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Releases(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("应有 3 条发布，实际 %d", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i].Revision >= list[i-1].Revision {
			t.Fatalf("发布历史应倒序: %d >= %d", list[i].Revision, list[i-1].Revision)
		}
	}
	if list[0].Status != "active" || list[1].Status != "done" {
		t.Fatalf("状态异常: %s/%s", list[0].Status, list[1].Status)
	}
}

// TestDeleteArchivedRoute 删除规则后不再参与编译。
func TestDeleteArchivedRoute(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	row, err := s.Create(ctx, baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, row.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	var count int64
	db.Unscoped().Model(&model.GovRoute{}).Where("id = ?", row.ID).Count(&count)
	if count != 0 {
		t.Fatalf("规则应被物理删除，实际剩 %d", count)
	}
	if _, err := s.Publish(ctx, PublishInput{}); err == nil {
		t.Fatal("没有规则时发布应报错")
	}
}
