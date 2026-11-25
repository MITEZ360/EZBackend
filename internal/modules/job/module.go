package job

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:    "github.com/keel-framework/keel/internal/modules/job",
		Name:   "job",
		Label:  "定时任务",
		Desc:   "任务配置、手动触发与执行记录；关闭后不再启动调度器",
		Models: []any{&model.Job{}, &model.JobLog{}},
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_job", Parent: "m_ops", Name: "定时任务", Type: model.MenuPage, Path: "/ops/job",
				Icon: "clock", Perm: "ops:job:list", Sort: 1},
			{ID: "m_job_add", Parent: "m_job", Name: "新增任务", Type: model.MenuButton, Perm: "ops:job:create"},
			{ID: "m_job_edit", Parent: "m_job", Name: "编辑任务", Type: model.MenuButton, Perm: "ops:job:update"},
			{ID: "m_job_del", Parent: "m_job", Name: "删除任务", Type: model.MenuButton, Perm: "ops:job:delete"},
			{ID: "m_job_run", Parent: "m_job", Name: "手动执行", Type: model.MenuButton, Perm: "ops:job:run"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Cache, c.Log)
	// 其它模块声明的任务实现在装配末尾统一灌进来，调度器不认识它们的来源。
	c.OnJobSink(func(handler string, fn modulekit.JobFunc) {
		svc.Register(handler, HandlerFunc(fn))
	})

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/jobs", middleware.RequirePerm("ops:job:list"), h.List)
		g.GET("/jobs/handlers", middleware.RequirePerm("ops:job:list"), h.Handlers)
		g.POST("/jobs", middleware.RequirePerm("ops:job:create"), h.Create)
		g.PUT("/jobs/:id", middleware.RequirePerm("ops:job:update"), h.Update)
		g.DELETE("/jobs/:id", middleware.RequirePerm("ops:job:delete"), h.Delete)
		g.PUT("/jobs/:id/status", middleware.RequirePerm("ops:job:update"), h.SetStatus)
		g.POST("/jobs/:id/run", middleware.RequirePerm("ops:job:run"), h.Run)
		g.GET("/jobs/logs", middleware.RequirePerm("ops:job:list"), h.Logs)
	})

	// 调度器随模块启停：模块被裁掉时不会有这个回调，也就不会起 cron。
	c.OnStart(svc.Start)
	c.OnStop(svc.Stop)
	return nil
}
