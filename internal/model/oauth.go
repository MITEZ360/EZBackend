package model

import "time"

// UserOAuthBinding 第三方身份与本地账号的绑定关系。
//
// 两个唯一约束：
//   - (provider, subject)：同一个外部账号只能属于一个本地用户，
//     与 resolveUser 按该组合查绑定的逻辑保持一致；
//   - (user_id, provider)：同一个本地用户在单个渠道只能绑一个外部账号，
//     与绑定流程里的「该渠道已绑定其它账号」检查保持一致。
//
// 解绑采用物理删除（Unscoped），避免软删除行继续占用唯一键导致无法重新绑定。
type UserOAuthBinding struct {
	Base
	UserID      string     `gorm:"size:32;index;not null;uniqueIndex:uk_oauth_user_provider,priority:1" json:"userId"`
	Provider    string     `gorm:"size:32;not null;uniqueIndex:uk_oauth_subject,priority:1;uniqueIndex:uk_oauth_user_provider,priority:2" json:"provider"`
	Subject     string     `gorm:"size:128;not null;uniqueIndex:uk_oauth_subject,priority:2" json:"subject"`
	Email       string     `gorm:"size:128" json:"email,omitempty"`
	Username    string     `gorm:"size:64" json:"username,omitempty"`
	Nickname    string     `gorm:"size:64" json:"nickname,omitempty"`
	Avatar      string     `gorm:"size:255" json:"avatar,omitempty"`
	RawProfile  string     `gorm:"type:text" json:"-"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// TableName 指定表名。
func (UserOAuthBinding) TableName() string { return "sys_user_oauth_binding" }
