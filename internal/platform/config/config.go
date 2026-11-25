// Package config 基于 Viper 提供分层配置：默认值 -> 配置文件 -> 环境变量 -> 动态覆盖。
// 环境变量约定：TENON_SERVER_PORT 对应 server.port（点号替换为下划线）。
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
	I18n      I18n      `mapstructure:"i18n"`
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
	Modules   Modules   `mapstructure:"modules"`
	Runtime   Runtime   `mapstructure:"runtime"`
	News      News      `mapstructure:"news"`
	Migration Migration `mapstructure:"migration"`
	Backup    Backup    `mapstructure:"backup"`
	OAuth     OAuth     `mapstructure:"oauth"`
	Docs      Docs      `mapstructure:"docs"`
	Cluster   Cluster   `mapstructure:"cluster"`

	// 可选能力：自助注册/短信登录、短信网关、支付网关。
	// 默认全部关闭；只有装配了 cnauth / recharge 模块时才需要填。
	CN      CN      `mapstructure:"cn"`
	SMS     SMS     `mapstructure:"sms"`
	Payment Payment `mapstructure:"payment"`
}

// Cluster 多节点协同参数。
//
// 节点身份用于在共享数据库里登记自己，重启后必须保持不变，因此默认取
// runtime.instance_id，留空时用 hostname:port 推导。所有节点必须连接
// 同一个数据库；SQLite 部署会自动退化为单节点（无跨进程协调能力）。
type Cluster struct {
	Enabled       bool          `mapstructure:"enabled"`
	Name          string        `mapstructure:"name"`
	AdvertiseAddr string        `mapstructure:"advertise_addr"`
	Heartbeat     time.Duration `mapstructure:"heartbeat"`
	OfflineAfter  time.Duration `mapstructure:"offline_after"`
}

// UseCluster 判断是否启用节点登记与指令下发。
func (c *Config) UseCluster() bool {
	return c.Cluster.Enabled && !strings.EqualFold(strings.TrimSpace(c.DB.ResolveDriver()), "sqlite")
}

// I18n 语言协商参数。
// 支持的语言由 pkg/i18n 决定；此处只配置客户端未声明语言时的兜底。
type I18n struct {
	DefaultLocale string `mapstructure:"default_locale"`
}

// Modules 业务模块启停参数。
// Enabled 为空时全部启用；非空时只启用列出的模块（自动补齐依赖，基础模块常驻）。
// 与 micro.modules 的区别：后者只在 micro.mode=service 生效，本项在单体模式也能裁剪。
type Modules struct {
	Enabled []string `mapstructure:"enabled"`
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
	return "tenon_schema_migrations"
}

// Backup 数据库备份参数。
//
// 备份产物落在 Dir，文件名形如 tenon-sqlite-20261112-152524.db.gz；
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

	// GFS 分档保留：按小时/天/周/月的「槽位」各留 N 份，避免断档时把窗口内唯一备份删掉。
	// 均为 0 时退回旧行为（仅按 Retain 保留最近 N 份）；无论哪档都永不删除最新一份。
	RetainHourly  int `mapstructure:"retain_hourly"`
	RetainDaily   int `mapstructure:"retain_daily"`
	RetainWeekly  int `mapstructure:"retain_weekly"`
	RetainMonthly int `mapstructure:"retain_monthly"`
}

