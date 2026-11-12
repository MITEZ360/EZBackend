// Package router 组装 Gin 引擎、全局中间件与全部业务路由。
//
// 三种形态共用同一套引擎：
//   - mono：全部业务路由挂载在一个进程内；
//   - gateway：只保留健康检查、指标、静态资源与反向代理；
//   - service：按 micro.modules 裁剪业务路由，一个进程只暴露部分模块。
package router

import (
	"context"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/apidocs"
	"github.com/keel-framework/keel/internal/micro"
	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/modules/apikey"
	"github.com/keel-framework/keel/internal/modules/audit"
	"github.com/keel-framework/keel/internal/modules/auth"
	"github.com/keel-framework/keel/internal/modules/dashboard"
	"github.com/keel-framework/keel/internal/modules/dept"
	"github.com/keel-framework/keel/internal/modules/file"
	"github.com/keel-framework/keel/internal/modules/job"
	"github.com/keel-framework/keel/internal/modules/maintenance"
	"github.com/keel-framework/keel/internal/modules/membership"
	"github.com/keel-framework/keel/internal/modules/menu"
	"github.com/keel-framework/keel/internal/modules/message"
	"github.com/keel-framework/keel/internal/modules/monitor"
	"github.com/keel-framework/keel/internal/modules/news"
	"github.com/keel-framework/keel/internal/modules/role"
	"github.com/keel-framework/keel/internal/modules/system"
	"github.com/keel-framework/keel/internal/modules/user"
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/database"
	"github.com/keel-framework/keel/internal/platform/storage"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/event"
	"github.com/keel-framework/keel/pkg/response"
	"github.com/keel-framework/keel/web"
)

// Deps 路由依赖集合。
type Deps struct {
	Cfg      *config.Config
	Log      *zap.Logger
	DB       *gorm.DB
	Cache    cache.Cache
	Limiter  cache.Limiter
	Storage  *storage.Manager
	JWT      *authn.Manager
	Authz    *authz.Loader
	AuditSvc *audit.Service
	// Gateway 非空时按网关模式装配：仅保留健康检查、指标、静态资源与转发规则。
	Gateway *micro.Gateway
	// Modules 为 service 模式下启用的业务模块；为空表示全部启用。
	Modules map[string]bool
	// Events 进程内事件总线，可为空；用于发布审计等业务事件。
	Events *event.Bus

	Auth        *auth.Handler
	User        *user.Handler
	Role        *role.Handler
	Menu        *menu.Handler
	Dept        *dept.Handler
	Dict        *system.DictHandler
	Config      *system.ConfigHandler
	Notice      *system.NoticeHandler
	Message     *message.Handler
	Membership  *membership.Handler
	APIKey      *apikey.Handler
	APIKeySvc   *apikey.Service
	News        *news.Handler
	NewsOpen    *news.OpenHandler
	Audit       *audit.Handler
	Job         *job.Handler
	File        *file.Handler
	Dashboard   *dashboard.Handler
	Monitor     *monitor.Handler
	Maintenance *maintenance.Handler
}

