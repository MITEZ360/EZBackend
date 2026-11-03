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

// Redis 缓存参数。
type Redis struct {
	Enabled     bool          `mapstructure:"enabled"`
	Addr        string        `mapstructure:"addr"`
	Username    string        `mapstructure:"username"`
	Password    string        `mapstructure:"password"`
	DB          int           `mapstructure:"db"`
	PoolSize    int           `mapstructure:"pool_size"`
	Prefix      string        `mapstructure:"prefix"`
	DialTimeout time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	LocalSize   int           `mapstructure:"local_size"` // 未启用 Redis 时的本地缓存容量
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
type Storage struct {
	Type     string   `mapstructure:"type"` // local / s3
	LocalDir string   `mapstructure:"local_dir"`
	BaseURL  string   `mapstructure:"base_url"`
	MaxSize  int64    `mapstructure:"max_size"`
	AllowExt []string `mapstructure:"allow_ext"`
	S3       S3       `mapstructure:"s3"`
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
type Mail struct {
	Enabled  bool   `mapstructure:"enabled"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
	TLS      bool   `mapstructure:"tls"`
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
	for _, key := range []string{"micro.modules", "micro.registry.endpoints", "storage.allow_ext", "cors.allow_origins"} {
		_ = v.BindEnv(key)
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	applyEnvFallbacks(cfg)
	applyMicroRoutesEnv(cfg)
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
}

// Validate 做启动期关键校验，避免带病运行。
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port 非法: %d", c.Server.Port)
	}
	if c.DB.Driver != "" && c.DB.Driver != "sqlite" && c.DB.Driver != "mysql" && c.DB.Driver != "postgres" {
		return fmt.Errorf("不支持的数据库驱动: %s", c.DB.Driver)
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

	v.SetDefault("db.driver", "sqlite")
	v.SetDefault("db.dsn", "data/keel.db")
	v.SetDefault("db.max_open_conns", 50)
	v.SetDefault("db.max_idle_conns", 10)
	v.SetDefault("db.conn_max_lifetime", "1h")
	v.SetDefault("db.conn_max_idle_time", "10m")
	v.SetDefault("db.slow_threshold", "200ms")
	v.SetDefault("db.log_level", "warn")
	v.SetDefault("db.auto_migrate", true)
	v.SetDefault("db.seed", true)

	v.SetDefault("redis.enabled", false)
	v.SetDefault("redis.addr", "127.0.0.1:6379")
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

	v.SetDefault("storage.type", "local")
	v.SetDefault("storage.local_dir", "data/uploads")
	v.SetDefault("storage.base_url", "/uploads")
	v.SetDefault("storage.max_size", 20<<20)

	v.SetDefault("mail.port", 587)
	v.SetDefault("mail.tls", true)

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
}
