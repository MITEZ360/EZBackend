package system

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/crypto"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/query"
	"github.com/keel-framework/keel/pkg/utils"
)

// ConfigService 系统参数业务。
type ConfigService struct {
	db    *gorm.DB
	cache cache.Cache
	cfg   *config.Config
}

// NewConfigService 创建参数服务。
func NewConfigService(db *gorm.DB, c cache.Cache, cfg *config.Config) *ConfigService {
	return &ConfigService{db: db, cache: c, cfg: cfg}
}

// List 参数分页。
func (s *ConfigService) List(ctx context.Context, p *query.Params) ([]*model.SysConfig, int64, error) {
	allowed := map[string]string{"key": "config_key", "name": "name", "group": "group_name", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.SysConfig{}), allowed)
	db = query.Search(db, p.Keyword, "config_key", "name", "remark")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.SysConfig
	err := db.Order(p.Order(map[string]string{"created_at": "created_at", "key": "config_key"}, "group_name ASC, config_key ASC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	for _, c := range list {
		if c.Encrypted && c.Value != "" {
			c.Value = "******"
		}
	}
	return list, total, nil
}

// Public 返回可公开的配置集合（登录页等使用），带缓存。
func (s *ConfigService) Public(ctx context.Context) (map[string]string, error) {
	key := "config:public"
	var cached map[string]string
	if ok, err := cache.GetJSON(ctx, s.cache, key, &cached); err == nil && ok {
		return cached, nil
	}
	var list []model.SysConfig
	if err := s.db.WithContext(ctx).Where("public = ?", true).Find(&list).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, c := range list {
		val := c.Value
		if c.Encrypted {
			val, _ = crypto.Decrypt(val, s.cfg.Security.DataEncryptKey)
		}
		out[c.Key] = val
	}
	_ = cache.SetJSON(ctx, s.cache, key, out, 5*time.Minute)
	return out, nil
}

// Get 读取配置值。
func (s *ConfigService) Get(ctx context.Context, key string) (string, error) {
	var c model.SysConfig
	if err := s.db.WithContext(ctx).First(&c, "key = ?", key).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", errs.NotFound("配置项不存在")
		}
		return "", err
	}
	if c.Encrypted {
		return crypto.Decrypt(c.Value, s.cfg.Security.DataEncryptKey)
	}
	return c.Value, nil
}

// Input 配置入参。
type Input struct {
	Key       string `json:"key" binding:"required,max=64" label:"配置键"`
	Value     string `json:"value" label:"配置值"`
	Name      string `json:"name" binding:"required,max=64" label:"配置名称"`
	Group     string `json:"group" binding:"max=32" label:"分组"`
	Encrypted bool   `json:"encrypted"`
	Public    bool   `json:"public"`
	Remark    string `json:"remark" binding:"max=255" label:"备注"`
}

// Create 新增配置。
func (s *ConfigService) Create(ctx context.Context, in *Input) (*model.SysConfig, error) {
	var n int64
	s.db.WithContext(ctx).Model(&model.SysConfig{}).Where("config_key = ?", in.Key).Count(&n)
	if n > 0 {
		return nil, errs.Conflict("配置键已存在")
	}
	val := in.Value
	if in.Encrypted && val != "" {
		enc, err := crypto.Encrypt(val, s.cfg.Security.DataEncryptKey)
		if err != nil {
			return nil, err
		}
		val = enc
	}
	group := in.Group
	if group == "" {
		group = "default"
	}
	c := &model.SysConfig{Key: in.Key, Value: val, Name: in.Name, Group: group,
		Encrypted: in.Encrypted, Public: in.Public, Remark: in.Remark}
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		return nil, err
	}
	s.invalidate(ctx, in.Public)
	c.Value = maskIf(c.Encrypted, c.Value)
	return c, nil
}

// Update 更新配置。
func (s *ConfigService) Update(ctx context.Context, id string, in *Input) error {
	var c model.SysConfig
	if err := s.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return errs.NotFound("配置项不存在")
	}
	updates := map[string]any{"name": in.Name, "group_name": in.Group, "encrypted": in.Encrypted,
		"public": in.Public, "remark": in.Remark}
	// 值为 ****** 表示前端未修改敏感项
	if in.Value != "******" {
		val := in.Value
		if in.Encrypted && val != "" {
			enc, err := crypto.Encrypt(val, s.cfg.Security.DataEncryptKey)
			if err != nil {
				return err
			}
			val = enc
		}
		updates["value"] = val
	}
	err := s.db.WithContext(ctx).Model(&model.SysConfig{}).Where("id = ?", id).Updates(updates).Error
	if err == nil {
		s.invalidate(ctx, true)
		s.invalidate(ctx, c.Public)
	}
	return err
}

// Delete 删除配置。
func (s *ConfigService) Delete(ctx context.Context, id string) error {
	var c model.SysConfig
	if err := s.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return errs.NotFound("配置项不存在")
	}
	if err := s.db.WithContext(ctx).Delete(&model.SysConfig{}, "id = ?", id).Error; err != nil {
		return err
	}
	s.invalidate(ctx, true)
	return nil
}

func (s *ConfigService) invalidate(ctx context.Context, public bool) {
	if public {
		_ = s.cache.Delete(ctx, "config:public")
	}
}

func maskIf(enc bool, v string) string {
	if enc && v != "" {
		return "******"
	}
	return v
}

