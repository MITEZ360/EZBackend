package modulekit

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/internal/platform/authz"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/mailer"
	"github.com/MITEZ360/tenon/internal/platform/migrate"
	"github.com/MITEZ360/tenon/internal/platform/storage"
	"github.com/MITEZ360/tenon/pkg/event"
)

// 下面几个接口是模块之间传递能力的最小契约。定义在这里而不是各自包内，
// 是为了让提供方的实现类型无需导入消费方，两边结构匹配即可。
type (
	// Auditor 接收操作日志，由审计模块实现。
	Auditor interface {
		Push(entry *model.OperationLog)
	}
	// Publisher 是事件总线的最小接口（*event.Bus 直接满足）。
	Publisher interface {
		Publish(ctx context.Context, topic string, data any)
	}
	// SessionValidator 校验登录会话是否仍然有效，由认证模块实现。
	SessionValidator interface {
		ValidateSession(ctx context.Context, sid, uid string) (bool, error)
	}
)

// Route 是已注册路由的快照，供监控与路由管理页面展示。
type Route struct {
	Method string
	Path   string
	Name   string
}

// JobFunc 是定时任务的实现签名。
type JobFunc func(ctx context.Context, params map[string]any) (string, error)

// Context 是模块装配期拿到的全部依赖。
//
// 它是"能力的集合"而不是"容器的集合"：模块只能通过 Lookup 拿到自己声明
// 依赖的服务，其余一律走这里给出的平台能力。这样模块之间不会互相 import，
// 也不会出现 A 依赖 B 的 Service 却由框架硬塞的局面。
type Context struct {
	// ---- 平台能力 ----
	DB        *gorm.DB
	Cache     cache.Cache
	Limiter   cache.Limiter
	Config    *config.Config
	Log       *zap.Logger
	Events    *event.Bus
	Storage   *storage.Manager
	Mailer    mailer.Mailer
	JWT       *authn.Manager
	Captcha   *authn.Captcha
	Authz     *authz.Loader
	Migration *migrate.Runner
	// MigrationMode 当前迁移模式：versioned / auto / off。
	MigrationMode string
	// Instance 本实例标识，集群注册与日志排查使用。
	Instance string
	// Routes 返回已注册路由的快照，装配完成后才可调用。
	Routes func() []Route
	// Restart 请求进程原地重启；集群与监控模块使用。
	Restart func()
	// Sessions 由认证模块在 Setup 中赋值，供鉴权中间件校验会话。
	Sessions SessionValidator
	// Auditor 由审计模块在 Setup 中赋值，供审计中间件记录写操作。
	Auditor Auditor

	// ---- 装配收集 ----
	public []func(*gin.RouterGroup)
	authed []func(*gin.RouterGroup)
	open   []func(*gin.RouterGroup)
	jobs   map[string]JobFunc
	svcs   map[string]any
	after  []func(*Context) error
	sink   func(handler string, fn JobFunc)
	starts []func(context.Context) error
	stops  []func()
}

// NewContext 创建装配上下文。由框架调用，模块只读不建。
func NewContext(base Context) *Context {
	c := base
	c.jobs = map[string]JobFunc{}
	c.svcs = map[string]any{}
	return &c
}

// Public 注册无需登录的接口，挂在 /api/v1 下，按 IP 限流。
func (c *Context) Public(fn func(*gin.RouterGroup)) { c.public = append(c.public, fn) }

// Authed 注册登录后接口，挂在 /api/v1 下，带认证、限流与审计。
func (c *Context) Authed(fn func(*gin.RouterGroup)) { c.authed = append(c.authed, fn) }

// Open 注册程序调用接口，挂在 /api/open/v1 下，用 API 密钥认证。
func (c *Context) Open(fn func(*gin.RouterGroup)) { c.open = append(c.open, fn) }

// Job 注册任务实现。handler 必须与 Spec.Jobs 里声明的 Handler 同名。
func (c *Context) Job(handler string, fn JobFunc) {
	if c.jobs == nil {
		c.jobs = map[string]JobFunc{}
	}
	c.jobs[handler] = fn
}

// After 注册"全部模块装配完成后"的回调，用于跨模块依赖注入。
//
// 模块之间的构造顺序由依赖图决定，但依赖方向不总是一致的（例如用户模块
// 需要会员模块的补齐能力，会员模块又依赖用户表）。与其让框架维护一张
// 双向依赖图，不如让消费方在装配结束后再取一次。
func (c *Context) After(fn func(*Context) error) { c.after = append(c.after, fn) }

// OnJobSink 由调度模块调用：拿到"把任务实现挂到调度器"的出口。
// 各模块用 Job 注册实现，框架在装配结束后统一灌给调度器。
func (c *Context) OnJobSink(fn func(handler string, impl JobFunc)) { c.sink = fn }

// OnStart 注册进程启动后的回调，按注册顺序执行。用于调度器、心跳循环这类
// 需要后台 goroutine 的模块；模块被裁掉时回调自然不存在。
func (c *Context) OnStart(fn func(context.Context) error) { c.starts = append(c.starts, fn) }

// OnStop 注册停机回调，按注册的逆序执行（后启动的先停）。
func (c *Context) OnStop(fn func()) { c.stops = append(c.stops, fn) }

// Provide 向其它模块供给一个服务实例。name 建议用 "包名" 或 "包名.服务名"。
func (c *Context) Provide(name string, svc any) {
	if c.svcs == nil {
		c.svcs = map[string]any{}
	}
	c.svcs[name] = svc
}

// Lookup 取出由其它模块供给的服务；不存在返回 nil。
// 消费方应做类型断言并在失败时给出明确错误，而不是直接 panic。
func (c *Context) Lookup(name string) any { return c.svcs[name] }

// Need 取出服务并断言为 T；缺失或类型不符时返回错误，附上模块名便于定位。
func Need[T any](c *Context, name string) (T, error) {
	var zero T
	v := c.Lookup(name)
	if v == nil {
		return zero, fmt.Errorf("依赖的服务 %q 未提供（检查依赖模块是否已导入并启用）", name)
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("服务 %q 类型不符: %T", name, v)
	}
	return t, nil
}

// PublicRoutes / AuthedRoutes / OpenRoutes 供框架挂载，模块不应调用。
func (c *Context) PublicRoutes() []func(*gin.RouterGroup) { return c.public }
func (c *Context) AuthedRoutes() []func(*gin.RouterGroup) { return c.authed }
func (c *Context) OpenRoutes() []func(*gin.RouterGroup)   { return c.open }

// RunAfter 依次执行装配完成回调，供框架调用。
func (c *Context) RunAfter() error {
	for _, fn := range c.after {
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

// FlushJobs 把收集到的任务实现交给调度模块，供框架调用。
func (c *Context) FlushJobs() {
	if c.sink == nil {
		return
	}
	for handler, fn := range c.jobs {
		c.sink(handler, fn)
	}
}

// Start 依次执行启动回调，返回第一个错误但继续启动其余模块。
// 单模块启动失败只告警不阻断：例如调度器起不来，HTTP 服务仍应可用。
func (c *Context) Start(ctx context.Context) []error {
	var errs []error
	for _, fn := range c.starts {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Stop 逆序执行停机回调。
func (c *Context) Stop() {
	for i := len(c.stops) - 1; i >= 0; i-- {
		c.stops[i]()
	}
}

// JobImpls 供框架注册任务实现，模块不应调用。
func (c *Context) JobImpls() map[string]JobFunc { return c.jobs }
