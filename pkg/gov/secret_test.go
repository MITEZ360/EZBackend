package gov

import (
	"encoding/base64"
	"strings"
	"testing"
)

const testKey = "unit-test-master-key"

// TestEncryptDecryptRoundTrip 加解密往返，且输出带 enc: 前缀。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	cases := []string{
		"S3cret-2026",
		"",
		"含中文与符号 !@#$%^&*()_+",
		strings.Repeat("x", 4096),
	}
	for _, plain := range cases {
		enc, err := Encrypt(plain, testKey)
		if err != nil {
			t.Fatalf("加密 %q 失败: %v", plain, err)
		}
		if !strings.HasPrefix(enc, EncPrefix) {
			t.Fatalf("密文应带 %s 前缀，实际 %q", EncPrefix, enc)
		}
		if plain != "" && strings.Contains(enc, plain) {
			t.Fatalf("密文不应包含明文: %q", enc)
		}
		got, err := Decrypt(enc, testKey)
		if err != nil {
			t.Fatalf("解密失败: %v", err)
		}
		if got != plain {
			t.Fatalf("往返不一致: %q -> %q", plain, got)
		}
	}
}

// TestEncryptRandomNonce 同一明文两次加密结果不同（随机 nonce），但都能解回。
func TestEncryptRandomNonce(t *testing.T) {
	a, err := Encrypt("same", testKey)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt("same", testKey)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("两次加密结果相同，说明 nonce 复用")
	}
	for _, v := range []string{a, b} {
		if got, err := Decrypt(v, testKey); err != nil || got != "same" {
			t.Fatalf("解密失败: %q %v", got, err)
		}
	}
}

// TestEncryptNoKey 未配置主密钥时必须显式报错，不能退化成明文。
func TestEncryptNoKey(t *testing.T) {
	if _, err := Encrypt("x", ""); err == nil {
		t.Fatal("空密钥应报错")
	}
	if _, err := Decrypt(EncPrefix+"AAAA", ""); err == nil {
		t.Fatal("密文在无密钥时应报错")
	}
}

// TestDecryptPlainPassthrough 非密文原样返回，兼容历史明文数据。
func TestDecryptPlainPassthrough(t *testing.T) {
	got, err := Decrypt("plain-value", testKey)
	if err != nil || got != "plain-value" {
		t.Fatalf("明文应原样返回: %q %v", got, err)
	}
	// 密钥缺失时明文也不需要解密。
	if got, err := Decrypt("plain-value", ""); err != nil || got != "plain-value" {
		t.Fatalf("无密钥时明文应原样返回: %q %v", got, err)
	}
}

// TestDecryptWrongKey 错误密钥必须解密失败（GCM 认证标签校验）。
func TestDecryptWrongKey(t *testing.T) {
	enc, err := Encrypt("secret", testKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(enc, "another-key"); err == nil {
		t.Fatal("错误密钥应解密失败")
	}
}

// TestDecryptTampered 篡改密文任何一段都应被认证标签拒绝。
func TestDecryptTampered(t *testing.T) {
	enc, err := Encrypt("secret", testKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, EncPrefix))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF
	tampered := EncPrefix + base64.StdEncoding.EncodeToString(raw)
	if _, err := Decrypt(tampered, testKey); err == nil {
		t.Fatal("篡改后的密文应解密失败")
	}

	// base64 非法与长度不足也要报错而不是 panic。
	if _, err := Decrypt(EncPrefix+"!!!not-base64!!!", testKey); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if _, err := Decrypt(EncPrefix+"AAAA", testKey); err == nil {
		t.Fatal("长度不足 nonce 的密文应报错")
	}
}

// TestMasked 接口展示形态：密文一律掩码，明文原样。
func TestMasked(t *testing.T) {
	enc, err := Encrypt("secret", testKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := Masked(enc); got != "******" {
		t.Fatalf("密文应显示 ******，实际 %q", got)
	}
	if got := Masked("plain"); got != "plain" {
		t.Fatalf("明文应原样返回，实际 %q", got)
	}
	if got := Masked(""); got != "" {
		t.Fatalf("空值应原样返回，实际 %q", got)
	}
}

// TestMasterKeyFromEnv 主密钥只从环境变量读，且首尾空白被裁掉。
func TestMasterKeyFromEnv(t *testing.T) {
	t.Setenv("TENON_MASTER_KEY", "  env-key  ")
	if got := MasterKey(); got != "env-key" {
		t.Fatalf("应裁掉空白，实际 %q", got)
	}
	t.Setenv("TENON_MASTER_KEY", "   ")
	if got := MasterKey(); got != "" {
		t.Fatalf("全空白应视为未配置，实际 %q", got)
	}
}
