// Package gov 是服务治理面的运行时库：快照、路由匹配、负载均衡、限流与熔断。
//
// 它不依赖 internal 下的任何包，因为控制面模块与网关（含未来的独立
// tenon-gateway 二进制）都要用它。所有类型都是纯数据结构，可安全地在
// 进程间用 JSON 传输。
package gov

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync/atomic"
)

// 分区键。数据面按分区判断自己缺哪一块，轮询只比全局号。
const (
	PartRoute    = "route"
	PartRegistry = "registry"
	// PartConfigPrefix 后接命名空间，如 config:default。
	PartConfigPrefix = "config:"
)

// Snapshot 是一次完整的治理配置视图。
//
// 它必须不可变：任何更新都构造新对象后整体替换，绝不原地修改字段，
// 否则正在处理请求的 goroutine 会读到半新半旧的状态。
type Snapshot struct {
	GlobalRev int64           `json:"globalRev"`
	Parts     map[string]Part `json:"parts"`
}

// Part 是一个分区的载荷。Payload 保持原始 JSON，由消费方按需解码一次。
type Part struct {
	Rev      int64           `json:"rev"`
	Checksum string          `json:"checksum"`
	Payload  json.RawMessage `json:"payload"`
}

// Route 是控制面编译后的转发规则。
type Route struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Priority int               `json:"priority"`
	Prefix   string            `json:"prefix"`
	Methods  []string          `json:"methods,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Query    map[string]string `json:"query,omitempty"`

	Service   string `json:"service"`
	Version   string `json:"version,omitempty"`
	Weight    int    `json:"weight"`
	Strip     bool   `json:"strip"`
	TimeoutMs int    `json:"timeoutMs"`
	Retries   int    `json:"retries"`
}

// RoutePayload 是 route 分区的载荷。
type RoutePayload struct {
	Rev    int64   `json:"rev"`
	Routes []Route `json:"routes"`
}

// Instance 是服务实例的运行时视图。
type Instance struct {
	ID      string            `json:"id"`
	Addr    string            `json:"addr"`
	Version string            `json:"version,omitempty"`
	Zone    string            `json:"zone,omitempty"`
	Weight  int               `json:"weight"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// Service 是一个服务及其在线实例。
type Service struct {
	Name      string     `json:"name"`
	Instances []Instance `json:"instances"`
}

// RegistryPayload 是 registry 分区的载荷。
type RegistryPayload struct {
	Rev      int64              `json:"rev"`
	Services map[string]Service `json:"services"`
}

// ConfigItem 是配置中心下发给进程的一项配置。
type ConfigItem struct {
	Value     string `json:"value"`
	Type      string `json:"type"`
	Encrypted bool   `json:"encrypted"`
}

// ConfigPayload 是一个命名空间的配置视图。
type ConfigPayload struct {
	Rev       int64                 `json:"rev"`
	Namespace string                `json:"namespace"`
	Items     map[string]ConfigItem `json:"items"`
}

// Checksum 返回字节串的 SHA-256，用于校验快照完整性。
func Checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Fingerprint 是整个快照内容的指纹，由"分区键 + 分区校验和"排序后算出。
//
// 数据面用它判断"变了没有"，而不是用全局版本号。原因有两条：
//  1. registry 分区的内容会随时间变化——实例超时下线不需要任何写操作，
//     版本号不会跟着动，拿版本号做 304 判据会让数据面永远看到旧实例表；
//  2. 发布不改变内容时（例如原样重发一份路由）不应该波及所有数据面。
//
// 指纹只覆盖内容，不覆盖版本号，所以"版本号变了但内容一样"就是 304。
func (s *Snapshot) Fingerprint() string {
	if s == nil {
		return ""
	}
	keys := make([]string, 0, len(s.Parts))
	for k := range s.Parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(s.Parts[k].Checksum))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Store 是进程内的原子快照容器。
//
// 读路径只有一次 atomic load，没有锁、没有拷贝，这是网关能保持低延迟的前提。
type Store struct {
	ptr atomic.Pointer[Snapshot]
}

// Load 返回当前快照；尚未加载时返回空快照而不是 nil，调用方无需判空。
func (s *Store) Load() *Snapshot {
	if p := s.ptr.Load(); p != nil {
		return p
	}
	return &Snapshot{Parts: map[string]Part{}}
}

// Store 原子替换快照。
func (s *Store) Store(v *Snapshot) {
	if v == nil {
		return
	}
	if v.Parts == nil {
		v.Parts = map[string]Part{}
	}
	s.ptr.Store(v)
}

// Part 取出指定分区，不存在时返回 false。
func (s *Snapshot) Part(key string) (Part, bool) {
	if s == nil || s.Parts == nil {
		return Part{}, false
	}
	p, ok := s.Parts[key]
	return p, ok
}

// Routes 解码 route 分区。解码失败说明快照损坏，调用方应保留旧视图。
func (s *Snapshot) Routes() (RoutePayload, bool) {
	p, ok := s.Part(PartRoute)
	if !ok {
		return RoutePayload{}, false
	}
	var out RoutePayload
	if err := json.Unmarshal(p.Payload, &out); err != nil {
		return RoutePayload{}, false
	}
	return out, true
}

// Registry 解码 registry 分区。
func (s *Snapshot) Registry() (RegistryPayload, bool) {
	p, ok := s.Part(PartRegistry)
	if !ok {
		return RegistryPayload{}, false
	}
	var out RegistryPayload
	if err := json.Unmarshal(p.Payload, &out); err != nil {
		return RegistryPayload{}, false
	}
	return out, true
}

// Config 解码指定命名空间。
func (s *Snapshot) Config(namespace string) (ConfigPayload, bool) {
	p, ok := s.Part(PartConfigPrefix + namespace)
	if !ok {
		return ConfigPayload{}, false
	}
	var out ConfigPayload
	if err := json.Unmarshal(p.Payload, &out); err != nil {
		return ConfigPayload{}, false
	}
	return out, true
}