// UseGFS 是否启用了分档保留策略。
func (b Backup) UseGFS() bool {
	return b.RetainHourly > 0 || b.RetainDaily > 0 || b.RetainWeekly > 0 || b.RetainMonthly > 0
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
// 敏感凭据建议用环境变量 TENON_OAUTH_<NAME>_CLIENT_ID / _CLIENT_SECRET 覆盖，
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

	// ---- 国内渠道协议适配 ----
	// Protocol 协议适配器：空 / standard 走通用 OAuth2；wechat / qq 走专用流程。
	Protocol string `mapstructure:"protocol"`
	// AuthorizeSuffix 附加到授权 URL 末尾的片段，微信扫码登录需要 #wechat_redirect。
	AuthorizeSuffix string `mapstructure:"authorize_suffix"`
	// ExtraTokenParams 令牌交换时附加的参数（微信 appid/secret，QQ fmt=json）。
	ExtraTokenParams map[string]string `mapstructure:"extra_token_params"`
	// TokenParamsInQuery 为 true 时令牌交换参数放 query 而非表单 body。
	TokenParamsInQuery bool `mapstructure:"token_params_in_query"`
	// TokenResponseJSON 为 false 时按 URL 查询串解析令牌响应（QQ 默认格式）。
	TokenResponseJSON *bool `mapstructure:"token_response_json"`
	// ProfileQuery 用户信息接口的附加 query 参数（模板支持 {token} 与 {openid}）。
	ProfileQuery map[string]string `mapstructure:"profile_query"`
	// ProfilePrefetchURL 拉取用户信息前先请求的端点（QQ 的 /oauth2.0/me），用于取 openid。
	ProfilePrefetchURL string `mapstructure:"profile_prefetch_url"`
	// ProfilePrefetchFields 从 prefetch 响应中提取的字段映射，如 {openid: "openid"}。
	ProfilePrefetchFields map[string]string `mapstructure:"profile_prefetch_fields"`
	// ProfileJSONP 为 true 时 prefetch 响应是 callback({...}) 形式，需剥壳。
	ProfileJSONP bool `mapstructure:"profile_jsonp"`
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
//
// cfg 指针在进程生命周期内保持不变：热更新时直接覆盖结构体字段，
// 这样所有持有 *Config 的组件（认证、文件、监控等）无需重新装配即可
// 读到新值。代价是字段写入与业务读取之间没有锁保护，只适用于
// 标量/短字符串这类可以整体替换的字段（本文件注册的覆盖项满足该条件）。
type Holder struct {
	mu        sync.RWMutex
	cfg       *Config
	v         *viper.Viper
	path      string
	overrides map[string]any
	onApply   []func(*Config)
}

var holder = &Holder{}

// Load 加载配置；文件不存在时使用内置默认值，保证开箱即用。
func Load(path string) (*Config, error) {
	v, cfg, err := build(path, nil)
	if err != nil {
		return nil, err
	}
	holder.mu.Lock()
	holder.cfg, holder.v, holder.path, holder.overrides = cfg, v, path, nil
	holder.mu.Unlock()
	return cfg, nil
}

// Apply 用运行时覆盖值重建配置并立即生效。
//
// overrides 的键是 mapstructure 点分路径（如 security.captcha_enabled），
// 值可以是 string / bool / int / float64 / []string。覆盖值优先级高于
// 配置文件与默认值，低于显式环境变量绑定之外的部分——管理端可编辑项
// 不应同时用环境变量固定，否则以环境变量为准的部署约定会被打破。
func Apply(overrides map[string]any) (*Config, error) {
	holder.mu.RLock()
	path := holder.path
	holder.mu.RUnlock()
	_, cfg, err := build(path, overrides)
	if err != nil {
		return nil, err
	}
	holder.mu.Lock()
	if holder.cfg == nil {
		holder.cfg = cfg
	} else {
		*holder.cfg = *cfg
		cfg = holder.cfg
	}
	// 不替换 holder.v：文件监听绑定在 Load 时的 viper 实例上，
	// 换成新实例会让 Watch 静默失效。重建配置只需要 overrides。
	holder.overrides = overrides
	cbs := append([]func(*Config){}, holder.onApply...)
	holder.mu.Unlock()
	for _, cb := range cbs {
		cb(cfg)
	}
	return cfg, nil
}

// build 构建一份独立配置：默认值 -> 配置文件 -> 环境变量 -> 运行时覆盖。
func build(path string, overrides map[string]any) (*viper.Viper, *Config, error) {
	v := viper.New()
	setDefaults(v)
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			var nf viper.ConfigFileNotFoundError
			if !errors.As(err, &nf) {
				return nil, nil, fmt.Errorf("读取配置文件失败: %w", err)
			}
		}
	}
	v.SetEnvPrefix("TENON")
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
		"cluster.enabled", "cluster.name", "cluster.advertise_addr",
		// 可选模块（cnauth / recharge）用到的凭据，容器里直接注入
		"cn.register_enabled", "cn.sms_login_enabled", "cn.code_ttl", "cn.code_length", "cn.daily_send_limit",
		"sms.enabled", "sms.provider", "sms.access_key_id", "sms.access_key_secret", "sms.sign_name",
		"sms.template_code", "sms.region", "sms.endpoint",
		"payment.enabled",
		"payment.wechat.enabled", "payment.wechat.app_id", "payment.wechat.mch_id", "payment.wechat.api_key",
		"payment.wechat.notify_url", "payment.wechat.sign_type", "payment.wechat.sandbox",
		"payment.alipay.enabled", "payment.alipay.app_id", "payment.alipay.private_key",
		"payment.alipay.public_key", "payment.alipay.notify_url", "payment.alipay.return_url", "payment.alipay.sandbox",
	} {
		_ = v.BindEnv(key)
	}

	cfg := &Config{}
	for k, val := range overrides {
		// viper 的 Set 支持点分路径，且优先级高于配置文件与环境变量。
		// 管理端覆盖项只包含非部署类参数，部署类（DB/Redis/端口/密钥）
		// 不在可编辑清单内，因此不会与容器环境变量约定冲突。
		v.Set(k, val)
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, nil, fmt.Errorf("解析配置失败: %w", err)
	}
	applyEnvFallbacks(cfg)
	applyMicroRoutesEnv(cfg)
	applyOAuthEnvOverrides(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	return v, cfg, nil
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
		holder.mu.RLock()
		overrides := holder.overrides
		holder.mu.RUnlock()
		// 文件变化后仍叠加运行时覆盖：管理端配置的优先级高于本地文件，
		// 否则改一次文件就会把管理端设置悄悄打回。
		_, cfg, err := build(path, overrides)
		if err != nil {
			return
		}
		holder.mu.Lock()
		if holder.cfg == nil {
			holder.cfg = cfg
		} else {
			*holder.cfg = *cfg
			cfg = holder.cfg
		}
		cbs := append([]func(*Config){}, holder.onApply...)
		holder.mu.Unlock()
		for _, cb := range cbs {
			cb(cfg)
		}
	})
	v.WatchConfig()
}

