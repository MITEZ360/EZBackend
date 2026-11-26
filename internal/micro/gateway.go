// Package micro 提供微服务形态下的网关能力：按前缀路由、服务发现寻址、失败重试与熔断。
package micro

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/pkg/discovery"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/gov"
	"github.com/MITEZ360/tenon/pkg/response"
)

// Route 是一条编译后的转发规则。
type Route struct {
	Prefix  string
	Service string
	Targets []string
	Strip   bool
}

// routeTable 是数据面的路由视图，整体原子替换。
type routeTable struct {
	matcher  *gov.CompiledRoutes
	services map[string][]gov.Instance
}

// Gateway 是 API 网关。
//
// 路由有两个来源：治理面下发的快照（优先）与启动时读取的配置文件（兜底）。
// 两者都编译成同一套执行结构，请求路径上没有分支判断。
type Gateway struct {
	routes    []*Route // 按前缀长度倒序，保证最长匹配
	registry  discovery.Registry
	balancer  *discovery.Balancer
	govBal    *gov.Balancer
	transport http.RoundTripper
	log       *zap.Logger
	retries   int
	timeout   time.Duration
	token     string
	caller    string
	table     atomic.Pointer[routeTable]
}

// New 根据配置构建网关。
//
// 允许 micro.routes 为空：纯治理模式（gov.enabled）下路由全部来自控制面快照，
// 静态规则只是冷启动兜底。是否"两样都没有"属于配置错误，由装配层判断——
// 网关本身不该知道治理订阅的存在。
func New(cfg config.Micro, registry discovery.Registry, internalToken, caller string, log *zap.Logger) (*Gateway, error) {
	if log == nil {
		log = zap.NewNop()
	}
	g := &Gateway{
		registry: registry,
		balancer: discovery.NewBalancer(5 * time.Second),
		govBal:   gov.NewBalancer(5 * time.Second),
		log:      log,
		retries:  cfg.Retries,
		timeout:  cfg.Timeout,
		token:    internalToken,
		caller:   caller,
	}
	if g.timeout <= 0 {
		g.timeout = 5 * time.Second
	}
	if g.retries < 0 {
		g.retries = 0
	}
	g.transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: g.timeout,
	}

	seen := make(map[string]bool)
	for _, r := range cfg.Routes {
		p := strings.TrimSpace(r.Prefix)
		if p == "" {
			return nil, errors.New("micro.routes.prefix 不能为空")
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if seen[p] {
			return nil, fmt.Errorf("micro.routes 存在重复前缀: %s", p)
		}
		seen[p] = true
		svc := strings.TrimSpace(r.Service)
		if svc == "" && len(r.Targets) == 0 {
			return nil, fmt.Errorf("转发规则 %s 必须指定 service 或 targets", p)
		}
		g.routes = append(g.routes, &Route{Prefix: p, Service: svc, Targets: r.Targets, Strip: r.Strip})
	}
	sort.Slice(g.routes, func(i, j int) bool { return len(g.routes[i].Prefix) > len(g.routes[j].Prefix) })
	return g, nil
}

// Routes 返回已注册的转发规则描述，用于启动日志与监控展示。
func (g *Gateway) Routes() []string {
	out := make([]string, 0, len(g.routes))
	for _, r := range g.routes {
		out = append(out, r.Prefix+" -> "+r.Service)
	}
	return out
}

// Handler 返回网关主处理函数。
//
// 一旦治理快照就位，路由**只**以快照为准：没命中就是 404，不再回落到
// 配置文件。否则配置文件里的 /api/v1 兜底会把所有路径都接走，动态路由
// 永远无法下线一条规则，灰度也会被兜底绕过。
// 快照未就绪时（首次启动、控制面不可用且无本地快照）才用配置文件路由。
func (g *Gateway) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 判据是"快照是否就位"，不是"快照里有没有规则"：
		// 空路由表意味着运维把规则全部下线，必须以 404 表达出来，
		// 而不是悄悄回落到配置文件继续转发。
		if tbl := g.table.Load(); tbl != nil {
			rt := tbl.matcher.Match(c.Request.Method, c.Request.URL.Path, c.Request.Header, c.Request.URL.Query())
			if rt == nil {
				response.Fail(c, errs.NotFound("接口不存在"))
				return
			}
			g.serveGov(c, tbl, rt)
			return
		}
		rt := g.match(c.Request.URL.Path)
		if rt == nil {
			response.Fail(c, errs.NotFound("接口不存在"))
			return
		}
		g.serve(c, rt)
	}
}

