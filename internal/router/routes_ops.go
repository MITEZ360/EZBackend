package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// auditRoutes 登录日志、操作日志与在线用户。
func (d Deps) auditRoutes(g *gin.RouterGroup) {
	g.GET("/logs/login", middleware.RequirePerm("ops:loginlog:list"), d.Audit.LoginLogs)
	g.DELETE("/logs/login", middleware.RequirePerm("ops:loginlog:list"), d.Audit.ClearLoginLogs)
	g.GET("/logs/operation", middleware.RequirePerm("ops:oplog:list"), d.Audit.OperationLogs)
	g.GET("/logs/operation/:id", middleware.RequirePerm("ops:oplog:list"), d.Audit.OperationLogDetail)
	g.DELETE("/logs/operation", middleware.RequirePerm("ops:oplog:list"), d.Audit.ClearOperationLogs)
	g.GET("/online/sessions", middleware.RequirePerm("ops:online:list"), d.Audit.Online)
	g.DELETE("/online/sessions/:id", middleware.RequirePerm("ops:online:list"), d.Audit.Kick)
}

// jobRoutes 定时任务。
func (d Deps) jobRoutes(g *gin.RouterGroup) {
	g.GET("/jobs", middleware.RequirePerm("ops:job:list"), d.Job.List)
	g.GET("/jobs/handlers", middleware.RequirePerm("ops:job:list"), d.Job.Handlers)
	g.POST("/jobs", middleware.RequirePerm("ops:job:create"), d.Job.Create)
	g.PUT("/jobs/:id", middleware.RequirePerm("ops:job:update"), d.Job.Update)
	g.DELETE("/jobs/:id", middleware.RequirePerm("ops:job:delete"), d.Job.Delete)
	g.PUT("/jobs/:id/status", middleware.RequirePerm("ops:job:update"), d.Job.SetStatus)
	g.POST("/jobs/:id/run", middleware.RequirePerm("ops:job:run"), d.Job.Run)
	g.GET("/jobs/logs", middleware.RequirePerm("ops:job:list"), d.Job.Logs)
}

// fileRoutes 文件上传与下载。
func (d Deps) fileRoutes(g *gin.RouterGroup) {
	g.POST("/files", middleware.RequirePerm("ops:file:upload"), d.File.Upload)
	g.GET("/files", middleware.RequirePerm("ops:file:list"), d.File.List)
	g.GET("/files/:id/download", middleware.RequirePerm("ops:file:list"), d.File.Download)
	g.DELETE("/files/:id", middleware.RequirePerm("ops:file:delete"), d.File.Delete)
}

// monitorRoutes 运行时监控。
func (d Deps) monitorRoutes(g *gin.RouterGroup) {
	g.GET("/monitor/system", middleware.RequirePerm("ops:monitor:view"), d.Monitor.System)
	g.GET("/monitor/routes", middleware.RequirePerm("ops:monitor:view"), d.Monitor.Routes)
	g.POST("/monitor/restart", middleware.RequirePerm("ops:monitor:restart"), d.Monitor.Restart)
}
