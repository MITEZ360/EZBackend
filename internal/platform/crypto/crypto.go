// Package crypto 提供对称加密（AES-256-GCM）、摘要与签名工具，用于敏感配置与二次验证密钥落库加密。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

const prefix = "enc:v1:"

func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// Encrypt 使用 AES-256-GCM 加密，输出 enc:v1:base64(nonce|ciphertext)。
func Encrypt(plain, secret string) (string, error) {
	if plain == "" {
		return "", nil
	}
	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// Decrypt 解密 Encrypt 的输出；无前缀内容原样返回，便于平滑迁移历史明文数据。
func Decrypt(cipherText, secret string) (string, error) {
	if cipherText == "" {
		return "", nil
	}
	if !strings.HasPrefix(cipherText, prefix) {
		return cipherText, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(cipherText, prefix))
	if err != nil {
		return "", errors.New("密文格式非法")
	}
	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度非法")
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", errors.New("解密失败，密钥可能已变更")
	}
	return string(plain), nil
}

// SHA256 返回十六进制摘要。
func SHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// HMAC 返回 HMAC-SHA256 十六进制签名。
func HMAC(secret, data string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// VerifyHMAC 恒定时间比较签名。
func VerifyHMAC(secret, data, sig string) bool {
	want := HMAC(secret, data)
	return subtle.ConstantTimeCompare([]byte(want), []byte(sig)) == 1
}

// RandomBytes 生成随机字节。
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// AESKey 生成 32 字节密钥的 base64 表示，供运维初始化使用。
func AESKey() string {
	return base64.RawStdEncoding.EncodeToString(RandomBytes(32))
}
