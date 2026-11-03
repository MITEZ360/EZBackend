// Package bootstrap 负责应用装配：配置、日志、数据库、缓存、服务、路由与生命周期。
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/micro"
	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/modules/audit"
	"github.com/keel-framework/keel/internal/modules/auth"
	"github.com/keel-framework/keel/internal/modules/dashboard"
	"github.com/keel-framework/keel/internal/modules/dept"
	"github.com/keel-framework/keel/internal/modules/file"
	"github.com/keel-framework/keel/internal/modules/job"
	"github.com/keel-framework/keel/internal/modules/menu"
	"github.com/keel-framework/keel/internal/modules/message"
	"github.com/keel-framework/keel/internal/modules/monitor"
	"github.com/keel-framework/keel/internal/modules/role"
	"github.com/keel-framework/keel/internal/modules/system"
	"github.com/keel-framework/keel/internal/modules/user"
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/database"
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/internal/platform/logger"
	"github.com/keel-framework/keel/internal/router"
	"github.com/keel-framework/keel/pkg/discovery"
	"github.com/keel-framework/keel/pkg/event"
)

// App 是应用容器。
type App struct {
	Cfg     *config.Config
	Log     *zap.Logger
	DB      *gorm.DB
	Cache   cache.Cache
	Engine  *gin.Engine
	Jobs    *job.Service
	Audit   *audit.Service
	Events  *event.Bus
	Gateway *micro.Gateway
	Started time.Time

	server *http.Server
}

