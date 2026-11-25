package recharge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/pkg/query"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "recharge.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.RechargeOrder{}, &model.User{}, &model.MembershipLevel{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	cfg := &config.Config{}
	return NewService(db, cfg, nil, nil, zap.NewNop()), db
}

func TestAvailableRequiresEnabled(t *testing.T) {
	svc, _ := newTestService(t)
	if got := svc.Available(); len(got) != 0 {
		t.Fatalf("支付未启用时不应有可用渠道: %v", got)
	}
	svc.cfg.Payment.Enabled = true
	svc.cfg.Payment.Wechat.Enabled = true
	svc.cfg.Payment.Wechat.AppID = "wx"
	svc.cfg.Payment.Wechat.MchID = "mch"
	svc.cfg.Payment.Wechat.APIKey = "key"
	if got := svc.Available(); len(got) != 1 || got[0] != model.PayWechat {
		t.Fatalf("微信支付启用后渠道列表错误: %v", got)
	}
}

func TestOrderLifecycle(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	order := &model.RechargeOrder{
		OrderNo: "RC2026010100000001", UserID: "u1", LevelID: "lv1",
		Amount: 9900, PayType: model.PayWechat, Status: model.OrderPending,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	got, err := svc.Query(ctx, "u1", order.OrderNo)
	if err != nil || got.Amount != 9900 {
		t.Fatalf("查询订单失败: %v %+v", err, got)
	}
	p := &query.Params{Page: 1, PageSize: 10, Filters: map[string]string{}}
	list, total, err := svc.List(ctx, "u1", p)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("订单列表错误: %v total=%d", err, total)
	}
	if err := svc.Close(ctx, "u1", order.OrderNo); err != nil {
		t.Fatalf("关闭订单失败: %v", err)
	}
	if err := svc.Close(ctx, "u1", order.OrderNo); err == nil {
		t.Fatal("重复关闭应失败")
	}
	if _, err := svc.Query(ctx, "other", order.OrderNo); err == nil {
		t.Fatal("不应查到他人的订单")
	}
}

func TestOrderFinalState(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{model.OrderPending, false}, {model.OrderPaid, true},
		{model.OrderClosed, true}, {model.OrderRefunded, true},
	}
	for _, c := range cases {
		o := &model.RechargeOrder{Status: c.status}
		if got := o.IsFinal(); got != c.want {
			t.Errorf("状态 %s: got %v want %v", c.status, got, c.want)
		}
	}
}
