// Package auth 认证与会话管理：登录、刷新令牌、登出、个人资料、动态口令、在线会话。
package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/modules/menu"
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/authz"
	"github.com/keel-framework/keel/internal/platform/cache"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/crypto"
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/utils"
)

// Service 认证业务逻辑。
type Service struct {
	db      *gorm.DB
	cache   cache.Cache
	jwt     *authn.Manager
	captcha *authn.Captcha
	cfg     *config.Config
	log     *zap.Logger
	authz   *authz.Loader
	menuSvc *menu.Service
}

// NewService 创建认证服务。
func NewService(db *gorm.DB, c cache.Cache, jwtMgr *authn.Manager, captcha *authn.Captcha,
	cfg *config.Config, log *zap.Logger, al *authz.Loader, ms *menu.Service) *Service {
	return &Service{db: db, cache: c, jwt: jwtMgr, captcha: captcha, cfg: cfg, log: log, authz: al, menuSvc: ms}
}

// ClientMeta 客户端环境信息。
type ClientMeta struct {
	IP      string
	UA      string
	Browser string
	OS      string
}

// LoginInput 登录入参。
type LoginInput struct {
	Username    string `json:"username" binding:"required,min=2,max=32" label:"用户名"`
	Password    string `json:"password" binding:"required,min=6,max=64" label:"密码"`
	CaptchaID   string `json:"captchaId"`
	CaptchaCode string `json:"captchaCode"`
	TOTPCode    string `json:"totpCode"`
	// RecoveryCode 恢复码，与 TOTPCode 二选一；手机丢失时使用。
	RecoveryCode string `json:"recoveryCode"`
}

// 两步验证相关业务码，前端据此区分登录步骤而不是靠文案匹配。
const (
	CodeTOTPRequired    = 40104 // 该账号已开启两步验证，需要补充动态码
	CodeTOTPInvalid     = 40105 // 动态码错误
	CodeRecoveryInvalid = 40106 // 恢复码错误或已使用
)

