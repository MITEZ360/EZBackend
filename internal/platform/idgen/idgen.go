// Package idgen 统一 ID 生成：雪花 ID（字符串形式，避免前端精度丢失）、UUID 与安全随机令牌。
package idgen

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
)

var (
	mu   sync.RWMutex
	node *snowflake.Node
)

// Init 初始化雪花节点（0-1023）。未初始化时按主机名自动推导。
func Init(nodeID int64) error {
	if nodeID < 0 || nodeID > 1023 {
		nodeID = NodeFromHost()
	}
	n, err := snowflake.NewNode(nodeID)
	if err != nil {
		return err
	}
	mu.Lock()
	node = n
	mu.Unlock()
	return nil
}

// NodeFromHost 依据主机名哈希推导节点号，适配容器化部署。
func NodeFromHost() int64 {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "keel"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	return int64(h.Sum32() % 1024)
}

func getNode() *snowflake.Node {
	mu.RLock()
	n := node
	mu.RUnlock()
	if n != nil {
		return n
	}
	n, _ = snowflake.NewNode(NodeFromHost())
	mu.Lock()
	node = n
	mu.Unlock()
	return n
}

// Next 返回十进制字符串 ID。
func Next() string { return getNode().Generate().String() }

// NextInt 返回 int64 ID。
func NextInt() int64 { return getNode().Generate().Int64() }

// UUID 返回标准 UUID v4。
func UUID() string { return uuid.NewString() }

// UUIDv7 返回按时间有序的 UUID v7。
func UUIDv7() string {
	u, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return u.String()
}

// Token 返回 n 字节熵的 URL 安全随机串。
func Token(n int) string {
	if n <= 0 {
		n = 32
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// OrderNo 生成业务单号：前缀 + 时间 + 4 位随机。
func OrderNo(prefix string) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%s%02X%02X", prefix, time.Now().Format("20060102150405"), b[0], b[1])
}
