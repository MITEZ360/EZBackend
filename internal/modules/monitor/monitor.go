// Package monitor 提供服务监控信息：运行时、数据库连接池、缓存与路由清单。
package monitor

import (
	"context"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

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
}

// RouteInfo 路由信息。
type RouteInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
}

// NewService 创建监控服务。
func NewService(db *gorm.DB, c cache.Cache, cfg *config.Config, routes func() []RouteInfo) *Service {
	return &Service{db: db, cache: c, cfg: cfg, started: time.Now(), routes: routes}
}

// System 返回系统概览。
func (s *Service) System(ctx context.Context) map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	sqlStats := database.Stats(s.db)
	var version string
	_ = s.db.WithContext(ctx).Raw("SELECT sqlite_version()").Scan(&version).Error
	if version == "" {
		version = s.cfg.DB.Driver
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
		"database": map[string]any{"driver": s.cfg.DB.Driver, "version": version, "pool": sqlStats},
		"cache":    map[string]any{"kind": s.cache.Kind()},
		"http":     map[string]any{"routes": len(s.routes()), "port": s.cfg.Server.Port},
	}
}

// Handler 监控接口。
type Handler struct{ svc *Service }

// NewHandler 创建监控处理器。
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// System 系统信息。
func (h *Handler) System(c *gin.Context) {
	response.OK(c, h.svc.System(c.Request.Context()))
}

// Routes 路由清单。
func (h *Handler) Routes(c *gin.Context) {
	response.OK(c, h.svc.routes())
}
