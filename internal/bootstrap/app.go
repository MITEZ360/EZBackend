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
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/internal/platform/logger"
	"github.com/keel-framework/keel/internal/platform/mailer"
	"github.com/keel-framework/keel/internal/platform/migrate"
	"github.com/keel-framework/keel/internal/platform/storage"
	"github.com/keel-framework/keel/internal/router"
	"github.com/keel-framework/keel/pkg/discovery"
	"github.com/keel-framework/keel/pkg/event"
)

// App 是应用容器。
type App struct {
	Cfg      *config.Config
	Log      *zap.Logger
	DB       *gorm.DB
	Cache    cache.Cache
	Limiter  cache.Limiter
	Storage  *storage.Manager
	Mailer   mailer.Mailer
	Engine   *gin.Engine
	Jobs     *job.Service
	Audit    *audit.Service
	Events   *event.Bus
	Gateway  *micro.Gateway
	Instance string
	Started  time.Time

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
	// ---- 数据库结构变更 ----
	// versioned（默认）：按版本号执行，支持回滚与数据回填；
	// auto：沿用 AutoMigrate，只增不减，适合本地快速迭代；off：交给 CLI/CI 执行。
	migrationMode := cfg.Migration.ResolveMode()
	var migrationRunner *migrate.Runner
	allModels := append(append([]any{}, models()...), &model.UserOAuthBinding{})
	switch migrationMode {
	case "auto":
		if err := database.Migrate(db, allModels...); err != nil {
			return nil, err
		}
		lg.Info("数据库表结构已同步（AutoMigrate）")
	case "versioned":
		var err error
		migrationRunner, err = migrate.New(db, cfg.Migration, cfg.DB.ResolveDriver(), lg, models())
		if err != nil {
			return nil, err
		}
		applied, err := migrationRunner.Up(context.Background())
		if err != nil {
			return nil, fmt.Errorf("数据库版本迁移失败: %w", err)
		}
		lg.Info("数据库版本迁移完成", zap.Int("applied", len(applied)))
		if st, e := migrationRunner.Status(context.Background()); e == nil {
			lg.Info("当前数据库版本", zap.Int64("current", st.Current), zap.Int64("latest", st.Latest))
		}
	default:
		lg.Info("已跳过数据库迁移（migration.mode=off）")
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
	limiter := cache.NewLimiter(ch, cfg.RateLimit.RPS, cfg.RateLimit.Burst)

	store, err := storage.New(cfg.Storage, lg)
	if err != nil {
		return nil, err
	}
	// 未配置 mail.host 时返回 nil，不建立任何连接
	mail, err := mailer.New(cfg.Mail, lg)
	if err != nil {
		_ = store.Close()
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

	menuSvc := menu.NewService(db, authzLoader)
	authSvc := auth.NewService(db, ch, jwtMgr, captcha, cfg, lg, authzLoader, menuSvc)
	membershipSvc := membership.NewService(db, lg)
	userSvc := user.NewService(db, cfg, lg, authzLoader, membershipSvc)
	roleSvc := role.NewService(db, authzLoader)
	deptSvc := dept.NewService(db)
	dictSvc := system.NewDictService(db, ch)
	configSvc := system.NewConfigService(db, ch, cfg)
	msgSvc := message.NewService(db, lg)
	// 公告发布后同步投递站内信；message.Service 实现 system.NoticeNotifier。
	noticeSvc := system.NewNoticeService(db, msgSvc, lg)
	auditSvc := audit.NewService(db, ch, lg)
	apiKeySvc := apikey.NewService(db, ch, lg)
	newsSvc := news.NewService(db, ch, lg, cfg.News.RetentionDays)
	jobSvc := job.NewService(db, ch, lg)
	jobSvc.SetCleaner(auditSvc)
	jobSvc.SetMessageCleaner(msgSvc)
	// 资讯是滚动窗口数据，清理任务与业务同生命周期，不依赖外部调度。
	jobSvc.Register("news.cleanup", func(ctx context.Context, p map[string]any) (string, error) {
		days := 0
		if v, ok := p["days"].(float64); ok {
			days = int(v)
		}
		n, err := newsSvc.Cleanup(ctx, days)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已清理 %d 条过期资讯", n), nil
	})
	// 备份与版本迁移：同一份服务供 HTTP 管理接口、定时任务与 CLI 复用
	backupSvc := maintenance.NewBackupService(db, cfg.Backup, cfg.DB, lg)
	maintenanceHandler := maintenance.NewHandler(backupSvc, migrationRunner, migrationMode)
	jobSvc.Register("db.backup", func(ctx context.Context, _ map[string]any) (string, error) {
		if !cfg.Backup.Enabled {
			return "备份已在配置中关闭（backup.enabled=false），跳过", nil
		}
		meta, err := backupSvc.Backup(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已生成备份 %s（%d 字节）", meta.Name, meta.Size), nil
	})

	fileSvc := file.NewService(db, cfg, store, lg)
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
		Cfg: cfg, Log: lg, DB: db, Cache: ch, Limiter: limiter, Storage: store,
		JWT: jwtMgr, Authz: authzLoader, AuditSvc: auditSvc,
		Modules:     moduleSet(cfg.Micro),
		Events:      bus,
		Auth:        auth.NewHandler(authSvc, cfg.Security.CaptchaEnabled, cfg.Server.TrustProxy),
		User:        user.NewHandler(userSvc),
		Role:        role.NewHandler(roleSvc),
		Menu:        menu.NewHandler(menuSvc),
		Dept:        dept.NewHandler(deptSvc),
		Dict:        system.NewDictHandler(dictSvc),
		Config:      system.NewConfigHandler(configSvc),
		Notice:      system.NewNoticeHandler(noticeSvc),
		Message:     message.NewHandler(msgSvc),
		Membership:  membership.NewHandler(membershipSvc),
		APIKey:      apikey.NewHandler(apiKeySvc),
		APIKeySvc:   apiKeySvc,
		News:        news.NewHandler(newsSvc),
		NewsOpen:    news.NewOpenHandler(newsSvc),
		Audit:       audit.NewHandler(auditSvc),
		Job:         job.NewHandler(jobSvc),
		File:        file.NewHandler(fileSvc),
		Dashboard:   dashboard.NewHandler(dashSvc),
		Monitor:     monitor.NewHandler(monitorSvc),
		Maintenance: maintenanceHandler,
	})

	app := &App{Cfg: cfg, Log: lg, DB: db, Cache: ch, Limiter: limiter, Storage: store,
		Mailer: mail, Engine: engine, Jobs: jobSvc,
		Audit: auditSvc, Events: bus, Instance: resolveInstanceID(cfg), Started: time.Now()}
	if err := app.checkStateless(); err != nil {
		return nil, err
	}
	app.server = newServer(cfg, engine)
	app.logRuntime()
	return app, nil
}

// resolveInstanceID 返回实例标识：优先使用配置，否则用 hostname-pid，
// 便于在日志聚合与多实例排障时区分请求来自哪个进程。
func resolveInstanceID(cfg *config.Config) string {
	if id := strings.TrimSpace(cfg.Runtime.InstanceID); id != "" {
		return id
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "keel"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func models() []any {
	return []any{
		&model.User{}, &model.UserRole{}, &model.UserRecoveryCode{}, &model.Role{}, &model.RoleMenu{}, &model.Menu{},
		&model.Dept{}, &model.Session{}, &model.DictType{}, &model.DictItem{}, &model.SysConfig{},
		&model.Notice{}, &model.LoginLog{}, &model.OperationLog{}, &model.Job{}, &model.JobLog{},
		&model.File{}, &model.Message{}, &model.MessageRecipient{},
		&model.MembershipLevel{}, &model.APIKey{},
		&model.NewsSource{}, &model.NewsCategory{}, &model.NewsArticle{},
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
			fields = append(fields, zap.String("db", a.Cfg.DB.ResolveDriver()))
		}
		if a.Storage != nil {
			fields = append(fields, zap.String("storage", a.Storage.Kind()))
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
	if a.Storage != nil {
		_ = a.Storage.Close()
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
