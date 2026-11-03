// Package membership 会员体系：等级配置、开通/取消、权益判定。
//
// 与 RBAC 的分工：角色决定"能进哪些后台页面"，会员等级决定"能享受哪些业务权益"。
// 用户没有等级即非会员；等级被停用或已过期都按非会员处理，但保留原始数据，
// 便于运营查看开通历史，也避免依赖定时任务清理（无状态多实例下更稳）。
package membership

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/utils"
)

// Service 会员业务。
type Service struct {
	db  *gorm.DB
	log *zap.Logger
}

// NewService 创建会员服务。
func NewService(db *gorm.DB, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, log: log}
}

var (
	codePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
	benefitPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,63}$`)
	colorPattern   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// 权益值为这些文案时视为"未开通该权益"，便于运营用布尔值配置。
	falsyValues = map[string]bool{"": true, "false": true, "0": true, "off": true, "no": true}
)

// LevelInput 等级创建/更新入参。
type LevelInput struct {
	Code        string          `json:"code" binding:"required,max=32" label:"等级编码"`
	Name        string          `json:"name" binding:"required,max=64" label:"等级名称"`
	Rank        int             `json:"rank" binding:"omitempty,min=0,max=9999" label:"等级权重"`
	Status      *int8           `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Description string          `json:"description" binding:"max=255" label:"说明"`
	DefaultDays int             `json:"defaultDays" binding:"omitempty,min=0,max=36500" label:"默认时长"`
	Color       string          `json:"color" binding:"omitempty,max=16" label:"标识色"`
	Benefits    []model.Benefit `json:"benefits" label:"权益"`
}

func (in *LevelInput) normalize() error {
	in.Code = strings.TrimSpace(in.Code)
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	in.Color = strings.TrimSpace(in.Color)
	if !codePattern.MatchString(in.Code) {
		return errs.BadRequest("等级编码需以小写字母开头，仅含小写字母、数字与下划线（2-32 位）")
	}
	if in.Color != "" && !colorPattern.MatchString(in.Color) {
		return errs.BadRequest("标识色需为 #RRGGBB 格式")
	}
	seen := map[string]bool{}
	out := make([]model.Benefit, 0, len(in.Benefits))
	for _, b := range in.Benefits {
		b.Key = strings.TrimSpace(b.Key)
		b.Label = strings.TrimSpace(b.Label)
		b.Value = strings.TrimSpace(b.Value)
		if b.Key == "" && b.Label == "" && b.Value == "" {
			continue // 前端表格里的空行直接忽略
		}
		if !benefitPattern.MatchString(b.Key) {
			return errs.BadRequest("权益标识「" + b.Key + "」不合法，需为小写字母开头的字母、数字、下划线或点")
		}
		if seen[b.Key] {
			return errs.BadRequest("权益标识「" + b.Key + "」重复")
		}
		if b.Label == "" {
			b.Label = b.Key
		}
		seen[b.Key] = true
		out = append(out, b)
	}
	in.Benefits = out
	return nil
}

// List 分页查询等级，并统计各等级当前有效会员数。
func (s *Service) List(ctx context.Context, p *query.Params) ([]*model.MembershipLevel, int64, error) {
	allowed := map[string]string{
		"code": "code", "name": "name", "status": "status", "rank": "rank", "created_at": "created_at",
	}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.MembershipLevel{}), allowed)
	db = query.Search(db, p.Keyword, "code", "name", "description")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.MembershipLevel
	err := db.Order(p.Order(map[string]string{
		"rank": "rank", "created_at": "created_at", "code": "code",
	}, "rank DESC, created_at ASC")).Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	if err := s.fillUserCounts(ctx, list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *Service) fillUserCounts(ctx context.Context, levels []*model.MembershipLevel) error {
	if len(levels) == 0 {
		return nil
	}
	var rows []struct {
		LevelID string
		N       int64
	}
	now := time.Now()
	err := s.db.WithContext(ctx).Model(&model.User{}).
		Select("membership_level_id AS level_id, COUNT(*) AS n").
		Where("membership_level_id <> '' AND (membership_expire_at IS NULL OR membership_expire_at > ?)", now).
		Group("membership_level_id").Scan(&rows).Error
	if err != nil {
		return err
	}
	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.LevelID] = r.N
	}
	for _, lv := range levels {
		lv.UserCount = counts[lv.ID]
	}
	return nil
}