// UserInfo 登录用户信息。
type UserInfo struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	RealName    string `json:"realName,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
	DeptID      string `json:"deptId,omitempty"`
	DeptName    string `json:"deptName,omitempty"`
	IsSuper     bool   `json:"isSuper"`
	TOTPEnabled bool   `json:"totpEnabled"`
	// RecoveryCodesLeft 剩余可用恢复码数量，仅在个人中心返回
	RecoveryCodesLeft int      `json:"recoveryCodesLeft,omitempty"`
	Roles             []string `json:"roles"`
	Perms             []string `json:"perms"`
	LastLoginAt       string   `json:"lastLoginAt,omitempty"`
	// SessionID 当前请求所属会话，前端据此标记"当前会话"
	SessionID string `json:"sessionId,omitempty"`
}

// TokenPair 令牌对。
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int64  `json:"expiresIn"`
}

// Profile 个人中心数据。
type Profile struct {
	User  *UserInfo     `json:"user"`
	Menus []*model.Menu `json:"menus"`
}

// Login 校验凭据并签发令牌。
func (s *Service) Login(ctx context.Context, in *LoginInput, meta ClientMeta) (*TokenPair, *UserInfo, error) {
	// 先只校验不消费：开启两步验证时，密码通过后还要用同一个验证码提交动态码。
	// 若在这里就作废，第二次提交必然报"验证码已过期"。
	if s.cfg.Security.CaptchaEnabled && !s.captcha.Peek(in.CaptchaID, in.CaptchaCode) {
		return nil, nil, errs.BadRequest("验证码错误或已过期")
	}
	keepCaptcha := false
	if s.cfg.Security.CaptchaEnabled {
		defer func() {
			if !keepCaptcha {
				s.captcha.Drop(in.CaptchaID)
			}
		}()
	}
	if locked, ttl := s.isLocked(ctx, in.Username); locked {
		return nil, nil, errs.New(http.StatusLocked, errs.CodeAccountLocked,
			fmt.Sprintf("账号已锁定，请 %d 分钟后再试", int(ttl.Minutes())+1))
	}

	var user model.User
	err := s.db.WithContext(ctx).Where("username = ?", in.Username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.onLoginFail(ctx, "", in.Username, meta, "用户名或密码错误")
			return nil, nil, errs.Unauthorized("用户名或密码错误")
		}
		return nil, nil, err
	}
	if !authn.CheckPassword(user.Password, in.Password) {
		s.onLoginFail(ctx, user.ID, in.Username, meta, "用户名或密码错误")
		return nil, nil, errs.Unauthorized("用户名或密码错误")
	}
	if user.Status != model.StatusEnabled {
		s.onLoginFail(ctx, user.ID, in.Username, meta, "账号已被禁用")
		return nil, nil, errs.Forbidden("账号已被禁用，请联系管理员")
	}
	if user.TOTPEnabled {
		if err := s.verifySecondFactor(ctx, &user, in); err != nil {
			var e *errs.Error
			if errors.As(err, &e) && e.Code == CodeTOTPRequired {
				keepCaptcha = true // 留给第二次提交
			} else if errors.As(err, &e) {
				s.onLoginFail(ctx, user.ID, in.Username, meta, e.Message)
			}
			return nil, nil, err
		}
	}

	s.clearFail(ctx, in.Username)
	if s.cfg.Security.SessionSingle {
		s.revokeAllSessions(ctx, user.ID)
	}
	pair, err := s.issueSession(ctx, &user, meta)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_login_at": now,
		"last_login_ip": meta.IP,
		"login_count":   gorm.Expr("login_count + 1"),
	})
	s.writeLoginLog(ctx, &user, meta, 1, "登录成功")

	info, err := s.buildUserInfo(ctx, &user)
	if err != nil {
		return nil, nil, err
	}
	return pair, info, nil
}

func (s *Service) issueSession(ctx context.Context, user *model.User, meta ClientMeta) (*TokenPair, error) {
	sid := idgen.Next()
	refresh := idgen.Token(32)
	expires := time.Now().Add(s.jwt.RefreshTTL())
	sess := &model.Session{
		ID: sid, UserID: user.ID, RefreshHash: crypto.SHA256(refresh),
		UserAgent: utils.Truncate(meta.UA, 240), IP: meta.IP,
		Browser: meta.Browser, OS: meta.OS,
		ExpiresAt: expires, LastSeenAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(sess).Error; err != nil {
		return nil, err
	}
	_ = s.cache.Set(ctx, "sess:"+sid, []byte(user.ID), s.jwt.RefreshTTL())

	_, _, rp, _ := s.loadRolesAndPerms(ctx, user)
	access, exp, err := s.jwt.Issue(authn.Claims{
		UserID: user.ID, Username: user.Username, Nickname: user.Nickname,
		DeptID: user.DeptID, Super: user.IsSuper, SessionID: sid, Roles: rp.roles,
	})
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken: access, RefreshToken: refresh, TokenType: "Bearer",
		ExpiresIn: int64(time.Until(exp).Seconds()),
	}, nil
}

type rolePerm struct {
	roles []string
	perms []string
	super bool
}

// loadRolesAndPerms 查询用户角色码与权限点。
func (s *Service) loadRolesAndPerms(ctx context.Context, user *model.User) ([]model.Role, []string, rolePerm, error) {
	var roles []model.Role
	err := s.db.WithContext(ctx).
		Table("sys_role AS r").
		Select("r.*").
		Joins("JOIN sys_user_role AS ur ON ur.role_id = r.id").
		Where("ur.user_id = ? AND r.deleted_at IS NULL", user.ID).
		Find(&roles).Error
	if err != nil {
		return nil, nil, rolePerm{}, err
	}
	rp := rolePerm{super: user.IsSuper}
	ids := make([]string, 0, len(roles))
	for _, r := range roles {
		rp.roles = append(rp.roles, r.Code)
		ids = append(ids, r.ID)
	}
	if !user.IsSuper && len(ids) > 0 {
		var perms []string
		_ = s.db.WithContext(ctx).
			Table("sys_menu AS m").
			Distinct("m.perm").
			Joins("JOIN sys_role_menu AS rm ON rm.menu_id = m.id").
			Where("rm.role_id IN ? AND m.perm <> '' AND m.status = 1 AND m.deleted_at IS NULL", ids).
			Pluck("m.perm", &perms).Error
		rp.perms = perms
	}
	return roles, ids, rp, nil
}

func (s *Service) buildUserInfo(ctx context.Context, user *model.User) (*UserInfo, error) {
	roles, _, rp, err := s.loadRolesAndPerms(ctx, user)
	if err != nil {
		return nil, err
	}
	info := &UserInfo{
		ID: user.ID, Username: user.Username, Nickname: user.Nickname, RealName: user.RealName,
		Avatar: user.Avatar, Email: user.Email, Phone: user.Phone, DeptID: user.DeptID,
		IsSuper: user.IsSuper, TOTPEnabled: user.TOTPEnabled,
		Roles: rp.roles,
	}
	if user.LastLoginAt != nil {
		info.LastLoginAt = user.LastLoginAt.Format(time.RFC3339)
	}
	if user.IsSuper {
		info.Perms = []string{"*"}
	} else {
		info.Perms = rp.perms
	}
	if user.DeptID != "" {
		var dept model.Dept
		if err := s.db.WithContext(ctx).Select("name").First(&dept, "id = ?", user.DeptID).Error; err == nil {
			info.DeptName = dept.Name
		}
	}
	_ = roles
	return info, nil
}

// Refresh 使用刷新令牌换取新的访问令牌（同时轮换刷新令牌）。
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if refreshToken == "" {
		return nil, errs.Unauthorized("刷新令牌不能为空")
	}
	hash := crypto.SHA256(refreshToken)
	var sess model.Session
	if err := s.db.WithContext(ctx).Where("refresh_hash = ? AND revoked = ?", hash, false).First(&sess).Error; err != nil {
		return nil, errs.New(http.StatusUnauthorized, errs.CodeTokenInvalid, "刷新令牌无效")
	}
	if sess.ExpiresAt.Before(time.Now()) {
		return nil, errs.New(http.StatusUnauthorized, errs.CodeTokenExpired, "登录已过期，请重新登录")
	}
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", sess.UserID).Error; err != nil {
		return nil, errs.New(http.StatusUnauthorized, errs.CodeTokenInvalid, "账号不存在")
	}
	if user.Status != model.StatusEnabled {
		return nil, errs.Forbidden("账号已被禁用")
	}

	newRefresh := idgen.Token(32)
	if err := s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sess.ID).
		Updates(map[string]any{"refresh_hash": crypto.SHA256(newRefresh), "last_seen_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	_ = s.cache.Set(ctx, "sess:"+sess.ID, []byte(user.ID), s.jwt.RefreshTTL())

	_, _, rp, _ := s.loadRolesAndPerms(ctx, &user)
	access, exp, err := s.jwt.Issue(authn.Claims{
		UserID: user.ID, Username: user.Username, Nickname: user.Nickname,
		DeptID: user.DeptID, Super: user.IsSuper, SessionID: sess.ID, Roles: rp.roles,
	})
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: newRefresh, TokenType: "Bearer",
		ExpiresIn: int64(time.Until(exp).Seconds())}, nil
}

// Logout 登出并吊销当前会话。
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	s.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", sessionID).Update("revoked", true)
	_ = s.cache.Delete(ctx, "sess:"+sessionID)
	return nil
}

// ValidateSession 校验会话有效性（缓存优先，失效后回源数据库）。
func (s *Service) ValidateSession(ctx context.Context, sessionID, userID string) (bool, error) {
	if sessionID == "" {
		return true, nil
	}
	if b, err := s.cache.Get(ctx, "sess:"+sessionID); err == nil {
		return string(b) == userID, nil
	}
	var sess model.Session
	err := s.db.WithContext(ctx).Select("id", "user_id", "revoked", "expires_at").
		First(&sess, "id = ?", sessionID).Error
	if err != nil {
		return false, nil
	}
	if sess.Revoked || sess.ExpiresAt.Before(time.Now()) || sess.UserID != userID {
		return false, nil
	}
	ttl := time.Until(sess.ExpiresAt)
	if ttl > 0 {
		_ = s.cache.Set(ctx, "sess:"+sessionID, []byte(userID), ttl)
	}
	return true, nil
}

// Profile 返回个人资料、菜单与权限。
func (s *Service) Profile(ctx context.Context, identity *authn.Identity) (*Profile, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", identity.UserID).Error; err != nil {
		return nil, errs.NotFound("用户不存在")
	}
	info, err := s.buildUserInfo(ctx, &user)
	if err != nil {
		return nil, err
	}
	menus, perms, err := s.menuSvc.ForUser(ctx, user.ID, user.IsSuper)
	if err != nil {
		return nil, err
	}
	info.Perms = perms
	if user.IsSuper {
		info.Perms = []string{"*"}
	}
	info.SessionID = identity.SessionID
	if user.TOTPEnabled {
		info.RecoveryCodesLeft = s.countRecoveryCodes(ctx, user.ID)
	}
	return &Profile{User: info, Menus: menus}, nil
}

// UpdateProfileInput 个人资料入参。
type UpdateProfileInput struct {
	Nickname string `json:"nickname" binding:"required,max=64" label:"昵称"`
	RealName string `json:"realName" binding:"max=64" label:"姓名"`
	Email    string `json:"email" binding:"omitempty,email,max=128" label:"邮箱"`
	Phone    string `json:"phone" binding:"omitempty,mobile" label:"手机号"`
	Avatar   string `json:"avatar" binding:"max=255" label:"头像"`
	Gender   int8   `json:"gender" binding:"oneof=0 1 2" label:"性别"`
}

// UpdateProfile 更新个人资料。
func (s *Service) UpdateProfile(ctx context.Context, userID string, in *UpdateProfileInput) error {
	return s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"nickname": in.Nickname, "real_name": in.RealName, "email": in.Email,
		"phone": in.Phone, "avatar": in.Avatar, "gender": in.Gender,
	}).Error
}

// ChangePasswordInput 修改密码入参。
type ChangePasswordInput struct {
	OldPassword string `json:"oldPassword" binding:"required" label:"原密码"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=64" label:"新密码"`
}

