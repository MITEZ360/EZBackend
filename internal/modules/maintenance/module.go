package maintenance

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/backup"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/maintenance",
		Name:  "maintenance",
		Label: "系统维护",
		Desc:  "服务信息、自检与基础运维动作",
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_maintenance", Parent: "m_ops", Name: "备份与迁移", Type: model.MenuPage, Path: "/ops/maintenance",
				Icon: "server", Perm: "ops:backup:list", Sort: 7},
			{ID: "m_maintenance_backup", Parent: "m_maintenance", Name: "立即备份", Type: model.MenuButton, Perm: "ops:backup:create"},
			{ID: "m_maintenance_restore", Parent: "m_maintenance", Name: "恢复与回滚", Type: model.MenuButton, Perm: "ops:backup:restore"},
			{ID: "m_maintenance_del", Parent: "m_maintenance", Name: "删除备份", Type: model.MenuButton, Perm: "ops:backup:delete"},
		},
		Jobs: []modulekit.Job{
			{Name: "数据库备份", Cron: "0 2 * * *", Handler: "db.backup", Params: `{}`,
				Remark: "每天 02:00 生成数据库快照，按 backup.retain 保留最近 N 份；backup.enabled=false 时跳过"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	svc := backup.NewService(c.DB, c.Config.Backup, c.Config.DB, c.Config.Storage, c.Log)
	h := NewHandler(svc, c.Migration, c.MigrationMode)

	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/maintenance/backups", middleware.RequirePerm("ops:backup:list"), h.Backups)
		g.POST("/maintenance/backups", middleware.RequirePerm("ops:backup:create"), h.CreateBackup)
		g.GET("/maintenance/backups/:name/meta", middleware.RequirePerm("ops:backup:list"), h.BackupMeta)
		g.GET("/maintenance/backups/:name/download", middleware.RequirePerm("ops:backup:list"), h.DownloadBackup)
		g.POST("/maintenance/backups/:name/verify", middleware.RequirePerm("ops:backup:list"), h.VerifyBackup)
		g.POST("/maintenance/backups/:name/restore", middleware.RequirePerm("ops:backup:restore"), h.RestoreBackup)
		g.DELETE("/maintenance/backups/:name", middleware.RequirePerm("ops:backup:delete"), h.DeleteBackup)
		g.GET("/maintenance/prune-plan", middleware.RequirePerm("ops:backup:list"), h.PrunePlan)

		g.GET("/maintenance/migrations", middleware.RequirePerm("ops:backup:list"), h.MigrationStatus)
		g.POST("/maintenance/migrations/up", middleware.RequirePerm("ops:backup:restore"), h.MigrateUp)
		g.POST("/maintenance/migrations/down", middleware.RequirePerm("ops:backup:restore"), h.MigrateDown)
	})

	c.Job("db.backup", func(ctx context.Context, _ map[string]any) (string, error) {
		if !c.Config.Backup.Enabled {
			return "备份已在配置中关闭（backup.enabled=false），跳过", nil
		}
		meta, err := svc.Backup(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已生成备份 %s（%d 字节）", meta.Name, meta.Size), nil
	})
	return nil
}