// Catalog 返回所有启用的等级，按等级从高到低。个人中心用它做权益对比。
func (s *Service) Catalog(ctx context.Context) ([]*model.MembershipLevel, error) {
	var list []*model.MembershipLevel
	err := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled).
		Order("rank DESC, created_at ASC").Find(&list).Error
	if list == nil {
		list = []*model.MembershipLevel{}
	}
	return list, err
}

// Create 新增等级。
func (s *Service) Create(ctx context.Context, in *LevelInput) (*model.MembershipLevel, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	var n int64
	if err := s.db.WithContext(ctx).Unscoped().Model(&model.MembershipLevel{}).
		Where("code = ?", in.Code).Count(&n).Error; err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errs.Conflict("等级编码「" + in.Code + "」已被使用")
	}
	lv := &model.MembershipLevel{
		Code: in.Code, Name: in.Name, Rank: in.Rank,
		Status:      utils.Int8Or(in.Status, model.StatusEnabled),
		Description: in.Description, DefaultDays: in.DefaultDays,
		Color: in.Color, Benefits: in.Benefits,
	}
	if lv.Benefits == nil {
		lv.Benefits = []model.Benefit{}
	}
	if err := s.db.WithContext(ctx).Select("*").Create(lv).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, errs.Conflict("等级编码「" + in.Code + "」已被使用")
		}
		return nil, err
	}
	return lv, nil
}

// Update 更新等级。
func (s *Service) Update(ctx context.Context, id string, in *LevelInput) error {
	if err := in.normalize(); err != nil {
		return err
	}
	var lv model.MembershipLevel
	if err := s.db.WithContext(ctx).First(&lv, "id = ?", id).Error; err != nil {
		return errs.NotFound("会员等级不存在")
	}
	if in.Code != lv.Code {
		var n int64
		if err := s.db.WithContext(ctx).Unscoped().Model(&model.MembershipLevel{}).
			Where("code = ? AND id <> ?", in.Code, id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errs.Conflict("等级编码「" + in.Code + "」已被使用")
		}
	}
	benefits := in.Benefits
	if benefits == nil {
		benefits = []model.Benefit{}
	}
	// map 更新不经过 GORM 的 serializer:json，必须自己编码，否则驱动会把
	// []Benefit 当成结构体参数而报 unsupported type。
	encoded, err := json.Marshal(benefits)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Model(&model.MembershipLevel{}).Where("id = ?", id).
		Updates(map[string]any{
			"code": in.Code, "name": in.Name, "rank": in.Rank,
			"status":      utils.Int8Or(in.Status, model.StatusEnabled),
			"description": in.Description, "default_days": in.DefaultDays,
			"color": in.Color, "benefits": string(encoded),
		}).Error
	if err != nil && isUniqueViolation(err) {
		return errs.Conflict("等级编码「" + in.Code + "」已被使用")
	}
	return err
}

// Delete 删除等级。仍被用户引用时拒绝，避免历史会员突然变成"等级不存在"。
func (s *Service) Delete(ctx context.Context, id string) error {
	var lv model.MembershipLevel
	if err := s.db.WithContext(ctx).First(&lv, "id = ?", id).Error; err != nil {
		return errs.NotFound("会员等级不存在")
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).
		Where("membership_level_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errs.Conflict("仍有 " + strconv.FormatInt(n, 10) + " 个用户属于该等级，请先调整他们的等级或将等级停用")
	}
	return s.db.WithContext(ctx).Delete(&model.MembershipLevel{}, "id = ?", id).Error
}

// GrantInput 开通/续费入参。优先级：ExpireAt > Days > Permanent > 等级默认时长。
type GrantInput struct {
	LevelID   string `json:"levelId" binding:"required" label:"会员等级"`
	Days      int    `json:"days" binding:"omitempty,min=0,max=36500" label:"有效天数"`
	Permanent bool   `json:"permanent" label:"长期有效"`
	ExpireAt  string `json:"expireAt" binding:"omitempty,max=32" label:"到期时间"`
}

