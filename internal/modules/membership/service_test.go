package membership

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/pkg/query"
)

func int8p(v int8) *int8 { return &v }

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "membership.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.MembershipLevel{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return NewService(db, zap.NewNop()), db
}

func createLevel(t *testing.T, svc *Service, code string, days int) *model.MembershipLevel {
	t.Helper()
	lv, err := svc.Create(context.Background(), &LevelInput{
		Code: code, Name: code, Rank: 10, DefaultDays: days,
		Benefits: []model.Benefit{
			{Key: "report.export", Label: "导出报表", Value: "true"},
			{Key: "project.max", Label: "项目数量", Value: "5"},
			{Key: "support.priority", Label: "优先支持", Value: "false"},
		},
	})
	if err != nil {
		t.Fatalf("创建等级失败: %v", err)
	}
	return lv
}

func createUser(t *testing.T, db *gorm.DB, name string) *model.User {
	t.Helper()
	u := &model.User{Username: name, Nickname: name, Status: model.StatusEnabled}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

func TestEvaluateStates(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	active := &model.MembershipLevel{Status: model.StatusEnabled}
	active.ID = "lv1"
	disabled := &model.MembershipLevel{Status: model.StatusDisabled}
	disabled.ID = "lv2"
	future := now.Add(72 * time.Hour)
	past := now.Add(-time.Hour)

	cases := []struct {
		name    string
		level   *model.MembershipLevel
		levelID string
		expire  *time.Time
		want    string
		member  bool
		days    int
	}{
		{"从未开通", nil, "", nil, model.MembershipNone, false, -1},
		{"长期有效", active, "lv1", nil, model.MembershipActive, true, -1},
		{"有效期内", active, "lv1", &future, model.MembershipActive, true, 3},
		{"已过期", active, "lv1", &past, model.MembershipExpired, false, -1},
		{"等级停用", disabled, "lv2", &future, model.MembershipDisabled, false, -1},
		{"等级被删除", nil, "lv3", nil, model.MembershipDisabled, false, -1},
	}
	for _, c := range cases {
		got := model.Evaluate(c.level, c.levelID, nil, c.expire, now)
		if got.State != c.want || got.Member != c.member || got.DaysLeft != c.days {
			t.Fatalf("%s: got state=%s member=%v days=%d, want state=%s member=%v days=%d",
				c.name, got.State, got.Member, got.DaysLeft, c.want, c.member, c.days)
		}
	}
}

func TestGrantRenewAndRevoke(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	lv := createLevel(t, svc, "gold", 30)
	u := createUser(t, db, "alice")

	first, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, Days: 30})
	if err != nil {
		t.Fatalf("开通失败: %v", err)
	}
	if !first.Member || first.DaysLeft != 30 {
		t.Fatalf("开通后状态异常: %+v", first)
	}

	// 同等级续费 10 天：应在原到期日上顺延，而不是从今天重算。
	second, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, Days: 10})
	if err != nil {
		t.Fatalf("续费失败: %v", err)
	}
	if second.DaysLeft != 40 {
		t.Fatalf("续费未顺延，剩余 %d 天（期望 40）", second.DaysLeft)
	}

	// 改为长期有效。
	forever, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, Permanent: true})
	if err != nil {
		t.Fatalf("设为长期失败: %v", err)
	}
	if forever.DaysLeft != -1 || forever.ExpireAt != nil {
		t.Fatalf("长期会员仍带到期时间: %+v", forever)
	}

	if err := svc.Revoke(ctx, u.ID); err != nil {
		t.Fatalf("取消会员失败: %v", err)
	}
	after, err := svc.OfUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if after.Member || after.State != model.MembershipNone || after.LevelID != "" {
		t.Fatalf("取消后仍为会员: %+v", after)
	}
}

func TestGrantDefaultsAndValidation(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	lv := createLevel(t, svc, "silver", 15)
	u := createUser(t, db, "bob")

	// 不传天数时使用等级默认时长。
	snap, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID})
	if err != nil {
		t.Fatalf("开通失败: %v", err)
	}
	if snap.DaysLeft != 15 {
		t.Fatalf("未使用等级默认时长: %d", snap.DaysLeft)
	}

	if _, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, ExpireAt: "2020-01-01"}); err == nil {
		t.Fatal("过去时间应被拒绝")
	}
	if _, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, ExpireAt: "not-a-date"}); err == nil {
		t.Fatal("非法时间应被拒绝")
	}

	if err := svc.Update(ctx, lv.ID, &LevelInput{Code: lv.Code, Name: lv.Name, Status: int8p(model.StatusDisabled)}); err != nil {
		t.Fatalf("停用等级失败: %v", err)
	}
	if _, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, Days: 10}); err == nil {
		t.Fatal("已停用等级不应允许开通")
	}
}

