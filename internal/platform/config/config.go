// Package config 基于 Viper 提供分层配置：默认值 -> 配置文件 -> 环境变量 -> 动态覆盖。
// 环境变量约定：KEEL_SERVER_PORT 对应 server.port（点号替换为下划线）。
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Config 是应用全量配置。
type Config struct {
	App       App       `mapstructure:"app"`
	Server    Server    `mapstructure:"server"`
	Log       Log       `mapstructure:"log"`
	DB        DB        `mapstructure:"db"`
	Redis     Redis     `mapstructure:"redis"`
	JWT       JWT       `mapstructure:"jwt"`
	Security  Security  `mapstructure:"security"`
	Storage   Storage   `mapstructure:"storage"`
	Mail      Mail      `mapstructure:"mail"`
	CORS      CORS      `mapstructure:"cors"`
	RateLimit RateLimit `mapstructure:"rate_limit"`
	Metrics   Metrics   `mapstructure:"metrics"`
	Micro     Micro     `mapstructure:"micro"`
	Runtime   Runtime   `mapstructure:"runtime"`
	News      News      `mapstructure:"news"`
	Migration Migration `mapstructure:"migration"`
	Backup    Backup    `mapstructure:"backup"`
	OAuth     OAuth     `mapstructure:"oauth"`
	Docs      Docs      `mapstructure:"docs"`
}

// News 资讯聚合参数。
type News struct {
	// RetentionDays 资讯保留天数，超期由定时任务物理删除。
	RetentionDays int `mapstructure:"retention_days"`
}

// Migration 数据库结构变更策略。
//
// versioned：按版本号执行迁移函数（支持回滚与数据回填），生产推荐；
// auto：沿用 GORM AutoMigrate，只增不减，适合本地快速迭代；
// off：完全不迁移，由外部 DBA 或 CI 执行 migrate 子命令。
type Migration struct {
	Mode      string `mapstructure:"mode"`
	TableName string `mapstructure:"table_name"`
}

// Mode 归一化迁移模式。
func (m Migration) ResolveMode() string {
	switch strings.ToLower(strings.TrimSpace(m.Mode)) {
	case "auto":
		return "auto"
	case "off", "none", "disabled":
		return "off"
	default:
		return "versioned"
	}
}

// VersionTable 返回版本记录表名，留空时使用 goose 默认值。
func (m Migration) VersionTable() string {
	if v := strings.TrimSpace(m.TableName); v != "" {
		return v
	}
	return "keel_schema_migrations"
}

// Backup 数据库备份参数。
//
// 备份产物落在 Dir，文件名形如 keel-sqlite-20261112-152524.db.gz；
// Retain 只保留最近 N 份，Compress 用 gzip 压缩。MySQL / PostgreSQL
// 通过外部 dump 工具导出，路径可配置，未安装时备份任务会明确报错。
type Backup struct {
	Enabled      bool   `mapstructure:"enabled"`
	Dir          string `mapstructure:"dir"`
	Retain       int    `mapstructure:"retain"`
	Compress     bool   `mapstructure:"compress"`
	MySQLDump    string `mapstructure:"mysql_dump"`
	MySQLRestore string `mapstructure:"mysql_restore"`
	PGDump       string `mapstructure:"pg_dump"`
	PGRestore    string `mapstructure:"pg_restore"`
	// AllowRestore 打开后才允许通过管理接口触发恢复；恢复会替换当前库，
	// 需要重启进程，默认关闭以免误操作。
	AllowRestore bool `mapstructure:"allow_restore"`
}

// EffectiveDir 返回实际备份目录。
func (b Backup) EffectiveDir() string {
	if strings.TrimSpace(b.Dir) != "" {
		return b.Dir
	}
	return "data/backups"
}

