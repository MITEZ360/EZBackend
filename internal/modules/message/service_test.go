package message

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/glebarez/sqlite"

	"github.com/keel-framework/keel/internal/model"
)

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserRole{}, &model.Notice{}, &model.Message{}, &model.MessageRecipient{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return NewService(db, zap.NewNop()), db
}

func seedUsers(t *testing.T, db *gorm.DB) []model.User {
	t.Helper()
	users := []model.User{
		{Username: "admin", Nickname: "管理员", Status: model.StatusEnabled},
		{Username: "alice", Nickname: "爱丽丝", Status: model.StatusEnabled},
		{Username: "bob", Nickname: "鲍勃", Status: model.StatusEnabled},
		{Username: "off", Nickname: "已禁用", Status: model.StatusDisabled},
	}
	for i := range users {
		// Create 会把 default 值回填到结构体，先记下期望状态再插入。
		want := users[i].Status
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatalf("创建用户失败: %v", err)
		}
		// status 带 default:1，GORM 会省略零值列，这里显式落库禁用状态。
		if want == model.StatusDisabled {
			if err := db.Model(&model.User{}).Where("id = ?", users[i].ID).
				Update("status", model.StatusDisabled).Error; err != nil {
				t.Fatalf("设置禁用状态失败: %v", err)
			}
		}
	}
	if err := db.Create(&model.UserRole{UserID: users[1].ID, RoleID: "r_ops"}).Error; err != nil {
		t.Fatalf("创建角色关联失败: %v", err)
	}
	return users
}

func TestSanitizeLink(t *testing.T) {
	ok := []string{"", "/system/message", "/system/user?keyword=a"}
	for _, v := range ok {
		if _, err := sanitizeLink(v); err != nil {
			t.Fatalf("%q 应被接受: %v", v, err)
		}
	}
	bad := []string{"https://evil.com", "//evil.com", "javascript:alert(1)", "/a b", "/a\\b", "/a\nb"}
	for _, v := range bad {
		if _, err := sanitizeLink(v); err == nil {
			t.Fatalf("%q 应被拒绝", v)
		}
	}
}

func TestExcerpt(t *testing.T) {
	if got := excerpt("  第一行\n\n第二行\t末尾  ", 20); got != "第一行 第二行 末尾" {
		t.Fatalf("摘要折叠错误: %q", got)
	}
	long := "一二三四五六七八九十"
	if got := excerpt(long, 4); got != "一二三四…" {
		t.Fatalf("摘要截断错误: %q", got)
	}
}

func TestSendAudience(t *testing.T) {
	svc, db := newTestService(t)
	users := seedUsers(t, db)
	ctx := context.Background()

	// 全员只投递给启用账号
	res, err := svc.Send(ctx, &SendInput{Title: "全员通知", Content: "内容", Audience: model.AudienceAll}, "u1", "管理员")
	if err != nil {
		t.Fatalf("全员发送失败: %v", err)
	}
	if res.Recipients != 3 {
		t.Fatalf("全员应投递 3 人，实际 %d", res.Recipients)
	}

	// 指定角色
	res, err = svc.Send(ctx, &SendInput{
		Title: "角色通知", Content: "内容", Audience: model.AudienceRole, RoleIDs: []string{"r_ops"},
	}, "u1", "管理员")
	if err != nil {
		t.Fatalf("角色发送失败: %v", err)
	}
	if res.Recipients != 1 {
		t.Fatalf("角色应投递 1 人，实际 %d", res.Recipients)
	}

	// 指定用户里混入禁用账号，只投递启用的
	res, err = svc.Send(ctx, &SendInput{
		Title: "定向通知", Content: "内容", Audience: model.AudienceUser,
		UserIDs: []string{users[1].ID, users[1].ID, users[3].ID},
	}, "u1", "管理员")
	if err != nil {
		t.Fatalf("定向发送失败: %v", err)
	}
	if res.Recipients != 1 {
		t.Fatalf("定向应投递 1 人（去重且排除禁用），实际 %d", res.Recipients)
	}

	// 全部接收人都不可用时应明确报错，而不是发出一条没人收到的消息
	_, err = svc.Send(ctx, &SendInput{
		Title: "空投", Content: "内容", Audience: model.AudienceUser, UserIDs: []string{users[3].ID},
	}, "u1", "管理员")
	if err == nil {
		t.Fatal("没有可投递接收人时应返回错误")
	}

	if _, err := svc.Send(ctx, &SendInput{
		Title: "外链", Content: "内容", Audience: model.AudienceAll, Link: "//evil.com",
	}, "u1", "管理员"); err == nil {
		t.Fatal("站外跳转应被拒绝")
	}
}

