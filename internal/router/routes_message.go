package router

import (
	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
)

// messageRoutes 站内消息中心。
//
// 收件箱相关接口不做权限点限制：这是登录用户的个人能力，任何角色都能看自己的消息。
// 只有"查看发送记录 / 发送 / 撤回"属于管理动作，走独立权限点。
// 注意静态路径必须声明在 /messages/:id 之前，避免被参数路由吞掉。
func (d Deps) messageRoutes(g *gin.RouterGroup) {
	g.GET("/messages", d.Message.Inbox)
	g.GET("/messages/unread-count", d.Message.UnreadCount)
	g.GET("/messages/recent", d.Message.Recent)
	g.PUT("/messages/read-all", d.Message.MarkAllRead)
	g.DELETE("/messages/read", d.Message.DeleteRead)
	g.POST("/messages/batch-delete", d.Message.BatchDelete)

	g.POST("/messages/send", middleware.RequirePerm("system:message:send"), d.Message.Send)
	g.GET("/messages/sent", middleware.RequirePerm("system:message:list"), d.Message.Sent)
	g.DELETE("/messages/sent/:id", middleware.RequirePerm("system:message:delete"), d.Message.Revoke)

	g.GET("/messages/:id", d.Message.Detail)
	g.PUT("/messages/:id/read", d.Message.MarkRead)
	g.DELETE("/messages/:id", d.Message.Delete)
}
