// Package message 提供站内消息中心：发送、收件箱、已读回执，以及公告发布联动。
//
// 数据分成两层：
//   - sys_message          消息主体（标题、正文、投递范围），管理端可见；
//   - sys_message_recipient 收件记录（用户 × 消息），承载已读与删除。
//
// 广播类消息按用户展开写入收件记录。中小企业后台的用户规模（百到千级）下，
// 展开写换来的是收件箱查询只走单表、未读数可直接命中索引，比运行时 join
// 角色关系再判可见性更可控。
package message

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/pkg/errs"
)

// Service 消息中心业务。
type Service struct {
	db  *gorm.DB
	log *zap.Logger
}

// NewService 创建消息服务。
func NewService(db *gorm.DB, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, log: log}
}

// SendInput 发送入参。
type SendInput struct {
	Title    string   `json:"title" binding:"required,max=128" label:"标题"`
	Content  string   `json:"content" binding:"required,max=5000" label:"内容"`
	Type     string   `json:"type" binding:"omitempty,oneof=notice announcement warning system" label:"类型"`
	Link     string   `json:"link" binding:"omitempty,max=255" label:"跳转地址"`
	Audience string   `json:"audience" binding:"required,oneof=all role user" label:"接收范围"`
	RoleIDs  []string `json:"roleIds"`
	UserIDs  []string `json:"userIds"`
}

// SendResult 发送结果。
type SendResult struct {
	ID         string `json:"id"`
	Recipients int    `json:"recipients"`
}

// Send 发送站内信。
func (s *Service) Send(ctx context.Context, in *SendInput, senderID, senderName string) (*SendResult, error) {
	link, err := sanitizeLink(in.Link)
	if err != nil {
		return nil, err
	}
	if in.Type == "" {
		in.Type = model.MessageNotice
	}
	msg := &model.Message{
		Title: strings.TrimSpace(in.Title), Content: in.Content, Type: in.Type, Link: link,
		SenderID: senderID, SenderName: senderName, Audience: in.Audience,
		Status: model.MessageStatusSent,
	}
	if msg.Title == "" {
		return nil, errs.BadRequest("标题不能为空")
	}

	var count int
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids, err := resolveUsers(tx, in.Audience, in.RoleIDs, in.UserIDs)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return errs.BadRequest("没有可投递的接收人，请确认所选用户或角色已启用")
		}
		if err := tx.Create(msg).Error; err != nil {
			return err
		}
		if err := insertRecipients(tx, msg.ID, ids); err != nil {
			return err
		}
		count = len(ids)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &SendResult{ID: msg.ID, Recipients: count}, nil
}

/* ------------------------------------------------------------ 收件箱读取 */

// InboxQuery 收件箱查询条件。
type InboxQuery struct {
	Page     int
	PageSize int
	Status   string // unread / read / 空
	Type     string
	Keyword  string
}

// InboxItem 收件箱条目。列表只返回摘要，正文由详情接口提供。
type InboxItem struct {
	ID         string     `json:"id"`
	MessageID  string     `json:"messageId"`
	Title      string     `json:"title"`
	Content    string     `json:"-"`
	Excerpt    string     `json:"excerpt"`
	Type       string     `json:"type"`
	Link       string     `json:"link,omitempty"`
	SenderName string     `json:"senderName,omitempty"`
	ReadAt     *time.Time `json:"readAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Detail 消息详情。
type Detail struct {
	ID         string     `json:"id"`
	MessageID  string     `json:"messageId"`
	Title      string     `json:"title"`
	Content    string     `json:"content"`
	Type       string     `json:"type"`
	Link       string     `json:"link,omitempty"`
	SenderName string     `json:"senderName,omitempty"`
	ReadAt     *time.Time `json:"readAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

const inboxColumns = `r.id AS id, r.message_id AS message_id, m.title AS title, m.content AS content,
	m.type AS type, m.link AS link, m.sender_name AS sender_name, r.read_at AS read_at, m.created_at AS created_at`

func (s *Service) inboxBase(ctx context.Context, userID string, q *InboxQuery) *gorm.DB {
	db := s.db.WithContext(ctx).Table("sys_message_recipient AS r").
		Joins("JOIN sys_message m ON m.id = r.message_id AND m.deleted_at IS NULL AND m.status = ?", model.MessageStatusSent).
		Where("r.user_id = ?", userID)

	switch q.Status {
	case "unread":
		db = db.Where("r.read_at IS NULL")
	case "read":
		db = db.Where("r.read_at IS NOT NULL")
	}
	if q.Type != "" {
		db = db.Where("m.type = ?", q.Type)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		db = db.Where(`(m.title LIKE ? ESCAPE '!' OR m.content LIKE ? ESCAPE '!')`, like, like)
	}
	return db
}

// Inbox 分页查询当前用户的消息。
func (s *Service) Inbox(ctx context.Context, userID string, q *InboxQuery) ([]*InboxItem, int64, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 100 {
		q.PageSize = 20
	}
	var total int64
	if err := s.inboxBase(ctx, userID, q).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*InboxItem
	err := s.inboxBase(ctx, userID, q).
		Select(inboxColumns).
		Order("m.created_at DESC, r.id DESC").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).
		Scan(&list).Error
	if err != nil {
		return nil, 0, err
	}
	for _, it := range list {
		it.Excerpt = excerpt(it.Content, 90)
	}
	return list, total, nil
}

