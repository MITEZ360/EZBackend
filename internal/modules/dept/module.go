package dept

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:   "github.com/MITEZ360/tenon/internal/modules/dept",
		Name:  "dept",
		Label: "组织架构",
		Desc:  "部门树与用户归属",
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_dept", Parent: "m_system", Name: "部门管理", Type: model.MenuPage, Path: "/system/dept",
				Icon: "org", Perm: "system:dept:list", Sort: 4},
			{ID: "m_dept_add", Parent: "m_dept", Name: "新增部门", Type: model.MenuButton, Perm: "system:dept:create"},
			{ID: "m_dept_edit", Parent: "m_dept", Name: "编辑部门", Type: model.MenuButton, Perm: "system:dept:update"},
			{ID: "m_dept_del", Parent: "m_dept", Name: "删除部门", Type: model.MenuButton, Perm: "system:dept:delete"},
		},
		Setup: setup,
	})
}

func setup(c *modulekit.Context) error {
	h := NewHandler(NewService(c.DB))
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/depts/tree", middleware.RequirePerm("system:dept:list"), h.Tree)
		g.POST("/depts", middleware.RequirePerm("system:dept:create"), h.Create)
		g.PUT("/depts/:id", middleware.RequirePerm("system:dept:update"), h.Update)
		g.DELETE("/depts/:id", middleware.RequirePerm("system:dept:delete"), h.Delete)
		// 下拉选项不做权限点限制：用户表单需要它，但不应因此要求部门管理权限。
		g.GET("/options/depts", h.Tree)
	})
	return nil
}