// ChangePassword 修改密码并吊销其它会话。
func (s *Service) ChangePassword(ctx context.Context, userID, sessionID string, in *ChangePasswordInput) error {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return errs.NotFound("用户不存在")
	}
	if !authn.CheckPassword(user.Password, in.OldPassword) {
		return errs.BadRequest("原密码不正确")
	}
	if in.OldPassword == in.NewPassword {
		return errs.BadRequest("新密码不能与原密码相同")
	}
	if err := s.checkPasswordPolicy(in.NewPassword); err != nil {
		return err
	}
	hash, err := authn.HashPassword(in.NewPassword)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Update("password", hash).Error; err != nil {
		return err
	}
	// 吊销除当前会话外的其它会话
	s.db.WithContext(ctx).Model(&model.Session{}).
		Where("user_id = ? AND id <> ? AND revoked = ?", userID, sessionID, false).
		Update("revoked", true)
	return nil
}

func (s *Service) checkPasswordPolicy(pw string) error {
	minLen := s.cfg.Security.PasswordMinLen
	if minLen < 8 {
		minLen = 8
	}
	level := s.cfg.Security.PasswordStrength
	var upper, lower, digit, special bool
	for _, r := range pw {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			special = true
		}
	}
	if len([]rune(pw)) < minLen {
		return errs.BadRequest(fmt.Sprintf("密码长度至少 %d 位", minLen))
	}
	kinds := 0
	for _, b := range []bool{upper, lower, digit, special} {
		if b {
			kinds++
		}
	}
	if level >= 2 && (!digit || !(upper || lower)) {
		return errs.BadRequest("密码需同时包含字母与数字")
	}
	if level >= 3 && kinds < 4 {
		return errs.BadRequest("密码需包含大写字母、小写字母、数字与特殊符号")
	}
	return nil
}