// OAuth 第三方登录参数。
//
// 新增渠道只需在 providers 里加一段配置，代码不需要改动：
// 每个 provider 自行声明端点、scope 以及用户信息字段到本系统字段的映射。
// 敏感凭据建议用环境变量 KEEL_OAUTH_<NAME>_CLIENT_ID / _CLIENT_SECRET 覆盖，
// 避免明文写进配置文件。
type OAuth struct {
	Enabled      bool            `mapstructure:"enabled"`
	CallbackBase string          `mapstructure:"callback_base"`
	FrontendPath string          `mapstructure:"frontend_path"`
	StateTTL     time.Duration   `mapstructure:"state_ttl"`
	Providers    []OAuthProvider `mapstructure:"providers"`
}

// OAuthProvider 单个第三方身份提供商（通用 OAuth2 / OIDC）。
type OAuthProvider struct {
	Name         string            `mapstructure:"name"`
	Label        string            `mapstructure:"label"`
	Icon         string            `mapstructure:"icon"`
	Enabled      bool              `mapstructure:"enabled"`
	ClientID     string            `mapstructure:"client_id"`
	ClientSecret string            `mapstructure:"client_secret"`
	AuthURL      string            `mapstructure:"auth_url"`
	TokenURL     string            `mapstructure:"token_url"`
	UserInfoURL  string            `mapstructure:"user_info_url"`
	EmailURL     string            `mapstructure:"email_url"`
	Scopes       []string          `mapstructure:"scopes"`
	FieldMap     map[string]string `mapstructure:"field_map"`
	// AutoRegister 未绑定且匹配不到本地账号时自动创建用户。
	AutoRegister bool `mapstructure:"auto_register"`
	// LinkExisting 允许按 MatchBy 把外部身份关联到已有本地账号。
	LinkExisting bool     `mapstructure:"link_existing"`
	MatchBy      string   `mapstructure:"match_by"`
	DefaultRoles []string `mapstructure:"default_roles"`
	// AuthStyle 对应 oauth2.AuthStyle：in_header / in_params / auto。
	AuthStyle   string            `mapstructure:"auth_style"`
	ExtraAuth   map[string]string `mapstructure:"extra_auth"`
	UserInfoRaw bool              `mapstructure:"user_info_raw"` // true 时用 POST 提交 token
}

// Active 判断 provider 是否可以对外提供。
func (p OAuthProvider) Active() bool {
	return p.Enabled && p.ClientID != "" && p.ClientSecret != "" &&
		p.AuthURL != "" && p.TokenURL != "" && p.UserInfoURL != ""
}

