// Package utils 汇集无业务耦合的通用工具函数。
package utils

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// ClientIP 返回客户端 IP。代理头只由 Gin 按 SetTrustedProxies 的可信列表解析；
// 直接取 X-Forwarded-For 最左值会让外部请求伪造 IP，绕过白名单和限流。
// trustProxy 参数保留以兼容调用方，实际信任范围由服务端配置决定。
func ClientIP(c *gin.Context, _ bool) string {
	if ip := c.ClientIP(); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}

// BuildTree 将扁平列表组装为树，适用于菜单、部门等层级数据。
// parentOf 返回空字符串或根标记表示顶层。
func BuildTree[T any](items []T, id func(T) string, parentOf func(T) string, setChildren func(T, []T), isRoot func(string) bool) []T {
	children := make(map[string][]T, len(items))
	roots := make([]T, 0, len(items))
	for _, it := range items {
		pid := parentOf(it)
		if pid == "" || isRoot(pid) {
			roots = append(roots, it)
			continue
		}
		children[pid] = append(children[pid], it)
	}
	var attach func(nodes []T)
	attach = func(nodes []T) {
		for i := range nodes {
			kids := children[id(nodes[i])]
			if len(kids) > 0 {
				attach(kids)
				setChildren(nodes[i], kids)
			}
		}
	}
	attach(roots)
	return roots
}

// DefaultIsRoot 判断父级标识是否表示根节点。
func DefaultIsRoot(pid string) bool { return pid == "" || pid == "0" }

// MaskPhone 手机号脱敏：138****8000。
func MaskPhone(phone string) string {
	if len(phone) < 7 {
		return strings.Repeat("*", len(phone))
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

// MaskEmail 邮箱脱敏：ab***@example.com。
func MaskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 1 {
		return "***"
	}
	name := email[:at]
	if len(name) <= 2 {
		return name[:1] + "***" + email[at:]
	}
	return name[:2] + "***" + email[at:]
}

// MaskSecret 保留首尾各 4 位，用于 API Key 展示。
func MaskSecret(s string) string {
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + "****" + s[len(s)-4:]
}

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandomString 生成 n 位密码学安全随机字符串。
func RandomString(n int) string {
	if n <= 0 {
		n = 16
	}
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			b[i] = 'x'
			continue
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}

// RandomToken 生成 n 字节熵的 URL 安全随机令牌。
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return RandomString(n * 2)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// FormatBytes 将字节数转为可读形式。
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	return strconv.FormatFloat(v, 'f', 1, 64) + string("KMGTP"[exp]) + "B"
}

// Truncate 按字符数截断字符串。
func Truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "..."
}

// SafeFilename 清理文件名，防止路径穿越。
func SafeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.ReplaceAll(name, "..", "")
	repl := strings.NewReplacer("/", "_", "\\", "_", "\x00", "")
	name = repl.Replace(name)
	if name == "" || name == "." {
		return "file"
	}
	return name
}

// TodayRange 返回今日 00:00:00 与明日 00:00:00（本地时区）。
func TodayRange(now time.Time) (time.Time, time.Time) {
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 0, 1)
}

// DaysAgo 返回 n 天前的零点。
func DaysAgo(now time.Time, n int) time.Time {
	loc := now.Location()
	d := now.AddDate(0, 0, -n)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// Contains 判断字符串切片是否包含目标。
func Contains(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// UniqueStrings 去重并保持顺序。
func UniqueStrings(list []string) []string {
	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// NoCache 关闭接口缓存。
func NoCache(c *gin.Context) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// IsAjax 判断是否为异步请求。
func IsAjax(c *gin.Context) bool {
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	return strings.Contains(c.GetHeader("Accept"), "application/json")
}

// Int8Or 返回指针指向的值，nil 时返回默认值。
// 用于区分"未提供"与"显式传 0"——后者在状态、排序等字段上有实际含义。
func Int8Or(p *int8, def int8) int8 {
	if p == nil {
		return def
	}
	return *p
}
