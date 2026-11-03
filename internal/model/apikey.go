package model

import (
	"net/netip"
	"strings"
	"time"
)

// API Key 类型。
//
// 区别只在「密钥是否可作为秘密保管」，与权限无关：
//   - service：服务端持有，用于爬虫、批处理、服务间调用，泄露即等于身份被盗；
//   - client：随安装包或前端资源分发，反编译即可获得，只能当作来源标识，
//     必须配合严格配额与最小 scope，不能靠它单独保护敏感数据。
const (
	APIKeyService = "service"
	APIKeyClient  = "client"
)

// API Key 不可用原因。
const (
	APIKeyOK       = ""
	APIKeyDisabled = "disabled"
	APIKeyExpired  = "expired"
)

// APIKey 是面向程序调用方的机器身份。
//
// 与用户体系解耦：不占用 sys_user，不依赖角色；Scopes 与 RBAC 权限点同构，
// 因此下游的 RequirePerm 可以原样复用，业务代码无需区分人还是程序在调用。
type APIKey struct {
	Base
	Name        string     `gorm:"size:64;not null" json:"name"`
	Prefix      string     `gorm:"size:20;index;not null" json:"prefix"`
	SecretHash  string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Kind        string     `gorm:"size:16;not null;default:service" json:"kind"`
	Scopes      []string   `gorm:"serializer:json;type:text" json:"scopes"`
	IPAllow     []string   `gorm:"serializer:json;type:text" json:"ipAllow"`
	RateRPS     int        `gorm:"default:0" json:"rateRps"`
	RateBurst   int        `gorm:"default:0" json:"rateBurst"`
	DailyQuota  int64      `gorm:"default:0" json:"dailyQuota"`
	ExpireAt    *time.Time `json:"expireAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt"`
	LastUsedIP  string     `gorm:"size:64" json:"lastUsedIp,omitempty"`
	Status      int8       `gorm:"index;default:1" json:"status"`
	Remark      string     `gorm:"size:255" json:"remark,omitempty"`
	CreatedBy   string     `gorm:"size:32" json:"createdBy,omitempty"`
	CreatorName string     `gorm:"size:64" json:"creatorName,omitempty"`
}

// TableName 指定表名。
func (APIKey) TableName() string { return "sys_api_key" }

// Secret 判断凭证是否按秘密保管。
func (k *APIKey) Secret() bool { return k.Kind == APIKeyService }

// Check 判断凭证当前是否可用，返回不可用原因。
// 每次请求实时判定过期与停用，不做定时清理，多实例天然一致。
func (k *APIKey) Check(now time.Time) string {
	if k == nil || k.Status != StatusEnabled || k.DeletedAt.Valid {
		return APIKeyDisabled
	}
	if k.ExpireAt != nil && !now.Before(*k.ExpireAt) {
		return APIKeyExpired
	}
	return APIKeyOK
}

// AllowIP 判断来源 IP 是否在白名单内。
// 留空表示不限制；规则支持单个 IP 与 CIDR 网段。
func (k *APIKey) AllowIP(ip string) bool {
	if len(k.IPAllow) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, rule := range k.IPAllow {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		if p, err := netip.ParsePrefix(rule); err == nil {
			if p.Contains(addr) {
				return true
			}
			continue
		}
		if a, err := netip.ParseAddr(rule); err == nil && a.Unmap() == addr {
			return true
		}
	}
	return false
}
