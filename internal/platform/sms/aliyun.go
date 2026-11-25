package sms

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

const (
	aliyunDefaultEndpoint = "https://dysmsapi.aliyuncs.com/"
	aliyunAction          = "SendSms"
	aliyunFormat          = "JSON"
	aliyunSigMethod       = "HMAC-SHA1"
	aliyunSigVersion      = "1.0"
	aliyunAPIVersion      = "2017-05-25"
)

// aliyunSender 通过阿里云 POP 网关发送短信（标准库实现签名）。
type aliyunSender struct {
	cfg      config.SMS
	log      *zap.Logger
	client   *http.Client
	endpoint string
}

// percentEncode 按阿里云规则编码：A-Za-z0-9 与 -_.~ 不编码；
// 空格编码为 %20；星号保持字面量 *；其余字节编码为大写 %XY。
// 这与 url.QueryEscape 不同（后者把空格变成 +、星号变成 %2A），故手写。
func percentEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~', c == '*':
			b.WriteByte(c)
		case c == ' ':
			b.WriteString("%20")
		default:
			b.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return b.String()
}

// aliyunCanonicalQuery 对参数名按字典序排序后拼接 canonical query。
func aliyunCanonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, percentEncode(k)+"="+percentEncode(params[k]))
	}
	return strings.Join(parts, "&")
}

// aliyunStringToSign 构造待签名串。
func aliyunStringToSign(canonicalQuery string) string {
	return "GET&" + percentEncode("/") + "&" + percentEncode(canonicalQuery)
}

// aliyunSignature 计算 POP 签名：Base64(HMAC-SHA1(secret+"&", stringToSign))。
func aliyunSignature(accessKeySecret, stringToSign string) string {
	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// aliyunNonce 生成随机签名 nonce。
func aliyunNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// newAliyun 构造阿里云短信发送器。
func newAliyun(cfg config.SMS, log *zap.Logger) (Sender, error) {
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		return nil, fmt.Errorf("sms aliyun: AccessKeyID is required")
	}
	if strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return nil, fmt.Errorf("sms aliyun: AccessKeySecret is required")
	}
	if strings.TrimSpace(cfg.SignName) == "" {
		return nil, fmt.Errorf("sms aliyun: SignName is required")
	}
	if strings.TrimSpace(cfg.TemplateCode) == "" {
		return nil, fmt.Errorf("sms aliyun: TemplateCode is required")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = aliyunDefaultEndpoint
	}
	return &aliyunSender{
		cfg:      cfg,
		log:      log,
		client:   &http.Client{Timeout: 10 * time.Second},
		endpoint: strings.TrimRight(endpoint, "/"),
	}, nil
}

func (a *aliyunSender) Kind() string { return "aliyun" }

func (a *aliyunSender) Send(ctx context.Context, phone, code string) error {
	if strings.TrimSpace(phone) == "" {
		return fmt.Errorf("sms aliyun: phone is empty")
	}
	nonce, err := aliyunNonce()
	if err != nil {
		return fmt.Errorf("sms aliyun: generate nonce: %w", err)
	}
	params := map[string]string{
		"AccessKeyId":      a.cfg.AccessKeyID,
		"Action":           aliyunAction,
		"Format":           aliyunFormat,
		"PhoneNumbers":     phone,
		"SignName":         a.cfg.SignName,
		"SignatureMethod":  aliyunSigMethod,
		"SignatureNonce":   nonce,
		"SignatureVersion": aliyunSigVersion,
		"TemplateCode":     a.cfg.TemplateCode,
		"TemplateParam":    fmt.Sprintf(`{"code":%q}`, code),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Version":          aliyunAPIVersion,
	}
	canonical := aliyunCanonicalQuery(params)
	signature := aliyunSignature(a.cfg.AccessKeySecret, aliyunStringToSign(canonical))
	url := a.endpoint + "?" + canonical + "&Signature=" + percentEncode(signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("sms aliyun: build request: %w", err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("sms aliyun: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("sms aliyun: read response: %w", err)
	}
	var out struct {
		Code      string `json:"Code"`
		Message   string `json:"Message"`
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("sms aliyun: decode response: %w", err)
	}
	if out.Code != "OK" {
		return fmt.Errorf("sms aliyun: send failed: code=%s message=%s request_id=%s",
			out.Code, out.Message, out.RequestID)
	}
	if a.log != nil {
		a.log.Info("sms aliyun sent", zap.String("phone", phone), zap.String("request_id", out.RequestID))
	}
	return nil
}

func (a *aliyunSender) Health(_ context.Context) error { return nil }

func (a *aliyunSender) Close() error { return nil }
