package apikey

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/internal/platform/config"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "apikey.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.APIKey{}, &model.Menu{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	c, err := cache.New(config.Redis{}, zap.NewNop())
	if err != nil {
		t.Fatalf("创建缓存失败: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return NewService(db, c, zap.NewNop()), db
}

func superActor() *authn.Identity {
	return &authn.Identity{UserID: "u_admin", Username: "admin", Super: true, Kind: authn.KindUser}
}

func ip(v ...string) []string { return v }

func TestIssueAndAuthenticate(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	out, err := svc.Create(ctx, &Input{
		Name:    "资讯爬虫",
		Scopes:  []string{"news:ingest"},
		IPAllow: []string{"10.0.0.0/8"},
	}, superActor())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if !strings.HasPrefix(out.Plain, "ak_s_") || len(out.Plain) != 45 {
		t.Fatalf("密钥格式不正确: %q", out.Plain)
	}
	if out.Key.Prefix != out.Plain[:authn.APIKeyPrefixLen] {
		t.Fatalf("前缀不匹配: %s", out.Key.Prefix)
	}
	if out.Key.SecretHash == out.Plain || out.Key.SecretHash == "" {
		t.Fatal("不得明文存储密钥")
	}

	k, err := svc.Authenticate(ctx, out.Plain, "10.1.2.3")
	if err != nil {
		t.Fatalf("认证失败: %v", err)
	}
	if k.ID != out.Key.ID || k.Name != "资讯爬虫" {
		t.Fatalf("认证返回的凭证不正确: %+v", k)
	}

	if _, err := svc.Authenticate(ctx, out.Plain+"x", "10.1.2.3"); err == nil {
		t.Fatal("错误的密钥应当被拒绝")
	}
	if _, err := svc.Authenticate(ctx, out.Plain, "8.8.8.8"); err == nil {
		t.Fatal("白名单外的 IP 应当被拒绝")
	}
}

func TestClientKeyIsNotSecret(t *testing.T) {
	svc, _ := newTestService(t)
	out, err := svc.Create(context.Background(), &Input{
		Name: "移动端", Kind: model.APIKeyClient, Scopes: []string{"news:read"},
	}, superActor())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if strings.HasPrefix(out.Plain, "ak_s_") || !strings.HasPrefix(out.Plain, "ak_c_") {
		t.Fatalf("客户端密钥前缀不正确: %q", out.Plain)
	}
	if out.Key.Secret() {
		t.Fatal("客户端标识不应被当作秘密")
	}
}

func TestScopeEscalationBlocked(t *testing.T) {
	svc, _ := newTestService(t)
	actor := &authn.Identity{
		UserID: "u_ops", Username: "ops",
		Perms: map[string]struct{}{"system:apikey:create": {}, "news:read": {}},
	}
	if _, err := svc.Create(context.Background(), &Input{
		Name: "越权", Scopes: []string{"news:ingest"},
	}, actor); err == nil {
		t.Fatal("不应允许授予自己不具备的权限")
	}
	if _, err := svc.Create(context.Background(), &Input{
		Name: "通配", Scopes: []string{"*"},
	}, actor); err == nil {
		t.Fatal("非超管不应能签发通配权限")
	}
	if _, err := svc.Create(context.Background(), &Input{
		Name: "正常", Scopes: []string{"news:read"},
	}, actor); err != nil {
		t.Fatalf("授予自己已有的权限应当成功: %v", err)
	}
}

func TestExpireAndDisable(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	out, err := svc.Create(ctx, &Input{
		Name: "过期", Scopes: []string{"news:read"}, ExpireAt: &past,
	}, superActor())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := svc.Authenticate(ctx, out.Plain, "127.0.0.1"); err == nil {
		t.Fatal("过期密钥应当被拒绝")
	}

	future := time.Now().Add(time.Hour)
	out2, _ := svc.Create(ctx, &Input{
		Name: "可停用", Scopes: []string{"news:read"}, ExpireAt: &future,
	}, superActor())
	if err := svc.SetStatus(ctx, out2.Key.ID, model.StatusDisabled); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if _, err := svc.Authenticate(ctx, out2.Plain, "127.0.0.1"); err == nil {
		t.Fatal("停用后应当被拒绝")
	}
}

func TestRotateInvalidatesOld(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	out, _ := svc.Create(ctx, &Input{Name: "轮换", Scopes: []string{"news:read"}}, superActor())

	rotated, err := svc.Rotate(ctx, out.Key.ID, superActor())
	if err != nil {
		t.Fatalf("轮换失败: %v", err)
	}
	if rotated.Plain == out.Plain {
		t.Fatal("轮换后必须生成新密钥")
	}
	if _, err := svc.Authenticate(ctx, out.Plain, "127.0.0.1"); err == nil {
		t.Fatal("旧密钥应当立即失效")
	}
	if _, err := svc.Authenticate(ctx, rotated.Plain, "127.0.0.1"); err != nil {
		t.Fatalf("新密钥应当可用: %v", err)
	}
}

func TestDeleteInvalidates(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	out, _ := svc.Create(ctx, &Input{Name: "删除", Scopes: []string{"news:read"}}, superActor())
	if err := svc.Delete(ctx, out.Key.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := svc.Authenticate(ctx, out.Plain, "127.0.0.1"); err == nil {
		t.Fatal("删除后应当被拒绝")
	}
	if _, err := svc.Get(ctx, out.Key.ID); err == nil {
		t.Fatal("删除后不应再查到")
	}
}

func TestDailyQuota(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	out, _ := svc.Create(ctx, &Input{
		Name: "低配额", Scopes: []string{"news:read"}, DailyQuota: 2,
	}, superActor())

	for i := 0; i < 2; i++ {
		ok, err := svc.Allow(ctx, out.Key)
		if err != nil || !ok {
			t.Fatalf("第 %d 次调用应当放行: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, err := svc.Allow(ctx, out.Key)
	if err != nil {
		t.Fatalf("配额判断出错: %v", err)
	}
	if ok {
		t.Fatal("超出日配额应当拒绝")
	}
	if n := svc.Usage(ctx, []string{out.Key.ID})[out.Key.ID]; n != 3 {
		t.Fatalf("用量统计不正确: %d", n)
	}
}

func TestScopeFormat(t *testing.T) {
	cases := map[string]bool{
		"news:read":          true,
		"news:ingest":        true,
		"system:user:export": true,
		"news":               false,
		"news::read":         false,
		"news:re ad":         false,
		"*:*":                false,
	}
	for in, want := range cases {
		if got := validScope(in); got != want {
			t.Errorf("validScope(%q)=%v，期望 %v", in, got, want)
		}
	}
}

func TestIPRules(t *testing.T) {
	k := &model.APIKey{IPAllow: ip("10.0.0.0/8", "192.168.1.10")}
	if !k.AllowIP("10.9.9.9") || !k.AllowIP("192.168.1.10") {
		t.Fatal("白名单内应当放行")
	}
	if k.AllowIP("192.168.1.11") || k.AllowIP("11.0.0.1") {
		t.Fatal("白名单外应当拒绝")
	}
	// IPv4-mapped IPv6 需要归一化，否则 Gin 在双栈监听下会误判
	if !k.AllowIP("::ffff:10.1.1.1") {
		t.Fatal("IPv4-mapped 地址应当按 IPv4 判断")
	}
	open := &model.APIKey{}
	if !open.AllowIP("1.2.3.4") {
		t.Fatal("未配置白名单表示不限制")
	}
}
