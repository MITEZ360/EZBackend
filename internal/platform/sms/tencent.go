package sms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

const (
	tencentEndpoint    = "https://sms.tencentcloudapi.com/"
	tencentHost        = "sms.tencentcloudapi.com"
	tencentService     = "sms"
	tencentAction      = "SendSms"
	tencentVersion     = "2021-01-11"
	tencentContentType = "application/json; charset=utf-8"
)

// tencentSender 通过腾讯云 TC3-HMAC-SHA256 签名发送短信。
type tencentSender struct {
	cfg      config.SMS
	log      *zap.Logger
	client   *http.Client
	sdkAppID string
}

// tencentPayload 为 SendSms 接口请求体。
type tencentPayload struct {
	PhoneNumberSet   []string `json:"PhoneNumberSet"`
	TemplateID       string   `json:"TemplateID"`
	SignName         string   `json:"SignName"`
	TemplateParamSet []string `json:"TemplateParamSet"`
	SmsSdkAppId      string   `json:"SmsSdkAppId"`
}

// firstDigits 返回字符串中第一段连续数字。
func firstDigits(s string) string {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
		} else if start >= 0 {
			return s[start:i]
		}
	}
	if start >= 0 {
		return s[start:]
	}
	return ""
}

// tencentSDKAppID 解析短信应用 SmsSdkAppId。
// 兼容策略：config.SMS 没有专门的 sdkappid 字段，优先使用 cfg.Endpoint 承载
// （可直接填数字 sdkappid，也可填包含数字的 URL，取其中第一段数字）；
// 若 Endpoint 为空，则用 cfg.SignName 作为占位值。接入方可按实际部署调整。
func tencentSDKAppID(cfg config.SMS) string {
	ep := strings.TrimSpace(cfg.Endpoint)
	if ep != "" {
		if digits := firstDigits(ep); digits != "" {
			return digits
		}
		return ep
	}
	return cfg.SignName
}

// tencentPhone 为手机号加中国区号 +86，已是 E.164 格式的保持原样。
func tencentPhone(phone string) string {
	p := strings.TrimSpace(phone)
	if strings.HasPrefix(p, "+") {
		return p
	}
	return "+86" + p
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

// tc3CanonicalRequest 构造 TC3 规范请求串。
func tc3CanonicalRequest(payload string) string {
	canonicalHeaders := "content-type:" + tencentContentType + "\n" +
		"host:" + tencentHost + "\n"
	signedHeaders := "content-type;host"
	return "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex([]byte(payload))
}

// tc3StringToSign 构造待签名串。
func tc3StringToSign(timestamp, date, canonicalRequest string) string {
	return "TC3-HMAC-SHA256\n" + timestamp + "\n" +
		date + "/sms/tc3_request\n" + sha256Hex([]byte(canonicalRequest))
}

// tc3Signature 按 TC3 派生密钥流程计算签名（十六进制）。
func tc3Signature(secretKey, date, timestamp, canonicalRequest string) string {
	kDate := hmacSHA256([]byte("TC3"+secretKey), date)
	kService := hmacSHA256(kDate, tencentService)
	kSigning := hmacSHA256(kService, "tc3_request")
	return hex.EncodeToString(hmacSHA256(kSigning, tc3StringToSign(timestamp, date, canonicalRequest)))
}

// newTencent 构造腾讯云短信发送器。
func newTencent(cfg config.SMS, log *zap.Logger) (Sender, error) {
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		return nil, fmt.Errorf("sms tencent: SecretId (AccessKeyID) is required")
	}
	if strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return nil, fmt.Errorf("sms tencent: SecretKey (AccessKeySecret) is required")
	}
	return &tencentSender{
		cfg:      cfg,
		log:      log,
		client:   &http.Client{Timeout: 10 * time.Second},
		sdkAppID: tencentSDKAppID(cfg),
	}, nil
}

func (t *tencentSender) Kind() string { return "tencent" }

func (t *tencentSender) Send(ctx context.Context, phone, code string) error {
	if strings.TrimSpace(phone) == "" {
		return fmt.Errorf("sms tencent: phone is empty")
	}
	payload := tencentPayload{
		PhoneNumberSet:   []string{tencentPhone(phone)},
		TemplateID:       t.cfg.TemplateCode,
		SignName:         t.cfg.SignName,
		TemplateParamSet: []string{fmt.Sprintf(`{"code":%q}`, code)},
		SmsSdkAppId:      t.sdkAppID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("sms tencent: encode payload: %w", err)
	}

	ts := time.Now().Unix()
	timestamp := strconv.FormatInt(ts, 10)
	date := time.Unix(ts, 0).UTC().Format("2006-01-02")
	canonical := tc3CanonicalRequest(string(body))
	signature := tc3Signature(t.cfg.AccessKeySecret, date, timestamp, canonical)
	authorization := "TC3-HMAC-SHA256 Credential=" + t.cfg.AccessKeyID + "/" + date +
		"/sms/tc3_request, SignedHeaders=content-type;host, Signature=" + signature

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tencentEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sms tencent: build request: %w", err)
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", tencentContentType)
	req.Header.Set("X-TC-Action", tencentAction)
	req.Header.Set("X-TC-Timestamp", timestamp)
	req.Header.Set("X-TC-Version", tencentVersion)
	req.Header.Set("X-TC-Region", t.cfg.Region)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sms tencent: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("sms tencent: read response: %w", err)
	}
	var out struct {
		Response struct {
			Error *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
			SendStatusSet []struct {
				Code        string `json:"Code"`
				Message     string `json:"Message"`
				PhoneNumber string `json:"PhoneNumber"`
				SerialNo    string `json:"SerialNo"`
			} `json:"SendStatusSet"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("sms tencent: decode response: %w", err)
	}
	if e := out.Response.Error; e != nil {
		return fmt.Errorf("sms tencent: api error: code=%s message=%s", e.Code, e.Message)
	}
	if len(out.Response.SendStatusSet) == 0 {
		return fmt.Errorf("sms tencent: empty SendStatusSet")
	}
	st := out.Response.SendStatusSet[0]
	if st.Code != "Ok" {
		return fmt.Errorf("sms tencent: send failed: code=%s message=%s serial=%s",
			st.Code, st.Message, st.SerialNo)
	}
	if t.log != nil {
		t.log.Info("sms tencent sent", zap.String("phone", phone), zap.String("serial", st.SerialNo))
	}
	return nil
}

func (t *tencentSender) Health(_ context.Context) error { return nil }

func (t *tencentSender) Close() error { return nil }
