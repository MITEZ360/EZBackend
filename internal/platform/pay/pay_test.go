package pay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/keel-framework/keel/internal/platform/config"
)

func TestSignMap(t *testing.T) {
	tests := []struct {
		name string
		sign func(map[string]string, string) string
		want string
	}{
		{
			name: "MD5",
			sign: SignMapMD5,
			want: "9F565CCD686CFA5DC3B06B3A89E4E3AD",
		},
		{
			name: "HMAC-SHA256",
			sign: SignMapHMACSHA256,
			want: "3C1BCC36E976DED8AABBE1901A67B59016DFFCAFF3370EC35396FD4D664440A4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := map[string]string{"b": "2", "a": "1", "empty": "", "sign": "IGNORED"}
			got := tt.sign(m, "secret")
			if got != tt.want {
				t.Fatalf("签名 = %q, 期望 %q", got, tt.want)
			}
			if again := tt.sign(m, "secret"); again != got {
				t.Fatalf("签名不稳定: %q != %q", again, got)
			}
		})
	}
}

func TestSignStringExclusions(t *testing.T) {
	m := map[string]string{"b": "2", "a": "1", "sign": "X", "sign_type": "RSA2", "empty": ""}
	if got, want := signString(m), "a=1&b=2&sign_type=RSA2"; got != want {
		t.Fatalf("signString() = %q, 期望 %q", got, want)
	}
	if got, want := signString(m, "sign_type"), "a=1&b=2"; got != want {
		t.Fatalf("signString(排除 sign_type) = %q, 期望 %q", got, want)
	}
}

func TestRSASignVerify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	privPKCS1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	privPKCS8DER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	privPKCS8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privPKCS8DER})

	pubPKIXDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPKCS1DER := x509.MarshalPKCS1PublicKey(&key.PublicKey)
	publicKeys := []struct {
		name string
		pem  string
	}{
		{"标准 PUBLIC KEY", string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubPKIXDER}))},
		{"PKCS#1 PEM", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: pubPKCS1DER}))},
		{"支付宝自定义头", "-----BEGIN ALIPAY PUBLIC KEY-----\n" + base64.StdEncoding.EncodeToString(pubPKCS1DER) + "\n-----END ALIPAY PUBLIC KEY-----"},
		{"裸 base64", base64.StdEncoding.EncodeToString(pubPKCS1DER)},
	}

	const data = "a=1&b=2"
	for _, priv := range []struct {
		name string
		pem  string
	}{{"PKCS#1", string(privPKCS1)}, {"PKCS#8", string(privPKCS8)}} {
		sign, err := RSASign(data, priv.pem)
		if err != nil {
			t.Fatalf("RSASign(%s): %v", priv.name, err)
		}
		for _, pub := range publicKeys {
			t.Run(priv.name+"/"+pub.name, func(t *testing.T) {
				if err := RSAVerify(data, sign, pub.pem); err != nil {
					t.Fatalf("验签失败: %v", err)
				}
				if err := RSAVerify(data+"!", sign, pub.pem); err == nil {
					t.Fatal("篡改数据后验签仍通过")
				}
			})
		}
		if _, err := RSASign(data, "bad key"); err == nil {
			t.Fatal("非法私钥应返回错误")
		}
		if err := RSAVerify(data, "not-base64", publicKeys[0].pem); err == nil {
			t.Fatal("非法签名应返回错误")
		}
	}
}

