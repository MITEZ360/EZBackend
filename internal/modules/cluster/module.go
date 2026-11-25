package cluster

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/configcenter"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/cluster",
		Name:  "cluster",
		Label: "集群协同",
		Desc:  "节点登记、心跳、配置下发与重启指令；关闭后退化为单机",
		// 配置下发指令直接调用配置中心的热加载，缺了它指令无法执行。
		DependsOn: []string{"configcenter"},
		Models: []any{
			&model.ClusterNode{}, &model.ClusterCommand{}, &model.ClusterCommandTarget{},
		},
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_cluster", Parent: "m_ops", Name: "集群管理", Type: model.MenuPage, Path: "/ops/cluster",
				Icon: "cluster", Perm: "ops:cluster:view", Sort: 11},
			{ID: "m_cluster_operate", Parent: "m_cluster", Name: "节点运维", Type: model.MenuButton, Perm: "ops:cluster:operate"},
			// 危险操作依赖集群指令下发，节点模块关闭后单独留着没有意义。
			{ID: "m_danger", Parent: "m_ops", Name: "危险操作", Type: model.MenuPage, Path: "/ops/danger",
				Icon: "alert", Perm: "ops:danger:view", Sort: 12},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	cfgSvc, err := modulekit.Need[*configcenter.Service](c, "configcenter")
	if err != nil {
		return err
	}
	svc := NewService(c.DB, c.Config, c.Log, cfgSvc, c.Restart)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/ops/cluster/nodes", middleware.RequirePerm("ops:cluster:view"), h.Nodes)
		g.POST("/ops/cluster/dispatch", middleware.RequirePerm("ops:cluster:operate"), h.Dispatch)
		g.GET("/ops/cluster/commands", middleware.RequirePerm("ops:cluster:view"), h.Commands)
	})

	// 心跳循环随模块存在：模块被裁掉时这两个回调根本不会注册。
	c.OnStart(func(ctx context.Context) error {
		svc.Start(ctx)
		return nil
	})
	c.OnStop(svc.Close)
	return nil
}
