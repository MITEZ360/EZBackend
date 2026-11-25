// Package bootstrap 负责应用装配：配置、日志、数据库、缓存、服务、路由与生命周期。
//
// 本包不认识任何具体业务模块：模块清单来自 modulekit 的注册结果（由
// internal/distribution 的导入决定），装配动作由各模块的 Setup 完成。
// 加一个模块不需要改这里的任何代码。
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
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/database"
	"github.com/keel-framework/keel/internal/platform/datetime"
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/internal/platform/logger"
	"github.com/keel-framework/keel/internal/platform/mailer"
	"github.com/keel-framework/keel/internal/platform/migrate"
	"github.com/keel-framework/keel/internal/platform/modulekit"
	"github.com/keel-framework/keel/internal/platform/storage"
	"github.com/keel-framework/keel/internal/router"
	"github.com/keel-framework/keel/pkg/discovery"
	"github.com/keel-framework/keel/pkg/event"
)

// App 是应用容器。
type App struct {
	Cfg       *config.Config
	Log       *zap.Logger
	DB        *gorm.DB
	Cache     cache.Cache
	Limiter   cache.Limiter
	Storage   *storage.Manager
	Mailer    mailer.Mailer
	Engine    *gin.Engine
	Events    *event.Bus
	Gateway   *micro.Gateway
	Instance  string
	Started   time.Time
	SelfCheck *SelfCheckReport

	// Modules 记录本次启动启用的模块，供管理端与自检展示。
	Modules map[string]bool
	// mods 是模块装配上下文：启动与停机回调都由模块自己注册。
	mods *modulekit.Context

	server    *http.Server
	restartCh chan struct{}
}