// TOTPSetupOutput 动态口令初始化结果。
// QRCode 是 PNG 的 data URI，前端直接渲染，不依赖外网二维码服务。
type TOTPSetupOutput struct {
	Secret string `json:"secret"`
	URL    string `json:"url"`
	QRCode string `json:"qrCode"`
}

// TOTPEnableOutput 启用结果。恢复码只在启用时明文返回一次，服务端仅存哈希。
type TOTPEnableOutput struct {
	RecoveryCodes []string `json:"recoveryCodes"`
}

// SetupTOTP 生成动态口令密钥（尚未启用）。
func (s *Service) SetupTOTP(ctx context.Context, userID string) (*TOTPSetupOutput, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return nil, errs.NotFound("用户不存在")
	}
	// 已开启时不允许重新生成：否则任何已登录会话都能悄悄换绑认证器，
	// 二次验证就失去了"必须持有原设备"的意义。
	if user.TOTPEnabled {
		return nil, errs.Conflict("两步验证已开启，请先关闭后再重新绑定")
	}
	secret, url, err := authn.GenerateTOTP(s.cfg.App.Name, user.Username)
	if err != nil {
		return nil, errs.Internal("动态口令生成失败")
	}
	qr, err := authn.QRCodeDataURI(url)
	if err != nil {
		return nil, errs.Internal("二维码生成失败")
	}
	enc, err := crypto.Encrypt(secret, s.cfg.Security.DataEncryptKey)
	if err != nil {
		return nil, err
	}
	// 只写密钥、不置 enabled：必须再提交一次动态码，确认认证器已正确添加。
	if err := s.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]any{"totp_secret": enc, "totp_enabled": false}).Error; err != nil {
		return nil, err
	}
	return &TOTPSetupOutput{Secret: secret, URL: url, QRCode: qr}, nil
}

