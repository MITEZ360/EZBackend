package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/platform/backup"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/database"
	"github.com/MITEZ360/tenon/internal/platform/migrate"
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
			return true, fmt.Errorf("用法: tenon restore [--dry-run] <备份文件名或路径>")
		}
		if len(args) >= 3 && (args[1] == "--dry-run" || args[1] == "-n") {
			return true, restoreCommand(cfgPath, args[2], true)
		}
		return true, restoreCommand(cfgPath, args[1], false)
	case "backup-prune":
		dryRun := len(args) >= 2 && (args[1] == "--dry-run" || args[1] == "-n")
		return true, pruneCommand(cfgPath, dryRun)
	case "help", "-h", "--help":
		printCommandUsage()
		return true, nil
	default:
		return false, fmt.Errorf("未知子命令: %s（可用 migrate / backup / restore / backup-prune / help）", args[0])
	}
}

func printCommandUsage() {
	fmt.Fprintln(os.Stdout, `Tenon 运维子命令（均需 -config 指定同一份配置文件）：

  tenon -config configs/config.yaml migrate status          查看版本状态
  tenon -config configs/config.yaml migrate up              升级到最新版本
  tenon -config configs/config.yaml migrate down            回滚最近一个版本
  tenon -config configs/config.yaml migrate up-to 2         升级到指定版本
  tenon -config configs/config.yaml migrate down-to 1       回滚到指定版本
  tenon -config configs/config.yaml backup                 生成一份备份（数据库 + 本地文件资产）
  tenon -config configs/config.yaml restore <文件名|路径>   停机恢复 SQLite 数据文件与文件资产
  tenon -config configs/config.yaml restore --dry-run <文件> 只输出恢复计划，不改动任何文件
  tenon -config configs/config.yaml backup-prune --dry-run  演练 GFS 保留策略，列出将删除的备份

恢复 SQLite 前必须先停止服务；恢复会自动把当前库另存为 pre-restore-*.db，
文件资产会先改名保留旧目录再原子替换。备份产物为 tar 容器，含 database.db 与 files/。`)
}

func migrateCommand(args []string, cfgPath string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: tenon migrate <status|up|down|up-to N|down-to N>")
	}
	cfg, db, cleanup, err := openDB(cfgPath)
	if err != nil {
		return err
	}
	defer cleanup()
	runner, err := migrate.New(db, cfg.Migration, cfg.DB.ResolveDriver(), zap.NewNop(), allModels())
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
			return fmt.Errorf("用法: tenon migrate %s <版本号>", op)
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
	svc := backup.NewService(db, cfg.Backup, cfg.DB, cfg.Storage, zap.NewNop())
	meta, err := svc.Backup(context.Background())
	if err != nil {
		return err
	}
	return printJSON(meta)
}

func restoreCommand(cfgPath, name string, dryRun bool) error {
	cfg, db, cleanup, err := openDB(cfgPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc := backup.NewService(db, cfg.Backup, cfg.DB, cfg.Storage, zap.NewNop())
	if dryRun {
		res, err := svc.RestorePlan(name)
		if err != nil {
			return err
		}
		return printJSON(res)
	}
	res, err := svc.RestoreOffline(name)
	if err != nil {
		return err
	}
	return printJSON(res)
}

func pruneCommand(cfgPath string, dryRun bool) error {
	cfg, db, cleanup, err := openDB(cfgPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc := backup.NewService(db, cfg.Backup, cfg.DB, cfg.Storage, zap.NewNop())
	plan, err := svc.PruneDryRun()
	if err != nil {
		return err
	}
	if dryRun {
		return printJSON(plan)
	}
	// 非演练：真正执行删除，再返回执行后的保留/删除清单
	for _, name := range plan.Remove {
		if err := svc.Delete(name); err != nil {
			return err
		}
	}
	plan.DryRun = false
	return printJSON(plan)
}
