package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/authn"
	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/idgen"
	"github.com/keel-framework/keel/pkg/errs"
)

// oauthFlow 通用 OAuth2 / OIDC 登录流程。
//
// 渠道完全由配置驱动：端点、scope、用户信息字段映射、绑定与注册策略
// 都在 config.OAuthProvider 里声明，新增渠道不需要改代码。
type oauthFlow struct {
	svc       *Service
	providers map[string]*oauthProvider
	order     []string
	client    *http.Client
}

type oauthProvider struct {
	spec config.OAuthProvider
	conf oauth2.Config
}

const oauthStatePrefix = "oauth:state:"

// defaultFieldMap 覆盖常见 provider 的字段差异：每个值是一组逗号分隔的
// 候选路径，按顺序取第一个非空结果。用户在 field_map 里可整体覆盖。
var defaultFieldMap = map[string]string{
	"id":             "sub,id,user_id,openid,uid",
	"username":       "preferred_username,login,username,screen_name,name",
	"nickname":       "name,nickname,display_name,login",
	"email":          "email,mail,email_address",
	"avatar":         "picture,avatar_url,avatar,headimgurl",
	"email_verified": "email_verified,verified_email,emailVerified",
}

func newOAuthFlow(svc *Service) *oauthFlow {
	f := &oauthFlow{
		svc: svc, providers: make(map[string]*oauthProvider),
		client: &http.Client{Timeout: 10 * time.Second},
	}
	if !svc.cfg.OAuth.Enabled {
		return f
	}
	for _, p := range svc.cfg.OAuth.Providers {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if !p.Active() {
			if p.Enabled {
				svc.log.Warn("OAuth 渠道配置不完整，已跳过", zap.String("provider", name))
			}
			continue
		}
		style := oauth2.AuthStyleAutoDetect
		switch strings.ToLower(strings.TrimSpace(p.AuthStyle)) {
		case "in_header":
			style = oauth2.AuthStyleInHeader
		case "in_params":
			style = oauth2.AuthStyleInParams
		}
		f.providers[name] = &oauthProvider{
			spec: p,
			conf: oauth2.Config{
				ClientID: p.ClientID, ClientSecret: p.ClientSecret,
				Endpoint: oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL, AuthStyle: style},
				Scopes:   p.Scopes,
			},
		}
		f.order = append(f.order, name)
		svc.log.Info("已启用 OAuth 渠道", zap.String("provider", name),
			zap.Strings("scopes", p.Scopes))
	}
	return f
}

// OAuthProviderView 登录页可公开使用的渠道信息（不含任何凭据）。
type OAuthProviderView struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Icon  string `json:"icon,omitempty"`
}

// Providers 返回启用中的渠道列表，保持配置顺序。
func (f *oauthFlow) Providers() []OAuthProviderView {
	out := make([]OAuthProviderView, 0, len(f.order))
	for _, n := range f.order {
		p := f.providers[n]
		label := strings.TrimSpace(p.spec.Label)
		if label == "" {
			label = strings.ToUpper(n[:1]) + n[1:]
		}
		out = append(out, OAuthProviderView{Name: n, Label: label, Icon: p.spec.Icon})
	}
	return out
}

func (f *oauthFlow) Enabled() bool { return len(f.order) > 0 }

func (f *oauthFlow) provider(name string) (*oauthProvider, error) {
	p, ok := f.providers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, errs.NotFound("未启用该第三方登录渠道")
	}
	return p, nil
}

