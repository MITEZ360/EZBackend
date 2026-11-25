// Package cnauth 提供中国大陆环境特有的认证入口：手机号注册与短信验证码登录。
// 会话签发复用 auth.Service.SignIn，避免令牌与登录日志逻辑分叉。
package cnauth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/modules/auth"
	"github.com/MITEZ360/tenon/internal/platform/authn"
	"github.com/MITEZ360/tenon/internal/platform/authz"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/crypto"
	"github.com/MITEZ360/tenon/internal/platform/idgen"
	"github.com/MITEZ360/tenon/internal/platform/sms"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/i18n"
)

var (
	rePhone    = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
	reUsername = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{2,31}$`)
)

// Service 自助注册与短信登录业务。
type Service struct {
	db      *gorm.DB
	cfg     *config.Config
	authSvc *auth.Service
	authz   *authz.Loader
	sender  sms.Sender
	log     *zap.Logger
}

// NewService 构造服务。sender 可为 nil（未配置短信网关）。
func NewService(db *gorm.DB, cfg *config.Config, authSvc *auth.Service, authzLoader *authz.Loader, sender sms.Sender, log *zap.Logger) *Service {
	return &Service{db: db, cfg: cfg, authSvc: authSvc, authz: authzLoader, sender: sender, log: log}
}

// RegisterInput 用户自助注册入参。
type RegisterInput struct {
	Username string `json:"username" binding:"required,max=32" label:"用户名"`
	Password string `json:"password" binding:"required,max=64" label:"密码"`
	Nickname string `json:"nickname" binding:"omitempty,max=64" label:"昵称"`
	Phone    string `json:"phone" binding:"required,mobile" label:"手机号"`
	Code     string `json:"code" binding:"required,len=6" label:"短信验证码"`
	Email    string `json:"email" binding:"omitempty,email,max=128" label:"邮箱"`
}

// SMSCodeInput 发送验证码入参。
type SMSCodeInput struct {
	Phone string `json:"phone" binding:"required,mobile" label:"手机号"`
	Scene string `json:"scene" binding:"required,oneof=register login reset" label:"场景"`
}

// PhoneLoginInput 短信验证码登录入参。
type PhoneLoginInput struct {
	Phone string `json:"phone" binding:"required,mobile" label:"手机号"`
	Code  string `json:"code" binding:"required,len=6" label:"短信验证码"`
}

// PublicConfig 返回前端需要的功能开关。
func (s *Service) PublicConfig() map[string]any {
	return map[string]any{
		"registerEnabled": s.cfg.CN.RegisterEnabled,
		"smsLoginEnabled": s.cfg.CN.SMSLoginEnabled,
	}
}

// SendCode 下发短信验证码。验证码只以哈希入库，且同一手机号有发送频率限制。
//
// 开关按场景判断：注册验证码只看「开放自助注册」，登录/找回密码才看
// 「开放短信登录」。否则只开注册、不开短信登录时，注册流程会在发码这步
// 被误拦。
func (s *Service) SendCode(ctx context.Context, in *SMSCodeInput, ip string) error {
	if !rePhone.MatchString(in.Phone) {
		return errs.Validation(i18n.Tf(ctx, "手机号格式不正确"))
	}
	switch in.Scene {
	case model.SMSCodeRegister:
		if !s.cfg.CN.RegisterEnabled {
			return errs.Forbidden(i18n.Tf(ctx, "注册未开放"))
		}
	case model.SMSCodeLogin, model.SMSCodeReset:
		if !s.cfg.CN.SMSLoginEnabled {
			return errs.Forbidden(i18n.Tf(ctx, "短信登录未开放"))
		}
	default:
		return errs.Validation(i18n.Tf(ctx, "验证码场景不合法"))
	}
	if s.sender == nil {
		return errs.Internal(i18n.Tf(ctx, "短信网关未配置"))
	}

	now := time.Now()
	var recent int64
	if err := s.db.WithContext(ctx).Model(&model.SMSCode{}).
		Where("phone = ? AND scene = ? AND created_at > ?", in.Phone, in.Scene, now.Add(-60*time.Second)).
		Count(&recent).Error; err != nil {
		return err
	}
	if recent > 0 {
		return errs.TooMany(i18n.Tf(ctx, "发送过于频繁，请 60 秒后再试"))
	}
	var today int64
	if err := s.db.WithContext(ctx).Model(&model.SMSCode{}).
		Where("phone = ? AND created_at > ?", in.Phone, now.Add(-24*time.Hour)).
		Count(&today).Error; err != nil {
		return err
	}
	if today >= int64(s.cfg.CN.DailySendLimit) {
		return errs.TooMany(i18n.Tf(ctx, "今日发送次数已达上限"))
	}

	code, err := randomDigits(s.cfg.CN.CodeLength)
	if err != nil {
		return err
	}
	record := &model.SMSCode{
		Phone: in.Phone, CodeHash: codeHash(in.Phone, in.Scene, code), Scene: in.Scene,
		ExpiresAt: now.Add(s.cfg.CN.CodeTTL), IP: ip,
	}
	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	if err := s.sender.Send(ctx, in.Phone, code); err != nil {
		// 发送失败时作废记录，避免用户拿到一个实际没下发的验证码。
		_ = s.db.WithContext(ctx).Delete(record).Error
		s.log.Warn("短信下发失败", zap.String("phone", in.Phone), zap.Error(err))
		return errs.Internal(i18n.Tf(ctx, "短信发送失败，请稍后重试"))
	}
	return nil
}

// Register 创建用户并直接返回登录态。
func (s *Service) Register(ctx context.Context, in *RegisterInput, meta auth.ClientMeta) (*auth.TokenPair, *auth.UserInfo, error) {
	if !s.cfg.CN.RegisterEnabled {
		return nil, nil, errs.Forbidden(i18n.Tf(ctx, "注册未开放"))
	}
	in.Username = strings.TrimSpace(in.Username)
	in.Nickname = strings.TrimSpace(in.Nickname)
	in.Phone = strings.TrimSpace(in.Phone)
	if !reUsername.MatchString(in.Username) {
		return nil, nil, errs.Validation(i18n.Tf(ctx, "用户名需以字母开头，仅含字母、数字、下划线或连字符，长度 3-32"))
	}
	if !rePhone.MatchString(in.Phone) {
		return nil, nil, errs.Validation(i18n.Tf(ctx, "手机号格式不正确"))
	}
	if err := s.authSvc.ValidatePassword(ctx, in.Password); err != nil {
		return nil, nil, err
	}
	// 先做静态查重再消费验证码，避免用户输入了重名账号却白白浪费一条短信。
	if err := s.ensureAvailable(ctx, in.Username, in.Phone); err != nil {
		return nil, nil, err
	}
	if s.cfg.CN.SMSLoginEnabled {
		if err := s.verifyCode(ctx, in.Phone, model.SMSCodeRegister, in.Code, true); err != nil {
			return nil, nil, err
		}
	}
	if in.Nickname == "" {
		in.Nickname = in.Username
	}
	hash, err := authn.HashPassword(in.Password)
	if err != nil {
		return nil, nil, err
	}
	u, err := s.createUser(ctx, in.Username, hash, in.Nickname, in.Phone, in.Email)
	if err != nil {
		return nil, nil, err
	}
	return s.authSvc.SignIn(ctx, u, meta)
}

// LoginByPhone 短信验证码登录；手机号未注册且开放注册时自动创建账号。
func (s *Service) LoginByPhone(ctx context.Context, in *PhoneLoginInput, meta auth.ClientMeta) (*auth.TokenPair, *auth.UserInfo, error) {
	if !s.cfg.CN.SMSLoginEnabled {
		return nil, nil, errs.Forbidden(i18n.Tf(ctx, "短信登录未开放"))
	}
	if !rePhone.MatchString(in.Phone) {
		return nil, nil, errs.Validation(i18n.Tf(ctx, "手机号格式不正确"))
	}
	if err := s.verifyCode(ctx, in.Phone, model.SMSCodeLogin, in.Code, true); err != nil {
		return nil, nil, err
	}
	var u model.User
	err := s.db.WithContext(ctx).Where("phone = ?", in.Phone).First(&u).Error
	if err == gorm.ErrRecordNotFound {
		if !s.cfg.CN.RegisterEnabled {
			return nil, nil, errs.NotFound(i18n.Tf(ctx, "账号不存在，请先注册"))
		}
		hash, e := authn.HashPassword(idgen.Token(32))
		if e != nil {
			return nil, nil, e
		}
		var created *model.User
		created, err = s.createUser(ctx, "u"+in.Phone, hash, "用户"+in.Phone[len(in.Phone)-4:], in.Phone, "")
		if err == nil {
			u = *created
		}
	}
	if err != nil {
		return nil, nil, err
	}
	return s.authSvc.SignIn(ctx, &u, meta)
}

// ResetPassword 短信验证码重置密码（供未登录用户找回账号）。
func (s *Service) ResetPassword(ctx context.Context, phone, code, newPassword string) error {
	if !s.cfg.CN.SMSLoginEnabled {
		return errs.Forbidden(i18n.Tf(ctx, "短信登录未开放"))
	}
	if err := s.verifyCode(ctx, phone, model.SMSCodeReset, code, true); err != nil {
		return err
	}
	if err := s.authSvc.ValidatePassword(ctx, newPassword); err != nil {
		return err
	}
	hash, err := authn.HashPassword(newPassword)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&model.User{}).Where("phone = ?", phone).Update("password", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound(i18n.Tf(ctx, "账号不存在"))
	}
	return nil
}

// ensureAvailable 检查用户名与手机号是否已被占用。
func (s *Service) ensureAvailable(ctx context.Context, username, phone string) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", username).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errs.Conflict(i18n.Tf(ctx, "用户名已存在"))
	}
	if phone != "" {
		if err := s.db.WithContext(ctx).Model(&model.User{}).Where("phone = ?", phone).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return errs.Conflict(i18n.Tf(ctx, "手机号已被使用"))
		}
	}
	return nil
}

func (s *Service) createUser(ctx context.Context, username, hash, nickname, phone, email string) (*model.User, error) {
	if err := s.ensureAvailable(ctx, username, phone); err != nil {
		return nil, err
	}
	u := &model.User{
		Username: username, Password: hash, Nickname: nickname,
		Phone: phone, Email: email, Status: model.StatusEnabled,
		Locale: "zh-CN", Timezone: "Asia/Shanghai",
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("*").Create(u).Error; err != nil {
			return err
		}
		var roles []model.Role
		if len(s.cfg.CN.DefaultRoles) > 0 {
			if err := tx.Where("code IN ?", s.cfg.CN.DefaultRoles).Find(&roles).Error; err != nil {
				return err
			}
		}
		for _, r := range roles {
			if err := tx.Create(&model.UserRole{UserID: u.ID, RoleID: r.ID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.authz.Invalidate(ctx)
	return u, nil
}

// verifyCode 校验验证码；consume 为 true 时一次性消费。
func (s *Service) verifyCode(ctx context.Context, phone, scene, code string, consume bool) error {
	if s.sender == nil {
		return errs.Internal(i18n.Tf(ctx, "短信网关未配置"))
	}
	var rec model.SMSCode
	err := s.db.WithContext(ctx).Where("phone = ? AND scene = ? AND used_at IS NULL", phone, scene).
		Order("created_at DESC").First(&rec).Error
	if err != nil || rec.CodeHash != codeHash(phone, scene, code) || rec.ExpiresAt.Before(time.Now()) || rec.FailCount >= 5 {
		return errs.BadRequest(i18n.Tf(ctx, "验证码错误或已过期"))
	}
	if !consume {
		return nil
	}
	now := time.Now()
	return s.db.WithContext(ctx).Model(&model.SMSCode{}).
		Where("id = ? AND used_at IS NULL", rec.ID).
		Update("used_at", now).Error
}

func codeHash(phone, scene, code string) string {
	return crypto.SHA256(fmt.Sprintf("%s:%s:%s", phone, scene, code))
}

func randomDigits(n int) (string, error) {
	out := make([]byte, n)
	for i := range out {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + v.Int64())
	}
	return string(out), nil
}