// New 创建 HTTP 引擎。
func New(d Deps) *gin.Engine {
	if d.Cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	e := gin.New()
	e.RedirectTrailingSlash = true
	e.RemoveExtraSlash = true

	// 本地上传目录公开只读：资讯图标、封面这类资源要能被客户端直接渲染，
	// 不能再经过登录态。目录仅存放上传产物，不做目录列表。
	// 对象存储模式下返回的是外链，无需挂载。
	if d.Storage != nil && d.Storage.Kind() == "local" {
		base := strings.TrimSuffix(d.Cfg.Storage.BaseURL, "/")
		if base == "" {
			base = "/uploads"
		}
		dir := d.Cfg.Storage.LocalDir
		if dir == "" {
			dir = "data/uploads"
		}
		e.Static(base, dir)
	}
	e.ContextWithFallback = true
	_ = e.SetTrustedProxies(nil)
	if d.Cfg.Server.TrustProxy {
		_ = e.SetTrustedProxies([]string{"0.0.0.0/0"})
	}

	e.Use(
		middleware.Recovery(d.Log),
		middleware.Trace(),
		middleware.AccessLog(d.Log, 500*time.Millisecond),
		middleware.CORS(d.Cfg.CORS),
		middleware.SecurityHeaders(),
		middleware.BodyLimit(d.Cfg.Server.MaxBodySize),
		middleware.Metrics(),
	)

	// ---- 健康检查与监控 ----
	d.healthRoutes(e)
	if d.Cfg.Metrics.Enabled {
		e.GET(d.Cfg.Metrics.Path, middleware.MetricsHandler(d.Cfg.Metrics.Token))
	}

	// ---- 静态资源与单页应用 ----
	mountSPA(e)

	// ---- 自动接口文档（开发环境默认开启，生产默认关闭）----
	if d.Cfg.Docs.UseDocs(d.Cfg.IsProd()) {
		mountDocs(e, d.Cfg.Docs)
	}

	// ---- 网关形态：只做转发 ----
	if d.Gateway != nil {
		e.Any("/api/*path", d.Gateway.Handler())
		e.NoRoute(d.notFound)
		return e
	}

	authMW := middleware.Auth(middleware.AuthDeps{
		JWT: d.JWT, Authz: d.Authz, Sessions: sessionValidator{d.Auth},
	})

	// 限流实现由缓存决定：Redis 缓存对应跨实例共享配额，否则进程内令牌桶。
	limiter := d.Limiter
	if limiter == nil {
		limiter = cache.NewLimiter(d.Cache, d.Cfg.RateLimit.RPS, d.Cfg.RateLimit.Burst)
	}

	// ---- 公开接口：按 IP 限流，避免登录爆破把服务打满 ----
	v1 := e.Group("/api/v1")
	publicLimit := d.Cfg.RateLimit
	publicLimit.ByUser = false
	public := v1.Group("", middleware.RateLimit(publicLimit, limiter, d.Cfg.Server.TrustProxy))
	d.publicRoutes(public)

	// ---- 登录后接口：认证之后按用户维度限流 ----
	authed := v1.Group("", authMW,
		middleware.RateLimit(d.Cfg.RateLimit, limiter, d.Cfg.Server.TrustProxy),
		middleware.Audit(d.AuditSvc, d.Events))
	d.authRoutes(authed)
	if d.on("dashboard") {
		d.dashboardRoutes(authed)
	}
	if d.on("user") {
		d.userRoutes(authed)
	}
	if d.on("membership") {
		d.membershipRoutes(authed)
	}
	if d.on("apikey") {
		d.apiKeyRoutes(authed)
	}
	if d.on("news") {
		d.newsRoutes(authed)
	}
	if d.on("role") {
		d.roleRoutes(authed)
	}
	if d.on("menu") {
		d.menuRoutes(authed)
	}
	if d.on("dept") {
		d.deptRoutes(authed)
	}
	if d.on("dict") {
		d.dictRoutes(authed)
	}
	if d.on("config") {
		d.configRoutes(authed)
	}
	if d.on("notice") {
		d.noticeRoutes(authed)
	}
	if d.on("message") {
		d.messageRoutes(authed)
	}
	if d.on("audit") {
		d.auditRoutes(authed)
	}
	if d.on("job") {
		d.jobRoutes(authed)
	}
	if d.on("file") {
		d.fileRoutes(authed)
	}
	if d.on("monitor") {
		d.monitorRoutes(authed)
	}
	if d.on("maintenance") && d.Maintenance != nil {
		d.maintenanceRoutes(authed)
	}

	// ---- 开放接口：面向程序调用方，用 API 密钥认证，不经过用户会话 ----
	if d.APIKeySvc != nil {
		open := e.Group("/api/open/v1",
			middleware.APIKeyAuth(d.APIKeySvc, d.Cfg.Server.TrustProxy))
		open.GET("/whoami", d.APIKey.Whoami)
		if d.NewsOpen != nil {
			open.POST("/news/batch", middleware.RequirePerm("news:ingest"), d.NewsOpen.Ingest)
			open.GET("/news/meta", middleware.RequirePerm("news:read"), d.NewsOpen.Meta)
			open.GET("/news", middleware.RequirePerm("news:read"), d.NewsOpen.List)
			open.GET("/news/:id", middleware.RequirePerm("news:read"), d.NewsOpen.Detail)
		}
	}

	e.NoRoute(d.notFound)
	return e
}

// on 判断某业务模块在当前进程是否启用。
func (d Deps) on(module string) bool {
	if len(d.Modules) == 0 {
		return true
	}
	return d.Modules[module]
}

