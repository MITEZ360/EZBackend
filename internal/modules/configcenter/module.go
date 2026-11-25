package configcenter

import (
	"context"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	// 管理端保存的覆盖值必须在解析 modules.enabled 之前合并进运行配置，
	// 因此注册为早期动作，由框架在开库后立刻执行。首次启动覆盖表还不存在，
	// 直接跳过。
	modulekit.RegisterBootstrap(func(ctx context.Context, db *gorm.DB) error {
		if !db.Migrator().HasTable(&model.RuntimeConfig{}) {
			return nil
		}
		return NewService(db, config.Get(), zap.L()).ApplyFromDB(ctx)
	})

	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/configcenter",
		Name:  "configcenter",
		Label: "配置中心",
		Desc:  "运行时配置的管理端入口。关闭后本页消失，只能改配置文件或直接改数据库里的覆盖表",
		// 覆盖表是模块开关自己的存储位置，必须无条件建表，因此 RuntimeConfig
		// 不在 Models 里声明，而是由框架的底座表维护。
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_ops_config", Parent: "m_ops", Name: "配置中心", Type: model.MenuPage, Path: "/ops/config",
				Icon: "sliders", Perm: "ops:config:view", Sort: 10},
			{ID: "m_ops_config_update", Parent: "m_ops_config", Name: "保存配置", Type: model.MenuButton, Perm: "ops:config:update"},
			{ID: "m_ops_config_reload", Parent: "m_ops_config", Name: "刷新配置", Type: model.MenuButton, Perm: "ops:config:reload"},
		},
		Setup: setup,
	})
}

// setup 运行时配置中心。
//
// 与 /configs（业务参数，如站点名称）刻意分开：这里的每一项都会直接
// 改写进程配置，权限也更严格。
func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Config, c.Log)
	// 集群的配置下发指令直接调用它的热加载能力。
	c.Provide("configcenter", svc)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/ops/config/items", middleware.RequirePerm("ops:config:view"), h.List)
		g.PUT("/ops/config/items", middleware.RequirePerm("ops:config:update"), h.Save)
		g.POST("/ops/config/reload", middleware.RequirePerm("ops:config:reload"), h.Reload)
	})
	return nil
}