// EnableTOTP 校验并启用两步验证，同时下发一次性恢复码。
func (s *Service) EnableTOTP(ctx context.Context, userID, code string) (*TOTPEnableOutput, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return nil, errs.NotFound("用户不存在")
	}
	if user.TOTPEnabled {
		return nil, errs.Conflict("两步验证已开启")
	}
	if user.TOTPSecret == "" {
		return nil, errs.BadRequest("请先获取密钥")
	}
	secret, err := crypto.Decrypt(user.TOTPSecret, s.cfg.Security.DataEncryptKey)
	if err != nil {
		return nil, errs.Internal("密钥解析失败")
	}
	if !authn.ValidateTOTP(secret, code) {
		return nil, errs.BadRequest("动态验证码错误，请确认认证器中的验证码与手机时间")
	}

	codes, err := authn.GenerateRecoveryCodes(authn.RecoveryCodeCount)
	if err != nil {
		return nil, errs.Internal("恢复码生成失败")
	}
	now := time.Now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			Update("totp_enabled", true).Error; err != nil {
			return err
		}
		// 重新绑定会覆盖旧密钥，旧恢复码必须一并作废
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserRecoveryCode{}).Error; err != nil {
			return err
		}
		rows := make([]model.UserRecoveryCode, 0, len(codes))
		for _, c := range codes {
			rows = append(rows, model.UserRecoveryCode{
				ID: idgen.Next(), UserID: userID,
				CodeHash: authn.HashRecoveryCode(c, s.recoveryKey()), CreatedAt: now,
			})
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return &TOTPEnableOutput{RecoveryCodes: codes}, nil
}

