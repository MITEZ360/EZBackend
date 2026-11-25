// Package system 提供字典、参数与公告三块基础能力。
//
// 这三块都声明为独立模块：字典与参数被登录页、表单等公开页面依赖，公告被
// 消息模块依赖，但它们各自有独立的管理页面，可以按需裁剪（三者均为常驻，
// 因此实际不会被裁掉，声明成独立模块是为了让依赖关系与开关规则保持完整）。
package system

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:  "github.com/MITEZ360/tenon/internal/modules/system",
		Name: "dict", Label: "数据字典", AlwaysOn: true,
		Desc: "下拉选项等字典数据，多处表单依赖，不可关闭",
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_dict", Parent: "m_system", Name: "字典管理", Type: model.MenuPage, Path: "/system/dict",
				Icon: "book", Perm: "system:dict:list", Sort: 5},
			{ID: "m_dict_add", Parent: "m_dict", Name: "新增字典", Type: model.MenuButton, Perm: "system:dict:create"},
			{ID: "m_dict_edit", Parent: "m_dict", Name: "编辑字典", Type: model.MenuButton, Perm: "system:dict:update"},
			{ID: "m_dict_del", Parent: "m_dict", Name: "删除字典", Type: model.MenuButton, Perm: "system:dict:delete"},
		},
		Setup: setupDict,
	})

	modulekit.Register(modulekit.Spec{
		Pkg:  "github.com/MITEZ360/tenon/internal/modules/system",
		Name: "config", Label: "公开配置", AlwaysOn: true,
		Desc: "前端启动时读取的站点配置与开关，不可关闭",
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_config", Parent: "m_system", Name: "参数配置", Type: model.MenuPage, Path: "/system/config",
				Icon: "sliders", Perm: "system:config:list", Sort: 6},
			{ID: "m_config_edit", Parent: "m_config", Name: "修改参数", Type: model.MenuButton, Perm: "system:config:update"},
		},
		Setup: setupConfig,
	})

	modulekit.Register(modulekit.Spec{
		Pkg:  "github.com/MITEZ360/tenon/internal/modules/system",
		Name: "notice", Label: "站内公告", AlwaysOn: true,
		Desc: "公告读取接口，消息模块依赖，不可关闭",
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_notice", Parent: "m_system", Name: "通知公告", Type: model.MenuPage, Path: "/system/notice",
				Icon: "bell", Perm: "system:notice:list", Sort: 7},
			{ID: "m_notice_add", Parent: "m_notice", Name: "发布公告", Type: model.MenuButton, Perm: "system:notice:create"},
			{ID: "m_notice_edit", Parent: "m_notice", Name: "编辑公告", Type: model.MenuButton, Perm: "system:notice:update"},
			{ID: "m_notice_del", Parent: "m_notice", Name: "删除公告", Type: model.MenuButton, Perm: "system:notice:delete"},
		},
		Setup: setupNotice,
	})
}

func setupDict(c *modulekit.Context) error {
	h := NewDictHandler(NewDictService(c.DB, c.Cache))
	c.Public(func(g *gin.RouterGroup) {
		g.GET("/public/dicts/:code", h.Data)
	})
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/dicts/types", middleware.RequirePerm("system:dict:list"), h.ListTypes)
		g.POST("/dicts/types", middleware.RequirePerm("system:dict:create"), h.CreateType)
		g.PUT("/dicts/types/:id", middleware.RequirePerm("system:dict:update"), h.UpdateType)
		g.DELETE("/dicts/types/:id", middleware.RequirePerm("system:dict:delete"), h.DeleteType)
		g.GET("/dicts/types/:code/items", middleware.RequirePerm("system:dict:list"), h.Items)
		g.POST("/dicts/items", middleware.RequirePerm("system:dict:create"), h.CreateItem)
		g.PUT("/dicts/items/:id", middleware.RequirePerm("system:dict:update"), h.UpdateItem)
		g.DELETE("/dicts/items/:id", middleware.RequirePerm("system:dict:delete"), h.DeleteItem)
	})
	return nil
}

func setupConfig(c *modulekit.Context) error {
	h := NewConfigHandler(NewConfigService(c.DB, c.Cache, c.Config))
	c.Public(func(g *gin.RouterGroup) {
		g.GET("/public/configs", h.Public)
	})
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/configs", middleware.RequirePerm("system:config:list"), h.List)
		g.POST("/configs", middleware.RequirePerm("system:config:update"), h.Create)
		g.PUT("/configs/:id", middleware.RequirePerm("system:config:update"), h.Update)
		g.DELETE("/configs/:id", middleware.RequirePerm("system:config:update"), h.Delete)
	})
	return nil
}

func setupNotice(c *modulekit.Context) error {
	svc := NewNoticeService(c.DB, nil, c.Log)
	// 站内信的投递能力来自消息模块；未启用时公告只做管理、不推送。
	c.After(func(c *modulekit.Context) error {
		if n, ok := c.Lookup("message").(NoticeNotifier); ok {
			svc.SetNotifier(n)
		}
		return nil
	})

	h := NewNoticeHandler(svc)
	c.Public(func(g *gin.RouterGroup) {
		g.GET("/public/notices", h.Latest)
	})
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/notices", middleware.RequirePerm("system:notice:list"), h.List)
		g.GET("/notices/:id", middleware.RequirePerm("system:notice:list"), h.Get)
		g.POST("/notices", middleware.RequirePerm("system:notice:create"), h.Create)
		g.PUT("/notices/:id", middleware.RequirePerm("system:notice:update"), h.Update)
		g.DELETE("/notices/:id", middleware.RequirePerm("system:notice:delete"), h.Delete)
	})
	return nil
}
