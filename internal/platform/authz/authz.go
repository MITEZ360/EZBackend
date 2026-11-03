// Package authz 负责加载用户的角色与权限点，并做短时缓存；权限变更时通过版本号整体失效。
package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/cache"
)

const (
	cacheVersionKey = "authz:version"
	permCacheTTL    = 5 * time.Minute
)

// Loader 加载并缓存用户权限。
type Loader struct {
	db  *gorm.DB
	c   cache.Cache
	log *zap.Logger
}

// NewLoader 创建权限加载器。
func NewLoader(db *gorm.DB, c cache.Cache, log *zap.Logger) *Loader {
	if log == nil {
		log = zap.NewNop()
	}
	return &Loader{db: db, c: c, log: log}
}

type cachedAuth struct {
	UserID   string   `json:"userId"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	DeptID   string   `json:"deptId"`
	Roles    []string `json:"roles"`
	RoleIDs  []string `json:"roleIds"`
	Super    bool     `json:"super"`
	Perms    []string `json:"perms"`
	Status   int8     `json:"status"`
}

// Load 返回用户身份与权限集合。
func (l *Loader) Load(ctx context.Context, userID string) (*authn.Identity, error) {
	ver := l.version(ctx)
	key := fmt.Sprintf("authz:user:%s:v%d", userID, ver)
	var cached cachedAuth
	if ok, err := cache.GetJSON(ctx, l.c, key, &cached); err == nil && ok {
		if cached.Status != model.StatusEnabled {
			return nil, errors.New("账号已被禁用")
		}
		return toIdentity(&cached), nil
	}

	var user model.User
	if err := l.db.WithContext(ctx).Select("id", "username", "nickname", "dept_id", "is_super", "status").
		First(&user, "id = ?", userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("账号不存在")
		}
		return nil, err
	}
	if user.Status != model.StatusEnabled {
		return nil, errors.New("账号已被禁用")
	}

	var roles []model.Role
	err := l.db.WithContext(ctx).
		Table("sys_role AS r").
		Select("r.id, r.code, r.name, r.status").
		Joins("JOIN sys_user_role AS ur ON ur.role_id = r.id").
		Where("ur.user_id = ? AND r.status = ? AND r.deleted_at IS NULL", userID, model.StatusEnabled).
		Scan(&roles).Error
	if err != nil {
		return nil, err
	}

	ca := cachedAuth{
		UserID: user.ID, Username: user.Username, Nickname: user.Nickname,
		DeptID: user.DeptID, Super: user.IsSuper, Status: user.Status,
	}
	roleIDs := make([]string, 0, len(roles))
	for _, r := range roles {
		ca.Roles = append(ca.Roles, r.Code)
		roleIDs = append(roleIDs, r.ID)
	}
	ca.RoleIDs = roleIDs

	if user.IsSuper {
		ca.Perms = []string{"*"}
	} else if len(roleIDs) > 0 {
		var perms []string
		err := l.db.WithContext(ctx).
			Table("sys_menu AS m").
			Distinct("m.perm").
			Joins("JOIN sys_role_menu AS rm ON rm.menu_id = m.id").
			Where("rm.role_id IN ? AND m.status = ? AND m.deleted_at IS NULL AND m.perm <> ''", roleIDs, model.StatusEnabled).
			Pluck("m.perm", &perms).Error
		if err != nil {
			return nil, err
		}
		ca.Perms = perms
	}

	_ = cache.SetJSON(ctx, l.c, key, &ca, permCacheTTL)
	return toIdentity(&ca), nil
}

func toIdentity(c *cachedAuth) *authn.Identity {
	id := &authn.Identity{
		UserID: c.UserID, Username: c.Username, Nickname: c.Nickname,
		DeptID: c.DeptID, Roles: c.Roles, Super: c.Super,
		Perms: make(map[string]struct{}, len(c.Perms)),
	}
	for _, p := range c.Perms {
		id.Perms[p] = struct{}{}
	}
	return id
}

func (l *Loader) version(ctx context.Context) int64 {
	b, err := l.c.Get(ctx, cacheVersionKey)
	if err != nil {
		return 1
	}
	var v int64
	if _, err := fmt.Sscanf(string(b), "%d", &v); err != nil || v == 0 {
		return 1
	}
	return v
}

// Invalidate 使全部用户权限缓存失效（角色/菜单变更后调用）。
func (l *Loader) Invalidate(ctx context.Context) {
	_, err := l.c.Incr(ctx, cacheVersionKey, 1)
	if err != nil {
		l.log.Warn("刷新权限缓存版本失败", zap.Error(err))
	}
}