// Docs 自动接口文档参数。
//
// Enabled 为三态：nil 时开发环境自动开启、生产环境自动关闭。
type Docs struct {
	Enabled *bool  `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

// UseDocs 判断是否挂载 Swagger UI。
func (d Docs) UseDocs(isProd bool) bool {
	if d.Enabled != nil {
		return *d.Enabled
	}
	return !isProd
}

// EffectivePath 返回文档挂载前缀。
func (d Docs) EffectivePath() string {
	p := strings.TrimSpace(d.Path)
	if p == "" {
		return "/docs"
	}
	return "/" + strings.Trim(p, "/")
}

// App 应用基础信息。
type App struct {
	Name        string `mapstructure:"name"`
	Env         string `mapstructure:"env"` // dev / test / prod
	Version     string `mapstructure:"version"`
	Description string `mapstructure:"description"`
	Timezone    string `mapstructure:"timezone"`
}

// Server HTTP 服务参数。
type Server struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Mode            string        `mapstructure:"mode"` // debug / release
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	MaxBodySize     int64         `mapstructure:"max_body_size"`
	TrustProxy      bool          `mapstructure:"trust_proxy"`
	TLSCert         string        `mapstructure:"tls_cert"`
	TLSKey          string        `mapstructure:"tls_key"`
	WebDir          string        `mapstructure:"web_dir"`
}

// Log 日志参数。
type Log struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"` // json / console
	Dir        string `mapstructure:"dir"`
	Filename   string `mapstructure:"filename"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	MaxBackups int    `mapstructure:"max_backups"`
	Compress   bool   `mapstructure:"compress"`
	Console    bool   `mapstructure:"console"`
	Caller     bool   `mapstructure:"caller"`
}

// DB 数据库参数。
type DB struct {
	Driver          string        `mapstructure:"driver"` // sqlite / mysql / postgres
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
	SlowThreshold   time.Duration `mapstructure:"slow_threshold"`
	LogLevel        string        `mapstructure:"log_level"` // silent / error / warn / info
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
	Seed            bool          `mapstructure:"seed"`
}

// ResolveDriver 解析实际使用的数据库驱动：driver 留空时按 DSN 推断。
// 这样只改一条 DSN 就能从 SQLite 切到 MySQL / PostgreSQL。
func (d DB) ResolveDriver() string {
	if driver := strings.ToLower(strings.TrimSpace(d.Driver)); driver != "" {
		return driver
	}
	dsn := strings.ToLower(strings.TrimSpace(d.DSN))
	switch {
	case strings.HasPrefix(dsn, "mysql://"), strings.HasPrefix(dsn, "mysql:"),
		strings.Contains(dsn, "@tcp("), strings.Contains(dsn, "@unix("):
		return "mysql"
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"),
		strings.HasPrefix(dsn, "postgres:"):
		return "postgres"
	case strings.Contains(dsn, "host=") && strings.Contains(dsn, "dbname="):
		return "postgres"
	default:
		return "sqlite"
	}
}

// Redis 缓存参数。
//
// Enabled 为三态指针：nil 表示自动判断——Addr 非空即启用；
// 显式 false 可强制使用进程内缓存，true 可在 Addr 留空时使用默认地址。
type Redis struct {
	Enabled     *bool         `mapstructure:"enabled"`
	Addr        string        `mapstructure:"addr"`
	Username    string        `mapstructure:"username"`
	Password    string        `mapstructure:"password"`
	DB          int           `mapstructure:"db"`
	PoolSize    int           `mapstructure:"pool_size"`
	Prefix      string        `mapstructure:"prefix"`
	DialTimeout time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	LocalSize   int           `mapstructure:"local_size"` // 未启用 Redis 时的本地缓存容量
	// Optional 为 true 时，Redis 连接失败会降级为进程内缓存；默认 false，
	// 配置了就用不上不如直接启动失败，避免多实例状态悄悄分裂。
	Optional bool `mapstructure:"optional"`
}

// UseRedis 判断是否启用 Redis：未显式配置时由 addr 是否为空决定。
func (r Redis) UseRedis() bool {
	if r.Enabled != nil {
		return *r.Enabled
	}
	return strings.TrimSpace(r.Addr) != ""
}

// EffectiveAddr 返回实际使用的地址，启用但未填地址时回落本地默认端口。
func (r Redis) EffectiveAddr() string {
	if addr := strings.TrimSpace(r.Addr); addr != "" {
		return addr
	}
	return "127.0.0.1:6379"
}

// JWT 令牌参数。
type JWT struct {
	Secret     string        `mapstructure:"secret"`
	Issuer     string        `mapstructure:"issuer"`
	AccessTTL  time.Duration `mapstructure:"access_ttl"`
	RefreshTTL time.Duration `mapstructure:"refresh_ttl"`
}

// Security 安全参数。
type Security struct {
	CaptchaEnabled   bool          `mapstructure:"captcha_enabled"`
	MaxLoginFail     int           `mapstructure:"max_login_fail"`
	LockDuration     time.Duration `mapstructure:"lock_duration"`
	PasswordMinLen   int           `mapstructure:"password_min_len"`
	PasswordStrength int           `mapstructure:"password_strength"` // 1 弱(仅长度) 2 中(字母+数字) 3 强(大小写+数字+符号)
	DataEncryptKey   string        `mapstructure:"data_encrypt_key"`
	SessionSingle    bool          `mapstructure:"session_single"` // 同一账号是否只允许一处登录
	InternalToken    string        `mapstructure:"internal_token"`
}

// Storage 文件存储参数。
//
// Type 支持 auto / local / s3：auto 时根据 s3.bucket 是否配置自动选择，
// 未配置对象存储就落到本地磁盘，零依赖也能跑。
type Storage struct {
	Type     string   `mapstructure:"type"`
	LocalDir string   `mapstructure:"local_dir"`
	BaseURL  string   `mapstructure:"base_url"`
	MaxSize  int64    `mapstructure:"max_size"`
	AllowExt []string `mapstructure:"allow_ext"`
	S3       S3       `mapstructure:"s3"`
}

// Kind 解析最终生效的存储类型。
func (s Storage) Kind() string {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "", "auto":
		if strings.TrimSpace(s.S3.Bucket) != "" && strings.TrimSpace(s.S3.Endpoint) != "" {
			return "s3"
		}
		return "local"
	case "s3", "oss", "minio", "cos":
		return "s3"
	default:
		return strings.ToLower(strings.TrimSpace(s.Type))
	}
}

// S3 兼容对象存储参数（MinIO、阿里云 OSS、AWS S3）。
type S3 struct {
	Endpoint  string `mapstructure:"endpoint"`
	Region    string `mapstructure:"region"`
	Bucket    string `mapstructure:"bucket"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	UseSSL    bool   `mapstructure:"use_ssl"`
	PublicURL string `mapstructure:"public_url"`
}

