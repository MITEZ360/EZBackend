package membership

import (
	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/MITEZ360/tenon/internal/modules/membership",
		Name:      "membership",
		Label:     "会员等级",
		Desc:      "演示业务：会员等级与用户等级关联",
		DependsOn: []string{"user"},
		Models:    []any{&model.MembershipLevel{}},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			{ID: "m_membership", Parent: "m_system", Name: "会员等级", Type: model.MenuPage, Path: "/system/membership",
				Icon: "trophy", Perm: "system:membership:list", Sort: 9},
			{ID: "m_membership_add", Parent: "m_membership", Name: "新增等级", Type: model.MenuButton, Perm: "system:membership:create"},
			{ID: "m_membership_edit", Parent: "m_membership", Name: "编辑等级", Type: model.MenuButton, Perm: "system:membership:update"},
			{ID: "m_membership_del", Parent: "m_membership", Name: "删除等级", Type: model.MenuButton, Perm: "system:membership:delete"},
		},
		Setup: setup,
		Sync:  Sync,
	})
}

// setup 会员体系。
//
// 等级目录与「我的会员」对所有登录用户开放：个人中心需要展示自己的等级与权益，
// 以及在售等级对比；等级增删改与开通会员则需对应权限点。
func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Log)
	// 用户管理用它补齐列表里的等级列；未启用本模块时该能力缺失，用户管理照常工作。
	c.Provide("membership", svc)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/membership/me", h.Me)
		g.GET("/membership/catalog", h.Catalog)

		g.GET("/membership/levels", middleware.RequirePerm("system:membership:list"), h.List)
		g.POST("/membership/levels", middleware.RequirePerm("system:membership:create"), h.Create)
		g.PUT("/membership/levels/:id", middleware.RequirePerm("system:membership:update"), h.Update)
		g.DELETE("/membership/levels/:id", middleware.RequirePerm("system:membership:delete"), h.Delete)

		g.PUT("/users/:id/membership", middleware.RequirePerm("system:user:grant-membership"), h.Grant)
		g.DELETE("/users/:id/membership", middleware.RequirePerm("system:user:grant-membership"), h.Revoke)
	})
	return nil
}