// Grant 给用户开通或续费会员，返回最新会员状态。
func (s *Service) Grant(ctx context.Context, userID string, in *GrantInput) (*model.MembershipSnapshot, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, errs.BadRequest("请选择用户")
	}
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", userID).Error; err != nil {
		return nil, errs.NotFound("用户不存在")
	}
	var lv model.MembershipLevel
	if err := s.db.WithContext(ctx).First(&lv, "id = ?", in.LevelID).Error; err != nil {
		return nil, errs.NotFound("会员等级不存在")
	}
	if !lv.Active() {
		return nil, errs.BadRequest("等级「" + lv.Name + "」已停用，不能开通")
	}

	now := time.Now()
	expire, err := resolveExpire(in, &lv, now)
	if err != nil {
		return nil, err
	}
	sameLevel := u.MembershipLevelID == lv.ID
	startAt := now
	if sameLevel && u.MembershipStartAt != nil {
		startAt = *u.MembershipStartAt
	}
	// 续费：同一等级且尚未过期时，从原到期日顺延，而不是把剩余时间抹掉。
	if sameLevel && expire != nil && u.MembershipExpireAt != nil && u.MembershipExpireAt.After(now) {
		extended := u.MembershipExpireAt.Add(expire.Sub(now))
		expire = &extended
	}

	err = s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]any{
			"membership_level_id":  lv.ID,
			"membership_start_at":  startAt,
			"membership_expire_at": expire,
		}).Error
	if err != nil {
		return nil, err
	}
	snap := model.Evaluate(&lv, lv.ID, &startAt, expire, now)
	return &snap, nil
}

func resolveExpire(in *GrantInput, lv *model.MembershipLevel, now time.Time) (*time.Time, error) {
	if strings.TrimSpace(in.ExpireAt) != "" {
		t, err := parseTime(in.ExpireAt)
		if err != nil {
			return nil, errs.BadRequest("到期时间格式不正确")
		}
		if !t.After(now) {
			return nil, errs.BadRequest("到期时间必须晚于当前时间")
		}
		return &t, nil
	}
	days := in.Days
	if days == 0 && !in.Permanent {
		days = lv.DefaultDays
	}
	if days <= 0 {
		return nil, nil // 长期有效
	}
	t := now.AddDate(0, 0, days)
	return &t, nil
}

// parseTime 兼容 RFC3339 与日期字符串；只给日期时按当天 23:59:59 截止。
func parseTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return time.Time{}, err
	}
	return t.Add(24*time.Hour - time.Second), nil
}

// Revoke 取消会员，回到非会员状态。幂等：未开通时同样返回成功。
func (s *Service) Revoke(ctx context.Context, userID string) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return errs.NotFound("用户不存在")
	}
	return s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]any{
			"membership_level_id":  "",
			"membership_start_at":  nil,
			"membership_expire_at": nil,
		}).Error
}

// OfUser 查询单个用户的会员状态。
func (s *Service) OfUser(ctx context.Context, userID string) (*model.MembershipSnapshot, error) {
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", userID).Error; err != nil {
		return nil, errs.NotFound("用户不存在")
	}
	users := []*model.User{&u}
	if err := s.Fill(ctx, users); err != nil {
		return nil, err
	}
	snap := model.Evaluate(u.Membership, u.MembershipLevelID, u.MembershipStartAt, u.MembershipExpireAt, time.Now())
	return &snap, nil
}

// Fill 批量填充用户会员信息，避免列表页 N+1 查询。
func (s *Service) Fill(ctx context.Context, users []*model.User) error {
	ids := make([]string, 0, len(users))
	seen := map[string]bool{}
	for _, u := range users {
		if u.MembershipLevelID != "" && !seen[u.MembershipLevelID] {
			seen[u.MembershipLevelID] = true
			ids = append(ids, u.MembershipLevelID)
		}
	}
	levels := map[string]*model.MembershipLevel{}
	if len(ids) > 0 {
		var list []*model.MembershipLevel
		if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&list).Error; err != nil {
			return err
		}
		for _, lv := range list {
			levels[lv.ID] = lv
		}
	}
	now := time.Now()
	for _, u := range users {
		lv := levels[u.MembershipLevelID]
		snap := model.Evaluate(lv, u.MembershipLevelID, u.MembershipStartAt, u.MembershipExpireAt, now)
		u.Membership = lv
		u.MembershipState = snap.State
	}
	return nil
}

// HasBenefit 判断用户当前是否拥有某项权益。值为空 / false / 0 / off / no 时视为没有。
func (s *Service) HasBenefit(ctx context.Context, userID, key string) (bool, error) {
	snap, err := s.OfUser(ctx, userID)
	if err != nil {
		return false, err
	}
	if !snap.Member {
		return false, nil
	}
	v, ok := snap.Benefits[key]
	if !ok {
		return false, nil
	}
	return !falsyValues[strings.ToLower(strings.TrimSpace(v))], nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
