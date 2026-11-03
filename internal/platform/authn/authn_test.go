package authn

import (
	"testing"
	"time"

	"github.com/keel-framework/keel/internal/platform/config"
)

func newManager() *Manager {
	return NewManager(config.JWT{
		Secret: "test-secret-key-for-unit-test-only-32bytes", Issuer: "keel-test",
		AccessTTL: time.Minute, RefreshTTL: time.Hour,
	})
}

func TestJWTIssueAndParse(t *testing.T) {
	m := newManager()
	token, exp, err := m.Issue(Claims{UserID: "u1", Username: "admin", Roles: []string{"super"}, Super: true, SessionID: "s1"})
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if token == "" || exp.Before(time.Now()) {
		t.Fatal("签发的令牌无效")
	}
	claims, err := m.Parse(token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.UserID != "u1" || claims.SessionID != "s1" || !claims.Super {
		t.Fatalf("载荷丢失: %+v", claims)
	}
}

func TestJWTExpired(t *testing.T) {
	m := NewManager(config.JWT{Secret: "test-secret-key-for-unit-test-only-32bytes", Issuer: "keel-test",
		AccessTTL: -time.Minute, RefreshTTL: time.Hour})
	token, _, err := m.Issue(Claims{UserID: "u1"})
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := m.Parse(token); err != ErrTokenExpired {
		t.Fatalf("过期令牌应返回 ErrTokenExpired，实际 %v", err)
	}
}

func TestJWTWrongSignature(t *testing.T) {
	m := newManager()
	token, _, _ := m.Issue(Claims{UserID: "u1"})
	other := NewManager(config.JWT{Secret: "another-secret-key-another-secret-key", Issuer: "keel-test",
		AccessTTL: time.Minute, RefreshTTL: time.Hour})
	if _, err := other.Parse(token); err == nil {
		t.Fatal("不同密钥应校验失败")
	}
	if _, err := m.Parse("not-a-token"); err == nil {
		t.Fatal("非法令牌应报错")
	}
}

func TestPassword(t *testing.T) {
	hash, err := HashPassword("Admin@123456")
	if err != nil {
		t.Fatalf("散列失败: %v", err)
	}
	if hash == "Admin@123456" {
		t.Fatal("密码不应明文存储")
	}
	if !CheckPassword(hash, "Admin@123456") {
		t.Fatal("正确密码校验失败")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("错误密码不应通过")
	}
	if CheckPassword("", "x") {
		t.Fatal("空散列不应通过")
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	policy := PasswordPolicy{MinLen: 8, Level: 2}
	if err := ValidatePassword("abc12345", policy); err != nil {
		t.Fatalf("合规密码不应报错: %v", err)
	}
	if err := ValidatePassword("short", policy); err == nil {
		t.Fatal("长度不足应报错")
	}
	if err := ValidatePassword("abcdefgh", policy); err == nil {
		t.Fatal("纯字母应不满足强度 2")
	}
	if err := ValidatePassword("Admin@123456", PasswordPolicy{MinLen: 8, Level: 3}); err != nil {
		t.Fatalf("四类字符应满足强度 3: %v", err)
	}
}

func TestTOTP(t *testing.T) {
	secret, url, err := GenerateTOTP("keel", "admin")
	if err != nil {
		t.Fatalf("生成 TOTP 失败: %v", err)
	}
	if secret == "" || url == "" {
		t.Fatal("TOTP 密钥或地址为空")
	}
	if ValidateTOTP(secret, "000000") {
		t.Fatal("错误验证码不应通过")
	}
}

func TestIdentityHas(t *testing.T) {
	id := &Identity{Super: true}
	if !id.Has("any:perm") {
		t.Fatal("超级管理员应拥有全部权限")
	}
	id = &Identity{Perms: map[string]struct{}{"system:user:list": {}, "system:user:create": {}}, Roles: []string{"ops"}}
	if !id.Has("system:user:list") || id.Has("system:user:delete") {
		t.Fatal("权限判断错误")
	}
	if !id.HasRole("ops") || id.HasRole("super") {
		t.Fatal("角色判断错误")
	}
}
