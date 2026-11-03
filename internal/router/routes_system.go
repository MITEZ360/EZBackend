package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// dashboardRoutes 工作台。
func (d Deps) dashboardRoutes(g *gin.RouterGroup) {
	g.GET("/dashboard/overview", middleware.RequirePerm("dashboard:view"), d.Dashboard.Overview)
}

// userRoutes 用户管理。
func (d Deps) userRoutes(g *gin.RouterGroup) {
	g.GET("/users", middleware.RequirePerm("system:user:list"), d.User.List)
	g.POST("/users", middleware.RequirePerm("system:user:create"), d.User.Create)
	g.GET("/users/:id", middleware.RequirePerm("system:user:list"), d.User.Get)
	g.PUT("/users/:id", middleware.RequirePerm("system:user:update"), d.User.Update)
	g.DELETE("/users/:id", middleware.RequirePerm("system:user:delete"), d.User.Delete)
	g.PUT("/users/:id/status", middleware.RequirePerm("system:user:update"), d.User.SetStatus)
	g.PUT("/users/:id/password", middleware.RequirePerm("system:user:reset"), d.User.ResetPassword)
	g.PUT("/users/:id/roles", middleware.RequirePerm("system:user:update"), d.User.SetRoles)
	g.GET("/options/users", middleware.RequirePerm("system:user:list"), d.User.Options)
	g.GET("/exports/users", middleware.RequirePerm("system:user:export"), d.User.Export)
	g.POST("/users/batch-delete", middleware.RequirePerm("system:user:delete"), d.User.BatchDelete)
	g.GET("/imports/users/template", middleware.RequirePerm("system:user:create"), d.User.ImportTemplate)
	g.POST("/imports/users", middleware.RequirePerm("system:user:create"), d.User.Import)
}

// roleRoutes 角色与权限分配。
func (d Deps) roleRoutes(g *gin.RouterGroup) {
	g.GET("/roles", middleware.RequirePerm("system:role:list"), d.Role.List)
	g.POST("/roles", middleware.RequirePerm("system:role:create"), d.Role.Create)
	g.GET("/roles/:id", middleware.RequirePerm("system:role:list"), d.Role.Get)
	g.PUT("/roles/:id", middleware.RequirePerm("system:role:update"), d.Role.Update)
	g.DELETE("/roles/:id", middleware.RequirePerm("system:role:delete"), d.Role.Delete)
	g.GET("/roles/:id/menus", middleware.RequirePerm("system:role:list"), d.Role.Menus)
	g.PUT("/roles/:id/menus", middleware.RequirePerm("system:role:assign"), d.Role.SetMenus)
	g.GET("/options/roles", d.Role.All)
}

// menuRoutes 菜单与按钮权限。
func (d Deps) menuRoutes(g *gin.RouterGroup) {
	g.GET("/menus", middleware.RequirePerm("system:menu:list"), d.Menu.List)
	g.GET("/menus/tree", middleware.RequirePerm("system:menu:list"), d.Menu.Tree)
	g.POST("/menus", middleware.RequirePerm("system:menu:create"), d.Menu.Create)
	g.PUT("/menus/:id", middleware.RequirePerm("system:menu:update"), d.Menu.Update)
	g.DELETE("/menus/:id", middleware.RequirePerm("system:menu:delete"), d.Menu.Delete)
}

// deptRoutes 组织架构。
func (d Deps) deptRoutes(g *gin.RouterGroup) {
	g.GET("/depts/tree", middleware.RequirePerm("system:dept:list"), d.Dept.Tree)
	g.POST("/depts", middleware.RequirePerm("system:dept:create"), d.Dept.Create)
	g.PUT("/depts/:id", middleware.RequirePerm("system:dept:update"), d.Dept.Update)
	g.DELETE("/depts/:id", middleware.RequirePerm("system:dept:delete"), d.Dept.Delete)
	g.GET("/options/depts", d.Dept.Tree)
}

// dictRoutes 数据字典。
func (d Deps) dictRoutes(g *gin.RouterGroup) {
	g.GET("/dicts/types", middleware.RequirePerm("system:dict:list"), d.Dict.ListTypes)
	g.POST("/dicts/types", middleware.RequirePerm("system:dict:create"), d.Dict.CreateType)
	g.PUT("/dicts/types/:id", middleware.RequirePerm("system:dict:update"), d.Dict.UpdateType)
	g.DELETE("/dicts/types/:id", middleware.RequirePerm("system:dict:delete"), d.Dict.DeleteType)
	g.GET("/dicts/types/:code/items", middleware.RequirePerm("system:dict:list"), d.Dict.Items)
	g.POST("/dicts/items", middleware.RequirePerm("system:dict:create"), d.Dict.CreateItem)
	g.PUT("/dicts/items/:id", middleware.RequirePerm("system:dict:update"), d.Dict.UpdateItem)
	g.DELETE("/dicts/items/:id", middleware.RequirePerm("system:dict:delete"), d.Dict.DeleteItem)
}

// configRoutes 参数配置。
func (d Deps) configRoutes(g *gin.RouterGroup) {
	g.GET("/configs", middleware.RequirePerm("system:config:list"), d.Config.List)
	g.POST("/configs", middleware.RequirePerm("system:config:update"), d.Config.Create)
	g.PUT("/configs/:id", middleware.RequirePerm("system:config:update"), d.Config.Update)
	g.DELETE("/configs/:id", middleware.RequirePerm("system:config:update"), d.Config.Delete)
}

// noticeRoutes 通知公告。
func (d Deps) noticeRoutes(g *gin.RouterGroup) {
	g.GET("/notices", middleware.RequirePerm("system:notice:list"), d.Notice.List)
	g.GET("/notices/:id", middleware.RequirePerm("system:notice:list"), d.Notice.Get)
	g.POST("/notices", middleware.RequirePerm("system:notice:create"), d.Notice.Create)
	g.PUT("/notices/:id", middleware.RequirePerm("system:notice:update"), d.Notice.Update)
	g.DELETE("/notices/:id", middleware.RequirePerm("system:notice:delete"), d.Notice.Delete)
}