// Recent 最近消息，用于顶部铃铛预览。
func (s *Service) Recent(ctx context.Context, userID string, limit int) ([]*InboxItem, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var list []*InboxItem
	err := s.inboxBase(ctx, userID, &InboxQuery{}).
		Select(inboxColumns).
		Order("m.created_at DESC, r.id DESC").Limit(limit).
		Scan(&list).Error
	if err != nil {
		return nil, err
	}
	for _, it := range list {
		it.Excerpt = excerpt(it.Content, 60)
	}
	return list, nil
}

// UnreadCount 未读数量。
func (s *Service) UnreadCount(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := s.inboxBase(ctx, userID, &InboxQuery{Status: "unread"}).Count(&n).Error
	return n, err
}

// Get 读取消息详情；不改变已读状态，已读由前端显式标记。
func (s *Service) Get(ctx context.Context, userID, messageID string) (*Detail, error) {
	var d Detail
	err := s.inboxBase(ctx, userID, &InboxQuery{}).
		Select(inboxColumns).Where("r.message_id = ?", messageID).
		Limit(1).Scan(&d).Error
	if err != nil {
		return nil, err
	}
	if d.MessageID == "" {
		return nil, errs.NotFound("消息不存在或已删除")
	}
	return &d, nil
}

/* ------------------------------------------------------------ 收件箱写入 */

// MarkRead 标记单条已读；已读时幂等返回。
func (s *Service) MarkRead(ctx context.Context, userID, messageID string) error {
	res := s.db.WithContext(ctx).Model(&model.MessageRecipient{}).
		Where("user_id = ? AND message_id = ?", userID, messageID).
		Update("read_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.MessageRecipient{}).
			Where("user_id = ? AND message_id = ?", userID, messageID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return errs.NotFound("消息不存在或已删除")
		}
	}
	return nil
}

// MarkAllRead 全部标记已读，可按类型过滤。
func (s *Service) MarkAllRead(ctx context.Context, userID, typ string) (int64, error) {
	db := s.db.WithContext(ctx).Model(&model.MessageRecipient{}).Where("user_id = ? AND read_at IS NULL", userID)
	if typ != "" {
		db = db.Where("message_id IN (?)",
			s.db.Model(&model.Message{}).Select("id").Where("type = ? AND deleted_at IS NULL", typ))
	}
	res := db.Update("read_at", time.Now())
	return res.RowsAffected, res.Error
}

// Delete 删除自己收到的一条消息。
func (s *Service) Delete(ctx context.Context, userID, messageID string) error {
	res := s.db.WithContext(ctx).
		Where("user_id = ? AND message_id = ?", userID, messageID).
		Delete(&model.MessageRecipient{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("消息不存在或已删除")
	}
	return nil
}

// BatchDelete 批量删除，返回实际删除条数。
func (s *Service) BatchDelete(ctx context.Context, userID string, ids []string) (int64, error) {
	ids = uniqStrings(ids)
	if len(ids) == 0 {
		return 0, errs.BadRequest("请选择要删除的消息")
	}
	if len(ids) > 200 {
		return 0, errs.BadRequest("单次最多删除 200 条消息")
	}
	res := s.db.WithContext(ctx).
		Where("user_id = ? AND message_id IN ?", userID, ids).
		Delete(&model.MessageRecipient{})
	return res.RowsAffected, res.Error
}

// DeleteRead 清空已读消息。
func (s *Service) DeleteRead(ctx context.Context, userID string) (int64, error) {
	res := s.db.WithContext(ctx).
		Where("user_id = ? AND read_at IS NOT NULL", userID).
		Delete(&model.MessageRecipient{})
	return res.RowsAffected, res.Error
}

/* -------------------------------------------------------------- 管理侧 */

// SentQuery 发送记录查询。
type SentQuery struct {
	Page     int
	PageSize int
	Keyword  string
	Type     string
	Status   string // sent / revoked / 空
}

// SentItem 发送记录。
type SentItem struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	Title      string    `json:"title"`
	Type       string    `json:"type"`
	Audience   string    `json:"audience"`
	Link       string    `json:"link,omitempty"`
	SenderName string    `json:"senderName,omitempty"`
	Status     int8      `json:"status"`
	Total      int64     `gorm:"column:total_count" json:"total"`
	Read       int64     `gorm:"column:read_count" json:"read"`
	RefType    string    `json:"refType,omitempty"`
}

