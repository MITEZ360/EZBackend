package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// maintenanceRoutes 数据库备份与版本化迁移。
func (d Deps) maintenanceRoutes(g *gin.RouterGroup) {
	g.GET("/maintenance/backups", middleware.RequirePerm("ops:backup:list"), d.Maintenance.Backups)
	g.POST("/maintenance/backups", middleware.RequirePerm("ops:backup:create"), d.Maintenance.CreateBackup)
	g.GET("/maintenance/backups/:name/meta", middleware.RequirePerm("ops:backup:list"), d.Maintenance.BackupMeta)
	g.GET("/maintenance/backups/:name/download", middleware.RequirePerm("ops:backup:list"), d.Maintenance.DownloadBackup)
	g.POST("/maintenance/backups/:name/verify", middleware.RequirePerm("ops:backup:list"), d.Maintenance.VerifyBackup)
	g.POST("/maintenance/backups/:name/restore", middleware.RequirePerm("ops:backup:restore"), d.Maintenance.RestoreBackup)
	g.DELETE("/maintenance/backups/:name", middleware.RequirePerm("ops:backup:delete"), d.Maintenance.DeleteBackup)

	g.GET("/maintenance/migrations", middleware.RequirePerm("ops:backup:list"), d.Maintenance.MigrationStatus)
	g.POST("/maintenance/migrations/up", middleware.RequirePerm("ops:backup:restore"), d.Maintenance.MigrateUp)
	g.POST("/maintenance/migrations/down", middleware.RequirePerm("ops:backup:restore"), d.Maintenance.MigrateDown)
}
