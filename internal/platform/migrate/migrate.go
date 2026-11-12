// Package migrate 提供版本化数据库结构变更。
//
// 版本账本、执行顺序与回滚由 mature 组件 goose 负责；每个版本的实际 DDL
// 用 GORM Migrator 编写，因此同一份迁移函数可覆盖 SQLite / MySQL / PostgreSQL，
// 不必为每种方言维护一套 SQL 文件。迁移随二进制编译，单文件部署不依赖外部脚本。
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
	goosedb "github.com/pressly/goose/v3/database"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/platform/config"
)

// Runner 版本化迁移执行器。
type Runner struct {
	db     *gorm.DB
	sqlDB  *sql.DB
	driver string
	table  string
	log    *zap.Logger
	models []any
}

// New 创建迁移执行器。models 是 baseline 版本需要同步的全量模型。
func New(db *gorm.DB, cfg config.Migration, driver string, log *zap.Logger, models []any) (*Runner, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层连接失败: %w", err)
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Runner{
		db: db, sqlDB: sqlDB, driver: driver,
		table: cfg.VersionTable(), log: log, models: models,
	}, nil
}

func dialect(driver string) (goosedb.Dialect, error) {
	switch driver {
	case "sqlite", "":
		return goosedb.DialectSQLite3, nil
	case "mysql":
		return goosedb.DialectMySQL, nil
	case "postgres":
		return goosedb.DialectPostgres, nil
	default:
		return "", fmt.Errorf("版本化迁移暂不支持数据库驱动: %s", driver)
	}
}

// provider 每次构造一个独立 Provider：不使用 goose 全局注册表，
// 避免同一进程内多次装配（测试、CLI 子命令）互相污染。
func (r *Runner) provider() (*goose.Provider, error) {
	d, err := dialect(r.driver)
	if err != nil {
		return nil, err
	}
	store, err := goosedb.NewStore(d, r.table)
	if err != nil {
		return nil, fmt.Errorf("创建迁移版本存储失败: %w", err)
	}
	defs := r.definitions()
	migrations := make([]*goose.Migration, 0, len(defs))
	for _, def := range defs {
		up, down := def.Up, def.Down
		upFn := &goose.GoFunc{RunDB: func(ctx context.Context, _ *sql.DB) error {
			return up(ctx, r.db)
		}}
		downFn := &goose.GoFunc{RunDB: func(ctx context.Context, _ *sql.DB) error {
			return down(ctx, r.db)
		}}
		migrations = append(migrations, goose.NewGoMigration(def.Version, upFn, downFn))
	}
	return goose.NewProvider("", r.sqlDB, nil,
		goose.WithStore(store),
		goose.WithGoMigrations(migrations...),
		goose.WithDisableGlobalRegistry(true),
	)
}

// ApplyResult 单个版本的执行结果。
type ApplyResult struct {
	Version   int64  `json:"version"`
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Duration  string `json:"duration"`
}

func (r *Runner) nameOf(version int64) string {
	for _, d := range r.definitions() {
		if d.Version == version {
			return d.Name
		}
	}
	return ""
}

func toResults(list []*goose.MigrationResult) []ApplyResult {
	out := make([]ApplyResult, 0, len(list))
	for _, m := range list {
		out = append(out, ApplyResult{
			Version: m.Source.Version, Name: "", Direction: m.Direction,
			Duration: m.Duration.Round(time.Millisecond).String(),
		})
	}
	return out
}

func (r *Runner) fillNames(out []ApplyResult) []ApplyResult {
	for i := range out {
		out[i].Name = r.nameOf(out[i].Version)
	}
	return out
}

// Up 升级到最新版本。
func (r *Runner) Up(ctx context.Context) ([]ApplyResult, error) {
	p, err := r.provider()
	if err != nil {
		return nil, err
	}
	res, err := p.Up(ctx)
	if err != nil {
		return nil, err
	}
	return r.fillNames(toResults(res)), nil
}

// UpTo 升级到指定版本。
func (r *Runner) UpTo(ctx context.Context, version int64) ([]ApplyResult, error) {
	p, err := r.provider()
	if err != nil {
		return nil, err
	}
	res, err := p.UpTo(ctx, version)
	if err != nil {
		return nil, err
	}
	return r.fillNames(toResults(res)), nil
}

// Down 回滚最近一个版本。
func (r *Runner) Down(ctx context.Context) ([]ApplyResult, error) {
	p, err := r.provider()
	if err != nil {
		return nil, err
	}
	res, err := p.Down(ctx)
	if err != nil {
		return nil, err
	}
	return r.fillNames(toResults([]*goose.MigrationResult{res})), nil
}

// DownTo 回滚到指定版本（该版本保留，之上的全部撤销）。
func (r *Runner) DownTo(ctx context.Context, version int64) ([]ApplyResult, error) {
	p, err := r.provider()
	if err != nil {
		return nil, err
	}
	res, err := p.DownTo(ctx, version)
	if err != nil {
		return nil, err
	}
	return r.fillNames(toResults(res)), nil
}

// Version 返回当前已应用的版本号。
func (r *Runner) Version(ctx context.Context) (int64, error) {
	p, err := r.provider()
	if err != nil {
		return 0, err
	}
	return p.GetDBVersion(ctx)
}

// VersionInfo 版本状态。
type VersionInfo struct {
	Version   int64  `json:"version"`
	Name      string `json:"name"`
	Applied   bool   `json:"applied"`
	AppliedAt string `json:"appliedAt,omitempty"`
}

// Status 返回全部版本的应用状态。
func (r *Runner) Status(ctx context.Context) (*StatusResult, error) {
	p, err := r.provider()
	if err != nil {
		return nil, err
	}
	current, err := p.GetDBVersion(ctx)
	if err != nil {
		return nil, err
	}
	states, err := p.Status(ctx)
	if err != nil {
		return nil, err
	}
	appliedAt := map[int64]string{}
	for _, s := range states {
		if s.Source == nil {
			continue
		}
		if s.State == goose.StateApplied && !s.AppliedAt.IsZero() {
			appliedAt[s.Source.Version] = s.AppliedAt.UTC().Format(time.RFC3339)
		}
	}
	defs := r.definitions()
	versions := make([]VersionInfo, 0, len(defs))
	pending := 0
	for _, d := range defs {
		at := appliedAt[d.Version]
		applied := at != "" || d.Version <= current
		if !applied {
			pending++
		}
		versions = append(versions, VersionInfo{
			Version: d.Version, Name: d.Name, Applied: applied, AppliedAt: at,
		})
	}
	return &StatusResult{
		Driver: r.driver, Table: r.table, Current: current,
		Latest: defs[len(defs)-1].Version, Pending: pending, Versions: versions,
	}, nil
}

// StatusResult 迁移状态汇总。
type StatusResult struct {
	Driver   string        `json:"driver"`
	Table    string        `json:"table"`
	Current  int64         `json:"current"`
	Latest   int64         `json:"latest"`
	Pending  int           `json:"pending"`
	Versions []VersionInfo `json:"versions"`
}
