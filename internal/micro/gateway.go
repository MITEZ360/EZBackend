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
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/pkg/discovery"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
)

// Route 是一条编译后的转发规则。
type Route struct {
	Prefix  string
	Service string
	Targets []string
	Strip   bool
}

// Gateway 是 API 网关。
type Gateway struct {
	routes    []*Route // 按前缀长度倒序，保证最长匹配
	registry  discovery.Registry
	balancer  *discovery.Balancer
	transport http.RoundTripper
	log       *zap.Logger
	retries   int
	timeout   time.Duration
	token     string
	caller    string
}

// New 根据配置构建网关。
func New(cfg config.Micro, registry discovery.Registry, internalToken, caller string, log *zap.Logger) (*Gateway, error) {
	if len(cfg.Routes) == 0 {
		return nil, errors.New("网关模式必须配置 micro.routes 转发规则")
	}
	if log == nil {
		log = zap.NewNop()
	}
	g := &Gateway{
		registry: registry,
		balancer: discovery.NewBalancer(5 * time.Second),
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
func (g *Gateway) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		rt := g.match(c.Request.URL.Path)
		if rt == nil {
			response.Fail(c, errs.NotFound("接口不存在"))
			return
		}
		g.serve(c, rt)
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
				req.Header.Set("User-Agent", "keel-gateway")
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
