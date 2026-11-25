package model

import (
	"math"
	"time"
)

// 会员状态。用户没有等级、等级被停用或已过期时，都按非会员处理。
const (
	MembershipNone     = "none"     // 非会员：从未开通
	MembershipActive   = "active"   // 有效会员
	MembershipExpired  = "expired"  // 已过期
	MembershipDisabled = "disabled" // 所属等级已停用
)

// Benefit 一项会员权益。用键值对描述，业务侧按 Key 判断，
// Value 支持数量、开关等任意文案，避免为每种权益加一列。
type Benefit struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// MembershipLevel 会员等级。
// Rank 表示等级高低（数值越大越高），用于排序与升级判断；
// 与 RBAC 角色无关：角色决定能进哪些后台页面，等级决定能享受哪些业务权益。
type MembershipLevel struct {
	Base
	Code        string `gorm:"size:32;uniqueIndex;not null" json:"code"`
	Name        string `gorm:"size:64;not null" json:"name"`
	Rank        int    `gorm:"default:0" json:"rank"`
	Status      int8   `gorm:"index" json:"status"`
	Description string `gorm:"size:255" json:"description,omitempty"`
	DefaultDays int    `gorm:"default:0" json:"defaultDays"`
	// Price 充值售价，单位「分」；0 表示该等级不参与自助购买。
	// 只有装配 recharge 模块时才有意义，默认发行版里恒为 0。
	Price     int64     `gorm:"default:0" json:"price"`
	Color     string    `gorm:"size:16" json:"color,omitempty"`
	Benefits  []Benefit `gorm:"serializer:json;type:text" json:"benefits"`
	UserCount int64     `gorm:"-" json:"userCount"`
}

// TableName 指定表名。
func (MembershipLevel) TableName() string { return "sys_membership_level" }

// Active 判断等级本身是否可用（未被停用、未删除）。
func (l *MembershipLevel) Active() bool {
	return l != nil && l.Status == StatusEnabled && !l.DeletedAt.Valid
}

// BenefitMap 把权益列表展开为键值对，便于业务侧直接读取。
func (l *MembershipLevel) BenefitMap() map[string]string {
	out := make(map[string]string, len(l.Benefits))
	if l == nil {
		return out
	}
	for _, b := range l.Benefits {
		out[b.Key] = b.Value
	}
	return out
}

// MembershipSnapshot 是用户会员状态的只读视图，用于接口返回与权益判断。
type MembershipSnapshot struct {
	Member   bool              `json:"member"`
	State    string            `json:"state"`
	Level    *MembershipLevel  `json:"level,omitempty"`
	LevelID  string            `json:"levelId,omitempty"`
	StartAt  *time.Time        `json:"startAt,omitempty"`
	ExpireAt *time.Time        `json:"expireAt,omitempty"`
	DaysLeft int               `json:"daysLeft"` // -1 表示长期有效
	Benefits map[string]string `json:"benefits"`
}

// Evaluate 依据当前时间计算会员状态。等级为空即非会员；
// 到期不做物理清理，保留等级与到期时间便于运营查看"曾开通/已过期"。
func Evaluate(level *MembershipLevel, levelID string, start, expire *time.Time, now time.Time) MembershipSnapshot {
	snap := MembershipSnapshot{
		State: MembershipNone, LevelID: levelID, StartAt: start, ExpireAt: expire,
		DaysLeft: -1, Benefits: map[string]string{},
	}
	if levelID == "" {
		return snap
	}
	if level == nil || !level.Active() {
		snap.State = MembershipDisabled
		return snap
	}
	if expire != nil && !expire.After(now) {
		snap.State = MembershipExpired
		return snap
	}
	snap.Member = true
	snap.State = MembershipActive
	snap.Level = level
	snap.Benefits = level.BenefitMap()
	if expire != nil {
		days := int(math.Ceil(expire.Sub(now).Hours() / 24))
		if days < 1 {
			days = 1
		}
		snap.DaysLeft = days
	}
	return snap
}