type oauthState struct {
	Provider  string `json:"provider"`
	Mode      string `json:"mode"` // login / bind
	UserID    string `json:"userId,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// Authorize 生成授权地址与一次性 state。
func (f *oauthFlow) Authorize(ctx context.Context, name, mode, userID, redirectURI string) (string, string, error) {
	p, err := f.provider(name)
	if err != nil {
		return "", "", err
	}
	conf := p.conf
	conf.RedirectURL = redirectURI
	state := idgen.Token(24)
	payload, _ := json.Marshal(oauthState{
		Provider: strings.ToLower(name), Mode: mode, UserID: userID,
		CreatedAt: time.Now().Unix(),
	})
	if err := f.svc.cache.Set(ctx, oauthStatePrefix+state, payload, f.svc.cfg.OAuth.StateTTL); err != nil {
		return "", "", errs.Internal("登录状态保存失败")
	}
	opts := make([]oauth2.AuthCodeOption, 0, len(p.spec.ExtraAuth))
	for k, v := range p.spec.ExtraAuth {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	return conf.AuthCodeURL(state, opts...), state, nil
}

// OAuthCallbackResult 回调结果；绑定模式不签发令牌。
type OAuthCallbackResult struct {
	Mode     string     `json:"mode"`
	Provider string     `json:"provider"`
	Token    *TokenPair `json:"token,omitempty"`
	User     *UserInfo  `json:"user,omitempty"`
	Message  string     `json:"message,omitempty"`
}

type oauthProfile struct {
	Subject       string
	Username      string
	Nickname      string
	Email         string
	Avatar        string
	EmailVerified bool
	Raw           []byte
}

// Callback 完成授权码换取、用户信息拉取、账号绑定与本地会话签发。
func (f *oauthFlow) Callback(ctx context.Context, name, code, state, redirectURI string,
	meta ClientMeta) (*OAuthCallbackResult, error) {
	p, err := f.provider(name)
	if err != nil {
		return nil, err
	}
	key := oauthStatePrefix + state
	raw, err := f.svc.cache.Get(ctx, key)
	if err != nil || len(raw) == 0 {
		return nil, errs.BadRequest("登录状态已失效，请重新发起登录")
	}
	// state 一次性消费，防止重放
	_ = f.svc.cache.Delete(ctx, key)
	var st oauthState
	if err := json.Unmarshal(raw, &st); err != nil || st.Provider != strings.ToLower(name) {
		return nil, errs.BadRequest("登录状态不匹配")
	}
	if f.svc.cfg.OAuth.StateTTL > 0 &&
		time.Since(time.Unix(st.CreatedAt, 0)) > f.svc.cfg.OAuth.StateTTL {
		return nil, errs.BadRequest("登录已超时，请重新发起")
	}

	conf := p.conf
	conf.RedirectURL = redirectURI
	tok, err := conf.Exchange(ctx, code)
	if err != nil {
		return nil, errs.BadRequest("换取访问令牌失败：" + err.Error())
	}
	prof, err := f.fetchProfile(ctx, p, tok.AccessToken)
	if err != nil {
		return nil, err
	}
	if prof.Subject == "" {
		return nil, errs.BadRequest("第三方用户信息缺少唯一标识，请检查 field_map.id")
	}

	user, err := f.resolveUser(ctx, p, st, prof)
	if err != nil {
		return nil, err
	}
	if user.Status != model.StatusEnabled {
		return nil, errs.Forbidden("账号已被禁用，请联系管理员")
	}

	if st.Mode == "bind" {
		if err := f.touchBinding(ctx, st.Provider, prof.Subject); err != nil {
			return nil, err
		}
		return &OAuthCallbackResult{Mode: "bind", Provider: name, Message: "已绑定第三方账号"}, nil
	}

	// 登录：复用账号密码登录的同一套会话签发逻辑
	if f.svc.cfg.Security.SessionSingle {
		f.svc.revokeAllSessions(ctx, user.ID)
	}
	pair, err := f.svc.issueSession(ctx, user, meta)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	f.svc.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_login_at": now, "last_login_ip": meta.IP,
		"login_count": gorm.Expr("login_count + 1"),
	})
	f.svc.writeLoginLog(ctx, user, meta, 1, "OAuth 登录成功（"+name+"）")
	_ = f.touchBinding(ctx, st.Provider, prof.Subject)
	info, err := f.svc.buildUserInfo(ctx, user)
	if err != nil {
		return nil, err
	}
	return &OAuthCallbackResult{Mode: "login", Provider: name, Token: pair, User: info}, nil
}

// resolveUser 按「已有绑定 → 关联已有账号 → 自动注册」顺序确定本地用户。
func (f *oauthFlow) resolveUser(ctx context.Context, p *oauthProvider, st oauthState,
	prof *oauthProfile) (*model.User, error) {
	provider := st.Provider
	var binding model.UserOAuthBinding
	err := f.svc.db.WithContext(ctx).
		Where("provider = ? AND subject = ?", provider, prof.Subject).First(&binding).Error
	if err == nil {
		// 绑定流程不允许把已被其它本地账号占用的外部账号静默「绑定成功」：
		// 否则这里会直接返回该绑定所属的用户，前端提示成功而关系并未建立。
		if st.Mode == "bind" && binding.UserID != st.UserID {
			return nil, errs.Conflict("该第三方账号已绑定其它本地账号，请先解绑")
		}
		var user model.User
		if e := f.svc.db.WithContext(ctx).First(&user, "id = ?", binding.UserID).Error; e == nil {
			return &user, nil
		}
		return nil, errs.Unauthorized("绑定账号不存在，请联系管理员")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if st.Mode == "bind" {
		if st.UserID == "" {
			return nil, errs.Unauthorized("绑定会话已失效")
		}
		var user model.User
		if e := f.svc.db.WithContext(ctx).First(&user, "id = ?", st.UserID).Error; e != nil {
			return nil, errs.NotFound("待绑定账号不存在")
		}
		// 同一渠道只允许绑定一个外部账号，避免覆盖已有关系
		var n int64
		_ = f.svc.db.WithContext(ctx).Model(&model.UserOAuthBinding{}).
			Where("user_id = ? AND provider = ?", st.UserID, provider).Count(&n)
		if n > 0 {
			return nil, errs.Conflict("该渠道已绑定其它账号，请先解绑")
		}
		if e := f.createBinding(ctx, st.UserID, provider, prof); e != nil {
			return nil, e
		}
		return &user, nil
	}

	// 关联已有账号：只在邮箱已验证（或 provider 未提供该字段）时允许，
	// 否则攻击者可用任意邮箱抢占本地账号。
	if p.spec.LinkExisting && prof.Email != "" && prof.EmailVerified {
		matchBy := strings.ToLower(strings.TrimSpace(p.spec.MatchBy))
		if matchBy == "" {
			matchBy = "email"
		}
		column := "email"
		value := prof.Email
		if matchBy == "username" {
			column, value = "username", prof.Username
		}
		var user model.User
		if value != "" {
			e := f.svc.db.WithContext(ctx).Where(column+" = ?", value).First(&user).Error
			if e == nil {
				if e := f.createBinding(ctx, user.ID, provider, prof); e != nil {
					return nil, e
				}
				return &user, nil
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return nil, e
			}
		}
	}

	if !p.spec.AutoRegister {
		return nil, errs.Forbidden("该第三方账号尚未绑定本地账号，请联系管理员或改用账号密码登录")
	}
	return f.registerUser(ctx, p, provider, prof)
}

func (f *oauthFlow) createBinding(ctx context.Context, userID, provider string, prof *oauthProfile) error {
	b := &model.UserOAuthBinding{
		UserID: userID, Provider: provider, Subject: prof.Subject,
		Email: prof.Email, Username: prof.Username, Nickname: prof.Nickname,
		Avatar: prof.Avatar, RawProfile: string(prof.Raw),
	}
	if len(b.RawProfile) > 8192 {
		b.RawProfile = b.RawProfile[:8192]
	}
	now := time.Now()
	b.LastLoginAt = &now
	if err := f.svc.db.WithContext(ctx).Create(b).Error; err != nil {
		// 并发下由唯一索引兜底，把各驱动差异化的约束错误转成可读提示
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate") {
			return errs.Conflict("该第三方账号已被绑定，请刷新后重试")
		}
		return err
	}
	return nil
}

func (f *oauthFlow) touchBinding(ctx context.Context, provider, subject string) error {
	return f.svc.db.WithContext(ctx).Model(&model.UserOAuthBinding{}).
		Where("provider = ? AND subject = ?", provider, subject).
		Update("last_login_at", time.Now()).Error
}

// registerUser 自动注册本地账号。密码为随机串（外部账号无法直接密码登录，
// 需要时由管理员重置），并分配配置里的默认角色。
func (f *oauthFlow) registerUser(ctx context.Context, p *oauthProvider, provider string,
	prof *oauthProfile) (*model.User, error) {
	username := strings.TrimSpace(prof.Username)
	if username == "" {
		username = provider + "_" + shorten(prof.Subject, 12)
	}
	username = sanitizeUsername(username)
	base := username
	for i := 2; ; i++ {
		var n int64
		if err := f.svc.db.WithContext(ctx).Model(&model.User{}).Where("username = ?", username).Count(&n).Error; err != nil {
			return nil, err
		}
		if n == 0 {
			break
		}
		username = fmt.Sprintf("%s_%d", base, i)
	}
	nickname := strings.TrimSpace(prof.Nickname)
	if nickname == "" {
		nickname = username
	}
	hash, err := authn.HashPassword(idgen.Token(32))
	if err != nil {
		return nil, err
	}
	user := &model.User{
		Username: username, Password: hash, Nickname: nickname,
		Email: prof.Email, Avatar: prof.Avatar, Status: model.StatusEnabled,
		Remark: "由 " + provider + " 自动注册",
	}
	if len(user.Avatar) > 255 {
		user.Avatar = ""
	}

	err = f.svc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		if len(p.spec.DefaultRoles) > 0 {
			var roleIDs []string
			if err := tx.Model(&model.Role{}).
				Where("code IN ? AND status = ?", p.spec.DefaultRoles, model.StatusEnabled).
				Pluck("id", &roleIDs).Error; err != nil {
				return err
			}
			for _, rid := range roleIDs {
				if err := tx.Create(&model.UserRole{UserID: user.ID, RoleID: rid}).Error; err != nil {
					return err
				}
			}
		}
		b := &model.UserOAuthBinding{
			UserID: user.ID, Provider: provider, Subject: prof.Subject,
			Email: prof.Email, Username: prof.Username, Nickname: prof.Nickname,
			Avatar: prof.Avatar, RawProfile: string(prof.Raw),
		}
		if len(b.RawProfile) > 8192 {
			b.RawProfile = b.RawProfile[:8192]
		}
		return tx.Create(b).Error
	})
	if err != nil {
		return nil, err
	}
	f.svc.log.Info("OAuth 自动注册用户", zap.String("provider", provider),
		zap.String("username", username))
	return user, nil
}

// fetchProfile 拉取用户信息并按字段映射提取标准字段。
func (f *oauthFlow) fetchProfile(ctx context.Context, p *oauthProvider, accessToken string) (*oauthProfile, error) {
	method := http.MethodGet
	body := io.Reader(nil)
	if p.spec.UserInfoRaw {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, p.spec.UserInfoURL, body)
	if err != nil {
		return nil, errs.Internal("构造用户信息请求失败")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, errs.BadRequest("读取第三方用户信息失败：" + err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errs.Internal("读取第三方用户信息失败")
	}
	if resp.StatusCode >= 400 {
		return nil, errs.BadRequest(fmt.Sprintf("第三方用户信息接口返回 %d", resp.StatusCode))
	}
	var obj any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, errs.BadRequest("第三方用户信息不是合法 JSON")
	}
	m := p.spec.FieldMap
	pick := func(key string) string {
		paths := defaultFieldMap[key]
		if v, ok := m[key]; ok && strings.TrimSpace(v) != "" {
			paths = v
		}
		return firstValue(obj, paths)
	}
	prof := &oauthProfile{
		Subject: pick("id"), Username: pick("username"), Nickname: pick("nickname"),
		Email: pick("email"), Avatar: pick("avatar"), Raw: data,
	}
	verified := strings.ToLower(pick("email_verified"))
	// provider 未返回该字段时按「已验证」处理，否则 link_existing 会永远不生效
	prof.EmailVerified = verified == "" || verified == "true" || verified == "1"

	if prof.Email == "" && p.spec.EmailURL != "" {
		prof.Email, prof.EmailVerified = f.fetchEmail(ctx, p.spec.EmailURL, accessToken)
	}
	return prof, nil
}

// fetchEmail 处理 GitHub 一类把邮箱放在独立接口、返回数组的情况。
func (f *oauthFlow) fetchEmail(ctx context.Context, endpoint, accessToken string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := f.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return "", false
	}
	var obj any
	if json.Unmarshal(data, &obj) != nil {
		return "", false
	}
	if list, ok := obj.([]any); ok {
		var fallback string
		var fallbackVerified bool
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			email := stringVal(m["email"])
			if email == "" {
				continue
			}
			verified := truthy(m["verified"]) || truthy(m["email_verified"])
			primary := truthy(m["primary"])
			if fallback == "" {
				fallback, fallbackVerified = email, verified
			}
			if primary && verified {
				return email, true
			}
			if verified && fallbackVerified {
				fallback, fallbackVerified = email, verified
			}
		}
		return fallback, fallbackVerified
	}
	if m, ok := obj.(map[string]any); ok {
		return stringVal(m["email"]), truthy(m["verified"]) || truthy(m["email_verified"])
	}
	return "", false
}

// Bind 把当前登录用户与外部账号建立绑定（发起绑定授权时调用）。
func (f *oauthFlow) Bindings(ctx context.Context, userID string) ([]model.UserOAuthBinding, error) {
	var list []model.UserOAuthBinding
	err := f.svc.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

// Unbind 解绑；物理删除以免软删除行继续占用 (provider, subject) 唯一键。
func (f *oauthFlow) Unbind(ctx context.Context, userID, provider string) error {
	res := f.svc.db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND provider = ?", userID, strings.ToLower(provider)).
		Delete(&model.UserOAuthBinding{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("未找到绑定记录")
	}
	return nil
}

// ---- JSON 字段提取工具 ----

func firstValue(obj any, paths string) string {
	for _, path := range strings.Split(paths, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if v := stringVal(lookupJSON(obj, path)); v != "" {
			return v
		}
	}
	return ""
}

// lookupJSON 支持点路径与数组下标，如 "emails.0.value" 或 "response.user.id"。
func lookupJSON(v any, path string) any {
	cur := v
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		switch t := cur.(type) {
		case map[string]any:
			cur = t[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			cur = t[i]
		default:
			return nil
		}
	}
	return cur
}

func stringVal(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func truthy(v any) bool {
	s := strings.ToLower(stringVal(v))
	return s == "true" || s == "1" || s == "yes"
}

func sanitizeUsername(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		return "oauth_user"
	}
	if len([]rune(out)) > 32 {
		out = string([]rune(out)[:32])
	}
	return out
}

func shorten(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