// DisableTOTP 关闭两步验证。code 既可以是 6 位动态码，也可以是恢复码。
func (s *Service) DisableTOTP(ctx context.Context, userID, code string) error {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return errs.NotFound("用户不存在")
	}
	if !user.TOTPEnabled {
		return nil
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errs.BadRequest("请输入动态验证码或恢复码")
	}
	if isRecoveryCode(code) {
		if err := s.consumeRecoveryCode(ctx, &user, code); err != nil {
			return err
		}
	} else {
		secret, err := crypto.Decrypt(user.TOTPSecret, s.cfg.Security.DataEncryptKey)
		if err != nil || !authn.ValidateTOTP(secret, code) {
			return errs.BadRequest("动态验证码错误")
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", userID).
			Updates(map[string]any{"totp_enabled": false, "totp_secret": ""}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&model.UserRecoveryCode{}).Error
	})
}

// RegenerateRecoveryCodes 重新生成恢复码，旧码立即失效。
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID, code string) (*TOTPEnableOutput, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error; err != nil {
		return nil, errs.NotFound("用户不存在")
	}
	if !user.TOTPEnabled {
		return nil, errs.BadRequest("请先开启两步验证")
	}
	secret, err := crypto.Decrypt(user.TOTPSecret, s.cfg.Security.DataEncryptKey)
	if err != nil {
		return nil, errs.Internal("密钥解析失败")
	}
	if !authn.ValidateTOTP(secret, code) {
		return nil, errs.BadRequest("动态验证码错误")
	}
	codes, err := authn.GenerateRecoveryCodes(authn.RecoveryCodeCount)
	if err != nil {
		return nil, errs.Internal("恢复码生成失败")
	}
	now := time.Now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserRecoveryCode{}).Error; err != nil {
			return err
		}
		rows := make([]model.UserRecoveryCode, 0, len(codes))
		for _, c := range codes {
			rows = append(rows, model.UserRecoveryCode{
				ID: idgen.Next(), UserID: userID,
				CodeHash: authn.HashRecoveryCode(c, s.recoveryKey()), CreatedAt: now,
			})
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return &TOTPEnableOutput{RecoveryCodes: codes}, nil
}

// verifySecondFactor 校验登录的第二因子：优先恢复码，其次动态码。
func (s *Service) verifySecondFactor(ctx context.Context, user *model.User, in *LoginInput) error {
	code := strings.TrimSpace(in.TOTPCode)
	recovery := strings.TrimSpace(in.RecoveryCode)
	switch {
	case code == "" && recovery == "":
		return errs.New(http.StatusUnauthorized, CodeTOTPRequired, "该账号已开启两步验证，请输入动态验证码")
	case recovery != "":
		return s.consumeRecoveryCode(ctx, user, recovery)
	default:
		secret, err := crypto.Decrypt(user.TOTPSecret, s.cfg.Security.DataEncryptKey)
		if err != nil || !authn.ValidateTOTP(secret, code) {
			return errs.New(http.StatusUnauthorized, CodeTOTPInvalid, "动态验证码错误")
		}
		return nil
	}
}

// consumeRecoveryCode 校验并消费一个恢复码。条件更新保证并发下同一码只能用一次。
func (s *Service) consumeRecoveryCode(ctx context.Context, user *model.User, code string) error {
	if len(authn.NormalizeRecoveryCode(code)) != authn.RecoveryCodeLength {
		return errs.New(http.StatusUnauthorized, CodeRecoveryInvalid, "恢复码格式不正确")
	}
	hash := authn.HashRecoveryCode(code, s.recoveryKey())
	var rows []model.UserRecoveryCode
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND used_at IS NULL", user.ID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if subtle.ConstantTimeCompare([]byte(row.CodeHash), []byte(hash)) != 1 {
			continue
		}
		res := s.db.WithContext(ctx).Model(&model.UserRecoveryCode{}).
			Where("id = ? AND used_at IS NULL", row.ID).Update("used_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			break // 并发下已被其它请求消费
		}
		s.log.Warn("使用恢复码完成二次验证", zap.String("userId", user.ID))
		return nil
	}
	return errs.New(http.StatusUnauthorized, CodeRecoveryInvalid, "恢复码无效或已被使用")
}

// countRecoveryCodes 统计剩余可用恢复码，用于个人中心提示。
func (s *Service) countRecoveryCodes(ctx context.Context, userID string) int {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.UserRecoveryCode{}).
		Where("user_id = ? AND used_at IS NULL", userID).Count(&n).Error; err != nil {
		return 0
	}
	return int(n)
}