// RequestRestart 由管理端运维接口调用，触发进程原地重启。
func (a *App) RequestRestart() {
	if a == nil || a.restartCh == nil {
		return
	}
	select {
	case a.restartCh <- struct{}{}:
	default: // 已有重启请求在处理中
	}
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

	// 时区必须在任何时间相关组件（GORM、cron、日志轮转、备份）之前装配。
	if err := datetime.Configure(cfg.App.Timezone); err != nil {
		return nil, err
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

	// 数据库先于模块解析打开：管理端保存的覆盖值（含 modules.enabled）存在
	// runtime_config 表里，不先读库就无法按最新开关装配。覆盖值的合并由
	// 配置中心模块注册的早期动作完成，本包不知道它的存在。
	db, err := database.Open(cfg.DB, lg)
	if err != nil {
		return nil, err
	}
	for _, err := range modulekit.RunBootstrap(context.Background(), db) {
		lg.Warn("启动早期动作失败，继续使用配置文件", zap.Error(err))
	}
	cfg = config.Get()

	// 模块注册表：解析启停白名单、补齐依赖、按依赖排序、未知模块 fail-fast。
	registry, err := resolveModules(cfg)
	if err != nil {
		closeDB(db)
		return nil, err
	}
	lg.Info("业务模块已解析",
		zap.String("source", registry.Source),
		zap.Int("count", len(registry.Ordered)),
		zap.Strings("modules", registry.Ordered))

	// ---- 数据库结构变更 ----
	// versioned（默认）：按版本号执行，支持回滚与数据回填；
	// auto：沿用 AutoMigrate，只增不减，适合本地快速迭代；off：交给 CLI/CI 执行。
	migrationMode := cfg.Migration.ResolveMode()
	var migrationRunner *migrate.Runner
	// 建表跟随模块开关：关闭的模块不建表。版本化迁移的 baseline 也用它，
	// 因此后续新增的模块专属迁移必须先用 HasTable 判空，否则关闭该模块后
	// 迁移会因目标表不存在而失败。
	switch migrationMode {
	case "auto":
		if err := database.Migrate(db, registry.scopeModels()...); err != nil {
			return nil, err
		}
		lg.Info("数据库表结构已同步（AutoMigrate）")
	case "versioned":
		var err error
		migrationRunner, err = migrate.New(db, cfg.Migration, cfg.DB.ResolveDriver(), lg, registry.scopeModels())
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

	// 补齐可选模块缺失的表。
	// 版本化迁移是一次性的：模块先关闭后重新启用时，baseline 早已执行完毕，
	// 不会回头建表。这里只对「启用且表不存在」的可选模块做幂等建表，
	// 不碰底座表，也不修改已存在的表结构。
	if missing := missingModuleModels(db, registry); len(missing) > 0 {
		if err := database.Migrate(db, missing...); err != nil {
			return nil, fmt.Errorf("补齐模块数据表失败: %w", err)
		}
		lg.Info("已补齐重新启用模块的数据表", zap.Int("tables", len(missing)))
	}

	if cfg.DB.Seed {
		if err := seed(db, lg, registry); err != nil {
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

	// 配置热更新的组件侧动作：限流桶速率、令牌有效期、日志级别。
	// 其余配置项由持有 cfg 指针的服务直接读取，config.Apply 已原地覆盖。
	lastRate := cfg.RateLimit
	config.OnChange(func(c *config.Config) {
		if c.RateLimit.RPS != lastRate.RPS || c.RateLimit.Burst != lastRate.Burst {
			limiter.SetRate(c.RateLimit.RPS, c.RateLimit.Burst)
		}
		lastRate = c.RateLimit
		jwtMgr.Update(c.JWT)
		if !logger.SetLevel(c.Log.Level) {
			lg.Warn("日志级别非法，保持原级别", zap.String("level", c.Log.Level))
		}
	})
	authzLoader := authz.NewLoader(db, ch, lg)

	// 模块装配上下文。engine 与 app 在装配末尾才就绪，闭包延迟解引用。
	var engine *gin.Engine
	var app *App
	mods := modulekit.NewContext(modulekit.Context{
		DB: db, Cache: ch, Limiter: limiter, Config: cfg, Log: lg, Events: bus,
		Storage: store, Mailer: mail, JWT: jwtMgr, Captcha: captcha, Authz: authzLoader,
		Migration: migrationRunner, MigrationMode: migrationMode,
		Instance: resolveInstanceID(cfg),
		Restart:  func() { app.RequestRestart() },
		Routes: func() []modulekit.Route {
			if engine == nil {
				return nil
			}
			routes := engine.Routes()
			out := make([]modulekit.Route, 0, len(routes))
			for _, r := range routes {
				out = append(out, modulekit.Route{Method: r.Method, Path: r.Path, Name: r.Handler})
			}
			return out
		},
	})

	// 依次装配模块。顺序由依赖图决定：被依赖的模块先 Setup 并 Provide 服务。
	for _, spec := range registry.specs {
		if spec.Setup == nil {
			continue
		}
		if err := spec.Setup(mods); err != nil {
			closeDB(db)
			return nil, fmt.Errorf("模块 %s 装配失败: %w", spec.Name, err)
		}
	}
	// 跨模块依赖注入：此时所有服务都已 Provide。
	if err := mods.RunAfter(); err != nil {
		closeDB(db)
		return nil, err
	}
	// 把各模块注册的任务实现交给调度模块。
	mods.FlushJobs()

	engine = router.New(router.Deps{
		Cfg: cfg, Log: lg, DB: db, Cache: ch, Limiter: limiter, Storage: store,
		JWT: jwtMgr, Authz: authzLoader, Modules: mods,
	})

	app = &App{Cfg: cfg, Log: lg, DB: db, Cache: ch, Limiter: limiter, Storage: store,
		Mailer: mail, Engine: engine, restartCh: make(chan struct{}, 1),
		Events: bus, Instance: resolveInstanceID(cfg), Started: time.Now(),
		Modules: registry.asSet(), mods: mods}

	// 配置中心需要知道实际启用的集合（含依赖补齐），供管理端区分
	// 「配置值」与「当前运行」。
	if r, ok := mods.Lookup("configcenter").(interface {
		SetActiveModules([]string, string)
	}); ok {
		r.SetActiveModules(registry.Ordered, registry.Source)
	}

	// 启动自检：关键依赖（数据库/缓存/存储）不可用时直接 fail-fast；
	// 迁移版本落后、种子为空等只告警不阻断。报告挂在 App 上便于测试断言。
	report, err := runSelfChecks(context.Background(), selfCheckDeps{
		DB: db, Cache: ch, Storage: store,
		MigrationRunner: migrationRunner, MigrationMode: migrationMode, SeedEnabled: cfg.DB.Seed,
	}, lg)
	if err != nil {
		return nil, err
	}
	app.SelfCheck = report
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

// Run 启动服务并阻塞，收到信号后优雅停机。
func (a *App) Run() error {
	// 后台循环（调度器、集群心跳）由模块在装配时注册，模块被裁掉时不存在。
	for _, err := range a.mods.Start(context.Background()) {
		a.Log.Warn("模块启动回调失败", zap.Error(err))
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
	case <-a.restartCh:
		a.Log.Info("管理端触发重启，正在重新执行进程")
		ctx, cancel := context.WithTimeout(context.Background(), a.Cfg.Server.ShutdownTimeout)
		if err := a.Shutdown(ctx); err != nil {
			a.Log.Warn("重启前优雅停机异常", zap.Error(err))
		}
		cancel()
		if err := execSelf(); err != nil {
			a.Log.Error("自重启失败", zap.Error(err))
			return err
		}
		return nil
	}
}

// Shutdown 依次停止模块后台循环、HTTP、事件总线与存储连接。
func (a *App) Shutdown(ctx context.Context) error {
	var err error
	// 先停后台循环，避免重启过程中又领到新指令。
	if a.mods != nil {
		a.mods.Stop()
	}
	if a.server != nil {
		err = a.server.Shutdown(ctx)
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

// closeDB 在装配中途失败时释放连接池。数据库现在先于模块解析打开，
// 白名单写错之类的早期错误必须顺手把连接关掉再退出。
func closeDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
