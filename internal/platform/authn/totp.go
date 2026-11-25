package authn

import (
	"crypto/rand"
	"encoding/base64"
	"strings"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/MITEZ360/tenon/internal/platform/crypto"
)

// TOTP 参数固定为 30 秒 / 6 位 / SHA1。
// 这是 RFC 6238 的默认组合，Google Authenticator、Microsoft Authenticator、
// Duo、Bitwarden、1Password、Authy 等客户端不额外配置即可直接识别。
const (
	totpPeriod = 30
)

// GenerateTOTP 为用户创建 TOTP 密钥，返回密钥与 otpauth 链接。
func GenerateTOTP(issuer, account string) (secret, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTP 校验动态验证码，容忍前后各一个时间窗（约 ±30 秒时钟漂移）。
func ValidateTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if secret == "" || len(code) != 6 {
		return false
	}
	return totp.Validate(code, secret)
}

// QRCodeDataURI 把 otpauth 链接渲染成 PNG 的 data URI，前端可直接放进 <img src>。
// 不落盘、不外链，离线环境也能扫码。
func QRCodeDataURI(content string) (string, error) {
	png, err := qrcode.Encode(content, qrcode.Medium, 240)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// recoveryAlphabet 去掉 I、O 两个易混字符，保留 32 个符号：
// 与 256 整除，取模无偏；配合分组连字符，人工抄写不易出错。
const recoveryAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

const (
	// RecoveryCodeCount 每次生成的恢复码数量。
	RecoveryCodeCount = 10
	// RecoveryCodeLength 去掉分隔符后的恢复码长度。
	RecoveryCodeLength = 10
)

// GenerateRecoveryCodes 生成一次性恢复码，明文形如 7K3M9-QP2XR。
// 仅在启用两步验证时展示一次，服务端只保存哈希。
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		n = RecoveryCodeCount
	}
	codes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, RecoveryCodeLength)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		var b strings.Builder
		b.Grow(RecoveryCodeLength + 1)
		for j, v := range buf {
			if j == RecoveryCodeLength/2 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
		}
		codes = append(codes, b.String())
	}
	return codes, nil
}

// NormalizeRecoveryCode 统一大小写并去掉空格与连字符，允许用户随意输入。
func NormalizeRecoveryCode(code string) string {
	replacer := strings.NewReplacer("-", "", " ", "", "\t", "")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(code)))
}

// HashRecoveryCode 用带密钥的 HMAC 保存恢复码。
// 恢复码本身有 50 bit 熵（32^10），离线爆破不可行；登录时要逐条比对，
// 用 bcrypt 这类慢哈希会让每次登录多出数百毫秒，因此选择 HMAC-SHA256。
func HashRecoveryCode(code, key string) string {
	return crypto.HMAC(key, NormalizeRecoveryCode(code))
}