func TestInboxLifecycle(t *testing.T) {
	svc, db := newTestService(t)
	users := seedUsers(t, db)
	ctx := context.Background()
	alice := users[1]

	res, err := svc.Send(ctx, &SendInput{
		Title: "系统维护", Content: "今晚 22:00 开始维护，预计 30 分钟。",
		Type: model.MessageNotice, Audience: model.AudienceUser, UserIDs: []string{alice.ID},
		Link: "/system/message",
	}, "u1", "管理员")
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	list, total, err := svc.Inbox(ctx, alice.ID, &InboxQuery{Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("收件箱应有 1 条，实际 total=%d len=%d err=%v", total, len(list), err)
	}
	if list[0].Excerpt == "" || list[0].Excerpt != "今晚 22:00 开始维护，预计 30 分钟。" {
		t.Fatalf("列表摘要不正确: %+v", list[0])
	}

	if n, _ := svc.UnreadCount(ctx, alice.ID); n != 1 {
		t.Fatalf("未读应为 1，实际 %d", n)
	}

	detail, err := svc.Get(ctx, alice.ID, res.ID)
	if err != nil {
		t.Fatalf("读取详情失败: %v", err)
	}
	if detail.Content == "" || detail.MessageID != res.ID {
		t.Fatalf("详情内容缺失: %+v", detail)
	}

	// 其他用户不可见
	if _, err := svc.Get(ctx, users[2].ID, res.ID); err == nil {
		t.Fatal("非收件人不应读取到消息")
	}
	if err := svc.MarkRead(ctx, users[2].ID, res.ID); err == nil {
		t.Fatal("非收件人标记已读应失败")
	}

	if err := svc.MarkRead(ctx, alice.ID, res.ID); err != nil {
		t.Fatalf("标记已读失败: %v", err)
	}
	if n, _ := svc.UnreadCount(ctx, alice.ID); n != 0 {
		t.Fatalf("已读后未读数应为 0，实际 %d", n)
	}
	// 幂等
	if err := svc.MarkRead(ctx, alice.ID, res.ID); err != nil {
		t.Fatalf("重复标记已读应幂等: %v", err)
	}

	read, total, _ := svc.Inbox(ctx, alice.ID, &InboxQuery{Status: "read"})
	if total != 1 || len(read) != 1 {
		t.Fatalf("已读筛选应命中 1 条，实际 %d", total)
	}
	unread, total, _ := svc.Inbox(ctx, alice.ID, &InboxQuery{Status: "unread"})
	if total != 0 || len(unread) != 0 {
		t.Fatalf("未读筛选应为空，实际 %d", total)
	}

	if n, err := svc.DeleteRead(ctx, alice.ID); err != nil || n != 1 {
		t.Fatalf("清空已读应删除 1 条，实际 %d err=%v", n, err)
	}
	if _, total, _ = svc.Inbox(ctx, alice.ID, &InboxQuery{}); total != 0 {
		t.Fatalf("清空后收件箱应为空，实际 %d", total)
	}

	// 撤回后新发出的消息不应再出现在任何人的收件箱
	res2, _ := svc.Send(ctx, &SendInput{
		Title: "待撤回", Content: "内容", Audience: model.AudienceUser,
		UserIDs: []string{alice.ID, users[2].ID},
	}, "u1", "管理员")
	if err := svc.Revoke(ctx, res2.ID); err != nil {
		t.Fatalf("撤回失败: %v", err)
	}
	if _, total, _ = svc.Inbox(ctx, users[2].ID, &InboxQuery{}); total != 0 {
		t.Fatalf("撤回后收件箱应为空，实际 %d", total)
	}
	sent, total, _ := svc.Sent(ctx, &SentQuery{Page: 1, PageSize: 20})
	if total != 2 || len(sent) != 2 {
		t.Fatalf("发送记录应保留 2 条，实际 %d", total)
	}
	if sent[0].Status != model.MessageStatusRevoked {
		t.Fatalf("最新一条应为已撤回，实际 %d", sent[0].Status)
	}
}

func TestNoticeSync(t *testing.T) {
	svc, db := newTestService(t)
	seedUsers(t, db)
	ctx := context.Background()

	n := &model.Notice{Title: "版本发布", Content: "v1.0.0 已上线", Type: "notice", Status: model.StatusEnabled}
	if err := db.Create(n).Error; err != nil {
		t.Fatalf("创建公告失败: %v", err)
	}
	if err := svc.PublishNotice(ctx, n, "u1", "管理员"); err != nil {
		t.Fatalf("公告投递失败: %v", err)
	}

	var msgs []model.Message
	if err := db.Where("ref_id = ?", n.ID).Find(&msgs).Error; err != nil {
		t.Fatalf("查询消息失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("公告应只生成 1 条消息，实际 %d", len(msgs))
	}

	// 重复发布（如编辑正文）只更新，不产生新消息
	n.Title = "版本发布（修订）"
	n.Content = "v1.0.1 已上线"
	if err := svc.PublishNotice(ctx, n, "u1", "管理员"); err != nil {
		t.Fatalf("重复投递失败: %v", err)
	}
	if err := db.Where("ref_id = ?", n.ID).Find(&msgs).Error; err != nil || len(msgs) != 1 {
		t.Fatalf("重复发布不应新增消息，实际 %d err=%v", len(msgs), err)
	}
	if msgs[0].Title != "版本发布（修订）" || msgs[0].Content != "v1.0.1 已上线" {
		t.Fatalf("公告内容未同步: %+v", msgs[0])
	}

	if err := svc.RevokeNotice(ctx, n.ID); err != nil {
		t.Fatalf("撤回公告失败: %v", err)
	}
	var alice model.User
	db.Where("username = ?", "alice").First(&alice)
	if _, total, _ := svc.Inbox(ctx, alice.ID, &InboxQuery{}); total != 0 {
		t.Fatalf("公告撤回后收件箱应为空，实际 %d", total)
	}

	// 重新发布可原地恢复并补投
	if err := svc.PublishNotice(ctx, n, "u1", "管理员"); err != nil {
		t.Fatalf("重新发布失败: %v", err)
	}
	if _, total, _ := svc.Inbox(ctx, alice.ID, &InboxQuery{}); total != 1 {
		t.Fatalf("重新发布应补投 1 条，实际 %d", total)
	}
	if err := svc.RevokeNotice(ctx, "not-exist"); err != nil {
		t.Fatalf("撤回不存在的公告应幂等: %v", err)
	}
}

func TestCleanupMessages(t *testing.T) {
	svc, db := newTestService(t)
	users := seedUsers(t, db)
	ctx := context.Background()

	res, _ := svc.Send(ctx, &SendInput{
		Title: "旧消息", Content: "内容", Audience: model.AudienceUser, UserIDs: []string{users[1].ID},
	}, "u1", "管理员")
	if err := svc.MarkRead(ctx, users[1].ID, res.ID); err != nil {
		t.Fatalf("标记已读失败: %v", err)
	}
	old := time.Now().AddDate(0, 0, -400)
	if err := db.Model(&model.MessageRecipient{}).Where("message_id = ?", res.ID).
		Update("read_at", old).Error; err != nil {
		t.Fatalf("调整已读时间失败: %v", err)
	}
	n, err := svc.CleanupMessages(ctx, 180)
	if err != nil || n != 1 {
		t.Fatalf("应清理 1 条历史收件记录，实际 %d err=%v", n, err)
	}
	if _, total, _ := svc.Inbox(ctx, users[1].ID, &InboxQuery{}); total != 0 {
		t.Fatalf("清理后应为空，实际 %d", total)
	}
}
