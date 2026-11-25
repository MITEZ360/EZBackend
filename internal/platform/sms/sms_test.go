package sms

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

func TestPercentEncode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "unreserved", in: "abcXYZ0123-_.~", want: "abcXYZ0123-_.~"},
		{name: "space", in: "a b", want: "a%20b"},
		{name: "star stays literal", in: "*", want: "*"},
		{name: "plus encoded", in: "a+b", want: "a%2Bb"},
		{name: "template param", in: `{"code":"1234"}`, want: "%7B%22code%22%3A%221234%22%7D"},
		{name: "url", in: "https://dysmsapi.aliyuncs.com/", want: "https%3A%2F%2Fdysmsapi.aliyuncs.com%2F"},
		{name: "slash and colon", in: "/", want: "%2F"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := percentEncode(tt.in); got != tt.want {
				t.Fatalf("percentEncode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAliyunCanonicalQuery(t *testing.T) {
	params := map[string]string{
		"Version":       "2017-05-25",
		"Action":        "SendSms",
		"TemplateParam": `{"code":"1234"}`,
		"PhoneNumbers":  "13800138000",
	}
	want := "Action=SendSms&PhoneNumbers=13800138000&TemplateParam=%7B%22code%22%3A%221234%22%7D&Version=2017-05-25"
	if got := aliyunCanonicalQuery(params); got != want {
		t.Fatalf("aliyunCanonicalQuery() = %q, want %q", got, want)
	}
}

func TestAliyunStringToSignAndSignature(t *testing.T) {
	canonical := "Action=SendSms&PhoneNumbers=13800138000"
	want := "GET&%2F&Action%3DSendSms%26PhoneNumbers%3D13800138000"
	if got := aliyunStringToSign(canonical); got != want {
		t.Fatalf("aliyunStringToSign() = %q, want %q", got, want)
	}
	sig := aliyunSignature("secret", want)
	if len(sig) != 28 {
		t.Fatalf("signature length = %d, want 28 (Base64 of SHA-1)", len(sig))
	}
	if sig != aliyunSignature("secret", want) {
		t.Fatal("signature is not deterministic")
	}
	if aliyunSignature("other", want) == sig {
		t.Fatal("different secrets produced identical signatures")
	}
}

func TestMockSender(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	s := &mockSender{log: zap.New(core)}
	if s.Kind() != "mock" {
		t.Fatalf("Kind() = %q, want mock", s.Kind())
	}
	if err := s.Send(context.Background(), "13800138000", "654321"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if err := s.Health(context.Background()); err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	entries := logs.FilterMessage("sms mock send").All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["phone"] != "13800138000" || fields["code"] != "654321" {
		t.Fatalf("log fields = %v, want phone/code", fields)
	}
	// nil logger 不应 panic。
	if err := (&mockSender{}).Send(context.Background(), "13800138000", "123456"); err != nil {
		t.Fatalf("Send() with nil logger: %v", err)
	}
}

func TestNewAliyunValidation(t *testing.T) {
	valid := config.SMS{
		Provider:        "aliyun",
		AccessKeyID:     "ak",
		AccessKeySecret: "sk",
		SignName:        "sign",
		TemplateCode:    "SMS_1",
	}
	tests := []struct {
		name    string
		mutate  func(*config.SMS)
		wantErr string
	}{
		{name: "valid", mutate: func(c *config.SMS) {}},
		{name: "missing AccessKeyID", mutate: func(c *config.SMS) { c.AccessKeyID = "" }, wantErr: "AccessKeyID"},
		{name: "missing AccessKeySecret", mutate: func(c *config.SMS) { c.AccessKeySecret = "" }, wantErr: "AccessKeySecret"},
		{name: "missing SignName", mutate: func(c *config.SMS) { c.SignName = "" }, wantErr: "SignName"},
		{name: "missing TemplateCode", mutate: func(c *config.SMS) { c.TemplateCode = "" }, wantErr: "TemplateCode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			got, err := newAliyun(cfg, zap.NewNop())
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err, tt.wantErr)
				}
				if got != nil {
					t.Fatalf("sender = %v, want nil on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind() != "aliyun" {
				t.Fatalf("Kind() = %q, want aliyun", got.Kind())
			}
		})
	}
}

func TestNewTencentValidation(t *testing.T) {
	valid := config.SMS{
		Provider:        "tencent",
		AccessKeyID:     "secret-id",
		AccessKeySecret: "secret-key",
		SignName:        "sign",
		TemplateCode:    "SMS_1",
		Region:          "ap-guangzhou",
	}
	tests := []struct {
		name    string
		mutate  func(*config.SMS)
		wantErr string
	}{
		{name: "valid", mutate: func(c *config.SMS) {}},
		{name: "missing SecretId", mutate: func(c *config.SMS) { c.AccessKeyID = "" }, wantErr: "AccessKeyID"},
		{name: "missing SecretKey", mutate: func(c *config.SMS) { c.AccessKeySecret = "" }, wantErr: "AccessKeySecret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			got, err := newTencent(cfg, zap.NewNop())
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err, tt.wantErr)
				}
				if got != nil {
					t.Fatalf("sender = %v, want nil on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Kind() != "tencent" {
				t.Fatalf("Kind() = %q, want tencent", got.Kind())
			}
		})
	}
}

