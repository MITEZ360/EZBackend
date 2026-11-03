package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	apiKeyAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// APIKeyPrefixLen 是列表中展示的可识别前缀长度（含 ak_x_ 头）。
	APIKeyPrefixLen = 12
)

// GenerateAPIKey 生成新密钥，返回明文、可读前缀与入库哈希。
//
// head 与 size 由调用方按凭证类型决定，本函数只负责格式与熵，
// 不关心 service / client 之类的业务语义。
// 明文只在创建与轮换时返回一次，数据库只保存前缀与哈希，任何接口都无法再读回。
func GenerateAPIKey(head string, size int) (plain, prefix, hash string) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		// 熵源不可用时继续生成等于产出可预测密钥，必须直接中止。
		panic("authn: 无法读取系统随机源: " + err.Error())
	}
	var sb strings.Builder
	sb.Grow(len(head) + size)
	sb.WriteString(head)
	for _, v := range b {
		// 256 % 62 = 8，字符分布有极小偏置（约 0.3%），
		// 对 40 字符密钥的熵几乎没有影响，换取实现足够简单。
		sb.WriteByte(apiKeyAlphabet[int(v)%len(apiKeyAlphabet)])
	}
	plain = sb.String()
	if len(plain) < APIKeyPrefixLen {
		return plain, plain, HashAPIKey(plain)
	}
	return plain, plain[:APIKeyPrefixLen], HashAPIKey(plain)
}

// HashAPIKey 计算入库哈希。密钥本身由系统生成且熵足够高，
// 不需要加盐与慢哈希——加盐反而无法再用唯一索引直接定位。
func HashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(sum[:])
}
