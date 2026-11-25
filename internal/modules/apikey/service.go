// Package apikey 提供面向程序调用方的机器身份体系。
//
// 设计边界：它只解决「谁在调用、能调什么、调用多快」，不掺入任何业务语义。
// 爬虫、Webhook 回调、CI 上报、合作方取数都复用同一套凭证，
// 下游权限判断仍走 RequirePerm，业务代码不需要区分人还是程序。
package apikey

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/internal/platform/cache"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
	"github.com/MITEZ360/tenon/pkg/query"
)

const (
	// 未显式配置配额时的默认值。客户端默认更严，因为它分发在不可信环境。
	defaultServiceRPS = 60
	defaultClientRPS  = 10
	// maxScopes 限制单个凭证的权限点数量，避免一条凭证等于超级管理员。
	maxScopes = 64
)

// Service 凭证业务。
type Service struct {
	db    *gorm.DB
	cache cache.Cache
	log   *zap.Logger

	mu       sync.Mutex
	limiters map[string]*limiterEntry
}

type limiterEntry struct {
	l     cache.Limiter
	rps   float64
	burst int
}

// NewService 创建凭证服务。
func NewService(db *gorm.DB, c cache.Cache, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, cache: c, log: log, limiters: map[string]*limiterEntry{}}
}

