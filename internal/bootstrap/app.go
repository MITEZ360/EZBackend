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
	"github.com/keel-framework/keel/internal/modules/cacheadmin"
	"github.com/keel-framework/keel/internal/modules/cluster"
	"github.com/keel-framework/keel/internal/modules/configcenter"
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
	"github.com/keel-framework/keel/internal/modules/routemanage"
	"github.com/keel-framework/keel/internal/modules/system"
	"github.com/keel-framework/keel/internal/modules/user"
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
	Jobs      *job.Service
	Audit     *audit.Service
	Cluster   *cluster.Service
	Events    *event.Bus
	Gateway   *micro.Gateway
	Instance  string
	Started   time.Time
	SelfCheck *SelfCheckReport

	// Modules 记录本次启动启用的模块，供运行期判断是否启动相关后台循环。
	Modules map[string]bool

	server    *http.Server
	restartCh chan struct{}
}

// moduleOn 判断模块是否启用；未设置时按全部启用处理。
func (a *App) moduleOn(name string) bool {
	if len(a.Modules) == 0 {
		return true
	}
	return a.Modules[name]
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
	// runtime_config 表里，不先读库就无法按最新开关装配。
	db, err := database.Open(cfg.DB, lg)
	if err != nil {
		return nil, err
	}
	// 配置中心：把管理端覆盖值合并进运行配置。必须早于模块解析与其它组件，
	// 否则它们会拿到配置文件里的旧值。首次启动覆盖表尚不存在，跳过即可。
	runtimeCfg := configcenter.NewService(db, cfg, lg)
	if db.Migrator().HasTable(&model.RuntimeConfig{}) {
		if err := runtimeCfg.ApplyFromDB(context.Background()); err != nil {
			lg.Warn("加载运行时配置失败，继续使用配置文件", zap.Error(err))
		}
		cfg = config.Get()
	}

	// 模块注册表：解析启停白名单、补齐依赖、未知模块 fail-fast。
	registry, err := resolveModules(cfg)
	if err != nil {
		closeDB(db)
		return nil, err
	}
	lg.Info("业务模块已解析",
		zap.String("source", registry.Source),
		zap.Int("count", len(registry.Ordered)),
		zap.Strings("modules", registry.Ordered))
	// 实际启用集合会因依赖补齐而大于配置值，管理端用它说明"当前运行"。
	runtimeCfg.SetActiveModules(registry.Ordered, registry.Source)

	// ---- 数据库结构变更 ----
	// versioned（默认）：按版本号执行，支持回滚与数据回填；
	// auto：沿用 AutoMigrate，只增不减，适合本地快速迭代；off：交给 CLI/CI 执行。
	migrationMode := cfg.Migration.ResolveMode()
	var migrationRunner *migrate.Runner
	// 建表跟随模块开关：关闭的模块不建表。版本化迁移的 baseline 也用它，
	// 因此后续新增的模块专属迁移必须先用 HasTable 判空，否则关闭该模块后
	// 迁移会因目标表不存在而失败。
	allModels := append(append([]any{}, registry.scopeModels()...), &model.UserOAuthBinding{})
	switch migrationMode {
	case "auto":
		if err := database.Migrate(db, allModels...); err != nil {
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

	// 配置中心已在模块解析前完成覆盖合并，这里直接复用同一实例注册接口。
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
	if registry.On("news") {
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
	}
	// 备份与版本迁移：同一份服务供 HTTP 管理接口、定时任务与 CLI 复用
	backupSvc := maintenance.NewBackupService(db, cfg.Backup, cfg.DB, cfg.Storage, lg)
	maintenanceHandler := maintenance.NewHandler(backupSvc, migrationRunner, migrationMode)
	if registry.On("maintenance") {
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
	}

	fileSvc := file.NewService(db, cfg, store, lg)
	dashSvc := dashboard.NewService(db, ch)

	var engine *gin.Engine
	var app *App // 重启回调在运行时才解引用，此处先声明
	// 集群服务需要 app 的重启入口，闭包延迟解引用即可。
	clusterSvc := cluster.NewService(db, cfg, lg, runtimeCfg, func() { app.RequestRestart() })
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
	}, func() { app.RequestRestart() })
	routeSvc := routemanage.NewService(func() []routemanage.Route {
		if engine == nil {
			return nil
		}
		routes := engine.Routes()
		out := make([]routemanage.Route, 0, len(routes))
		for _, r := range routes {
			// gin 的 Handler 形如 pkg.(*Handler).List-fm，去掉包路径与编译后缀
			name := r.Handler
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i+1:]
			}
			if i := strings.Index(name, "-"); i >= 0 {
				name = name[:i]
			}
			out = append(out, routemanage.Route{Method: r.Method, Path: r.Path, Name: name})
		}
		return out
	})

	engine = router.New(router.Deps{
		Cfg: cfg, Log: lg, DB: db, Cache: ch, Limiter: limiter, Storage: store,
		JWT: jwtMgr, Authz: authzLoader, AuditSvc: auditSvc,
		Modules:      registry.asSet(),
		Events:       bus,
		Auth:         auth.NewHandler(authSvc, cfg.Security.CaptchaEnabled, cfg.Server.TrustProxy),
		User:         user.NewHandler(userSvc),
		Role:         role.NewHandler(roleSvc),
		Menu:         menu.NewHandler(menuSvc),
		Dept:         dept.NewHandler(deptSvc),
		Dict:         system.NewDictHandler(dictSvc),
		Config:       system.NewConfigHandler(configSvc),
		Notice:       system.NewNoticeHandler(noticeSvc),
		Message:      message.NewHandler(msgSvc),
		Membership:   membership.NewHandler(membershipSvc),
		APIKey:       apikey.NewHandler(apiKeySvc),
		APIKeySvc:    apiKeySvc,
		News:         news.NewHandler(newsSvc),
		NewsOpen:     news.NewOpenHandler(newsSvc),
		Audit:        audit.NewHandler(auditSvc),
		Job:          job.NewHandler(jobSvc),
		File:         file.NewHandler(fileSvc),
		Dashboard:    dashboard.NewHandler(dashSvc),
		Monitor:      monitor.NewHandler(monitorSvc),
		Route:        routemanage.NewHandler(routeSvc),
		CacheAdmin:   cacheadmin.NewHandler(cacheadmin.NewService(ch)),
		Maintenance:  maintenanceHandler,
		ConfigCenter: configcenter.NewHandler(runtimeCfg),
		Cluster:      cluster.NewHandler(clusterSvc),
	})

	app = &App{Cfg: cfg, Log: lg, DB: db, Cache: ch, Limiter: limiter, Storage: store,
		Mailer: mail, Engine: engine, Jobs: jobSvc, Cluster: clusterSvc, restartCh: make(chan struct{}, 1),
		Audit: auditSvc, Events: bus, Instance: resolveInstanceID(cfg), Started: time.Now(),
		Modules: registry.asSet()}
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

