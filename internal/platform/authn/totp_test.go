package authn

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestTOTPGenerateAndValidate(t *testing.T) {
	secret, url, err := GenerateTOTP("Keel 管理平台", "admin")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if secret == "" {
		t.Fatal("密钥为空")
	}
	for _, want := range []string{"otpauth://totp/", "issuer=", "secret=", "period=30", "digits=6"} {
		if !strings.Contains(url, want) {
			t.Fatalf("otpauth 链接缺少 %q: %s", want, url)
		}
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("生成动态码失败: %v", err)
	}
	if !ValidateTOTP(secret, code) {
		t.Fatal("当前动态码应校验通过")
	}
	if ValidateTOTP(secret, "000000") && code != "000000" {
		t.Fatal("错误动态码不应通过")
	}
	if ValidateTOTP(secret, "12345") {
		t.Fatal("长度非法不应通过")
	}
	if ValidateTOTP("", code) {
		t.Fatal("空密钥不应通过")
	}
}

func TestQRCodeDataURI(t *testing.T) {
	_, url, err := GenerateTOTP("Keel", "admin")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	uri, err := QRCodeDataURI(url)
	if err != nil {
		t.Fatalf("二维码生成失败: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatalf("data URI 前缀错误: %.32s", uri)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if len(raw) < 100 || string(raw[1:4]) != "PNG" {
		t.Fatalf("不是有效的 PNG，长度 %d", len(raw))
	}
}

func TestGenerateRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("数量应为 %d，实际 %d", RecoveryCodeCount, len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != RecoveryCodeLength+1 || c[5] != '-' {
			t.Fatalf("格式错误: %s", c)
		}
		if NormalizeRecoveryCode(c) == "" || len(NormalizeRecoveryCode(c)) != RecoveryCodeLength {
			t.Fatalf("归一化后长度错误: %s", c)
		}
		if seen[c] {
			t.Fatalf("出现重复恢复码: %s", c)
		}
		seen[c] = true
	}
}

func TestRecoveryCodeHashAndNormalize(t *testing.T) {
	const key = "test-key"
	code := "7K3M9-QP2XR"
	want := HashRecoveryCode(code, key)

	// 用户输入大小写、空格、缺少连字符都应命中同一个哈希
	for _, variant := range []string{"7k3m9-qp2xr", " 7K3M9 QP2XR ", "7K3M9QP2XR"} {
		if got := HashRecoveryCode(variant, key); got != want {
			t.Fatalf("%q 归一化后哈希不一致", variant)
		}
	}
	if HashRecoveryCode(code, "other-key") == want {
		t.Fatal("不同密钥应产生不同哈希")
	}
	if len(want) != 64 {
		t.Fatalf("SHA-256 十六进制长度应为 64，实际 %d", len(want))
	}
}
