package message

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/keel-framework/keel/internal/middleware"
	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

func init() {
	modulekit.Register(modulekit.Spec{
		Pkg:       "github.com/keel-framework/keel/internal/modules/message",
		Name:      "message",
		Label:     "站内消息",
		Desc:      "演示业务：消息发送与收件箱",
		DependsOn: []string{"notice"},
		Models:    []any{&model.Message{}, &model.MessageRecipient{}},
		Menus: []modulekit.Menu{
			modulekit.DirSystem,
			// 收件箱是全员能力：普通角色也要有入口，管理动作另行按权限点控制。
			{ID: "m_message", Parent: "m_system", Name: "消息中心", Type: model.MenuPage, Path: "/system/message",
				Icon: "message", Sort: 8, Roles: []string{"r_user"}},
			{ID: "m_message_send", Parent: "m_message", Name: "发送消息", Type: model.MenuButton, Perm: "system:message:send"},
			{ID: "m_message_sent", Parent: "m_message", Name: "查看发送记录", Type: model.MenuButton, Perm: "system:message:list"},
			{ID: "m_message_del", Parent: "m_message", Name: "撤回消息", Type: model.MenuButton, Perm: "system:message:delete"},
		},
		Jobs: []modulekit.Job{
			{Name: "清理历史消息", Cron: "0 4 * * *", Handler: "message.cleanup", Params: `{"days":180}`,
				Remark: "每天 04:00 清理 180 天前已读的收件记录"},
		},
		Setup: setup,
	})
}

// setup 站内消息中心。
//
// 收件箱相关接口不做权限点限制：这是登录用户的个人能力，任何角色都能看自己的消息。
// 只有"查看发送记录 / 发送 / 撤回"属于管理动作，走独立权限点。
// 注意静态路径必须声明在 /messages/:id 之前，避免被参数路由吞掉。
func setup(c *modulekit.Context) error {
	svc := NewService(c.DB, c.Log)
	// 公告发布后通过它投递站内信；未启用本模块时公告只做管理，不推送。
	c.Provide("message", svc)

	h := NewHandler(svc)
	c.Authed(func(g *gin.RouterGroup) {
		g.GET("/messages", h.Inbox)
		g.GET("/messages/unread-count", h.UnreadCount)
		g.GET("/messages/recent", h.Recent)
		g.PUT("/messages/read-all", h.MarkAllRead)
		g.DELETE("/messages/read", h.DeleteRead)
		g.POST("/messages/batch-delete", h.BatchDelete)

		g.POST("/messages/send", middleware.RequirePerm("system:message:send"), h.Send)
		g.GET("/messages/sent", middleware.RequirePerm("system:message:list"), h.Sent)
		g.DELETE("/messages/sent/:id", middleware.RequirePerm("system:message:delete"), h.Revoke)

		g.GET("/messages/:id", h.Detail)
		g.PUT("/messages/:id/read", h.MarkRead)
		g.DELETE("/messages/:id", h.Delete)
	})

	c.Job("message.cleanup", func(ctx context.Context, p map[string]any) (string, error) {
		days := 180
		if v, ok := p["days"].(float64); ok {
			days = int(v)
		}
		n, err := svc.CleanupMessages(ctx, days)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("清理历史收件记录 %d 条", n), nil
	})
	return nil
}
