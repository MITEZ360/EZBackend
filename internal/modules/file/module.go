package file

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:    "github.com/keel-framework/keel/internal/modules/file",
		Name:   "file",
		Label:  "文件管理",
		Desc:   "上传记录浏览与清理",
		Models: []any{&model.File{}},
		Menus: []modulekit.Menu{
			modulekit.DirOps,
			{ID: "m_file", Parent: "m_ops", Name: "文件管理", Type: model.MenuPage, Path: "/ops/file",
				Icon: "folder", Perm: "ops:file:list", Sort: 2},
			{ID: "m_file_upload", Parent: "m_file", Name: "上传文件", Type: model.MenuButton, Perm: "ops:file:upload"},
			{ID: "m_file_del", Parent: "m_file", Name: "删除文件", Type: model.MenuButton, Perm: "ops:file:delete"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	h := NewHandler(NewService(c.DB, c.Config, c.Storage, c.Log))
	c.Authed(func(g *gin.RouterGroup) {
		g.POST("/files", middleware.RequirePerm("ops:file:upload"), h.Upload)
		g.GET("/files", middleware.RequirePerm("ops:file:list"), h.List)
		g.GET("/files/:id/download", middleware.RequirePerm("ops:file:list"), h.Download)
		g.DELETE("/files/:id", middleware.RequirePerm("ops:file:delete"), h.Delete)
	})
	return nil
}