func TestTencentHelpers(t *testing.T) {
	phones := []struct{ in, want string }{
		{in: "13800138000", want: "+8613800138000"},
		{in: "+8613800138000", want: "+8613800138000"},
	}
	for _, tt := range phones {
		if got := tencentPhone(tt.in); got != tt.want {
			t.Fatalf("tencentPhone(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	sdkTests := []struct {
		cfg  config.SMS
		want string
	}{
		{cfg: config.SMS{Endpoint: "1400001234", SignName: "sign"}, want: "1400001234"},
		{cfg: config.SMS{Endpoint: "https://sms.example.com/1400005678", SignName: "sign"}, want: "1400005678"},
		{cfg: config.SMS{Endpoint: "app-id", SignName: "sign"}, want: "app-id"},
		{cfg: config.SMS{SignName: "fallback"}, want: "fallback"},
	}
	for _, tt := range sdkTests {
		if got := tencentSDKAppID(tt.cfg); got != tt.want {
			t.Fatalf("tencentSDKAppID(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
}

func TestTC3Signature(t *testing.T) {
	canonical := tc3CanonicalRequest(`{"PhoneNumberSet":["+8613800138000"]}`)
	for _, part := range []string{"POST\n/\n\n", "content-type:" + tencentContentType, "host:" + tencentHost, "content-type;host"} {
		if !strings.Contains(canonical, part) {
			t.Fatalf("canonical request missing %q: %q", part, canonical)
		}
	}
	sts := tc3StringToSign("1700000000", "2023-11-14", canonical)
	lines := strings.Split(sts, "\n")
	want := []string{"TC3-HMAC-SHA256", "1700000000", "2023-11-14/sms/tc3_request"}
	if !reflect.DeepEqual(lines[:3], want) || len(lines) != 4 {
		t.Fatalf("stringToSign = %q, want prefix %v", sts, want)
	}
	sig := tc3Signature("secret", "2023-11-14", "1700000000", canonical)
	if len(sig) != 64 {
		t.Fatalf("signature length = %d, want 64 hex chars", len(sig))
	}
	if sig != tc3Signature("secret", "2023-11-14", "1700000000", canonical) {
		t.Fatal("signature is not deterministic")
	}
}

func TestNewDispatch(t *testing.T) {
	enabled := true
	tests := []struct {
		name    string
		cfg     config.SMS
		want    string
		wantErr bool
	}{
		{name: "disabled", cfg: config.SMS{Enabled: &enabled}, want: ""},
		{name: "mock by default", cfg: config.SMS{Provider: "mock"}, want: "mock"},
		{name: "unknown", cfg: config.SMS{Provider: "unknown"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.cfg, zap.NewNop())
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == "" {
				if got != nil {
					t.Fatalf("sender = %v, want nil", got)
				}
				return
			}
			if got.Kind() != tt.want {
				t.Fatalf("Kind() = %q, want %q", got.Kind(), tt.want)
			}
		})
	}
}
