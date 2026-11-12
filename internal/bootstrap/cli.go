package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/modules/maintenance"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/database"
	"github.com/keel-framework/keel/internal/platform/migrate"
)

// openDB 供运维子命令使用：只加载配置与数据库，不启动 HTTP / 定时任务。
func openDB(cfgPath string) (*config.Config, *gorm.DB, func(), error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, nil, err
	}
	db, err := database.Open(cfg.DB, zap.NewNop())
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() {
		if sqlDB, e := db.DB(); e == nil {
			_ = sqlDB.Close()
		}
	}
	return cfg, db, cleanup, nil
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err
}

// RunCommand 执行运维子命令，返回 (是否识别该命令, error)。
func RunCommand(args []string, cfgPath string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "migrate":
		return true, migrateCommand(args[1:], cfgPath)
	case "backup":
		return true, backupCommand(cfgPath)
	case "restore":
		if len(args) < 2 {
			return true, fmt.Errorf("用法: keel restore <备份文件名或路径>")
		}
		return true, restoreCommand(cfgPath, args[1])
	case "help", "-h", "--help":
		printCommandUsage()
		return true, nil
	default:
		return false, fmt.Errorf("未知子命令: %s（可用 migrate / backup / restore / help）", args[0])
	}
}

func printCommandUsage() {
	fmt.Fprintln(os.Stdout, `Keel 运维子命令（均需 -config 指定同一份配置文件）：

  keel -config configs/config.yaml migrate status          查看版本状态
  keel -config configs/config.yaml migrate up              升级到最新版本
  keel -config configs/config.yaml migrate down            回滚最近一个版本
  keel -config configs/config.yaml migrate up-to 2         升级到指定版本
  keel -config configs/config.yaml migrate down-to 1       回滚到指定版本
  keel -config configs/config.yaml backup                 立即生成一份备份
  keel -config configs/config.yaml restore <文件名|路径>   停机恢复 SQLite 数据文件

恢复 SQLite 前必须先停止服务；恢复会自动把当前库另存为 pre-restore-*.db。`)
}

func migrateCommand(args []string, cfgPath string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: keel migrate <status|up|down|up-to N|down-to N>")
	}
	cfg, db, cleanup, err := openDB(cfgPath)
	if err != nil {
		return err
	}
	defer cleanup()
	runner, err := migrate.New(db, cfg.Migration, cfg.DB.ResolveDriver(), zap.NewNop(), models())
	if err != nil {
		return err
	}
	ctx := context.Background()
	op := args[0]
	switch op {
	case "status":
		st, err := runner.Status(ctx)
		if err != nil {
			return err
		}
		return printJSON(st)
	case "up":
		res, err := runner.Up(ctx)
		if err != nil {
			return err
		}
		return printJSON(res)
	case "down":
		res, err := runner.Down(ctx)
		if err != nil {
			return err
		}
		return printJSON(res)
	case "up-to", "down-to":
		if len(args) < 2 {
			return fmt.Errorf("用法: keel migrate %s <版本号>", op)
		}
		var version int64
		if _, err := fmt.Sscanf(args[1], "%d", &version); err != nil || version < 0 {
			return fmt.Errorf("版本号必须是非负整数: %s", args[1])
		}
		if op == "up-to" {
			res, err := runner.UpTo(ctx, version)
			if err != nil {
				return err
			}
			return printJSON(res)
		}
		res, err := runner.DownTo(ctx, version)
		if err != nil {
			return err
		}
		return printJSON(res)
	default:
		return fmt.Errorf("未知的 migrate 操作: %s", op)
	}
}

func backupCommand(cfgPath string) error {
	cfg, db, cleanup, err := openDB(cfgPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc := maintenance.NewBackupService(db, cfg.Backup, cfg.DB, zap.NewNop())
	meta, err := svc.Backup(context.Background())
	if err != nil {
		return err
	}
	return printJSON(meta)
}

func restoreCommand(cfgPath, name string) error {
	cfg, db, cleanup, err := openDB(cfgPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc := maintenance.NewBackupService(db, cfg.Backup, cfg.DB, zap.NewNop())
	res, err := svc.RestoreOffline(name)
	if err != nil {
		return err
	}
	return printJSON(res)
}
