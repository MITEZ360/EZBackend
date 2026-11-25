package apikey

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:    "github.com/MITEZ360/tenon/internal/modules/apikey",
		Name:   "apikey",
		Label:  "开放密钥",
		Desc:   "第三方调用所需的 API Key 管理（资讯模块依赖它）",
		Models: []any{&model.APIKey{}},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_apikey", Parent: "m_system", Name: "API 密钥", Type: model.MenuPage, Path: "/system/apikey",
				Icon: "key", Perm: "system:apikey:list", Sort: 10},
			{ID: "m_apikey_add", Parent: "m_apikey", Name: "签发密钥", Type: model.MenuButton, Perm: "system:apikey:create"},
			{ID: "m_apikey_edit", Parent: "m_apikey", Name: "编辑密钥", Type: model.MenuButton, Perm: "system:apikey:update"},
			{ID: "m_apikey_rotate", Parent: "m_apikey", Name: "轮换密钥", Type: model.MenuButton, Perm: "system:apikey:rotate"},
			{ID: "m_apikey_del", Parent: "m_apikey", Name: "删除密钥", Type: model.MenuButton, Perm: "system:apikey:delete"},
		},
		Setup: setup,
	})
}

// setup 机器身份管理。
//
// 只有后台用户可以管理凭证；凭证本身反过来只能调用开放接口，
// 不能回到后台管理面，避免一条泄露的密钥接管整个系统。
func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Cache, c.Log)
	// 开放接口的鉴权中间件由框架按此实例构造。
	c.Provide("apikey", svc)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/api-keys", middleware.RequirePerm("system:apikey:list"), h.List)
		g.GET("/api-keys/scopes", middleware.RequirePerm("system:apikey:list"), h.Scopes)
		g.POST("/api-keys", middleware.RequirePerm("system:apikey:create"), h.Create)
		g.PUT("/api-keys/:id", middleware.RequirePerm("system:apikey:update"), h.Update)
		g.POST("/api-keys/:id/rotate", middleware.RequirePerm("system:apikey:rotate"), h.Rotate)
		g.PATCH("/api-keys/:id/status", middleware.RequirePerm("system:apikey:update"), h.SetStatus)
		g.DELETE("/api-keys/:id", middleware.RequirePerm("system:apikey:delete"), h.Delete)
	})
	c.Open(func(g *gin.RouterGroup) {
		g.GET("/whoami", h.Whoami)
	})
	return nil
}