// applyMicroRoutesEnv 支持用环境变量提供网关转发规则，
// 形如 TENON_GATEWAY_ROUTES="/api/v1=tenon-core,/uploads=tenon-file"。
// 配置文件中已显式声明 routes 时以文件为准。
func applyMicroRoutesEnv(c *Config) {
	if len(c.Micro.Routes) > 0 {
		return
	}
	raw := strings.TrimSpace(os.Getenv("TENON_GATEWAY_ROUTES"))
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
const DefaultJWTSecret = "tenon-dev-secret-please-change-in-production"

// applyEnvFallbacks 处理未绑定到结构体字段的关键环境变量（如密钥）。
func applyEnvFallbacks(c *Config) {
	if c.JWT.Secret == "" {
		c.JWT.Secret = DefaultJWTSecret
	}
	if c.Security.DataEncryptKey == "" {
		c.Security.DataEncryptKey = c.JWT.Secret
	}
	if c.Security.InternalToken == "" {
		c.Security.InternalToken = "tenon-internal-token"
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
// 约定：TENON_OAUTH_GITHUB_CLIENT_ID / TENON_OAUTH_GITHUB_CLIENT_SECRET，
// 名称中的非字母数字统一转为下划线。仅配置了一个 provider 时也可用
// TENON_OAUTH_CLIENT_ID / TENON_OAUTH_CLIENT_SECRET 简写。
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
		prefix := "TENON_OAUTH_" + envKey(p.Name) + "_"
		id, secret := os.Getenv(prefix+"CLIENT_ID"), os.Getenv(prefix+"CLIENT_SECRET")
		if len(c.OAuth.Providers) == 1 {
			if v := os.Getenv("TENON_OAUTH_CLIENT_ID"); v != "" {
				id = v
			}
			if v := os.Getenv("TENON_OAUTH_CLIENT_SECRET"); v != "" {
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
	for label, n := range map[string]int{
		"retain_hourly": c.Backup.RetainHourly, "retain_daily": c.Backup.RetainDaily,
		"retain_weekly": c.Backup.RetainWeekly, "retain_monthly": c.Backup.RetainMonthly,
	} {
		if n < 0 {
			return fmt.Errorf("backup.%s 不能为负数", label)
		}
	}
	if seen := map[string]bool{}; len(c.OAuth.Providers) > 0 {
		if c.App.Env == "prod" && strings.TrimSpace(c.OAuth.CallbackBase) == "" {
			return fmt.Errorf("生产环境启用 OAuth 时必须配置 oauth.callback_base，不能由 Host 头推导回调地址")
		}
		for _, p := range c.OAuth.Providers {
			name := strings.ToLower(strings.TrimSpace(p.Name))
			if name == "" {
				return fmt.Errorf("oauth.providers 存在未命名的配置项")
			}
			if seen[name] {
				return fmt.Errorf("oauth.providers 名称重复: %s", name)
			}
			seen[name] = true
			switch strings.ToLower(strings.TrimSpace(p.Protocol)) {
			case "", "standard", "wechat", "qq":
			default:
				return fmt.Errorf("oauth.providers.%s.protocol 非法: %s（可选 standard / wechat / qq）", name, p.Protocol)
			}
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
	// 可选模块（cnauth / recharge）参数：未装配模块时这些值不参与启动，
	// 但校验一视同仁，避免配置写错却到运行时才暴露。
	if c.CN.CodeLength <= 0 {
		c.CN.CodeLength = 6
	}
	if c.CN.CodeLength < 4 || c.CN.CodeLength > 8 {
		return fmt.Errorf("cn.code_length 需在 4-8 之间")
	}
	if c.CN.CodeTTL <= 0 {
		c.CN.CodeTTL = 5 * time.Minute
	}
	if c.CN.DailySendLimit <= 0 {
		c.CN.DailySendLimit = 10
	}
	if c.CN.SMSLoginEnabled && !c.SMS.Use() {
		return fmt.Errorf("cn.sms_login_enabled 为 true 但未配置短信网关（sms.provider）")
	}
	switch strings.ToLower(strings.TrimSpace(c.SMS.Provider)) {
	case "", "mock", "aliyun", "tencent":
	default:
		return fmt.Errorf("sms.provider 非法: %s（可选 mock / aliyun / tencent）", c.SMS.Provider)
	}
	if c.Payment.Enabled {
		if c.Payment.Wechat.Enabled {
			w := c.Payment.Wechat
			if w.AppID == "" || w.MchID == "" || w.APIKey == "" {
				return fmt.Errorf("payment.wechat 已启用但 app_id / mch_id / api_key 未配置完整")
			}
			switch strings.ToUpper(w.SignType) {
			case "MD5", "HMAC-SHA256":
			default:
				return fmt.Errorf("payment.wechat.sign_type 非法: %s（可选 MD5 / HMAC-SHA256）", w.SignType)
			}
		}
		if c.Payment.Alipay.Enabled {
			a := c.Payment.Alipay
			if a.AppID == "" || a.PrivateKey == "" || a.PublicKey == "" {
				return fmt.Errorf("payment.alipay 已启用但 app_id / private_key / public_key 未配置完整")
			}
		}
	}
	if c.App.Env == "prod" {
		if len(c.JWT.Secret) < 32 {
			return fmt.Errorf("生产环境 jwt.secret 长度必须不少于 32 位")
		}
		// 网关不签发令牌，无需校验密钥强度。
		// 仓库自带配置里的占位串长度足够但不能当密钥，否则任何人都能自签超管令牌。
		if !strings.EqualFold(c.Micro.Mode, "gateway") {
			for _, weak := range []string{
				DefaultJWTSecret,
				"请务必在生产环境替换为至少32位的随机字符串-tenon-default-secret",
			} {
				if strings.EqualFold(strings.TrimSpace(c.JWT.Secret), weak) {
					return fmt.Errorf("生产环境必须显式配置 jwt.secret，不能使用内置默认值或仓库占位符")
				}
			}
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
	if c.Cluster.Heartbeat <= 0 {
		c.Cluster.Heartbeat = 15 * time.Second
	}
	if c.Cluster.OfflineAfter <= c.Cluster.Heartbeat {
		// 离线阈值必须显著大于心跳间隔，否则一次网络抖动就会把节点误判为离线。
		c.Cluster.OfflineAfter = c.Cluster.Heartbeat * 3
	}
	return nil
}

// IsProd 判断是否生产环境。
func (c *Config) IsProd() bool { return c.App.Env == "prod" }

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.name", "Tenon 企业服务端框架")
	v.SetDefault("app.env", "dev")
	v.SetDefault("app.version", "1.0.0")
	v.SetDefault("app.timezone", "Asia/Shanghai")
	v.SetDefault("i18n.default_locale", "zh-CN")

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
	v.SetDefault("log.filename", "tenon.log")
	v.SetDefault("log.max_size_mb", 64)
	v.SetDefault("log.max_age_days", 14)
	v.SetDefault("log.max_backups", 30)
	v.SetDefault("log.compress", true)
	v.SetDefault("log.console", true)
	v.SetDefault("log.caller", true)

	// driver 留空表示按 dsn 自动推断，单机默认落到 SQLite
	v.SetDefault("db.driver", "")
	v.SetDefault("db.dsn", "data/tenon.db")
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
	v.SetDefault("redis.prefix", "tenon")
	v.SetDefault("redis.dial_timeout", "3s")
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.local_size", 100000)

	v.SetDefault("jwt.issuer", "tenon")
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
	v.SetDefault("migration.table_name", "tenon_schema_migrations")

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

	v.SetDefault("cluster.enabled", true)
	v.SetDefault("cluster.heartbeat", "15s")
	v.SetDefault("cluster.offline_after", "45s")

	// 可选模块默认值：注册/短信登录/支付全部关闭，升级不会意外开放入口。
	v.SetDefault("cn.register_enabled", false)
	v.SetDefault("cn.sms_login_enabled", false)
	v.SetDefault("cn.code_ttl", "5m")
	v.SetDefault("cn.code_length", 6)
	v.SetDefault("cn.daily_send_limit", 10)
	v.SetDefault("sms.provider", "mock")
	v.SetDefault("sms.region", "cn-hangzhou")
	v.SetDefault("payment.enabled", false)
	v.SetDefault("payment.wechat.sign_type", "MD5")
}
