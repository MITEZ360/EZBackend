package recharge

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/modules/membership"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/pay"
)

type mockGateway struct {
	n *pay.Notify
}

func (m *mockGateway) Kind() string { return "mock" }
func (m *mockGateway) Charge(context.Context, pay.Charge) (*pay.Result, error) {
	return &pay.Result{PayURL: "https://pay.example.com/mock"}, nil
}
func (m *mockGateway) VerifyNotify([]byte, map[string]string) (*pay.Notify, error) {
	return m.n, nil
}
func (m *mockGateway) SuccessResponse() (string, string) { return "text/plain", "success" }

func TestNotifyIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "notify.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.MembershipLevel{}, &model.RechargeOrder{}); err != nil {
		t.Fatal(err)
	}
	user := &model.User{Username: "buyer", Password: "x", Status: model.StatusEnabled}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	lv := &model.MembershipLevel{
		Code: "silver", Name: "白银会员", Status: model.StatusEnabled,
		DefaultDays: 30, Price: 9900,
	}
	if err := db.Create(lv).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.RechargeOrder{
		OrderNo: "RC-TEST-1", UserID: user.ID, LevelID: lv.ID,
		Amount: 9900, Days: 30, PayType: model.PayWechat, Status: model.OrderPending,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}

	c, err := cache.New(config.Redis{}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, &config.Config{}, c, membership.NewService(db, zap.NewNop()), zap.NewNop())
	svc.newGateway = func(string) (pay.Gateway, error) {
		return &mockGateway{n: &pay.Notify{
			OrderNo: order.OrderNo, TradeNo: "TRX-1", Amount: 9900, PaidAt: time.Now(),
		}}, nil
	}

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		ct, body, err := svc.Notify(ctx, model.PayWechat, []byte("<xml/>"), nil)
		if err != nil || body != "success" || ct != "text/plain" {
			t.Fatalf("第 %d 次回调失败: %v %s %s", i+1, err, ct, body)
		}
	}
	var got model.RechargeOrder
	if err := db.First(&got, "order_no = ?", order.OrderNo).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != model.OrderPaid || got.TradeNo != "TRX-1" {
		t.Fatalf("订单状态错误: %+v", got)
	}
	var u model.User
	if err := db.First(&u, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if u.MembershipLevelID != lv.ID || u.MembershipExpireAt == nil {
		t.Fatalf("会员未发放: %+v", u)
	}
}

func TestNotifyRejectsMismatchedAmount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "amount.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.RechargeOrder{}); err != nil {
		t.Fatal(err)
	}
	order := &model.RechargeOrder{OrderNo: "RC-2", Amount: 9900, PayType: model.PayWechat, Status: model.OrderPending}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, &config.Config{}, nil, nil, zap.NewNop())
	svc.newGateway = func(string) (pay.Gateway, error) {
		return &mockGateway{n: &pay.Notify{OrderNo: "RC-2", Amount: 1}}, nil
	}
	_, _, err = svc.Notify(context.Background(), model.PayWechat, nil, nil)
	if err == nil {
		t.Fatal("金额不一致的回调必须被拒绝")
	}
}