func TestXMLRoundTrip(t *testing.T) {
	input := `<xml><return_code><![CDATA[SUCCESS]]></return_code><body><![CDATA[a < b & c]]></body></xml>`
	m, err := XMLToMap([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if m["return_code"] != "SUCCESS" || m["body"] != "a < b & c" {
		t.Fatalf("解析结果不正确: %#v", m)
	}
	if _, err := XMLToMap([]byte(`<xml><broken>`)); err == nil {
		t.Fatal("非法 XML 应返回错误")
	}

	built := MapToXML(map[string]string{"b": "2", "a": "1]]><x", "empty": ""})
	if !strings.HasPrefix(built, "<xml><a><![CDATA[") || !strings.HasSuffix(built, "</xml>") {
		t.Fatalf("构造的 XML 不正确: %s", built)
	}
	back, err := XMLToMap([]byte(built))
	if err != nil {
		t.Fatal(err)
	}
	if back["a"] != "1]]><x" || back["b"] != "2" {
		t.Fatalf("CDATA 内特殊字符往返丢失: %#v", back)
	}
}

func TestWechatVerifyNotify(t *testing.T) {
	tests := []struct {
		name     string
		signType string
		signFn   func(map[string]string, string) string
	}{
		{"MD5", "MD5", SignMapMD5},
		{"HMAC-SHA256", "HMAC-SHA256", SignMapHMACSHA256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := newWechat(config.WechatPay{AppID: "app", MchID: "mch", APIKey: "secret", SignType: tt.signType}, "")
			m := map[string]string{
				"return_code":  "SUCCESS",
				"sign_type":    tt.signType,
				"out_trade_no": "ORDER-1",
				"trade_no":     "WX-1",
				"total_fee":    "1234",
				"time_end":     "20261124153000",
			}
			m["sign"] = tt.signFn(m, "secret")
			n, err := gw.VerifyNotify([]byte(MapToXML(m)), nil)
			if err != nil {
				t.Fatal(err)
			}
			if n.OrderNo != "ORDER-1" || n.TradeNo != "WX-1" || n.Amount != 1234 || n.TradeState != "SUCCESS" {
				t.Fatalf("解析结果不正确: %#v", n)
			}
			want := time.Date(2026, 11, 24, 15, 30, 0, 0, chinaTimeZone)
			if !n.PaidAt.Equal(want) {
				t.Fatalf("PaidAt = %v, 期望 %v", n.PaidAt, want)
			}

			tampered := map[string]string{}
			for k, v := range m {
				tampered[k] = v
			}
			tampered["total_fee"] = "1"
			if _, err := gw.VerifyNotify([]byte(MapToXML(tampered)), nil); err == nil {
				t.Fatal("签名被破坏后仍验签通过")
			}
		})
	}

	gw := newWechat(config.WechatPay{AppID: "app", MchID: "mch", APIKey: "secret"}, "")
	bad := map[string]string{"return_code": "FAIL", "return_msg": "签名错误"}
	if _, err := gw.VerifyNotify([]byte(MapToXML(bad)), nil); err == nil {
		t.Fatal("return_code 非 SUCCESS 应返回错误")
	}
	ct, body := gw.SuccessResponse()
	if ct != "text/xml" || body != `<xml><return_code><![CDATA[SUCCESS]]></return_code><return_msg><![CDATA[OK]]></return_msg></xml>` {
		t.Fatalf("成功应答不正确: %q %q", ct, body)
	}
}

func TestAlipayChargeAndVerifyNotify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))

	cfg := config.AlipayPay{
		Enabled: true, AppID: "2021000000000000",
		PrivateKey: privPEM, PublicKey: pubPEM,
		NotifyURL: "https://example.com/pay/notify/alipay", ReturnURL: "https://example.com/done",
	}
	gw := newAlipay(cfg, "https://example.com/pay/notify")
	res, err := gw.Charge(context.Background(), Charge{OrderNo: "ORDER-2", AmountCents: 1234, Subject: "测试商品", Body: "描述"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(res.PayURL)
	if err != nil || u.Host != "openapi.alipay.com" {
		t.Fatalf("PayURL 不正确: %s (%v)", res.PayURL, err)
	}
	values := u.Query()
	sign := values.Get("sign")
	values.Del("sign")
	if sign == "" || !strings.Contains(u.RawQuery, "sign=") {
		t.Fatalf("PayURL 缺少 sign: %s", res.PayURL)
	}
	params := make(map[string]string)
	for k, v := range values {
		params[k] = v[0]
	}
	if err := RSAVerify(signString(params), sign, pubPEM); err != nil {
		t.Fatalf("下单签名校验失败: %v", err)
	}
	if params["method"] != alipayMethod || params["sign_type"] != "RSA2" ||
		!strings.Contains(params["biz_content"], `"total_amount":"12.34"`) {
		t.Fatalf("公共参数或 biz_content 不正确: %#v", params)
	}

	query := map[string]string{
		"app_id": "2021000000000000", "out_trade_no": "ORDER-2", "trade_no": "ALI-2",
		"total_amount": "12.34", "trade_status": "TRADE_SUCCESS", "gmt_payment": "2026-11-24 15:30:00",
		"sign_type": "RSA2",
	}
	query["sign"], err = RSASign(signString(query, "sign_type"), privPEM)
	if err != nil {
		t.Fatal(err)
	}
	n, err := gw.VerifyNotify(nil, query)
	if err != nil {
		t.Fatal(err)
	}
	if n.OrderNo != "ORDER-2" || n.TradeNo != "ALI-2" || n.Amount != 1234 || n.TradeState != "TRADE_SUCCESS" {
		t.Fatalf("解析结果不正确: %#v", n)
	}
	if n.Raw != signString(query, "sign_type") {
		t.Fatalf("Raw 不正确: %s", n.Raw)
	}
	if want := time.Date(2026, 11, 24, 15, 30, 0, 0, chinaTimeZone); !n.PaidAt.Equal(want) {
		t.Fatalf("PaidAt = %v, 期望 %v", n.PaidAt, want)
	}

	tampered := make(map[string]string)
	for k, v := range query {
		tampered[k] = v
	}
	tampered["total_amount"] = "0.01"
	if _, err := gw.VerifyNotify(nil, tampered); err == nil {
		t.Fatal("篡改回调后仍验签通过")
	}
	query["trade_status"] = "WAIT_BUYER_PAY"
	if _, err := gw.VerifyNotify(nil, query); err == nil {
		t.Fatal("未完成交易应返回错误")
	}
	ct, body := gw.SuccessResponse()
	if ct != "text/plain" || body != "success" {
		t.Fatalf("成功应答不正确: %q %q", ct, body)
	}

	sandbox := newAlipay(config.AlipayPay{Enabled: true, AppID: "a", PrivateKey: privPEM, PublicKey: pubPEM, Sandbox: true}, "")
	res, err = sandbox.Charge(context.Background(), Charge{OrderNo: "O", AmountCents: 100})
	if err != nil || !strings.HasPrefix(res.PayURL, "https://openapi.alipaydev.com/gateway.do?") {
		t.Fatalf("沙箱网关地址不正确: %s (%v)", res.PayURL, err)
	}
}

func TestNewDispatch(t *testing.T) {
	wechatCfg := config.Payment{Wechat: config.WechatPay{Enabled: true, AppID: "app", MchID: "mch", APIKey: "secret"}}
	alipayCfg := config.Payment{Alipay: config.AlipayPay{Enabled: true, AppID: "app", PrivateKey: "priv", PublicKey: "pub"}}
	tests := []struct {
		name    string
		typ     string
		cfg     config.Payment
		want    string
		wantErr bool
	}{
		{"微信", "wechat", wechatCfg, "wechat", false},
		{"大小写与空格", " WeChat ", wechatCfg, "wechat", false},
		{"支付宝", "alipay", alipayCfg, "alipay", false},
		{"未知渠道", "unionpay", wechatCfg, "", true},
		{"微信未配置", "wechat", config.Payment{}, "", true},
		{"支付宝未配置", "alipay", config.Payment{}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, err := New(tt.typ, tt.cfg, "https://example.com/notify")
			if (err != nil) != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && gw.Kind() != tt.want {
				t.Fatalf("Kind() = %q, 期望 %q", gw.Kind(), tt.want)
			}
		})
	}
	if gw := newWechat(wechatCfg.Wechat, "https://example.com/notify"); gw.Kind() != "wechat" {
		t.Fatal("newWechat 返回类型错误")
	}
	if gw := newAlipay(alipayCfg.Alipay, "https://example.com/notify"); gw.Kind() != "alipay" {
		t.Fatal("newAlipay 返回类型错误")
	}
}