// publicRoutes 注册无需登录的接口。
func (d Deps) publicRoutes(v1 *gin.RouterGroup) {
	v1.GET("/auth/captcha", d.Auth.Captcha)
	v1.POST("/auth/login", d.Auth.Login)
	v1.POST("/auth/refresh", d.Auth.Refresh)
	v1.GET("/public/config", d.Auth.PublicConfig)
	v1.GET("/public/configs", d.Config.Public)
	v1.GET("/public/dicts/:code", d.Dict.Data)
	v1.GET("/public/notices", d.Notice.Latest)

	// 配置化第三方登录：渠道列表、发起授权与回调均无需登录
	v1.GET("/oauth/providers", d.Auth.OAuthProviders)
	v1.GET("/oauth/authorize/:name", d.Auth.OAuthAuthorize)
	v1.GET("/oauth/callback/:name", d.Auth.OAuthCallback)
}

// healthRoutes 注册存活与就绪探针。
func (d Deps) healthRoutes(e *gin.Engine) {
	e.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		status := http.StatusOK
		checks := map[string]string{"self": "up"}
		if d.DB == nil {
			checks["database"] = "disabled"
		} else if err := database.Health(ctx, d.DB); err != nil {
			checks["database"] = "down: " + err.Error()
			status = http.StatusServiceUnavailable
		} else {
			checks["database"] = "up"
		}
		if d.Cache == nil {
			checks["cache"] = "disabled"
		} else if _, err := d.Cache.Exists(ctx, "__ping__"); err != nil {
			checks["cache"] = "down: " + err.Error()
			status = http.StatusServiceUnavailable
		} else {
			checks["cache"] = "up"
		}
		if d.Storage == nil {
			checks["storage"] = "disabled"
		} else if err := d.Storage.Health(ctx); err != nil {
			checks["storage"] = "down: " + err.Error()
			status = http.StatusServiceUnavailable
		} else {
			checks["storage"] = "up"
		}
		if d.Gateway != nil {
			checks["mode"] = "gateway"
		}
		c.JSON(status, gin.H{"status": map[bool]string{true: "up", false: "down"}[status == http.StatusOK],
			"version": d.Cfg.App.Version, "checks": checks, "time": time.Now().Format(time.RFC3339)})
	})
	e.GET("/readyz", func(c *gin.Context) {
		ctx := c.Request.Context()
		if d.DB != nil {
			if err := database.Health(ctx, d.DB); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "error": "database: " + err.Error()})
				return
			}
		}
		if d.Cache != nil {
			if _, err := d.Cache.Exists(ctx, "__ping__"); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "error": "cache: " + err.Error()})
				return
			}
		}
		if d.Storage != nil {
			if err := d.Storage.Health(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "error": "storage: " + err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"ready": true})
	})
}

// notFound 统一 404：接口返回 JSON，其余回退单页应用。
func (d Deps) notFound(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") || strings.HasPrefix(c.Request.URL.Path, "/uploads/") {
		response.Fail(c, errs.NotFound("接口不存在"))
		return
	}
	serveIndex(c)
}

type sessionValidator struct{ h *auth.Handler }

func (s sessionValidator) ValidateSession(ctx context.Context, sid, uid string) (bool, error) {
	if s.h == nil {
		return true, nil
	}
	return s.h.ValidateSession(ctx, sid, uid)
}

// mountDocs 挂载 Swagger UI 与生成的 OpenAPI 描述。
// 描述来自 swag 生成的 apidocs 包，随二进制嵌入，不依赖外部文件。
func mountDocs(e *gin.Engine, cfg config.Docs) {
	base := cfg.EffectivePath()
	docJSON := base + "/doc.json"
	ui := ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL(docJSON))
	// 只注册一条通配路由：同一层既有静态段又有 *any 会触发 Gin 路由冲突，
	// 因此 doc.json 在 handler 内分流，而不是单独注册静态路径。
	e.GET(base+"/*any", func(c *gin.Context) {
		switch c.Param("any") {
		case "/doc.json":
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.String(http.StatusOK, apidocs.SwaggerInfo.ReadDoc())
		case "", "/":
			c.Redirect(http.StatusFound, base+"/index.html")
		default:
			ui(c)
		}
	})
}

// mountSPA 托管 Vite 产物：带 hash 的静态资源长缓存，其余交给单页应用兜底。
func mountSPA(e *gin.Engine) {
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))

	e.GET("/assets/*filepath", func(c *gin.Context) {
		// 文件名含内容 hash，可安全长缓存
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
	e.GET("/favicon.svg", gin.WrapH(fileServer))
	e.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
}

func serveIndex(c *gin.Context) {
	data, err := fs.ReadFile(web.FS, "dist/index.html")
	if err != nil {
		c.String(http.StatusNotFound, "前端资源未打包")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
