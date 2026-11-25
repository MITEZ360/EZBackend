package pay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

const (
	alipayLiveGateway = "https://openapi.alipay.com/gateway.do"
	alipaySandbox     = "https://openapi.alipaydev.com/gateway.do"
	alipayMethod      = "alipay.trade.page.pay"
	alipayProductCode = "FAST_INSTANT_TRADE_PAY"
)

type alipayGateway struct {
	appID      string
	privateKey string
	publicKey  string
	notifyURL  string
	returnURL  string
	gateway    string
}

// newAlipay 构造支付宝电脑网站支付网关。
func newAlipay(cfg config.AlipayPay, notifyBase string) Gateway {
	notifyURL := strings.TrimSpace(cfg.NotifyURL)
	if notifyURL == "" {
		notifyURL = strings.TrimRight(notifyBase, "/") + "/alipay"
	}
	returnURL := strings.TrimSpace(cfg.ReturnURL)
	if returnURL == "" {
		returnURL = notifyBase
	}
	endpoint := alipayLiveGateway
	if cfg.Sandbox {
		endpoint = alipaySandbox
	}
	return &alipayGateway{
		appID:      cfg.AppID,
		privateKey: cfg.PrivateKey,
		publicKey:  cfg.PublicKey,
		notifyURL:  notifyURL,
		returnURL:  returnURL,
		gateway:    endpoint,
	}
}

func (g *alipayGateway) Kind() string { return "alipay" }

func (g *alipayGateway) Charge(_ context.Context, in Charge) (*Result, error) {
	biz, err := json.Marshal(struct {
		OutTradeNo  string `json:"out_trade_no"`
		TotalAmount string `json:"total_amount"`
		Subject     string `json:"subject"`
		Body        string `json:"body"`
		ProductCode string `json:"product_code"`
	}{
		OutTradeNo:  in.OrderNo,
		TotalAmount: centsToAmount(in.AmountCents),
		Subject:     in.Subject,
		Body:        in.Body,
		ProductCode: alipayProductCode,
	})
	if err != nil {
		return nil, fmt.Errorf("构造支付宝 biz_content 失败: %w", err)
	}
	params := map[string]string{
		"app_id":      g.appID,
		"method":      alipayMethod,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   "RSA2",
		"timestamp":   time.Now().In(chinaTimeZone).Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  g.notifyURL,
		"return_url":  g.returnURL,
		"biz_content": string(biz),
	}
	if strings.TrimSpace(in.NotifyURL) != "" {
		params["notify_url"] = in.NotifyURL
	}
	sign, err := RSASign(signString(params), g.privateKey)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return &Result{
		PayURL: g.gateway + "?" + values.Encode() + "&sign=" + url.QueryEscape(sign),
	}, nil
}

func (g *alipayGateway) VerifyNotify(_ []byte, query map[string]string) (*Notify, error) {
	if len(query) == 0 {
		return nil, errors.New("支付宝回调参数为空")
	}
	// 回调验签串排除 sign 与 sign_type，其余按 key 字典序拼接。
	content := signString(query, "sign_type")
	if err := RSAVerify(content, query["sign"], g.publicKey); err != nil {
		return nil, err
	}
	state := query["trade_status"]
	if state != "" && state != "TRADE_SUCCESS" && state != "TRADE_FINISHED" {
		return nil, fmt.Errorf("支付宝交易未完成: trade_status=%s", state)
	}
	amount, err := amountToCents(query["total_amount"])
	if err != nil {
		return nil, err
	}
	paidAt, err := parseAlipayTime(query)
	if err != nil {
		return nil, err
	}
	return &Notify{
		OrderNo:    query["out_trade_no"],
		TradeNo:    query["trade_no"],
		Amount:     amount,
		PaidAt:     paidAt,
		Raw:        content,
		TradeState: state,
	}, nil
}

func (g *alipayGateway) SuccessResponse() (string, string) {
	return "text/plain", "success"
}

// centsToAmount 将分转换为带两位小数的元字符串，避免浮点精度误差。
func centsToAmount(cents int64) string {
	if cents < 0 {
		return "-" + fmt.Sprintf("%d.%02d", -cents/100, -cents%100)
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func amountToCents(s string) (int64, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("支付宝回调金额非法: %w", err)
	}
	return int64(math.Round(v * 100)), nil
}

func parseAlipayTime(query map[string]string) (time.Time, error) {
	value := strings.TrimSpace(query["gmt_payment"])
	if value == "" {
		value = strings.TrimSpace(query["notify_time"])
	}
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, chinaTimeZone)
	if err != nil {
		return time.Time{}, fmt.Errorf("支付宝回调付款时间格式非法: %w", err)
	}
	return t, nil
}