// Sent 分页查询发送记录，附带送达与已读统计。
func (s *Service) Sent(ctx context.Context, q *SentQuery) ([]*SentItem, int64, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 || q.PageSize > 100 {
		q.PageSize = 20
	}
	base := s.db.WithContext(ctx).Model(&model.Message{})
	switch q.Status {
	case "sent":
		base = base.Where("status = ?", model.MessageStatusSent)
	case "revoked":
		base = base.Where("status = ?", model.MessageStatusRevoked)
	}
	if q.Type != "" {
		base = base.Where("type = ?", q.Type)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		base = base.Where(`title LIKE ? ESCAPE '!'`, like)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []*SentItem
	err := base.Select(`id, created_at, title, type, audience, link, sender_name, status, ref_type,
		(SELECT COUNT(*) FROM sys_message_recipient r WHERE r.message_id = sys_message.id) AS total_count,
		(SELECT COUNT(*) FROM sys_message_recipient r WHERE r.message_id = sys_message.id AND r.read_at IS NOT NULL) AS read_count`).
		Order("created_at DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).
		Scan(&list).Error
	return list, total, err
}

// Revoke 撤回消息：清空收件记录并软删消息主体。
func (s *Service) Revoke(ctx context.Context, messageID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Message
		if err := tx.First(&m, "id = ?", messageID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errs.NotFound("消息不存在")
			}
			return err
		}
		// 只改状态、保留收件记录：收件箱查询按 m.status=1 过滤，用户侧立即不可见，
		// 但发送记录仍能保留真实的送达/已读数据，公告重新发布时也无需补投。
		return tx.Model(&model.Message{}).Where("id = ?", messageID).
			Update("status", model.MessageStatusRevoked).Error
	})
}

// CleanupMessages 清理已读且超过保留期的收件记录，并物理删除已撤回的历史消息主体。
// 未读消息不参与清理，避免用户还没看到通知就从收件箱消失。
func (s *Service) CleanupMessages(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 180
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var removed int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("read_at IS NOT NULL AND read_at < ?", cutoff).Delete(&model.MessageRecipient{})
		if res.Error != nil {
			return res.Error
		}
		removed = res.RowsAffected
		return tx.Unscoped().
			Where("status = ? AND created_at < ?", model.MessageStatusRevoked, cutoff).
			Delete(&model.Message{}).Error
	})
	return removed, err
}

/* ------------------------------------------------------ 公告联动（供 system 调用） */

// PublishNotice 把公告同步为一条全员站内信。
// 重复发布同一公告时更新正文并保留已读状态，不会给用户造成重复打扰。
func (s *Service) PublishNotice(ctx context.Context, n *model.Notice, senderID, senderName string) error {
	if n == nil {
		return nil
	}
	if senderName == "" {
		senderName = "系统"
	}
	typ := n.Type
	if typ == "" {
		typ = model.MessageNotice
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var exist model.Message
		err := tx.Where("ref_type = ? AND ref_id = ?", "notice", n.ID).
			Order("created_at DESC").First(&exist).Error
		switch {
		case err == nil:
			if err := tx.Model(&model.Message{}).Where("id = ?", exist.ID).Updates(map[string]any{
				"title": n.Title, "content": n.Content, "type": typ,
				"status": model.MessageStatusSent,
			}).Error; err != nil {
				return err
			}
			// 恢复被撤回的消息时要补投，否则用户收件箱里不会有这条。
			var cnt int64
			if err := tx.Model(&model.MessageRecipient{}).
				Where("message_id = ?", exist.ID).Count(&cnt).Error; err != nil {
				return err
			}
			if cnt > 0 {
				return nil
			}
			ids, err := resolveUsers(tx, model.AudienceAll, nil, nil)
			if err != nil {
				return err
			}
			return insertRecipients(tx, exist.ID, ids)

		case errors.Is(err, gorm.ErrRecordNotFound):
			msg := &model.Message{
				Title: n.Title, Content: n.Content, Type: typ, Audience: model.AudienceAll,
				SenderID: senderID, SenderName: senderName, Status: model.MessageStatusSent,
				RefType: "notice", RefID: n.ID,
			}
			if err := tx.Create(msg).Error; err != nil {
				return err
			}
			ids, err := resolveUsers(tx, model.AudienceAll, nil, nil)
			if err != nil {
				return err
			}
			return insertRecipients(tx, msg.ID, ids)

		default:
			return err
		}
	})
}