func models() []any {
	return []any{
		&model.User{}, &model.UserRole{}, &model.UserRecoveryCode{}, &model.Role{}, &model.RoleMenu{}, &model.Menu{},
		&model.Dept{}, &model.Session{}, &model.DictType{}, &model.DictItem{}, &model.SysConfig{},
		&model.Notice{}, &model.LoginLog{}, &model.OperationLog{}, &model.Job{}, &model.JobLog{},
		&model.File{}, &model.Message{}, &model.MessageRecipient{},
		&model.MembershipLevel{}, &model.APIKey{},
		&model.NewsSource{}, &model.NewsCategory{}, &model.NewsArticle{},
		&model.RuntimeConfig{}, &model.ClusterNode{},
		&model.ClusterCommand{}, &model.ClusterCommandTarget{},
	}
}

// Run 启动服务并阻塞，收到信号后优雅停机。
func (a *App) Run() error {
	// 模块被关闭时不启动后台循环：相关表可能没建，跑起来只会反复报错。
	if a.Jobs != nil && a.moduleOn("job") {
		if err := a.Jobs.Start(context.Background()); err != nil {
			a.Log.Warn("定时任务启动失败", zap.Error(err))
		}
	}
	if a.Cluster != nil && a.moduleOn("cluster") {
		a.Cluster.Start(context.Background())
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

// Shutdown 依次停止 HTTP、定时任务、审计队列与存储连接。
func (a *App) Shutdown(ctx context.Context) error {
	var err error
	if a.Cluster != nil {
		// 先停后台循环，避免重启过程中又领到新指令。
		a.Cluster.Close()
	}
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
