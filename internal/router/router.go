// Package router 组装 Gin 引擎、全局中间件与业务路由。
//
// 三种形态共用同一套引擎：
//   - mono：全部业务路由挂载在一个进程内；
//   - gateway：只保留健康检查、指标、静态资源与反向代理；
//   - service：按 micro.modules 裁剪业务路由，一个进程只暴露部分模块。
//
// 本包不认识任何具体业务模块：路由由各模块在装配时通过 modulekit.Context
// 声明，这里只负责给出三组带中间件的挂载点（公开 / 登录后 / 开放接口）。
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

	"github.com/MITEZ360/tenon/internal/apidocs"
	"github.com/MITEZ360/tenon/internal/micro"
	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/internal/platform/authz"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/database"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
	"github.com/MITEZ360/tenon/internal/platform/storage"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
	"github.com/MITEZ360/tenon/pkg/response"
	"github.com/MITEZ360/tenon/web"
)

// Deps 路由依赖集合。业务侧的一切都在 Modules 里，本结构只含平台能力。
type Deps struct {
	Cfg     *config.Config
	Log     *zap.Logger
	DB      *gorm.DB
	Cache   cache.Cache
	Limiter cache.Limiter
	Storage *storage.Manager
	JWT     *authn.Manager
	Authz   *authz.Loader
	// Gateway 非空时按网关模式装配：仅保留健康检查、指标、静态资源与转发规则。
	Gateway *micro.Gateway
	// Modules 是模块装配上下文：路由闭包、会话校验器与审计器都由模块提供。
	Modules *modulekit.Context
}

// New 创建 HTTP 引擎。
func New(d Deps) *gin.Engine {
	if d.Cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	e := gin.New()
	e.RedirectTrailingSlash = true
	e.RemoveExtraSlash = true

	e.ContextWithFallback = true
	_ = e.SetTrustedProxies(nil)
	if d.Cfg.Server.TrustProxy {
		_ = e.SetTrustedProxies([]string{"0.0.0.0/0"})
	}

	e.Use(
		middleware.Recovery(d.Log),
		i18n.Middleware(d.Cfg.I18n.DefaultLocale),
		middleware.Trace(),
		middleware.AccessLog(d.Log, 500*time.Millisecond),
		middleware.CORS(d.Cfg.CORS),
		middleware.SecurityHeaders(),
		middleware.BodyLimit(d.Cfg.Server.MaxBodySize),
		middleware.Metrics(),
	)

	// 本地上传目录公开只读：资讯图标、封面这类资源要能被客户端直接渲染，
	// 不能再经过登录态。必须在 e.Use 之后挂载，才能带上 nosniff 等安全响应头。
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
		JWT: d.JWT, Authz: d.Authz, Sessions: d.Modules.Sessions,
	})

	// 限流实现由缓存决定：Redis 缓存对应跨实例共享配额，否则进程内令牌桶。
	limiter := d.Limiter
	if limiter == nil {
		limiter = cache.NewLimiter(d.Cache, d.Cfg.RateLimit.RPS, d.Cfg.RateLimit.Burst)
	}

	// ---- 公开接口：按 IP 限流，避免登录爆破把服务打满 ----
	// 使用动态版本：管理端调整限流阈值后无需重启即可生效。
	v1 := e.Group("/api/v1")
	public := v1.Group("", middleware.RateLimitDynamic(limiter, d.Cfg.Server.TrustProxy, false))
	for _, fn := range d.Modules.PublicRoutes() {
		fn(public)
	}

	// ---- 登录后接口：认证之后按用户维度限流 ----
	authed := v1.Group("", authMW,
		middleware.RateLimitDynamic(limiter, d.Cfg.Server.TrustProxy, true),
		middleware.Audit(d.Modules.Auditor, d.Modules.Events))
	for _, fn := range d.Modules.AuthedRoutes() {
		fn(authed)
	}

	// ---- 开放接口：面向程序调用方，用 API 密钥认证，不经过用户会话 ----
	// 密钥能力来自 apikey 模块；该模块被裁掉时整组接口不存在。
	if auth, ok := d.Modules.Lookup("apikey").(middleware.APIKeyAuthenticator); ok {
		open := e.Group("/api/open/v1",
			middleware.APIKeyAuth(auth, d.Cfg.Server.TrustProxy))
		for _, fn := range d.Modules.OpenRoutes() {
			fn(open)
		}
	}

	e.NoRoute(d.notFound)
	return e
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
		c.String(http.StatusNotFound, i18n.T(c, "前端资源未打包"))
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}
