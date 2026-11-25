package model

import (
	"time"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/platform/idgen"
)

// 站内信类型。
const (
	MessageNotice       = "notice"       // 通知
	MessageAnnouncement = "announcement" // 公告
	MessageWarning      = "warning"      // 预警
	MessageSystem       = "system"       // 系统消息
)

// 投递范围。
const (
	AudienceAll  = "all"  // 全员
	AudienceRole = "role" // 指定角色
	AudienceUser = "user" // 指定用户
)

// 消息状态。
const (
	MessageStatusRevoked int8 = 0 // 已撤回
	MessageStatusSent    int8 = 1 // 已发送
)

// Message 站内信主体。一条消息对应多条收件记录。
type Message struct {
	Base
	Title      string `gorm:"size:128;not null" json:"title"`
	Content    string `gorm:"type:text" json:"content"`
	Type       string `gorm:"size:16;default:notice;index" json:"type"`
	Link       string `gorm:"size:255" json:"link,omitempty"`
	SenderID   string `gorm:"size:32;index" json:"senderId,omitempty"`
	SenderName string `gorm:"size:64" json:"senderName,omitempty"`
	Audience   string `gorm:"size:16;default:all" json:"audience"`
	Status     int8   `gorm:"index" json:"status"`
	// RefType / RefID 指向消息来源（如 notice），用于公告的增量同步与撤回。
	RefType string `gorm:"size:16;index" json:"refType,omitempty"`
	RefID   string `gorm:"size:32;index" json:"refId,omitempty"`
}

// TableName 指定表名。
func (Message) TableName() string { return "sys_message" }

// MessageRecipient 收件记录：一条消息 × 一个用户。
//
// 这里刻意不嵌 Base、不使用软删除：对用户而言"删除消息"是移除自己收到的副本，
// 物理删除既能避免唯一索引与软删除标记互相冲突，也让收件箱查询始终只扫一张表。
type MessageRecipient struct {
	ID        string     `gorm:"primaryKey;size:32" json:"id"`
	MessageID string     `gorm:"size:32;not null;uniqueIndex:uk_message_user,priority:1" json:"messageId"`
	UserID    string     `gorm:"size:32;not null;uniqueIndex:uk_message_user,priority:2;index" json:"userId"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `gorm:"index" json:"createdAt"`
}

// BeforeCreate 自动生成主键。
func (r *MessageRecipient) BeforeCreate(*gorm.DB) error {
	if r.ID == "" {
		r.ID = idgen.Next()
	}
	return nil
}

// TableName 指定表名。
func (MessageRecipient) TableName() string { return "sys_message_recipient" }
