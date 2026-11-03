// Package database 封装 GORM 初始化、连接池调优、自动迁移与健康检查。
package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/logger"
)

// Open 根据配置创建数据库连接。默认使用纯 Go 的 SQLite 驱动，零外部依赖即可启动；
// 生产可通过配置切换到 MySQL / PostgreSQL。
func Open(cfg config.DB, log *zap.Logger) (*gorm.DB, error) {
	if log == nil {
		log = zap.NewNop()
	}
	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, err
	}
	gcfg := &gorm.Config{
		Logger:                                   logger.Gorm(log, cfg),
		NowFunc:                                  func() time.Time { return time.Now().Local() },
		DisableForeignKeyConstraintWhenMigrating: true,
		PrepareStmt:                              true,
		SkipDefaultTransaction:                   false,
	}
	db, err := gorm.Open(dialector, gcfg)
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(atLeast(cfg.MaxOpenConns, 10))
	sqlDB.SetMaxIdleConns(atLeast(cfg.MaxIdleConns, 5))
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("数据库连通性检查失败: %w", err)
	}
	if cfg.LogLevel == "info" {
		db = db.Debug()
	}
	return db, nil
}

func dialectorFor(cfg config.DB) (gorm.Dialector, error) {
	switch cfg.ResolveDriver() {
	case "mysql":
		dsn := cfg.DSN
		if !strings.Contains(dsn, "parseTime") {
			sep := "?"
			if strings.Contains(dsn, "?") {
				sep = "&"
			}
			dsn += sep + "parseTime=True&loc=Local&charset=utf8mb4&collation=utf8mb4_unicode_ci"
		}
		return mysql.Open(dsn), nil
	case "postgres":
		return postgres.Open(cfg.DSN), nil
	case "sqlite", "":
		path := strings.TrimPrefix(cfg.DSN, "file:")
		if path == "" {
			path = "data/keel.db"
		}
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("创建数据目录失败: %w", err)
			}
		}
		// _pragma 参数提升并发写入稳定性
		return sqlite.Open(path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"), nil
	}
	return nil, fmt.Errorf("不支持的数据库驱动: %s", cfg.ResolveDriver())
}

func atLeast(v, min int) int {
	if v < min {
		return min
	}
	return v
}

// Migrate 执行自动迁移。
func Migrate(db *gorm.DB, models ...any) error {
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	return nil
}

// Health 检查数据库可用性。
func Health(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Stats 返回连接池统计，用于监控面板。
func Stats(db *gorm.DB) map[string]any {
	sqlDB, err := db.DB()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	s := sqlDB.Stats()
	return map[string]any{
		"open":          s.OpenConnections,
		"in_use":        s.InUse,
		"idle":          s.Idle,
		"max_open":      s.MaxOpenConnections,
		"wait_count":    s.WaitCount,
		"wait_duration": s.WaitDuration.String(),
	}
}

// Paginate 是通用的分页 Scope。
func Paginate(page, size int) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if page <= 0 {
			page = 1
		}
		if size <= 0 {
			size = 20
		}
		return db.Offset((page - 1) * size).Limit(size)
	}
}

var _ gormlogger.Interface
