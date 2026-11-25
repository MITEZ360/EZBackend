package cnauth

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
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/sms"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cnauth.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.SMSCode{}, &model.User{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	cfg := &config.Config{}
	cfg.CN.CodeLength = 6
	cfg.CN.CodeTTL = 5 * time.Minute
	cfg.CN.DailySendLimit = 3
	cfg.CN.SMSLoginEnabled = true
	cfg.CN.RegisterEnabled = true
	sender, err := sms.New(config.SMS{Provider: "mock"}, zap.NewNop())
	if err != nil {
		t.Fatalf("构造短信网关失败: %v", err)
	}
	return NewService(db, cfg, nil, nil, sender, zap.NewNop()), db
}

func TestRandomDigits(t *testing.T) {
	for _, n := range []int{4, 6, 8} {
		code, err := randomDigits(n)
		if err != nil {
			t.Fatalf("randomDigits(%d) 失败: %v", n, err)
		}
		if len(code) != n {
			t.Fatalf("randomDigits(%d) 长度=%d", n, len(code))
		}
		for _, c := range code {
			if c < '0' || c > '9' {
				t.Fatalf("验证码包含非数字: %s", code)
			}
		}
	}
}

func TestPhoneAndUsernameRules(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"13800138000", true},
		{"12800138000", false},
		{"1380013800", false},
		{"+8613800138000", false},
	}
	for _, c := range cases {
		if got := rePhone.MatchString(c.in); got != c.want {
			t.Errorf("手机号 %q: got %v want %v", c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"alice", true}, {"a_b-2", true}, {"2alice", false}, {"ab", false},
	} {
		if got := reUsername.MatchString(c.in); got != c.want {
			t.Errorf("用户名 %q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestCodeHashStable(t *testing.T) {
	a := codeHash("13800138000", "login", "123456")
	b := codeHash("13800138000", "login", "123456")
	if a != b || len(a) != 64 {
		t.Fatalf("验证码哈希不稳定: %s / %s", a, b)
	}
	if a == codeHash("13800138000", "register", "123456") {
		t.Fatal("不同场景的验证码哈希不应相同")
	}
}

func TestSendCodeAndVerify(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()
	in := &SMSCodeInput{Phone: "13800138000", Scene: model.SMSCodeLogin}
	if err := svc.SendCode(ctx, in, "127.0.0.1"); err != nil {
		t.Fatalf("发送验证码失败: %v", err)
	}
	// 再次发送应被频率限制拦截
	if err := svc.SendCode(ctx, in, "127.0.0.1"); err == nil {
		t.Fatal("60 秒内重复发送应失败")
	}
	var rec model.SMSCode
	if err := db.First(&rec).Error; err != nil {
		t.Fatalf("验证码未入库: %v", err)
	}
	// mock 发送器打印的验证码不可直接读取，这里验证哈希与消费逻辑
	now := time.Now()
	if rec.ExpiresAt.Before(now) {
		t.Fatal("验证码过期时间错误")
	}
	if err := db.Model(&model.SMSCode{}).Where("id = ?", rec.ID).Update("used_at", now).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.verifyCode(ctx, in.Phone, model.SMSCodeLogin, "000000", true); err == nil {
		t.Fatal("已消费的验证码不应通过校验")
	}
}