// New 装配应用。
func New(cfgPath string) (*App, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	lg, err := logger.New(logger.Options{
		Config: cfg.Log, Service: cfg.App.Name, Env: cfg.App.Env, Version: cfg.App.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化日志失败: %w", err)
	}
	zap.ReplaceGlobals(lg)
	if err := idgen.Init(idgen.NodeFromHost()); err != nil {
		lg.Warn("雪花节点初始化失败，使用默认节点", zap.Error(err))
	}

	// 网关形态不加载数据库与业务模块，只做转发，进一步压低资源占用。
	if strings.EqualFold(cfg.Micro.Mode, "gateway") {
		return newGateway(cfg, lg)
	}

	db, err := database.Open(cfg.DB, lg)
	if err != nil {
		return nil, err
	}
	if cfg.DB.AutoMigrate {
		if err := database.Migrate(db, models()...); err != nil {
			return nil, err
		}
		lg.Info("数据库表结构已同步")
	}
	if cfg.DB.Seed {
		if err := seed(db, lg); err != nil {
			return nil, err
		}
	}

	ch, err := cache.New(cfg.Redis, lg)
	if err != nil {
		return nil, err
	}

	bus := event.New(event.Options{
		QueueSize: 2048,
		Workers:   4,
		OnError: func(topic string, err any, _ []byte) {
			lg.Error("事件处理失败", zap.String("topic", topic), zap.Any("error", err))
		},
	})
	// 审计事件 → 业务指标；后续可在此扩展通知、外部同步等订阅者
	bus.Subscribe("audit.operation", func(_ context.Context, e event.Event) {
		op, ok := e.Data.(middleware.OperationEvent)
		if !ok {
			return
		}
		result := "success"
		if op.Status >= 400 {
			result = "failure"
		}
		middleware.ObserveBusiness(op.Module, result)
	})

	jwtMgr := authn.NewManager(cfg.JWT)
	captcha := authn.NewCaptcha(ch)
	authzLoader := authz.NewLoader(db, ch, lg)

	menuSvc := menu.NewService(db)
	authSvc := auth.NewService(db, ch, jwtMgr, captcha, cfg, lg, authzLoader, menuSvc)
	userSvc := user.NewService(db, cfg, lg, authzLoader)
	roleSvc := role.NewService(db, authzLoader)
	deptSvc := dept.NewService(db)
	dictSvc := system.NewDictService(db, ch)
	configSvc := system.NewConfigService(db, ch, cfg)
	msgSvc := message.NewService(db, lg)
	// 公告发布后同步投递站内信；message.Service 实现 system.NoticeNotifier。
	noticeSvc := system.NewNoticeService(db, msgSvc, lg)
	auditSvc := audit.NewService(db, ch, lg)
	jobSvc := job.NewService(db, lg)
	jobSvc.SetCleaner(auditSvc)
	jobSvc.SetMessageCleaner(msgSvc)
	fileSvc := file.NewService(db, cfg, lg)
	dashSvc := dashboard.NewService(db, ch)

	var engine *gin.Engine
	monitorSvc := monitor.NewService(db, ch, cfg, func() []monitor.RouteInfo {
		if engine == nil {
			return nil
		}
		routes := engine.Routes()
		out := make([]monitor.RouteInfo, 0, len(routes))
		for _, r := range routes {
			out = append(out, monitor.RouteInfo{Method: r.Method, Path: r.Path})
		}
		return out
	})

	engine = router.New(router.Deps{
		Cfg: cfg, Log: lg, DB: db, Cache: ch, JWT: jwtMgr, Authz: authzLoader, AuditSvc: auditSvc,
		Modules:   moduleSet(cfg.Micro),
		Events:    bus,
		Auth:      auth.NewHandler(authSvc, cfg.Security.CaptchaEnabled, cfg.Server.TrustProxy),
		User:      user.NewHandler(userSvc),
		Role:      role.NewHandler(roleSvc),
		Menu:      menu.NewHandler(menuSvc),
		Dept:      dept.NewHandler(deptSvc),
		Dict:      system.NewDictHandler(dictSvc),
		Config:    system.NewConfigHandler(configSvc),
		Notice:    system.NewNoticeHandler(noticeSvc),
		Message:   message.NewHandler(msgSvc),
		Audit:     audit.NewHandler(auditSvc),
		Job:       job.NewHandler(jobSvc),
		File:      file.NewHandler(fileSvc),
		Dashboard: dashboard.NewHandler(dashSvc),
		Monitor:   monitor.NewHandler(monitorSvc),
	})

	app := &App{Cfg: cfg, Log: lg, DB: db, Cache: ch, Engine: engine, Jobs: jobSvc,
		Audit: auditSvc, Events: bus, Started: time.Now()}
	app.server = newServer(cfg, engine)
	return app, nil
}

func models() []any {
	return []any{
		&model.User{}, &model.UserRole{}, &model.UserRecoveryCode{}, &model.Role{}, &model.RoleMenu{}, &model.Menu{},
		&model.Dept{}, &model.Session{}, &model.DictType{}, &model.DictItem{}, &model.SysConfig{},
		&model.Notice{}, &model.LoginLog{}, &model.OperationLog{}, &model.Job{}, &model.JobLog{},
		&model.File{}, &model.Message{}, &model.MessageRecipient{},
	}
}

// Run 启动服务并阻塞，收到信号后优雅停机。
func (a *App) Run() error {
	if a.Jobs != nil {
		if err := a.Jobs.Start(context.Background()); err != nil {
			a.Log.Warn("定时任务启动失败", zap.Error(err))
		}
	}

	errCh := make(chan error, 1)
	go func() {
		fields := []zap.Field{
			zap.String("addr", a.server.Addr),
			zap.String("env", a.Cfg.App.Env),
			zap.String("mode", a.Mode()),
			zap.Int("routes", len(a.Engine.Routes())),
		}
		if a.DB != nil {
			fields = append(fields, zap.String("db", a.Cfg.DB.Driver))
		}
		if a.Cache != nil {
			fields = append(fields, zap.String("cache", a.Cache.Kind()))
		}
		a.Log.Info("服务已启动", fields...)
		if a.Cfg.Server.TLSCert != "" && a.Cfg.Server.TLSKey != "" {
			errCh <- a.server.ListenAndServeTLS(a.Cfg.Server.TLSCert, a.Cfg.Server.TLSKey)
			return
		}
		errCh <- a.server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(sigCh)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-sigCh:
		a.Log.Info("收到退出信号，开始优雅停机", zap.String("signal", sig.String()))
		ctx, cancel := context.WithTimeout(context.Background(), a.Cfg.Server.ShutdownTimeout)
		defer cancel()
		return a.Shutdown(ctx)
	}
}

// Shutdown 依次停止 HTTP、定时任务、审计队列与存储连接。
func (a *App) Shutdown(ctx context.Context) error {
	var err error
	if a.server != nil {
		err = a.server.Shutdown(ctx)
	}
	if a.Jobs != nil {
		a.Jobs.Stop()
	}
	if a.Audit != nil {
		a.Audit.Close()
	}
	if a.Events != nil {
		a.Events.Close()
	}
	if a.Cache != nil {
		_ = a.Cache.Close()
	}
	if a.DB != nil {
		if sqlDB, e := a.DB.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}
	a.Log.Info("服务已退出")
	_ = a.Log.Sync()
	return err
}

// Addr 返回监听地址。
func (a *App) Addr() string {
	if a.server == nil {
		return ""
	}
	return a.server.Addr
}

// Mode 返回当前运行形态：mono / gateway / service。
func (a *App) Mode() string {
	m := strings.ToLower(strings.TrimSpace(a.Cfg.Micro.Mode))
	if m == "" {
		return "mono"
	}
	return m
}

// newGateway 装配网关形态：无数据库、无业务模块、无定时任务。
func newGateway(cfg *config.Config, lg *zap.Logger) (*App, error) {
	reg, err := discovery.New(cfg.Micro.Registry.Type, cfg.Micro.Registry.Endpoints,
		cfg.Micro.Registry.DNSSuffix, cfg.Micro.Registry.Interval)
	if err != nil {
		return nil, fmt.Errorf("初始化服务发现失败: %w", err)
	}
	gw, err := micro.New(cfg.Micro, reg, cfg.Security.InternalToken, cfg.App.Name, lg)
	if err != nil {
		_ = reg.Close()
		return nil, err
	}
	engine := router.New(router.Deps{Cfg: cfg, Log: lg, Gateway: gw})
	app := &App{Cfg: cfg, Log: lg, Engine: engine, Gateway: gw, Started: time.Now()}
	app.server = newServer(cfg, engine)

	lg.Info("网关已装配",
		zap.String("registry", reg.Name()),
		zap.Int("routes", len(gw.Routes())),
		zap.Strings("rules", gw.Routes()))
	return app, nil
}

// moduleSet 计算 service 模式下启用的模块集合；返回 nil 表示全部启用。
func moduleSet(m config.Micro) map[string]bool {
	if !strings.EqualFold(m.Mode, "service") || len(m.Modules) == 0 {
		return nil
	}
	set := make(map[string]bool, len(m.Modules))
	for _, name := range m.Modules {
		set[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return set
}

func newServer(cfg *config.Config, engine *gin.Engine) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    1 << 20,
	}
}

var _ = middleware.NoCache