// recoveryKey 恢复码哈希使用的密钥；未单独配置时与加密密钥一致。
func (s *Service) recoveryKey() string { return s.cfg.Security.DataEncryptKey }

// isRecoveryCode 区分恢复码与 6 位动态码。
func isRecoveryCode(code string) bool {
	return len(authn.NormalizeRecoveryCode(code)) == authn.RecoveryCodeLength
}

// SessionInfo 在线会话信息。
type SessionInfo struct {
	ID         string    `json:"id"`
	IP         string    `json:"ip"`
	Location   string    `json:"location,omitempty"`
	Browser    string    `json:"browser"`
	OS         string    `json:"os"`
	UserAgent  string    `json:"userAgent"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Current    bool      `json:"current"`
}

// Sessions 返回用户在线会话。
func (s *Service) Sessions(ctx context.Context, userID, currentID string) ([]*SessionInfo, error) {
	var list []model.Session
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND revoked = ? AND expires_at > ?", userID, false, time.Now()).
		Order("last_seen_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]*SessionInfo, 0, len(list))
	for _, ss := range list {
		out = append(out, &SessionInfo{
			ID: ss.ID, IP: ss.IP, Location: ss.Location, Browser: ss.Browser, OS: ss.OS,
			UserAgent: ss.UserAgent, LastSeenAt: ss.LastSeenAt, CreatedAt: ss.CreatedAt,
			Current: ss.ID == currentID,
		})
	}
	return out, nil
}

// RevokeSession 踢下线指定会话（仅限本人）。
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	res := s.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ? AND user_id = ?", sessionID, userID).Update("revoked", true)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("会话不存在")
	}
	_ = s.cache.Delete(ctx, "sess:"+sessionID)
	return nil
}

func (s *Service) revokeAllSessions(ctx context.Context, userID string) {
	var list []model.Session
	s.db.WithContext(ctx).Select("id").Where("user_id = ? AND revoked = ?", userID, false).Find(&list)
	s.db.WithContext(ctx).Model(&model.Session{}).Where("user_id = ?", userID).Update("revoked", true)
	for _, ss := range list {
		_ = s.cache.Delete(ctx, "sess:"+ss.ID)
	}
}

// ---- 登录风控 ----

func (s *Service) failKey(username string) string { return "login:fail:" + strings.ToLower(username) }

func (s *Service) isLocked(ctx context.Context, username string) (bool, time.Duration) {
	b, err := s.cache.Get(ctx, s.failKey(username))
	if err != nil {
		return false, 0
	}
	var n int
	fmt.Sscanf(string(b), "%d", &n)
	if n < s.cfg.Security.MaxLoginFail {
		return false, 0
	}
	ttl, err := s.cache.TTL(ctx, s.failKey(username))
	if err != nil || ttl < 0 {
		ttl = s.cfg.Security.LockDuration
	}
	return true, ttl
}

func (s *Service) onLoginFail(ctx context.Context, userID, username string, meta ClientMeta, msg string) {
	key := s.failKey(username)
	n, _ := s.cache.Incr(ctx, key, 1)
	if n == 1 {
		_ = s.cache.Expire(ctx, key, s.cfg.Security.LockDuration)
	}
	s.writeLoginLog(ctx, &model.User{Base: model.Base{ID: userID}, Username: username}, meta, 0, msg)
}

func (s *Service) clearFail(ctx context.Context, username string) {
	_ = s.cache.Delete(ctx, s.failKey(username))
}

func (s *Service) writeLoginLog(ctx context.Context, user *model.User, meta ClientMeta, status int8, msg string) {
	entry := &model.LoginLog{
		ID: idgen.Next(), UserID: user.ID, Username: user.Username, IP: meta.IP,
		Browser: meta.Browser, OS: meta.OS, Status: status, Message: msg, CreatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
		s.log.Warn("写入登录日志失败", zap.Error(err))
	}
}