// NoticeNotifier 由消息中心实现：公告发布后同步投递站内信。
// 用接口而不是直接依赖 message 包，避免公告与消息两块的构造顺序耦合。
type NoticeNotifier interface {
	PublishNotice(ctx context.Context, n *model.Notice, senderID, senderName string) error
	RevokeNotice(ctx context.Context, noticeID string) error
}

// NoticeService 公告业务。
type NoticeService struct {
	db       *gorm.DB
	notifier NoticeNotifier
	log      *zap.Logger
}

// NewNoticeService 创建公告服务。notifier 可为 nil，此时只做公告管理、不投递站内信。
func NewNoticeService(db *gorm.DB, notifier NoticeNotifier, log *zap.Logger) *NoticeService {
	if log == nil {
		log = zap.NewNop()
	}
	return &NoticeService{db: db, notifier: notifier, log: log}
}

// SetNotifier 在消息模块装配完成后注入投递能力。
// 装配顺序由依赖图决定，公告模块可能先于消息模块构造。
func (s *NoticeService) SetNotifier(n NoticeNotifier) { s.notifier = n }

// List 公告分页。
func (s *NoticeService) List(ctx context.Context, p *query.Params) ([]*model.Notice, int64, error) {
	allowed := map[string]string{"title": "title", "type": "type", "status": "status", "created_at": "created_at"}
	db := p.Filter(s.db.WithContext(ctx).Model(&model.Notice{}), allowed)
	db = query.Search(db, p.Keyword, "title", "content")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.Notice
	err := db.Order(p.Order(map[string]string{"created_at": "created_at"}, "created_at DESC")).
		Offset(p.Offset()).Limit(p.PageSize).Find(&list).Error
	return list, total, err
}

// NoticeInput 公告入参。
type NoticeInput struct {
	Title     string `json:"title" binding:"required,max=128" label:"标题"`
	Content   string `json:"content" binding:"required" label:"内容"`
	Type      string `json:"type" binding:"oneof=notice announcement warning" label:"类型"`
	Status    *int8  `json:"status" binding:"omitempty,oneof=0 1" label:"状态"`
	Publisher string `json:"publisherId"`
	// PublisherName 由处理器填充，用于站内信的发送人展示。
	PublisherName string `json:"-"`
}

// Create 发布公告。
func (s *NoticeService) Create(ctx context.Context, in *NoticeInput) (*model.Notice, error) {
	now := time.Now()
	n := &model.Notice{Title: in.Title, Content: in.Content, Type: in.Type,
		Status:      utils.Int8Or(in.Status, model.StatusEnabled),
		PublisherID: in.Publisher, PublishAt: &now}
	if n.Type == "" {
		n.Type = "notice"
	}
	if err := s.db.WithContext(ctx).Select("*").Create(n).Error; err != nil {
		return nil, err
	}
	if n.Status == model.StatusEnabled {
		s.publishNotice(ctx, n, in.Publisher, in.PublisherName)
	}
	return n, nil
}

// publishNotice 投递失败只记日志：公告本身已经保存成功，不应因为
// 站内信写入异常而让运营重复提交。
func (s *NoticeService) publishNotice(ctx context.Context, n *model.Notice, senderID, senderName string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.PublishNotice(ctx, n, senderID, senderName); err != nil {
		s.log.Warn("公告站内信投递失败", zap.String("noticeId", n.ID), zap.Error(err))
	}
}

// Update 更新公告，并把状态变化同步到站内信。
func (s *NoticeService) Update(ctx context.Context, id string, in *NoticeInput) error {
	var old model.Notice
	if err := s.db.WithContext(ctx).First(&old, "id = ?", id).Error; err != nil {
		return errs.NotFound("公告不存在")
	}
	status := utils.Int8Or(in.Status, model.StatusEnabled)
	res := s.db.WithContext(ctx).Model(&model.Notice{}).Where("id = ?", id).Updates(map[string]any{
		"title": in.Title, "content": in.Content, "type": in.Type, "status": status,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("公告不存在")
	}

	// 草稿转发布、或已发布内容变更时同步；转为草稿则撤回。
	if status == model.StatusEnabled {
		n := old
		n.Title, n.Content, n.Type, n.Status = in.Title, in.Content, in.Type, status
		s.publishNotice(ctx, &n, in.Publisher, in.PublisherName)
	} else if s.notifier != nil {
		if err := s.notifier.RevokeNotice(ctx, id); err != nil {
			s.log.Warn("公告站内信撤回失败", zap.String("noticeId", id), zap.Error(err))
		}
	}
	return nil
}

// Delete 删除公告。
func (s *NoticeService) Delete(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Delete(&model.Notice{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("公告不存在")
	}
	if s.notifier != nil {
		if err := s.notifier.RevokeNotice(ctx, id); err != nil {
			s.log.Warn("公告站内信撤回失败", zap.String("noticeId", id), zap.Error(err))
		}
	}
	return nil
}

// Get 公告详情。
func (s *NoticeService) Get(ctx context.Context, id string) (*model.Notice, error) {
	var n model.Notice
	if err := s.db.WithContext(ctx).First(&n, "id = ?", id).Error; err != nil {
		return nil, errs.NotFound("公告不存在")
	}
	return &n, nil
}

// Latest 返回最新公告（首页展示）。
func (s *NoticeService) Latest(ctx context.Context, limit int) ([]*model.Notice, error) {
	if limit <= 0 {
		limit = 5
	}
	var list []*model.Notice
	err := s.db.WithContext(ctx).Where("status = ?", model.StatusEnabled).
		Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}
