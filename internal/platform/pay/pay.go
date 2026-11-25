// Package pay 提供中国大陆支付网关：微信支付 V2 Native 扫码与支付宝电脑网站支付。
// 签名与验签均用标准库实现，避免引入外部 SDK。
package pay

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MITEZ360/tenon/internal/platform/config"
)

// Charge 支付请求。
type Charge struct {
	OrderNo     string // 业务订单号（out_trade_no）
	AmountCents int64  // 金额，单位分
	Subject     string // 商品标题
	Body        string // 商品描述
	NotifyURL   string // 覆盖配置中的回调地址，可为空
	ClientIP    string
}

// Result 支付下单结果。
type Result struct {
	// PayURL 用于跳转或生成二维码的支付地址。
	PayURL string
	// QRCode 微信支付的 code_url，可直接生成二维码。
	QRCode string
	// TradeNo 渠道预支付单号，可为空。
	TradeNo string
}

// Notify 支付回调解析结果。
type Notify struct {
	OrderNo    string
	TradeNo    string
	Amount     int64
	PaidAt     time.Time
	Raw        string
	TradeState string
}

// Gateway 支付网关。
type Gateway interface {
	Kind() string
	Charge(ctx context.Context, in Charge) (*Result, error)
	// VerifyNotify 校验回调签名并解析。body 为原始请求体；query 为 URL 查询参数（支付宝）。
	VerifyNotify(body []byte, query map[string]string) (*Notify, error)
	// SuccessResponse 返回给渠道的成功应答，避免渠道重复通知。
	SuccessResponse() (string, string)
}

var errUnknownProvider = func(s string) error {
	return fmt.Errorf("未知支付渠道: %s", s)
}

// New 构造支付网关。payType 为 wechat / alipay。
func New(payType string, cfg config.Payment, notifyBase string) (Gateway, error) {
	switch strings.ToLower(strings.TrimSpace(payType)) {
	case "wechat":
		if !cfg.Wechat.Active() {
			return nil, errors.New("微信支付未配置完整")
		}
		return newWechat(cfg.Wechat, notifyBase), nil
	case "alipay":
		if !cfg.Alipay.Active() {
			return nil, errors.New("支付宝未配置完整")
		}
		return newAlipay(cfg.Alipay, notifyBase), nil
	default:
		return nil, errUnknownProvider(payType)
	}
}
