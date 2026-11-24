package model

import "time"

// User 系统用户。
type User struct {
	Base
	Username string `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Password string `gorm:"size:128;not null" json:"-"`
	Nickname string `gorm:"size:64" json:"nickname"`
	RealName string `gorm:"size:64" json:"realName,omitempty"`
	Email    string `gorm:"size:128;index" json:"email,omitempty"`
	Phone    string `gorm:"size:32;index" json:"phone,omitempty"`
	Avatar   string `gorm:"size:255" json:"avatar,omitempty"`
	Gender   int8   `gorm:"default:0" json:"gender"`
	DeptID   string `gorm:"size:32;index" json:"deptId,omitempty"`
	Status   int8   `gorm:"index" json:"status"`
	IsSuper  bool   `gorm:"default:false" json:"isSuper"`
	// Locale/Timezone 是用户级展示偏好；空值表示跟随系统默认。
	Locale      string     `gorm:"size:16;default:'zh-CN'" json:"locale,omitempty"`
	Timezone    string     `gorm:"size:64;default:'Asia/Shanghai'" json:"timezone,omitempty"`
	Remark      string     `gorm:"size:255" json:"remark,omitempty"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	LastLoginIP string     `gorm:"size:64" json:"lastLoginIp,omitempty"`
	LoginCount  int64      `gorm:"default:0" json:"loginCount"`
	TOTPSecret  string     `gorm:"size:255" json:"-"`
	TOTPEnabled bool       `gorm:"default:false" json:"totpEnabled"`
	// 会员信息。LevelID 为空即非会员；到期不清理数据，由 Evaluate 按时间判定，
	// 这样运营仍能看到"开通过什么等级、何时到期"。
	MembershipLevelID  string           `gorm:"size:32;index" json:"membershipLevelId,omitempty"`
	MembershipStartAt  *time.Time       `json:"membershipStartAt,omitempty"`
	MembershipExpireAt *time.Time       `gorm:"index" json:"membershipExpireAt,omitempty"`
	Membership         *MembershipLevel `gorm:"-" json:"membership,omitempty"`
	MembershipState    string           `gorm:"-" json:"membershipState,omitempty"`
	Roles              []Role           `gorm:"-" json:"roles,omitempty"`
	Dept               *Dept            `gorm:"-" json:"dept,omitempty"`
	RoleIDs            []string         `gorm:"-" json:"roleIds,omitempty"`
}

// TableName 指定表名。
func (User) TableName() string { return "sys_user" }

// UserRole 用户-角色关联。
type UserRole struct {
	UserID string `gorm:"primaryKey;size:32" json:"userId"`
	RoleID string `gorm:"primaryKey;size:32" json:"roleId"`
}

// TableName 指定表名。
func (UserRole) TableName() string { return "sys_user_role" }

// UserRecoveryCode 两步验证的一次性恢复码。
// 只存哈希，验证通过后写入 used_at；不设软删除与 Base ——
// 恢复码是消耗型记录，保留行才能统计"已用/未用"，软删除会让唯一性判断变形。
type UserRecoveryCode struct {
	ID        string     `gorm:"primaryKey;size:32" json:"id"`
	UserID    string     `gorm:"size:32;index:idx_recovery_user" json:"userId"`
	CodeHash  string     `gorm:"size:64;not null" json:"-"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// TableName 指定表名。
func (UserRecoveryCode) TableName() string { return "sys_user_recovery_code" }

// Role 角色。
type Role struct {
	Base
	Code        string   `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name        string   `gorm:"size:64;not null" json:"name"`
	Description string   `gorm:"size:255" json:"description,omitempty"`
	DataScope   int8     `gorm:"default:4" json:"dataScope"`
	Sort        int      `gorm:"default:0" json:"sort"`
	Status      int8     `gorm:"index" json:"status"`
	MenuIDs     []string `gorm:"-" json:"menuIds,omitempty"`
	UserCount   int64    `gorm:"-" json:"userCount,omitempty"`
}

// TableName 指定表名。
func (Role) TableName() string { return "sys_role" }

// RoleMenu 角色-菜单关联。
type RoleMenu struct {
	RoleID string `gorm:"primaryKey;size:32" json:"roleId"`
	MenuID string `gorm:"primaryKey;size:32" json:"menuId"`
}

// TableName 指定表名。
func (RoleMenu) TableName() string { return "sys_role_menu" }

// Menu 菜单与权限点。type=button 时仅作为权限点存在。
type Menu struct {
	Base
	ParentID  string  `gorm:"size:32;index" json:"parentId"`
	Name      string  `gorm:"size:64;not null" json:"name"`
	Type      string  `gorm:"size:16;default:menu;index" json:"type"`
	Path      string  `gorm:"size:128" json:"path,omitempty"`
	Component string  `gorm:"size:128" json:"component,omitempty"`
	Icon      string  `gorm:"size:64" json:"icon,omitempty"`
	Perm      string  `gorm:"size:96;index" json:"perm,omitempty"`
	Sort      int     `gorm:"default:0" json:"sort"`
	Visible   bool    `gorm:"default:true" json:"visible"`
	KeepAlive bool    `gorm:"default:false" json:"keepAlive"`
	Status    int8    `json:"status"`
	Children  []*Menu `gorm:"-" json:"children,omitempty"`
}

// TableName 指定表名。
func (Menu) TableName() string { return "sys_menu" }

// Dept 部门（树形）。
type Dept struct {
	Base
	ParentID  string  `gorm:"size:32;index" json:"parentId"`
	Name      string  `gorm:"size:64;not null" json:"name"`
	Leader    string  `gorm:"size:64" json:"leader,omitempty"`
	Phone     string  `gorm:"size:32" json:"phone,omitempty"`
	Email     string  `gorm:"size:128" json:"email,omitempty"`
	Sort      int     `gorm:"default:0" json:"sort"`
	Status    int8    `json:"status"`
	Children  []*Dept `gorm:"-" json:"children,omitempty"`
	UserCount int64   `gorm:"-" json:"userCount,omitempty"`
}

// TableName 指定表名。
func (Dept) TableName() string { return "sys_dept" }

// Session 登录会话，承载刷新令牌与在线状态。
type Session struct {
	ID          string    `gorm:"primaryKey;size:32" json:"id"`
	UserID      string    `gorm:"size:32;index;not null" json:"userId"`
	RefreshHash string    `gorm:"size:64;index" json:"-"`
	UserAgent   string    `gorm:"size:255" json:"userAgent,omitempty"`
	IP          string    `gorm:"size:64" json:"ip,omitempty"`
	Location    string    `gorm:"size:64" json:"location,omitempty"`
	Browser     string    `gorm:"size:32" json:"browser,omitempty"`
	OS          string    `gorm:"size:32" json:"os,omitempty"`
	ExpiresAt   time.Time `gorm:"index" json:"expiresAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	Revoked     bool      `gorm:"default:false;index" json:"revoked"`
	CreatedAt   time.Time `json:"createdAt"`
}

// TableName 指定表名。
func (Session) TableName() string { return "sys_session" }
