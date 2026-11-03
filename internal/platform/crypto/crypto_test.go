package crypto

import (
	"strings"
	"testing"
)

const secret = "unit-test-secret-key"

func TestEncryptDecrypt(t *testing.T) {
	plain := "JBSWY3DPEHPK3PXP"
	enc, err := Encrypt(plain, secret)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if enc == plain || !strings.HasPrefix(enc, "enc:v1:") {
		t.Fatalf("密文格式异常: %s", enc)
	}
	dec, err := Decrypt(enc, secret)
	if err != nil || dec != plain {
		t.Fatalf("解密失败: %q %v", dec, err)
	}
}

func TestEncryptRandomized(t *testing.T) {
	a, _ := Encrypt("same", secret)
	b, _ := Encrypt("same", secret)
	if a == b {
		t.Fatal("相同明文两次加密结果不应相同")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	enc, _ := Encrypt("secret-value", secret)
	if _, err := Decrypt(enc, "another-key"); err == nil {
		t.Fatal("错误密钥应解密失败")
	}
}

func TestDecryptPlainText(t *testing.T) {
	// 无前缀的历史明文原样返回，便于平滑迁移
	got, err := Decrypt("plain-value", secret)
	if err != nil || got != "plain-value" {
		t.Fatalf("明文透传失败: %q %v", got, err)
	}
}

func TestHashAndHMAC(t *testing.T) {
	if SHA256("abc") == "" || SHA256("abc") == SHA256("abd") {
		t.Fatal("SHA256 结果异常")
	}
	sig := HMAC(secret, "payload")
	if !VerifyHMAC(secret, "payload", sig) {
		t.Fatal("HMAC 校验失败")
	}
	if VerifyHMAC(secret, "payload2", sig) {
		t.Fatal("篡改数据不应通过校验")
	}
}

func TestRandomBytes(t *testing.T) {
	a := RandomBytes(16)
	b := RandomBytes(16)
	if len(a) != 16 || string(a) == string(b) {
		t.Fatal("随机数生成异常")
	}
}