// ApplySnapshot 用新快照原子替换路由视图。
//
// 解码与编译在切换前完成：任何一步失败都保留旧视图，网关继续按旧规则转发。
func (g *Gateway) ApplySnapshot(snap *gov.Snapshot) error {
	if snap == nil {
		return nil
	}
	rp, ok := snap.Routes()
	if !ok {
		return errors.New("快照缺少路由分区")
	}
	reg, _ := snap.Registry()
	next := &routeTable{
		matcher:  gov.Compile(rp.Routes),
		services: make(map[string][]gov.Instance, len(reg.Services)),
	}
	for name, svc := range reg.Services {
		next.services[name] = svc.Instances
	}
	g.table.Store(next)
	g.log.Info("治理快照已应用到网关",
		zap.Int64("globalRev", snap.GlobalRev),
		zap.Int("routes", next.matcher.Len()),
		zap.Int("services", len(next.services)))
	return nil
}

// serveGov 按快照规则转发；重试只在响应未写出时进行。
func (g *Gateway) serveGov(c *gin.Context, tbl *routeTable, rt *gov.Route) {
	list := tbl.services[rt.Service]
	if rt.Version != "" {
		filtered := make([]gov.Instance, 0, len(list))
		for _, ins := range list {
			if ins.Version == rt.Version {
				filtered = append(filtered, ins)
			}
		}
		list = filtered
	}
	if len(list) == 0 {
		response.Fail(c, errs.Unavailable("服务 "+rt.Service+" 无可用实例"))
		return
	}

	if rt.TimeoutMs > 0 {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(rt.TimeoutMs)*time.Millisecond)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
	}

	retries := rt.Retries
	if retries < 0 {
		retries = 0
	}
	if retries > 3 {
		retries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		ins, ok := g.govBal.Pick(rt.Service, list, "")
		if !ok {
			response.Fail(c, errs.Unavailable("服务 "+rt.Service+" 无可用实例"))
			return
		}
		failed := false
		proxy := g.newProxyFor(rt, ins.Addr)
		proxy.ErrorHandler = func(_ http.ResponseWriter, _ *http.Request, e error) {
			failed = true
			lastErr = e
		}
		proxy.ServeHTTP(c.Writer, c.Request)
		if !failed {
			g.govBal.Success(ins.ID)
			return
		}
		g.govBal.Fail(ins.ID)
		if c.Writer.Written() {
			break
		}
		if attempt < retries {
			g.log.Warn("网关转发失败，准备重试",
				zap.String("route", rt.Name), zap.String("target", ins.Addr),
				zap.Int("attempt", attempt+1), zap.Error(lastErr))
		}
	}
	msg := "网关转发失败"
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	response.Fail(c, errs.Unavailable(msg))
}

func (g *Gateway) newProxyFor(rt *gov.Route, addr string) *httputil.ReverseProxy {
	u, err := url.Parse("http://" + stripScheme(addr))
	if err != nil {
		u = &url.URL{Scheme: "http"}
	}
	return &httputil.ReverseProxy{
		Transport: g.transport,
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			if rt.Strip {
				p := strings.TrimPrefix(req.URL.Path, rt.Prefix)
				if p == "" {
					p = "/"
				}
				if !strings.HasPrefix(p, "/") {
					p = "/" + p
				}
				req.URL.Path = p
				req.URL.RawPath = ""
			}
			g.inject(req)
		},
	}
}

func (g *Gateway) inject(req *http.Request) {
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "tenon-gateway")
	}
	if g.token != "" {
		req.Header.Set("X-Internal-Token", g.token)
	}
	if g.caller != "" {
		req.Header.Set("X-Caller-Service", g.caller)
	}
	if req.Header.Get("X-Forwarded-Proto") == "" {
		proto := "http"
		if req.TLS != nil {
			proto = "https"
		}
		req.Header.Set("X-Forwarded-Proto", proto)
	}
}