// Mail 邮件通知参数。
//
// Enabled 同样是三态：nil 表示 host 非空即启用，显式 false 关闭。
type Mail struct {
	Enabled  *bool  `mapstructure:"enabled"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
	TLS      bool   `mapstructure:"tls"`
}

// UseSMTP 判断是否启用邮件：必须有 host，显式关闭时一律不启用。
func (m Mail) UseSMTP() bool {
	if strings.TrimSpace(m.Host) == "" {
		return false
	}
	if m.Enabled != nil {
		return *m.Enabled
	}
	return true
}

// Runtime 运行期行为参数。
type Runtime struct {
	// Stateless 无状态校验策略：auto 仅告警、strict 阻断启动、off 不检查。
	Stateless string `mapstructure:"stateless"`
	// Instances 计划运行的实例数，大于 1 时 auto 策略会启用无状态校验。
	Instances int `mapstructure:"instances"`
	// InstanceID 实例标识，留空时自动生成，用于日志与只跑一份的任务。
	InstanceID string `mapstructure:"instance_id"`
}

// CacheKindHint 返回将使用的缓存实现，用于启动日志与无状态自检。
func (c *Config) CacheKindHint() string {
	if c.Redis.UseRedis() {
		return "redis"
	}
	return "local"
}

// StatelessMode 归一化无状态校验策略。
func (c *Config) StatelessMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Runtime.Stateless)) {
	case "strict":
		return "strict"
	case "off":
		return "off"
	default:
		return "auto"
	}
}

// MultiInstance 判断是否按多实例部署运行：显式实例数优先，其次看微服务形态。
func (c *Config) MultiInstance() bool {
	if c.Runtime.Instances > 1 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(c.Micro.Mode), "service")
}

// CORS 跨域参数。
type CORS struct {
	AllowOrigins     []string `mapstructure:"allow_origins"`
	AllowMethods     []string `mapstructure:"allow_methods"`
	AllowHeaders     []string `mapstructure:"allow_headers"`
	ExposeHeaders    []string `mapstructure:"expose_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
	MaxAge           int      `mapstructure:"max_age"`
}

// RateLimit 限流参数。
type RateLimit struct {
	Enabled bool    `mapstructure:"enabled"`
	RPS     float64 `mapstructure:"rps"`
	Burst   int     `mapstructure:"burst"`
	ByUser  bool    `mapstructure:"by_user"`
}

// Metrics 监控参数。
type Metrics struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
	Token   string `mapstructure:"token"` // 为空表示仅内网可访问
}

