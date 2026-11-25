package pay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

const wechatUnifiedOrderURL = "https://api.mch.weixin.qq.com/pay/unifiedorder"

// 微信支付回调中的时间是北京时间（UTC+8）。
var chinaTimeZone = time.FixedZone("CST", 8*60*60)

type wechatGateway struct {
	appID     string
	mchID     string
	apiKey    string
	notifyURL string
	signType  string
	client    *http.Client
}

// newWechat 构造微信支付 V2 Native 扫码网关。
// 注意：沙箱模式的 unifiedorder 需要先调用 sandboxnew 换取沙箱 key，
// 流程较重；这里沙箱仍走正式域名，需手动在配置中填入沙箱 API key。
func newWechat(cfg config.WechatPay, notifyBase string) Gateway {
	notifyURL := strings.TrimSpace(cfg.NotifyURL)
	if notifyURL == "" {
		notifyURL = strings.TrimRight(notifyBase, "/") + "/wechat"
	}
	signType := strings.ToUpper(strings.TrimSpace(cfg.SignType))
	if signType == "" {
		signType = "MD5"
	}
	return &wechatGateway{
		appID:     cfg.AppID,
		mchID:     cfg.MchID,
		apiKey:    cfg.APIKey,
		notifyURL: notifyURL,
		signType:  signType,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *wechatGateway) Kind() string { return "wechat" }

func (g *wechatGateway) Charge(ctx context.Context, in Charge) (*Result, error) {
	m := map[string]string{
		"appid":            g.appID,
		"mch_id":           g.mchID,
		"nonce_str":        nonce(),
		"body":             in.Subject,
		"out_trade_no":     in.OrderNo,
		"total_fee":        strconv.FormatInt(in.AmountCents, 10),
		"spbill_create_ip": in.ClientIP,
		"notify_url":       g.notifyURL,
		"trade_type":       "NATIVE",
		"product_id":       in.OrderNo,
		"sign_type":        g.signType,
	}
	if strings.TrimSpace(in.NotifyURL) != "" {
		m["notify_url"] = in.NotifyURL
	}
	if strings.TrimSpace(in.Body) != "" {
		m["body"] = in.Body
	}
	if strings.TrimSpace(in.ClientIP) == "" {
		m["spbill_create_ip"] = "127.0.0.1"
	}
	m["sign"] = g.sign(m)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wechatUnifiedOrderURL,
		bytes.NewReader([]byte(MapToXML(m))))
	if err != nil {
		return nil, fmt.Errorf("构造微信支付请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求微信支付失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取微信支付响应失败: %w", err)
	}
	got, err := XMLToMap(body)
	if err != nil {
		return nil, err
	}
	if got["return_code"] != "SUCCESS" {
		return nil, fmt.Errorf("微信支付下单失败: return_msg=%s", got["return_msg"])
	}
	if got["result_code"] != "SUCCESS" {
		return nil, fmt.Errorf("微信支付下单失败: err_code=%s err_code_des=%s",
			got["err_code"], got["err_code_des"])
	}
	codeURL := strings.TrimSpace(got["code_url"])
	if codeURL == "" {
		return nil, errors.New("微信支付响应缺少 code_url")
	}
	return &Result{
		PayURL:  codeURL,
		QRCode:  codeURL,
		TradeNo: got["prepay_id"],
	}, nil
}

func (g *wechatGateway) VerifyNotify(body []byte, _ map[string]string) (*Notify, error) {
	m, err := XMLToMap(body)
	if err != nil {
		return nil, err
	}
	if m["return_code"] != "SUCCESS" {
		return nil, fmt.Errorf("微信回调 return_code 非 SUCCESS: %s", m["return_code"])
	}
	signType := strings.ToUpper(strings.TrimSpace(m["sign_type"]))
	if signType == "" {
		signType = g.signType
	}
	expect := signMap(m, signType, g.apiKey)
	if expect == "" || !strings.EqualFold(expect, m["sign"]) {
		return nil, errors.New("微信回调签名校验失败")
	}
	amount, err := strconv.ParseInt(m["total_fee"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("微信回调金额非法: %w", err)
	}
	paidAt, err := time.ParseInLocation("20060102150405", m["time_end"], chinaTimeZone)
	if err != nil {
		return nil, fmt.Errorf("微信回调付款时间格式非法: %w", err)
	}
	state := m["trade_state"]
	if state == "" {
		state = "SUCCESS"
	}
	return &Notify{
		OrderNo:    m["out_trade_no"],
		TradeNo:    m["trade_no"],
		Amount:     amount,
		PaidAt:     paidAt,
		Raw:        string(body),
		TradeState: state,
	}, nil
}

func (g *wechatGateway) SuccessResponse() (string, string) {
	return "text/xml", `<xml><return_code><![CDATA[SUCCESS]]></return_code><return_msg><![CDATA[OK]]></return_msg></xml>`
}

// sign 使用网关配置的签名类型对参数签名。
func (g *wechatGateway) sign(m map[string]string) string {
	return signMap(m, g.signType, g.apiKey)
}

func signMap(m map[string]string, signType, apiKey string) string {
	switch strings.ToUpper(strings.TrimSpace(signType)) {
	case "HMAC-SHA256":
		return SignMapHMACSHA256(m, apiKey)
	case "MD5", "":
		return SignMapMD5(m, apiKey)
	default:
		return ""
	}
}

// nonce 生成微信支付要求的随机字符串。
func nonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败属于极端情况，退化为时间戳仍可保证单次唯一性。
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}
