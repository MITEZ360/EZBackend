// Package authn 提供认证基础能力：JWT 签发校验、密码散列、TOTP 二次验证与图形验证码。
package authn

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/keel-framework/keel/internal/platform/config"
)

// 认证相关错误。
var (
	ErrTokenInvalid = errors.New("令牌无效")
	ErrTokenExpired = errors.New("令牌已过期")
)

// Claims 是访问令牌载荷。仅存放轻量身份信息，权限点按需从缓存加载。
type Claims struct {
	UserID    string   `json:"uid"`
	Username  string   `json:"username"`
	Nickname  string   `json:"nickname,omitempty"`
	DeptID    string   `json:"dept_id,omitempty"`
	Roles     []string `json:"roles,omitempty"`
	Super     bool     `json:"super,omitempty"`
	SessionID string   `json:"sid,omitempty"`
	TokenType string   `json:"typ,omitempty"`
	jwt.RegisteredClaims
}

// Manager 负责签发与解析访问令牌。
type Manager struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewManager 创建令牌管理器。
func NewManager(cfg config.JWT) *Manager {
	return &Manager{
		secret:     []byte(cfg.Secret),
		issuer:     cfg.Issuer,
		accessTTL:  cfg.AccessTTL,
		refreshTTL: cfg.RefreshTTL,
	}
}

// AccessTTL 返回访问令牌有效期。
func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

// RefreshTTL 返回刷新令牌有效期。
func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

// Issue 签发访问令牌。
func (m *Manager) Issue(c Claims) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(m.accessTTL)
	c.TokenType = "access"
	c.Issuer = m.issuer
	c.IssuedAt = jwt.NewNumericDate(now)
	c.NotBefore = jwt.NewNumericDate(now.Add(-5 * time.Second))
	c.ExpiresAt = jwt.NewNumericDate(exp)
	c.ID = c.SessionID
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	s, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("签发令牌失败: %w", err)
	}
	return s, exp, nil
}

// Parse 解析并校验令牌（签名算法、签发者、有效期）。
func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非法签名算法: %v", t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if !token.Valid || claims.UserID == "" {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// Identity 是请求上下文中的登录主体。
type Identity struct {
	UserID    string              `json:"userId"`
	Username  string              `json:"username"`
	Nickname  string              `json:"nickname"`
	DeptID    string              `json:"deptId,omitempty"`
	Roles     []string            `json:"roles"`
	Super     bool                `json:"super"`
	SessionID string              `json:"sessionId,omitempty"`
	Perms     map[string]struct{} `json:"-"`
}

// Has 判断是否具备权限点。
func (i *Identity) Has(perm string) bool {
	if i == nil {
		return false
	}
	if i.Super {
		return true
	}
	if i.Perms == nil {
		return false
	}
	_, ok := i.Perms["*"]
	if ok {
		return true
	}
	_, ok = i.Perms[perm]
	return ok
}

// HasRole 判断是否具备角色码。
func (i *Identity) HasRole(code string) bool {
	if i == nil {
		return false
	}
	for _, r := range i.Roles {
		if r == code {
			return true
		}
	}
	return false
}
