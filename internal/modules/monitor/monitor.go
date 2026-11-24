// Package monitor 提供服务监控信息：运行时、数据库连接池、缓存与路由清单。
package monitor

import (
	"context"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/database"
	"github.com/keel-framework/keel/pkg/response"
)

// Service 监控业务。
type Service struct {
	db      *gorm.DB
	cache   cache.Cache
	cfg     *config.Config
	started time.Time
	routes  func() []RouteInfo
	restart func()
}

// RouteInfo 路由信息。
type RouteInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
}

// NewService 创建监控服务。restart 为可选回调，用于管理端触发原地重启。
func NewService(db *gorm.DB, c cache.Cache, cfg *config.Config, routes func() []RouteInfo, restart ...func()) *Service {
	s := &Service{db: db, cache: c, cfg: cfg, started: time.Now(), routes: routes}
	if len(restart) > 0 {
		s.restart = restart[0]
	}
	return s
}

// System 返回系统概览。
func (s *Service) System(ctx context.Context) map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	sqlStats := database.Stats(s.db)
	var version string
	_ = s.db.WithContext(ctx).Raw("SELECT sqlite_version()").Scan(&version).Error
	if version == "" {
		version = s.cfg.DB.ResolveDriver()
	}
	return map[string]any{
		"app": map[string]any{
			"name": s.cfg.App.Name, "version": s.cfg.App.Version, "env": s.cfg.App.Env,
			"mode": s.cfg.Micro.Mode, "startedAt": s.started.Format(time.RFC3339),
			"uptime": int64(time.Since(s.started).Seconds()),
		},
		"runtime": map[string]any{
			"goVersion":  runtime.Version(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
			"cpu":        runtime.NumCPU(),
			"goroutines": runtime.NumGoroutine(),
			"memAllocMB": float64(m.Alloc) / 1024 / 1024,
			"memSysMB":   float64(m.Sys) / 1024 / 1024,
			"heapMB":     float64(m.HeapAlloc) / 1024 / 1024,
			"gcCount":    m.NumGC,
		},
		"database":  map[string]any{"driver": s.cfg.DB.ResolveDriver(), "version": version, "pool": sqlStats},
		"cache":     map[string]any{"kind": s.cache.Kind()},
		"ratelimit": map[string]any{"kind": s.cfg.CacheKindHint()},
		"storage":   map[string]any{"kind": s.cfg.Storage.Kind()},
		"http":      map[string]any{"routes": len(s.routes()), "port": s.cfg.Server.Port},
	}
}

// Handler 监控接口。
type Handler struct{ svc *Service }

// NewHandler 创建监控处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// System 系统信息。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary System
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/monitor/system [get]
// KE:SWAGGER-END
func (h *Handler) System(c *gin.Context) {
	response.OK(c, h.svc.System(c.Request.Context()))
}

// Routes 路由清单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Routes
// @Tags 运维
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/monitor/routes [get]
// KE:SWAGGER-END
func (h *Handler) Routes(c *gin.Context) {
	response.OK(c, h.svc.routes())
}

// Restart 管理端触发服务重启。先返回应答，再异步执行，避免优雅停机等待当前请求。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Restart
// @Tags 运维
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/monitor/restart [post]
// KE:SWAGGER-END
func (h *Handler) Restart(c *gin.Context) {
	middleware.AuditInfo(c, "运维", "重启服务")
	response.OK(c, gin.H{"message": "重启指令已受理，服务正在重新拉起"})
	go func() {
		// 留出时间让响应与审计事件写出，再关闭监听并重新执行进程。
		time.Sleep(400 * time.Millisecond)
		if h.svc.restart != nil {
			h.svc.restart()
		}
	}()
}
