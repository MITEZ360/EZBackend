package gov

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
)

// EncPrefix 标记密文值。配置项落库时加这个前缀，接口永远不回显明文。
const EncPrefix = "enc:"

// MasterKey 从环境变量读取主密钥；为空表示未配置加密能力。
func MasterKey() string { return strings.TrimSpace(os.Getenv("TENON_MASTER_KEY")) }

// Encrypt 用 AES-256-GCM 加密，返回 enc:<base64(nonce+cipher)>。
//
// 密钥由主密钥经 SHA-256 派生，避免要求运维提供恰好 32 字节的原始密钥。
func Encrypt(plain, key string) (string, error) {
	if key == "" {
		return "", errors.New("未配置主密钥 TENON_MASTER_KEY，无法加密")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return EncPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt 解密 Encrypt 的产物；非密文原样返回，便于历史数据平滑迁移。
func Decrypt(v, key string) (string, error) {
	if !strings.HasPrefix(v, EncPrefix) {
		return v, nil
	}
	if key == "" {
		return "", errors.New("配置项已加密，但未配置 TENON_MASTER_KEY")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, EncPrefix))
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度非法")
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Masked 返回密文的展示形态，接口不会把明文或完整密文吐出去。
func Masked(v string) string {
	if strings.HasPrefix(v, EncPrefix) {
		return "******"
	}
	return v
}

func newGCM(key string) (cipher.AEAD, error) {
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
