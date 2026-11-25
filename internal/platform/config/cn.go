package config

import (
	"strings"
	"time"
)

// CN 中国大陆环境功能参数。本分支特有，宿主分支不含这些能力。
type CN struct {
	// RegisterEnabled 开放用户自助注册。默认关闭，需显式开启。
	RegisterEnabled bool `mapstructure:"register_enabled"`
	// SMSLoginEnabled 开放短信验证码登录/注册。
	SMSLoginEnabled bool `mapstructure:"sms_login_enabled"`
	// DefaultRoles 自助注册用户默认角色编码。
	DefaultRoles []string `mapstructure:"default_roles"`
	// CodeTTL 短信验证码有效期。
	CodeTTL time.Duration `mapstructure:"code_ttl"`
	// CodeLength 短信验证码位数。
	CodeLength int `mapstructure:"code_length"`
	// DailySendLimit 同一手机号每日发送上限。
	DailySendLimit int `mapstructure:"daily_send_limit"`
}

// SMS 短信网关参数。provider 支持 aliyun / tencent / mock。
// mock 仅用于本地开发：验证码写入日志，不真实发送。
type SMS struct {
	Enabled         *bool  `mapstructure:"enabled"`
	Provider        string `mapstructure:"provider"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	AccessKeySecret string `mapstructure:"access_key_secret"`
	SignName        string `mapstructure:"sign_name"`
	TemplateCode    string `mapstructure:"template_code"`
	Region          string `mapstructure:"region"`
	Endpoint        string `mapstructure:"endpoint"`
}

// Use 判断是否启用短信服务：显式关闭优先，其次必须有 provider。
func (s SMS) Use() bool {
	if s.Enabled != nil && !*s.Enabled {
		return false
	}
	return strings.TrimSpace(s.Provider) != ""
}

// Payment 支付网关参数。
type Payment struct {
	Enabled bool      `mapstructure:"enabled"`
	Wechat  WechatPay `mapstructure:"wechat"`
	Alipay  AlipayPay `mapstructure:"alipay"`
}

// WechatPay 微信支付 V2（Native 扫码）参数。
type WechatPay struct {
	Enabled   bool   `mapstructure:"enabled"`
	AppID     string `mapstructure:"app_id"`
	MchID     string `mapstructure:"mch_id"`
	APIKey    string `mapstructure:"api_key"`
	NotifyURL string `mapstructure:"notify_url"`
	// SignType 支持 MD5 与 HMAC-SHA256。
	SignType string `mapstructure:"sign_type"`
	Sandbox  bool   `mapstructure:"sandbox"`
}

// Active 判断微信支付是否可用。
func (w WechatPay) Active() bool {
	return w.Enabled && w.AppID != "" && w.MchID != "" && w.APIKey != ""
}

// AlipayPay 支付宝开放平台参数。私钥/公钥为 PEM 文本（支持 PKCS#1 与 PKCS#8）。
type AlipayPay struct {
	Enabled    bool   `mapstructure:"enabled"`
	AppID      string `mapstructure:"app_id"`
	PrivateKey string `mapstructure:"private_key"`
	PublicKey  string `mapstructure:"public_key"`
	NotifyURL  string `mapstructure:"notify_url"`
	ReturnURL  string `mapstructure:"return_url"`
	Sandbox    bool   `mapstructure:"sandbox"`
}

// Active 判断支付宝是否可用。
func (a AlipayPay) Active() bool {
	return a.Enabled && a.AppID != "" && a.PrivateKey != "" && a.PublicKey != ""
}