// Input 创建与编辑入参。
type Input struct {
	Name       string     `json:"name" binding:"required,max=64" label:"名称"`
	Kind       string     `json:"kind" binding:"omitempty,oneof=service client" label:"类型"`
	Scopes     []string   `json:"scopes" label:"权限范围"`
	IPAllow    []string   `json:"ipAllow" label:"IP 白名单"`
	RateRPS    int        `json:"rateRps" binding:"omitempty,min=0,max=10000" label:"每秒请求数"`
	RateBurst  int        `json:"rateBurst" binding:"omitempty,min=0,max=20000" label:"突发容量"`
	DailyQuota int64      `json:"dailyQuota" binding:"omitempty,min=0" label:"每日配额"`
	ExpireAt   *time.Time `json:"expireAt" label:"过期时间"`
	Remark     string     `json:"remark" binding:"max=255" label:"备注"`
	Status     *int8      `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
}

// Issued 创建与轮换的结果：明文只在这里出现一次。
type Issued struct {
	Key   *model.APIKey `json:"key"`
	Plain string        `json:"plain"`
}

// List 凭证分页。SecretHash 有 json:"-"，不会外泄。
func (s *Service) List(ctx context.Context, p *query.Params) ([]*model.APIKey, int64, error) {
	allowed := map[string]string{
		"name": "name", "kind": "kind", "status": "status",
		"created_at": "created_at", "last_used_at": "last_used_at",
	}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.APIKey{}), allowed)
	db = query.Search(db, p.Keyword, "name", "prefix", "remark")

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.APIKey
	err := db.Order(p.Order(map[string]string{
		"created_at": "created_at", "last_used_at": "last_used_at", "name": "name",
	}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// Create 签发凭证。actor 用于防止越权：非超管只能授予自己已有的权限点。
func (s *Service) Create(ctx context.Context, in *Input, actor *authn.Identity) (*Issued, error) {
	kind := in.Kind
	if kind == "" {
		kind = model.APIKeyService
	}
	scopes, err := s.normalizeScopes(ctx, in.Scopes, actor)
	if err != nil {
		return nil, err
	}
	ips, err := normalizeIPs(ctx, in.IPAllow)
	if err != nil {
		return nil, err
	}
	head, size := "ak_s_", 40
	if kind == model.APIKeyClient {
		head, size = "ak_c_", 32
	}
	plain, prefix, hash := authn.GenerateAPIKey(head, size)
	k := &model.APIKey{
		Name:        strings.TrimSpace(in.Name),
		Prefix:      prefix,
		SecretHash:  hash,
		Kind:        kind,
		Scopes:      scopes,
		IPAllow:     ips,
		RateRPS:     in.RateRPS,
		RateBurst:   in.RateBurst,
		DailyQuota:  in.DailyQuota,
		ExpireAt:    in.ExpireAt,
		Status:      statusOr(in.Status, model.StatusEnabled),
		Remark:      in.Remark,
		CreatedBy:   actorID(actor),
		CreatorName: actorName(actor),
	}
	if err := s.db.WithContext(ctx).Create(k).Error; err != nil {
		return nil, err
	}
	return &Issued{Key: k, Plain: plain}, nil
}

// Update 编辑凭证。类型不可变：改动它会让「是否可作为秘密保管」的语义漂移，
// 需要另一种类型时应当新建并停用旧凭证。
func (s *Service) Update(ctx context.Context, id string, in *Input, actor *authn.Identity) (*model.APIKey, error) {
	k, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	scopes, err := s.normalizeScopes(ctx, in.Scopes, actor)
	if err != nil {
		return nil, err
	}
	ips, err := normalizeIPs(ctx, in.IPAllow)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"name":        strings.TrimSpace(in.Name),
		"scopes":      scopes,
		"ip_allow":    ips,
		"rate_rps":    in.RateRPS,
		"rate_burst":  in.RateBurst,
		"daily_quota": in.DailyQuota,
		"expire_at":   in.ExpireAt,
		"remark":      in.Remark,
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if err := s.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", k.ID).
		Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Rotate 轮换密钥：生成新密钥并立即作废旧密钥。
// 旧明文无法恢复，因此轮换是不可逆操作，需要调用方自行完成切换。
func (s *Service) Rotate(ctx context.Context, id string, actor *authn.Identity) (*Issued, error) {
	k, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	head, size := "ak_s_", 40
	if k.Kind == model.APIKeyClient {
		head, size = "ak_c_", 32
	}
	plain, prefix, hash := authn.GenerateAPIKey(head, size)
	if err := s.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", k.ID).
		Updates(map[string]any{"prefix": prefix, "secret_hash": hash, "last_used_at": nil}).
		Error; err != nil {
		return nil, err
	}
	k.Prefix, k.LastUsedAt = prefix, nil
	return &Issued{Key: k, Plain: plain}, nil
}

// SetStatus 启用或停用。
func (s *Service) SetStatus(ctx context.Context, id string, status int8) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).
		Update("status", status).Error
}

// Delete 删除凭证。软删除，保留审计轨迹。
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Delete(&model.APIKey{}, "id = ?", id).Error
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, id string) (*model.APIKey, error) {
	var k model.APIKey
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&k).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("凭证不存在")
		}
		return nil, err
	}
	return &k, nil
}

// Authenticate 校验明文密钥，供中间件调用。
func (s *Service) Authenticate(ctx context.Context, plain, ip string) (*model.APIKey, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return nil, errs.APIKeyInvalid("缺少 API 密钥")
	}
	var k model.APIKey
	err := s.db.WithContext(ctx).Where("secret_hash = ?", authn.HashAPIKey(plain)).First(&k).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 不区分「不存在」与「已删除」，避免被用来枚举凭证
			return nil, errs.APIKeyInvalid("API 密钥无效")
		}
		return nil, err
	}
	switch k.Check(time.Now()) {
	case model.APIKeyDisabled:
		return nil, errs.APIKeyInvalid("API 密钥已停用")
	case model.APIKeyExpired:
		return nil, errs.APIKeyInvalid("API 密钥已过期")
	}
	if !k.AllowIP(ip) {
		return nil, errs.Forbidden("来源 IP 不在白名单内")
	}
	s.touch(&k, ip)
	return &k, nil
}

// Allow 判断凭证当前是否在配额内。限流组件故障时放行，
// 不把缓存问题放大成业务不可用。
func (s *Service) Allow(ctx context.Context, k *model.APIKey) (bool, error) {
	if ok, err := s.limiterFor(k).Allow(ctx, "apikey:"+k.ID); err != nil {
		s.log.Warn("API 密钥限流降级", zap.String("key", k.Prefix), zap.Error(err))
	} else if !ok {
		return false, nil
	}
	if k.DailyQuota > 0 {
		key := "apikey:quota:" + k.ID + ":" + time.Now().Format("20060102")
		n, err := s.cache.Incr(ctx, key, 1)
		if err != nil {
			s.log.Warn("API 密钥配额统计失败", zap.String("key", k.Prefix), zap.Error(err))
			return true, nil
		}
		if n == 1 {
			_ = s.cache.Expire(ctx, key, untilTomorrow())
		}
		if n > k.DailyQuota {
			return false, nil
		}
	}
	return true, nil
}

// Scopes 返回可作为 scope 的权限点清单，供管理端选择。
func (s *Service) Scopes(ctx context.Context) ([]string, error) {
	var perms []string
	err := s.db.WithContext(ctx).Model(&model.Menu{}).
		Where("perm IS NOT NULL AND perm <> ''").
		Distinct().Pluck("perm", &perms).Error
	if err != nil {
		return nil, err
	}
	sort.Strings(perms)
	return perms, nil
}

// Usage 返回缓存的当日用量，用于列表展示。
// 用只读的 Get 而不是 Incr(0)：后者会在缓存里创建一堆零值键。
func (s *Service) Usage(ctx context.Context, ids []string) map[string]int64 {
	out := make(map[string]int64, len(ids))
	day := time.Now().Format("20060102")
	for _, id := range ids {
		b, err := s.cache.Get(ctx, "apikey:quota:"+id+":"+day)
		if err != nil {
			continue
		}
		if n, err := strconv.ParseInt(string(b), 10, 64); err == nil && n > 0 {
			out[id] = n
		}
	}
	return out
}

// ---- 内部 ----

func (s *Service) normalizeScopes(ctx context.Context, in []string, actor *authn.Identity) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		sc := strings.TrimSpace(raw)
		if sc == "" {
			continue
		}
		if sc != "*" && !validScope(sc) {
			return nil, errs.Validation(i18n.Tf(ctx, "权限范围格式不正确：{0}", sc))
		}
		if _, dup := seen[sc]; dup {
			continue
		}
		// 越权保护：持有凭证管理权限的人不能凭空造出比自己更大的凭证。
		if actor != nil && !actor.Super {
			if sc == "*" || !actor.Has(sc) {
				return nil, errs.Forbidden(i18n.Tf(ctx, "不能授予自己不具备的权限：{0}", sc))
			}
		}
		seen[sc] = struct{}{}
		out = append(out, sc)
	}
	if len(out) == 0 {
		return nil, errs.BadRequest("至少需要一个权限范围")
	}
	if len(out) > maxScopes {
		return nil, errs.BadRequest(i18n.Tf(ctx, "权限范围最多 {0} 个", maxScopes))
	}
	return out, nil
}

func validScope(sc string) bool {
	if len(sc) > 96 {
		return false
	}
	parts := strings.Split(sc, ":")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || p == "*" {
			return false
		}
		for _, r := range p {
			if !(r == '-' || r == '_' || r == '.' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

func normalizeIPs(ctx context.Context, in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		rule := strings.TrimSpace(raw)
		if rule == "" {
			continue
		}
		if _, err := netip.ParsePrefix(rule); err != nil {
			if _, err := netip.ParseAddr(rule); err != nil {
				return nil, errs.Validation(i18n.Tf(ctx, "IP 或网段格式不正确：{0}", rule))
			}
		}
		out = append(out, rule)
	}
	return out, nil
}

func (s *Service) limiterFor(k *model.APIKey) cache.Limiter {
	rps, burst := defaults(k)
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.limiters[k.ID]; ok && e.rps == rps && e.burst == burst {
		return e.l
	}
	l := cache.NewLimiter(s.cache, rps, burst)
	// 凭证量级很小（通常几十个），不额外做回收；
	// 删除凭证后残留的令牌桶不影响正确性。
	s.limiters[k.ID] = &limiterEntry{l: l, rps: rps, burst: burst}
	return l
}

func defaults(k *model.APIKey) (float64, int) {
	rps := k.RateRPS
	if rps <= 0 {
		if k.Kind == model.APIKeyClient {
			rps = defaultClientRPS
		} else {
			rps = defaultServiceRPS
		}
	}
	burst := k.RateBurst
	if burst <= 0 {
		burst = rps * 2
	}
	return float64(rps), burst
}

// touch 记录最后使用时间。按分钟节流，避免每个请求都写一次库。
func (s *Service) touch(k *model.APIKey, ip string) {
	now := time.Now()
	if k.LastUsedAt != nil && now.Sub(*k.LastUsedAt) < time.Minute {
		return
	}
	// 用条件更新而非覆盖：多实例同时打过来时只有一个能写成功。
	err := s.db.Model(&model.APIKey{}).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", k.ID, now.Add(-time.Minute)).
		Updates(map[string]any{"last_used_at": now, "last_used_ip": ip}).Error
	if err != nil {
		s.log.Debug("更新凭证使用时间失败", zap.String("id", k.ID), zap.Error(err))
	}
	k.LastUsedAt, k.LastUsedIP = &now, ip
}

func untilTomorrow() time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	return next.Sub(now) + time.Minute
}

func statusOr(v *int8, def int8) int8 {
	if v == nil {
		return def
	}
	return *v
}

func actorID(a *authn.Identity) string {
	if a == nil {
		return ""
	}
	return a.UserID
}

func actorName(a *authn.Identity) string {
	if a == nil {
		return ""
	}
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.Username
}
