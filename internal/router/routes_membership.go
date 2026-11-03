package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// membershipRoutes 会员体系。
//
// 等级目录与「我的会员」对所有登录用户开放：个人中心需要展示自己的等级与权益，
// 以及在售等级对比；等级增删改与开通会员则需对应权限点。
func (d Deps) membershipRoutes(g *gin.RouterGroup) {
	g.GET("/membership/me", d.Membership.Me)
	g.GET("/membership/catalog", d.Membership.Catalog)

	g.GET("/membership/levels", middleware.RequirePerm("system:membership:list"), d.Membership.List)
	g.POST("/membership/levels", middleware.RequirePerm("system:membership:create"), d.Membership.Create)
	g.PUT("/membership/levels/:id", middleware.RequirePerm("system:membership:update"), d.Membership.Update)
	g.DELETE("/membership/levels/:id", middleware.RequirePerm("system:membership:delete"), d.Membership.Delete)

	g.PUT("/users/:id/membership", middleware.RequirePerm("system:user:grant-membership"), d.Membership.Grant)
	g.DELETE("/users/:id/membership", middleware.RequirePerm("system:user:grant-membership"), d.Membership.Revoke)
}
