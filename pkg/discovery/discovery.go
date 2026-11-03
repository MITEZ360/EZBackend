// Package discovery 提供服务注册与发现能力，屏蔽静态配置、DNS 等不同实现。
//
// 设计目标：
//   - 单体模式（mono）无需注册中心，进程内直连；
//   - 微服务模式（service/gateway）通过 Registry 解析实例列表；
//   - 接口只保留 Resolve/Close 两个方法，便于接入 Consul/Nacos/Etcd 等实现。
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Instance 是一个服务实例。
type Instance struct {
	ID       string            `json:"id"`
	Service  string            `json:"service"`
	Addr     string            `json:"addr"` // host:port
	Weight   int               `json:"weight"`
	Meta     map[string]string `json:"meta,omitempty"`
	Healthy  bool              `json:"healthy"`
	LastSeen time.Time         `json:"lastSeen"`
}

// URL 返回实例的 HTTP 地址。
func (i Instance) URL(scheme string) string {
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + i.Addr
}

// Registry 服务发现接口。
type Registry interface {
	// Name 返回实现名称，用于日志与监控。
	Name() string
	// Resolve 返回指定服务的可用实例列表。
	Resolve(ctx context.Context, service string) ([]Instance, error)
	// Close 释放资源。
	Close() error
}

// ErrNotFound 表示服务未注册。
var ErrNotFound = errors.New("服务未注册")

// ---------------------------------------------------------------------------
// 静态注册表：从配置文件读取 service=addr1,addr2 形式的映射
// ---------------------------------------------------------------------------

// Static 基于配置的静态注册表，适用于小规模部署或 K8s 之外的环境。
type Static struct {
	mu       sync.RWMutex
	services map[string][]Instance
}

// NewStatic 解析形如 ["keel-core=127.0.0.1:9000", "keel-auth=10.0.0.1:9001,10.0.0.2:9001"] 的条目。
func NewStatic(entries []string) (*Static, error) {
	s := &Static{services: make(map[string][]Instance)}
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		name, addrs, ok := strings.Cut(e, "=")
		if !ok {
			return nil, fmt.Errorf("服务发现配置格式错误（应为 service=addr1,addr2）: %q", e)
		}
		name = strings.TrimSpace(name)
		var list []Instance
		for _, a := range strings.Split(addrs, ",") {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if _, _, err := net.SplitHostPort(a); err != nil {
				return nil, fmt.Errorf("服务 %s 的实例地址非法: %q", name, a)
			}
			list = append(list, Instance{
				ID: name + "@" + a, Service: name, Addr: a, Weight: 1, Healthy: true, LastSeen: time.Now(),
			})
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("服务 %s 未配置实例地址", name)
		}
		s.services[name] = list
	}
	return s, nil
}

// Name 实现 Registry。
func (s *Static) Name() string { return "static" }

// Resolve 实现 Registry。
func (s *Static) Resolve(_ context.Context, service string) ([]Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list, ok := s.services[service]
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, service)
	}
	out := make([]Instance, len(list))
	copy(out, list)
	return out, nil
}

// Close 实现 Registry。
func (s *Static) Close() error { return nil }

// ---------------------------------------------------------------------------
// DNS 注册表：K8s Headless Service / 内网 DNS 场景
// ---------------------------------------------------------------------------

// DNS 通过 DNS 解析获取实例地址，suffix 形如 "svc.cluster.local:9000"。
// 解析 service.suffix 的 A 记录，端口取自 suffix 中的端口。
type DNS struct {
	suffix string // 形如 default.svc.cluster.local:9000
	host   string
	port   string
	r      *net.Resolver
	ttl    time.Duration

	mu    sync.RWMutex
	cache map[string]dnsEntry
}

type dnsEntry struct {
	list  []Instance
	at    time.Time
	valid bool
}

// NewDNS 创建 DNS 注册表。
func NewDNS(suffix string, ttl time.Duration) (*DNS, error) {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return nil, errors.New("micro.registry.dns_suffix 不能为空")
	}
	host, port, err := net.SplitHostPort(suffix)
	if err != nil {
		host, port = suffix, "80"
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &DNS{suffix: suffix, host: host, port: port, r: net.DefaultResolver, ttl: ttl,
		cache: make(map[string]dnsEntry)}, nil
}

// Name 实现 Registry。
func (d *DNS) Name() string { return "dns" }

// Resolve 实现 Registry。
func (d *DNS) Resolve(ctx context.Context, service string) ([]Instance, error) {
	d.mu.RLock()
	e, ok := d.cache[service]
	d.mu.RUnlock()
	if ok && time.Since(e.at) < d.ttl {
		if !e.valid || len(e.list) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, service)
		}
		return e.list, nil
	}

	fqdn := service + "." + d.host
	ips, err := d.r.LookupHost(ctx, fqdn)
	if err != nil {
		d.mu.Lock()
		d.cache[service] = dnsEntry{at: time.Now(), valid: false}
		d.mu.Unlock()
		return nil, fmt.Errorf("%w: %s (%v)", ErrNotFound, fqdn, err)
	}
	list := make([]Instance, 0, len(ips))
	for _, ip := range ips {
		addr := net.JoinHostPort(ip, d.port)
		list = append(list, Instance{
			ID: service + "@" + addr, Service: service, Addr: addr,
			Weight: 1, Healthy: true, LastSeen: time.Now(),
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Addr < list[j].Addr })
	d.mu.Lock()
	d.cache[service] = dnsEntry{list: list, at: time.Now(), valid: true}
	d.mu.Unlock()
	return list, nil
}

// Close 实现 Registry。
func (d *DNS) Close() error { return nil }

// ---------------------------------------------------------------------------
// 工厂
// ---------------------------------------------------------------------------

// New 根据类型创建注册表。registryType: static / dns。
func New(registryType string, endpoints []string, dnsSuffix string, ttl time.Duration) (Registry, error) {
	switch strings.ToLower(strings.TrimSpace(registryType)) {
	case "", "static":
		return NewStatic(endpoints)
	case "dns":
		return NewDNS(dnsSuffix, ttl)
	default:
		return nil, fmt.Errorf("不支持的服务发现类型: %s（可选 static / dns）", registryType)
	}
}
