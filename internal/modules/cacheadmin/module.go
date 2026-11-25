package cacheadmin

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/cacheadmin",
		Name:  "cacheadmin",
		Label: "缓存管理",
		Desc:  "缓存键浏览与按前缀清理",
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_cache", Parent: "m_ops", Name: "缓存管理", Type: model.MenuPage, Path: "/ops/cache",
				Icon: "cache", Perm: "ops:cache:list", Sort: 9},
			{ID: "m_cache_delete", Parent: "m_cache", Name: "删除缓存键", Type: model.MenuButton, Perm: "ops:cache:delete"},
			{ID: "m_cache_flush", Parent: "m_cache", Name: "清空缓存", Type: model.MenuButton, Perm: "ops:cache:flush"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	h := NewHandler(NewService(c.Cache))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/cache/overview", middleware.RequirePerm("ops:cache:list"), h.Overview)
		g.GET("/cache/keys", middleware.RequirePerm("ops:cache:list"), h.Keys)
		g.DELETE("/cache/keys", middleware.RequirePerm("ops:cache:delete"), h.Delete)
		g.POST("/cache/flush", middleware.RequirePerm("ops:cache:flush"), h.Flush)
	})
	return nil
}