// match 按最长前缀匹配转发规则。
func (g *Gateway) match(path string) *Route {
	for _, r := range g.routes {
		if path == r.Prefix || strings.HasPrefix(path, r.Prefix+"/") || r.Prefix == "/" {
			return r
		}
	}
	return nil
}

// serve 带重试地转发请求；重试仅发生在响应未写出时。
func (g *Gateway) serve(c *gin.Context, rt *Route) {
	var lastErr error
	for attempt := 0; attempt <= g.retries; attempt++ {
		instance, err := g.pick(c.Request.Context(), rt)
		if err != nil {
			response.Fail(c, errs.Unavailable("服务不可用: "+err.Error()))
			return
		}
		failed := false
		proxy := g.newProxy(rt, instance.URL("http"))
		proxy.ErrorHandler = func(_ http.ResponseWriter, _ *http.Request, e error) {
			failed = true
			lastErr = e
		}
		proxy.ServeHTTP(c.Writer, c.Request)
		if !failed {
			g.balancer.Success(instance.ID)
			return
		}
		g.balancer.Fail(instance.ID)
		if c.Writer.Written() {
			// 已经开始写响应，无法安全重试
			break
		}
		if attempt < g.retries {
			g.log.Warn("网关转发失败，准备重试",
				zap.String("prefix", rt.Prefix), zap.String("target", instance.Addr),
				zap.Int("attempt", attempt+1), zap.Error(lastErr))
		}
	}
	msg := "网关转发失败"
	if lastErr != nil {
		msg += ": " + lastErr.Error()
	}
	response.Fail(c, errs.Unavailable(msg))
}

// newProxy 构造单次请求使用的反向代理，复用底层连接池。
func (g *Gateway) newProxy(rt *Route, target string) *httputil.ReverseProxy {
	u, err := url.Parse(target)
	if err != nil {
		u = &url.URL{Scheme: "http"}
	}
	return &httputil.ReverseProxy{
		Transport: g.transport,
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			if rt.Strip {
				p := strings.TrimPrefix(req.URL.Path, rt.Prefix)
				if p == "" {
					p = "/"
				}
				if !strings.HasPrefix(p, "/") {
					p = "/" + p
				}
				req.URL.Path = p
				req.URL.RawPath = ""
			}
			if _, ok := req.Header["User-Agent"]; !ok {
				req.Header.Set("User-Agent", "tenon-gateway")
			}
			if g.token != "" {
				req.Header.Set("X-Internal-Token", g.token)
			}
			if g.caller != "" {
				req.Header.Set("X-Caller-Service", g.caller)
			}
			if req.Header.Get("X-Forwarded-Proto") == "" {
				proto := "http"
				if req.TLS != nil {
					proto = "https"
				}
				req.Header.Set("X-Forwarded-Proto", proto)
			}
		},
	}
}

// pick 选择目标实例。
func (g *Gateway) pick(ctx context.Context, rt *Route) (discovery.Instance, error) {
	if len(rt.Targets) > 0 {
		list := make([]discovery.Instance, 0, len(rt.Targets))
		for _, t := range rt.Targets {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			list = append(list, discovery.Instance{ID: rt.Prefix + "@" + t, Addr: stripScheme(t), Healthy: true})
		}
		if ins, ok := g.balancer.Pick(rt.Prefix, list); ok {
			return ins, nil
		}
		return discovery.Instance{}, fmt.Errorf("规则 %s 未配置可用目标", rt.Prefix)
	}
	if g.registry == nil {
		return discovery.Instance{}, errors.New("未配置服务发现")
	}
	list, err := g.registry.Resolve(ctx, rt.Service)
	if err != nil {
		return discovery.Instance{}, err
	}
	if ins, ok := g.balancer.Pick(rt.Service, list); ok {
		return ins, nil
	}
	return discovery.Instance{}, fmt.Errorf("服务 %s 无可用实例", rt.Service)
}

func stripScheme(addr string) string {
	if i := strings.Index(addr, "://"); i >= 0 {
		return addr[i+3:]
	}
	return addr
}
