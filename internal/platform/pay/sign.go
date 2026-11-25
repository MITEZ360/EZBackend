package pay

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// signString 按 key 字典序拼接待签串：k=v&k2=v2。
// 空值与 sign 字段不参与签名；exclude 可追加额外排除项。
func signString(m map[string]string, exclude ...string) string {
	skip := map[string]bool{"sign": true}
	for _, k := range exclude {
		skip[k] = true
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if skip[k] || m[k] == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "&")
}

// SignMapMD5 微信支付 V2 的 MD5 签名，返回大写十六进制串。
func SignMapMD5(m map[string]string, apiKey string) string {
	s := signString(m) + "&key=" + apiKey
	sum := md5.Sum([]byte(s))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// SignMapHMACSHA256 微信支付 V2 的 HMAC-SHA256 签名，返回大写十六进制串。
func SignMapHMACSHA256(m map[string]string, apiKey string) string {
	s := signString(m) + "&key=" + apiKey
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(s))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

// RSASign 用 RSA 私钥对 data 做 SHA256withRSA 签名，返回 base64 编码的签名值。
// 私钥支持 PKCS#1（RSA PRIVATE KEY）与 PKCS#8（PRIVATE KEY）PEM。
func RSASign(data, privateKeyPEM string) (string, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(data))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", fmt.Errorf("RSA 签名失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// RSAVerify 用 RSA 公钥验证 SHA256withRSA 签名。
// 公钥支持标准 PEM（PUBLIC KEY / RSA PUBLIC KEY / ALIPAY PUBLIC KEY）以及
// 支付宝常见的无 PEM 头裸 base64 文本。
func RSAVerify(data, signature, publicKeyPEM string) error {
	key, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return fmt.Errorf("签名不是合法的 base64: %w", err)
	}
	h := sha256.Sum256([]byte(data))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, h[:], sig); err != nil {
		return fmt.Errorf("RSA 验签失败: %w", err)
	}
	return nil
}

func parsePrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("私钥不是合法的 PEM 文本")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败（需 PKCS#1 或 PKCS#8）: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("私钥不是 RSA 类型")
	}
	return key, nil
}

func parsePublicKey(pub string) (*rsa.PublicKey, error) {
	clean := strings.TrimSpace(pub)
	if clean == "" {
		return nil, errors.New("支付宝公钥为空")
	}
	block, _ := pem.Decode([]byte(clean))
	if block == nil {
		// 支付宝开放平台常给出不带 PEM 头的裸 base64 公钥。
		der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(clean), ""))
		if err != nil {
			return nil, fmt.Errorf("公钥既不是 PEM 也不是合法的 base64: %w", err)
		}
		return parsePublicDER(der)
	}
	return parsePublicDER(block.Bytes)
}

func parsePublicDER(der []byte) (*rsa.PublicKey, error) {
	// 标准 X.509 SubjectPublicKeyInfo（-----BEGIN PUBLIC KEY-----）。
	if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		if key, ok := parsed.(*rsa.PublicKey); ok {
			return key, nil
		}
	}
	// 支付宝工具生成的公钥常见 PKCS#1 编码，可能带自定义 PEM 头。
	if key, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("无法解析 RSA 公钥（需 PKIX 或 PKCS#1）")
}

// XMLToMap 将微信回调这类扁平 XML（含 CDATA）解析为 map。
func XMLToMap(data []byte) (map[string]string, error) {
	m := make(map[string]string)
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 XML 失败: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
		case xml.CharData:
			if len(stack) > 0 {
				m[stack[len(stack)-1]] += string(t)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return m, nil
}

// MapToXML 将 map 构造成微信支付 V2 要求的 <xml><k><![CDATA[v]]></k>...</xml>。
// key 按字典序输出，保证结果稳定。
func MapToXML(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<xml>")
	for _, k := range keys {
		b.WriteByte('<')
		b.WriteString(k)
		b.WriteString("><![CDATA[")
		b.WriteString(strings.ReplaceAll(m[k], "]]>", "]]]]><![CDATA[>"))
		b.WriteString("]]></")
		b.WriteString(k)
		b.WriteByte('>')
	}
	b.WriteString("</xml>")
	return b.String()
}