// Micro 微服务参数。
type Micro struct {
	Mode     string        `mapstructure:"mode"` // mono / gateway / service
	Modules  []string      `mapstructure:"modules"`
	Registry Registry      `mapstructure:"registry"`
	Routes   []ProxyRoute  `mapstructure:"routes"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Retries  int           `mapstructure:"retries"`
	Breaker  Breaker       `mapstructure:"breaker"`
}

// Registry 服务发现参数。
type Registry struct {
	Type      string        `mapstructure:"type"` // static / dns
	Endpoints []string      `mapstructure:"endpoints"`
	DNSSuffix string        `mapstructure:"dns_suffix"`
	Interval  time.Duration `mapstructure:"interval"`
}

// ProxyRoute 网关转发规则。
type ProxyRoute struct {
	Prefix  string   `mapstructure:"prefix"`
	Service string   `mapstructure:"service"`
	Targets []string `mapstructure:"targets"`
	Strip   bool     `mapstructure:"strip_prefix"`
}

// Breaker 熔断参数。
type Breaker struct {
	Enabled   bool `mapstructure:"enabled"`
	MaxFailed int  `mapstructure:"max_failed"`
	Interval  int  `mapstructure:"interval"`
	Timeout   int  `mapstructure:"timeout"`
}

// Holder 保存配置实例，支持并发读取与热更新回调。
type Holder struct {
	mu      sync.RWMutex
	cfg     *Config
	v       *viper.Viper
	path    string
	onApply []func(*Config)
}

var holder = &Holder{}

// Load 加载配置；文件不存在时使用内置默认值，保证开箱即用。
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			var nf viper.ConfigFileNotFoundError
			if !errors.As(err, &nf) {
				return nil, fmt.Errorf("读取配置文件失败: %w", err)
			}
		}
	}
	v.SetEnvPrefix("KEEL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	// 切片类型字段在配置文件中缺省时需要显式绑定，环境变量才会参与 Unmarshal
	for _, key := range []string{
		"micro.modules", "micro.registry.endpoints", "storage.allow_ext", "cors.allow_origins",
		// 敏感项与部署相关项显式绑定环境变量，容器里无需改配置文件
		"db.driver", "db.dsn",
		"redis.addr", "redis.password", "redis.username",
		"storage.type", "storage.local_dir", "storage.base_url",
		"storage.s3.endpoint", "storage.s3.region", "storage.s3.bucket",
		"storage.s3.access_key", "storage.s3.secret_key", "storage.s3.public_url", "storage.s3.use_ssl",
		"mail.host", "mail.username", "mail.password", "mail.from",
		"runtime.stateless", "runtime.instance_id", "runtime.instances",
		"db.auto_migrate", "db.seed", "security.captcha_enabled", "rate_limit.enabled",
		"migration.mode", "migration.table_name",
		"backup.enabled", "backup.dir", "backup.retain", "backup.compress", "backup.allow_restore",
		"oauth.enabled", "oauth.callback_base", "oauth.frontend_path", "oauth.state_ttl",
		"docs.enabled", "docs.path",
	} {
		_ = v.BindEnv(key)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	applyEnvFallbacks(cfg)
	applyMicroRoutesEnv(cfg)
	applyOAuthEnvOverrides(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	holder.mu.Lock()
	holder.cfg = cfg
	holder.v = v
	holder.path = path
	holder.mu.Unlock()
	return cfg, nil
}

// Get 返回当前配置。
func Get() *Config {
	holder.mu.RLock()
	defer holder.mu.RUnlock()
	return holder.cfg
}

// OnChange 注册配置热更新回调。
func OnChange(fn func(*Config)) {
	holder.mu.Lock()
	holder.onApply = append(holder.onApply, fn)
	holder.mu.Unlock()
}

// Watch 监听配置文件变更并热更新。
func Watch() {
	holder.mu.RLock()
	v, path := holder.v, holder.path
	holder.mu.RUnlock()
	if v == nil || path == "" {
		return
	}
	v.OnConfigChange(func(_ fsnotify.Event) {
		cfg := &Config{}
		if err := v.Unmarshal(cfg); err != nil {
			return
		}
		if cfg.Validate() != nil {
			return
		}
		holder.mu.Lock()
		holder.cfg = cfg
		cbs := append([]func(*Config){}, holder.onApply...)
		holder.mu.Unlock()
		for _, cb := range cbs {
			cb(cfg)
		}
	})
	v.WatchConfig()
}

// applyMicroRoutesEnv 支持用环境变量提供网关转发规则，
// 形如 KEEL_GATEWAY_ROUTES="/api/v1=keel-core,/uploads=keel-file"。
// 配置文件中已显式声明 routes 时以文件为准。
func applyMicroRoutesEnv(c *Config) {
	if len(c.Micro.Routes) > 0 {
		return
	}
	raw := strings.TrimSpace(os.Getenv("KEEL_GATEWAY_ROUTES"))
	if raw == "" {
		return
	}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, service, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		c.Micro.Routes = append(c.Micro.Routes, ProxyRoute{
			Prefix: strings.TrimSpace(prefix), Service: strings.TrimSpace(service),
		})
	}
}

// DefaultJWTSecret 是内置的开发用密钥，生产环境必须显式覆盖。
const DefaultJWTSecret = "keel-dev-secret-please-change-in-production"

// applyEnvFallbacks 处理未绑定到结构体字段的关键环境变量（如密钥）。
func applyEnvFallbacks(c *Config) {
	if c.JWT.Secret == "" {
		c.JWT.Secret = DefaultJWTSecret
	}
	if c.Security.DataEncryptKey == "" {
		c.Security.DataEncryptKey = c.JWT.Secret
	}
	if c.Security.InternalToken == "" {
		c.Security.InternalToken = "keel-internal-token"
	}
	if c.Backup.Retain <= 0 {
		c.Backup.Retain = 7
	}
	if c.Backup.MySQLDump == "" {
		c.Backup.MySQLDump = "mysqldump"
	}
	if c.Backup.MySQLRestore == "" {
		c.Backup.MySQLRestore = "mysql"
	}
	if c.Backup.PGDump == "" {
		c.Backup.PGDump = "pg_dump"
	}
	if c.Backup.PGRestore == "" {
		c.Backup.PGRestore = "psql"
	}
	if c.OAuth.StateTTL <= 0 {
		c.OAuth.StateTTL = 10 * time.Minute
	}
	if c.OAuth.FrontendPath == "" {
		c.OAuth.FrontendPath = "/oauth/callback"
	}
}

// applyOAuthEnvOverrides 允许用环境变量注入各 provider 的凭据，避免密钥落盘。
// 约定：KEEL_OAUTH_GITHUB_CLIENT_ID / KEEL_OAUTH_GITHUB_CLIENT_SECRET，
// 名称中的非字母数字统一转为下划线。仅配置了一个 provider 时也可用
// KEEL_OAUTH_CLIENT_ID / KEEL_OAUTH_CLIENT_SECRET 简写。
func applyOAuthEnvOverrides(c *Config) {
	if len(c.OAuth.Providers) == 0 {
		return
	}
	envKey := func(s string) string {
		var b strings.Builder
		upper := strings.ToUpper(s)
		for _, r := range upper {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteRune('_')
			}
		}
		return b.String()
	}
	for i := range c.OAuth.Providers {
		p := &c.OAuth.Providers[i]
		prefix := "KEEL_OAUTH_" + envKey(p.Name) + "_"
		id, secret := os.Getenv(prefix+"CLIENT_ID"), os.Getenv(prefix+"CLIENT_SECRET")
		if len(c.OAuth.Providers) == 1 {
			if v := os.Getenv("KEEL_OAUTH_CLIENT_ID"); v != "" {
				id = v
			}
			if v := os.Getenv("KEEL_OAUTH_CLIENT_SECRET"); v != "" {
				secret = v
			}
		}
		if id != "" {
			p.ClientID = id
		}
		if secret != "" {
			p.ClientSecret = secret
		}
	}
}

// Validate 做启动期关键校验，避免带病运行。
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	switch c.DB.ResolveDriver() {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("不支持的数据库驱动: %s（可选 sqlite / mysql / postgres）", c.DB.Driver)
	}
	switch c.Storage.Kind() {
	case "local", "s3":
	default:
		return fmt.Errorf("不支持的存储类型: %s（可选 auto / local / s3）", c.Storage.Type)
	}
	if mode := strings.ToLower(strings.TrimSpace(c.Runtime.Stateless)); mode != "" &&
		mode != "auto" && mode != "strict" && mode != "off" {
		return fmt.Errorf("runtime.stateless 非法: %s（可选 auto / strict / off）", c.Runtime.Stateless)
	}
	switch mode := strings.ToLower(strings.TrimSpace(c.Migration.Mode)); mode {
	case "", "versioned", "auto", "off", "none", "disabled":
	default:
		return fmt.Errorf("migration.mode 非法: %s（可选 versioned / auto / off）", c.Migration.Mode)
	}
	if c.Backup.Retain <= 0 {
		// 未配置时补默认值，兼容直接构造 Config 的调用方（如单测）
		c.Backup.Retain = 7
	}
	if seen := map[string]bool{}; len(c.OAuth.Providers) > 0 {
		for _, p := range c.OAuth.Providers {
			name := strings.ToLower(strings.TrimSpace(p.Name))
			if name == "" {
				return fmt.Errorf("oauth.providers 存在未命名的配置项")
			}
			if seen[name] {
				return fmt.Errorf("oauth.providers 名称重复: %s", name)
			}
			seen[name] = true
			for label, u := range map[string]string{"auth_url": p.AuthURL, "token_url": p.TokenURL, "user_info_url": p.UserInfoURL} {
				if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
					return fmt.Errorf("oauth.providers.%s.%s 必须是 http(s) URL", name, label)
				}
			}
		}
	}
	if c.Mail.Enabled != nil && *c.Mail.Enabled && strings.TrimSpace(c.Mail.Host) == "" {
		return fmt.Errorf("mail.enabled 为 true 但 mail.host 为空")
	}
	if c.App.Env == "prod" {
		if len(c.JWT.Secret) < 32 {
			return fmt.Errorf("生产环境 jwt.secret 长度必须不少于 32 位")
		}
		// 网关不签发令牌，无需校验密钥强度
		if !strings.EqualFold(c.Micro.Mode, "gateway") && c.JWT.Secret == DefaultJWTSecret {
			return fmt.Errorf("生产环境必须显式配置 jwt.secret，不能使用内置默认值")
		}
	}
	if c.JWT.AccessTTL <= 0 {
		c.JWT.AccessTTL = 2 * time.Hour
	}
	if c.JWT.RefreshTTL <= 0 {
		c.JWT.RefreshTTL = 7 * 24 * time.Hour
	}
	if c.Server.ShutdownTimeout <= 0 {
		c.Server.ShutdownTimeout = 15 * time.Second
	}
	return nil
}

// IsProd 判断是否生产环境。
func (c *Config) IsProd() bool { return c.App.Env == "prod" }

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "Keel 企业服务端框架")
	v.SetDefault("app.env", "dev")
	v.SetDefault("app.version", "1.0.0")
	v.SetDefault("app.timezone", "Asia/Shanghai")

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 9000)
	v.SetDefault("server.mode", "release")
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "60s")
	v.SetDefault("server.idle_timeout", "120s")
	v.SetDefault("server.shutdown_timeout", "15s")
	v.SetDefault("server.max_body_size", 32<<20)
	v.SetDefault("server.trust_proxy", false)
	v.SetDefault("server.web_dir", "web")

	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("log.dir", "logs")
	v.SetDefault("log.filename", "keel.log")
	v.SetDefault("log.max_size_mb", 64)
	v.SetDefault("log.max_age_days", 14)
	v.SetDefault("log.max_backups", 30)
	v.SetDefault("log.compress", true)
	v.SetDefault("log.console", true)
	v.SetDefault("log.caller", true)

	// driver 留空表示按 dsn 自动推断，单机默认落到 SQLite
	v.SetDefault("db.driver", "")
	v.SetDefault("db.dsn", "data/keel.db")
	v.SetDefault("db.max_open_conns", 50)
	v.SetDefault("db.max_idle_conns", 10)
	v.SetDefault("db.conn_max_lifetime", "1h")
	v.SetDefault("db.conn_max_idle_time", "10m")
	v.SetDefault("db.slow_threshold", "200ms")
	v.SetDefault("db.log_level", "warn")
	v.SetDefault("db.auto_migrate", true)
	v.SetDefault("db.seed", true)

	// addr 留空表示使用进程内缓存；填写地址即自动启用 Redis
	v.SetDefault("redis.addr", "")
	v.SetDefault("redis.pool_size", 32)
	v.SetDefault("redis.prefix", "keel")
	v.SetDefault("redis.dial_timeout", "3s")
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.local_size", 100000)

	v.SetDefault("jwt.issuer", "keel")
	v.SetDefault("jwt.access_ttl", "2h")
	v.SetDefault("jwt.refresh_ttl", "168h")

	v.SetDefault("security.captcha_enabled", false)
	v.SetDefault("security.max_login_fail", 5)
	v.SetDefault("security.lock_duration", "15m")
	v.SetDefault("security.password_min_len", 8)
	v.SetDefault("security.password_strength", 2)
	v.SetDefault("security.session_single", false)

	v.SetDefault("storage.type", "auto")
	v.SetDefault("storage.local_dir", "data/uploads")
	v.SetDefault("storage.base_url", "/uploads")
	v.SetDefault("storage.max_size", 20<<20)

	v.SetDefault("mail.port", 587)
	v.SetDefault("mail.tls", true)
	v.SetDefault("mail.from", "noreply@localhost")

	v.SetDefault("cors.allow_origins", []string{"*"})
	v.SetDefault("cors.allow_methods", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allow_headers", []string{"Origin", "Content-Type", "Authorization", "X-Requested-With", "X-Trace-Id"})
	v.SetDefault("cors.expose_headers", []string{"X-Trace-Id", "Content-Disposition"})
	v.SetDefault("cors.allow_credentials", false)
	v.SetDefault("cors.max_age", 86400)

	v.SetDefault("rate_limit.enabled", true)
	v.SetDefault("rate_limit.rps", 200)
	v.SetDefault("rate_limit.burst", 400)
	v.SetDefault("rate_limit.by_user", true)

	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.path", "/metrics")

	v.SetDefault("micro.mode", "mono")
	v.SetDefault("micro.timeout", "5s")
	v.SetDefault("micro.retries", 2)
	v.SetDefault("micro.breaker.enabled", true)
	v.SetDefault("micro.breaker.max_failed", 10)
	v.SetDefault("micro.breaker.interval", 30)
	v.SetDefault("micro.breaker.timeout", 15)
	v.SetDefault("micro.registry.type", "static")
	v.SetDefault("micro.registry.interval", "30s")

	v.SetDefault("runtime.stateless", "auto")
	v.SetDefault("runtime.instances", 1)

	v.SetDefault("news.retention_days", 7)

	v.SetDefault("migration.mode", "versioned")
	v.SetDefault("migration.table_name", "keel_schema_migrations")

	v.SetDefault("backup.enabled", true)
	v.SetDefault("backup.dir", "data/backups")
	v.SetDefault("backup.retain", 7)
	v.SetDefault("backup.compress", true)
	v.SetDefault("backup.mysql_dump", "mysqldump")
	v.SetDefault("backup.mysql_restore", "mysql")
	v.SetDefault("backup.pg_dump", "pg_dump")
	v.SetDefault("backup.pg_restore", "psql")
	v.SetDefault("backup.allow_restore", false)

	v.SetDefault("oauth.enabled", false)
	v.SetDefault("oauth.frontend_path", "/oauth/callback")
	v.SetDefault("oauth.state_ttl", "10m")

	// docs.enabled 不设默认值：保持 nil 三态，由环境（dev/prod）自动判断
	v.SetDefault("docs.path", "/docs")
}