func TestUpdateBenefits(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	lv := createLevel(t, svc, "gold", 30)

	err := svc.Update(ctx, lv.ID, &LevelInput{
		Code: "gold", Name: "黄金会员", Rank: 20, DefaultDays: 90,
		Benefits: []model.Benefit{
			{Key: "api.qps", Label: "接口配额", Value: "200"},
			{Key: "report.export", Label: "导出报表", Value: "true"},
		},
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	var got model.MembershipLevel
	if err := svc.db.First(&got, "id = ?", lv.ID).Error; err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Name != "黄金会员" || got.DefaultDays != 90 || len(got.Benefits) != 2 {
		t.Fatalf("更新未生效: %+v", got)
	}
	if got.Benefits[0].Key != "api.qps" || got.Benefits[0].Value != "200" {
		t.Fatalf("权益序列化异常: %+v", got.Benefits)
	}
}

func TestDeleteLevelInUse(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	lv := createLevel(t, svc, "platinum", 0)
	u := createUser(t, db, "carol")
	if _, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID}); err != nil {
		t.Fatalf("开通失败: %v", err)
	}
	if err := svc.Delete(ctx, lv.ID); err == nil {
		t.Fatal("有用户引用时应拒绝删除")
	}
	if err := svc.Revoke(ctx, u.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if err := svc.Delete(ctx, lv.ID); err != nil {
		t.Fatalf("无用户引用时应可删除: %v", err)
	}
}

func TestHasBenefit(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	lv := createLevel(t, svc, "vip", 30)
	u := createUser(t, db, "dave")

	ok, err := svc.HasBenefit(ctx, u.ID, "report.export")
	if err != nil || ok {
		t.Fatalf("非会员不应有权益: ok=%v err=%v", ok, err)
	}
	if _, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID}); err != nil {
		t.Fatalf("开通失败: %v", err)
	}
	for key, want := range map[string]bool{
		"report.export": true, "project.max": true, "support.priority": false, "not.exists": false,
	} {
		got, err := svc.HasBenefit(ctx, u.ID, key)
		if err != nil {
			t.Fatalf("%s 查询失败: %v", key, err)
		}
		if got != want {
			t.Fatalf("%s: got %v want %v", key, got, want)
		}
	}
	// 停用账号不影响会员权益判断，权益只看等级。
	_ = db.Model(&model.User{}).Where("id = ?", u.ID).Update("status", model.StatusDisabled).Error
	if ok, _ := svc.HasBenefit(ctx, u.ID, "report.export"); !ok {
		t.Fatal("账号状态不应影响权益判定")
	}
}

func TestListUserCountAndFilterSource(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	lv := createLevel(t, svc, "gold", 30)
	other := createLevel(t, svc, "silver", 30)
	for _, name := range []string{"u1", "u2"} {
		u := createUser(t, db, name)
		if _, err := svc.Grant(ctx, u.ID, &GrantInput{LevelID: lv.ID, Days: 30}); err != nil {
			t.Fatalf("开通失败: %v", err)
		}
	}
	expired := createUser(t, db, "u3")
	if _, err := svc.Grant(ctx, expired.ID, &GrantInput{LevelID: other.ID, Days: 1}); err != nil {
		t.Fatalf("开通失败: %v", err)
	}
	// 手工把到期时间改到过去，模拟过期会员。
	_ = db.Model(&model.User{}).Where("id = ?", expired.ID).
		Update("membership_expire_at", time.Now().Add(-time.Hour)).Error

	list, total, err := svc.List(ctx, &query.Params{Page: 1, PageSize: 20, Filters: map[string]string{}})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 2 {
		t.Fatalf("等级数量错误: %d", total)
	}
	counts := map[string]int64{}
	for _, item := range list {
		counts[item.Code] = item.UserCount
	}
	if counts["gold"] != 2 || counts["silver"] != 0 {
		t.Fatalf("有效会员数统计错误: %+v", counts)
	}
}
