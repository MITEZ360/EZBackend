package registry

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:    "github.com/MITEZ360/tenon/internal/modules/registry",
		Name:   "registry",
		Label:  "服务注册发现",
		Desc:   "服务目录、实例注册与心跳、健康检查、版本分布；动态路由与配置中心的数据来源",
		Models: []any{&model.GovRevision{}, &model.GovService{}, &model.GovInstance{}},
		Menus: []modulekit.Menu{
			modulekit.DirGov,
			{ID: "m_gov_services", Parent: "m_gov", Name: "服务目录", Type: model.MenuPage,
				Path: "/gov/services", Icon: "share", Perm: "registry:service:list", Sort: 1},
			{ID: "m_gov_service_create", Parent: "m_gov_services", Name: "新增服务", Type: model.MenuButton, Perm: "registry:service:create"},
			{ID: "m_gov_service_update", Parent: "m_gov_services", Name: "编辑服务", Type: model.MenuButton, Perm: "registry:service:update"},
			{ID: "m_gov_service_delete", Parent: "m_gov_services", Name: "删除服务", Type: model.MenuButton, Perm: "registry:service:delete"},
			{ID: "m_gov_instance_update", Parent: "m_gov_services", Name: "实例摘流", Type: model.MenuButton, Perm: "registry:instance:update"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	govSrv, _ := modulekit.Need[*govserver.Server](c, "govserver")
	svc := NewService(c.DB, c.Log, c.Config, govSrv)
	h := NewHandler(svc, c.Config.Security.InternalToken)

	// 供 route / configcenter 复用：路由校验要确认目标服务存在。
	c.Provide("registry", svc)

	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/gov/services", middleware.RequirePerm("registry:service:list"), h.Services)
		g.POST("/gov/services", middleware.RequirePerm("registry:service:create"), h.CreateService)
		g.PUT("/gov/services/:id", middleware.RequirePerm("registry:service:update"), h.UpdateService)
		g.DELETE("/gov/services/:id", middleware.RequirePerm("registry:service:delete"), h.DeleteService)
		g.GET("/gov/instances", middleware.RequirePerm("registry:service:list"), h.Instances)
		g.PUT("/gov/instances/:id/status", middleware.RequirePerm("registry:instance:update"), h.SetStatus)
		g.GET("/gov/versions", middleware.RequirePerm("registry:service:list"), h.Versions)
	})

	// 业务进程注册与续约：不能走用户会话，用内置令牌。
	c.Public(func(g *gin.RouterGroup) {
		g.POST("/gov/instances/register", h.internalOnly(), h.Register)
		g.POST("/gov/instances/heartbeat", h.internalOnly(), h.Heartbeat)
		g.DELETE("/gov/instances/:id", h.internalOnly(), h.Deregister)
	})

	c.OnStart(func(ctx context.Context) error {
		svc.Start(ctx)
		return nil
	})
	c.OnStop(svc.Close)
	return nil
}