// RevokeNotice 公告转草稿或被删除时，同步撤回站内信。幂等。
func (s *Service) RevokeNotice(ctx context.Context, noticeID string) error {
	var m model.Message
	err := s.db.WithContext(ctx).Where("ref_type = ? AND ref_id = ?", "notice", noticeID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Revoke(ctx, m.ID)
}

/* ---------------------------------------------------------------- 内部 */

// resolveUsers 把投递范围解析为去重后的用户 ID 列表，只包含启用中的账号。
func resolveUsers(tx *gorm.DB, audience string, roleIDs, userIDs []string) ([]string, error) {
	var ids []string
	var err error
	switch audience {
	case model.AudienceAll:
		err = tx.Model(&model.User{}).Where("status = ?", model.StatusEnabled).Pluck("id", &ids).Error
	case model.AudienceRole:
		roleIDs = uniqStrings(roleIDs)
		if len(roleIDs) == 0 {
			return nil, errs.BadRequest("请选择接收角色")
		}
		err = tx.Table("sys_user_role AS ur").
			Joins("JOIN sys_user u ON u.id = ur.user_id AND u.deleted_at IS NULL").
			Where("ur.role_id IN ? AND u.status = ?", roleIDs, model.StatusEnabled).
			Distinct().Pluck("ur.user_id", &ids).Error
	case model.AudienceUser:
		userIDs = uniqStrings(userIDs)
		if len(userIDs) == 0 {
			return nil, errs.BadRequest("请选择接收用户")
		}
		err = tx.Model(&model.User{}).
			Where("id IN ? AND status = ?", userIDs, model.StatusEnabled).Pluck("id", &ids).Error
	default:
		return nil, errs.BadRequest("接收范围不合法")
	}
	if err != nil {
		return nil, err
	}
	return uniqStrings(ids), nil
}

// insertRecipients 分批写入收件记录。SQLite 对单条语句的绑定参数有上限，
// 每批 200 行（4 列）可以安全落在限制内；重复投递由唯一索引兜底。
func insertRecipients(tx *gorm.DB, messageID string, userIDs []string) error {
	const batch = 200
	now := time.Now()
	for start := 0; start < len(userIDs); start += batch {
		end := start + batch
		if end > len(userIDs) {
			end = len(userIDs)
		}
		rows := make([]model.MessageRecipient, 0, end-start)
		for _, uid := range userIDs[start:end] {
			rows = append(rows, model.MessageRecipient{
				MessageID: messageID, UserID: uid, CreatedAt: now,
			})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; err != nil {
			return err
		}
	}
	return nil
}

// sanitizeLink 只允许站内相对路径，避免把消息变成开放跳板。
func sanitizeLink(link string) (string, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", nil
	}
	if !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") {
		return "", errs.BadRequest("跳转地址必须是站内路径，以单个 / 开头")
	}
	if strings.ContainsAny(link, "\\\r\n\t") || strings.IndexFunc(link, unicode.IsSpace) >= 0 {
		return "", errs.BadRequest("跳转地址不能包含空白或控制字符")
	}
	return link, nil
}

// excerpt 生成列表摘要：折叠换行与连续空白，按字符数截断。
func excerpt(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

func uniqStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// escapeLike 转义 LIKE 通配符，避免用户输入 % 时退化为全表扫描。
// 使用 ! 作为转义符，SQLite 与 MySQL 的字符串字面量规则都能正确解析。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "!", "!!")
	s = strings.ReplaceAll(s, "%", "!%")
	return strings.ReplaceAll(s, "_", "!_")
}
