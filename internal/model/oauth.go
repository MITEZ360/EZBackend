package model

import "time"

// UserOAuthBinding 第三方身份与本地账号的绑定关系。
//
// (provider, subject) 全局唯一：同一个外部账号不允许绑到多个本地用户；
// 解绑采用物理删除，避免软删除行继续占用唯一键导致无法重新绑定。
type UserOAuthBinding struct {
	Base
	UserID      string     `gorm:"size:32;index;not null;uniqueIndex:uk_oauth_subject,priority:1" json:"userId"`
	Provider    string     `gorm:"size:32;not null;uniqueIndex:uk_oauth_subject,priority:2" json:"provider"`
	Subject     string     `gorm:"size:128;not null;uniqueIndex:uk_oauth_subject,priority:3" json:"subject"`
	Email       string     `gorm:"size:128" json:"email,omitempty"`
	Username    string     `gorm:"size:64" json:"username,omitempty"`
	Nickname    string     `gorm:"size:64" json:"nickname,omitempty"`
	Avatar      string     `gorm:"size:255" json:"avatar,omitempty"`
	RawProfile  string     `gorm:"type:text" json:"-"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// TableName 指定表名。
func (UserOAuthBinding) TableName() string { return "sys_user_oauth_binding" }
