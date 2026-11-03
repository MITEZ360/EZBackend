package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置失败: %v", err)
	}
	return p
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("加载默认配置失败: %v", err)
	}
	if cfg.Server.Port != 9000 {
		t.Fatalf("默认端口错误: %d", cfg.Server.Port)
	}
	if cfg.DB.Driver != "sqlite" || cfg.Micro.Mode != "mono" {
		t.Fatalf("默认驱动/形态错误: %s %s", cfg.DB.Driver, cfg.Micro.Mode)
	}
	if cfg.JWT.AccessTTL != 2*time.Hour {
		t.Fatalf("默认令牌有效期错误: %v", cfg.JWT.AccessTTL)
	}
	if cfg.JWT.Secret == "" {
		t.Fatal("未填充默认密钥")
	}
	if cfg.Security.DataEncryptKey != cfg.JWT.Secret {
		t.Fatal("未配置加密密钥时应复用 JWT 密钥")
	}
}

func TestLoadFileOverride(t *testing.T) {
	p := writeConfig(t, `
app:
  name: 测试应用
  env: test
server:
  port: 18081
db:
  driver: postgres
  dsn: "host=127.0.0.1"
micro:
  mode: gateway
  routes:
    - prefix: /api/v1
      service: core
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.App.Name != "测试应用" || cfg.Server.Port != 18081 || cfg.DB.Driver != "postgres" {
		t.Fatalf("配置覆盖失败: %+v", cfg)
	}
	if cfg.Micro.Mode != "gateway" || len(cfg.Micro.Routes) != 1 || cfg.Micro.Routes[0].Service != "core" {
		t.Fatalf("微服务配置解析失败: %+v", cfg.Micro)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("KEEL_SERVER_PORT", "19000")
	t.Setenv("KEEL_APP_ENV", "test")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.Server.Port != 19000 {
		t.Fatalf("环境变量未生效: %d", cfg.Server.Port)
	}
	if cfg.App.Env != "test" {
		t.Fatalf("环境变量未生效: %s", cfg.App.Env)
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{Server: Server{Port: 0}}).Validate(); err == nil {
		t.Fatal("非法端口应报错")
	}
	bad := &Config{App: App{Env: "prod"}, Server: Server{Port: 9000}, JWT: JWT{Secret: DefaultJWTSecret, AccessTTL: time.Hour}}
	if err := bad.Validate(); err == nil {
		t.Fatal("生产环境使用默认密钥应报错")
	}
	good := &Config{App: App{Env: "prod"}, Server: Server{Port: 9000}, JWT: JWT{Secret: "a-very-long-and-random-production-secret-value"}}
	if err := good.Validate(); err != nil {
		t.Fatalf("合规配置不应报错: %v", err)
	}
	// 网关不签发令牌，允许使用默认密钥
	gw := &Config{App: App{Env: "prod"}, Server: Server{Port: 9000}, JWT: JWT{Secret: DefaultJWTSecret},
		Micro: Micro{Mode: "gateway"}}
	if err := gw.Validate(); err != nil {
		t.Fatalf("网关形态不应校验令牌密钥: %v", err)
	}
	badDriver := &Config{Server: Server{Port: 9000}, DB: DB{Driver: "oracle"}, JWT: JWT{Secret: "x"}}
	if err := badDriver.Validate(); err == nil {
		t.Fatal("不支持的数据库驱动应报错")
	}
}

func TestGetAndOnChange(t *testing.T) {
	if _, err := Load(""); err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if Get() == nil || Get().Server.Port == 0 {
		t.Fatal("Get 应返回当前配置")
	}
	// 注册回调不应 panic；热更新由文件监听触发，见 docs/配置说明
	OnChange(func(*Config) {})
}

func TestMicroRoutesFromEnv(t *testing.T) {
	t.Setenv("KEEL_MICRO_MODE", "gateway")
	t.Setenv("KEEL_GATEWAY_ROUTES", "/api/v1=keel-core, /uploads=keel-file")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.Micro.Mode != "gateway" {
		t.Fatalf("形态未生效: %s", cfg.Micro.Mode)
	}
	if len(cfg.Micro.Routes) != 2 {
		t.Fatalf("路由解析失败: %+v", cfg.Micro.Routes)
	}
	if cfg.Micro.Routes[1].Prefix != "/uploads" || cfg.Micro.Routes[1].Service != "keel-file" {
		t.Fatalf("路由内容错误: %+v", cfg.Micro.Routes[1])
	}
}

func TestMicroModulesFromEnv(t *testing.T) {
	t.Setenv("KEEL_MICRO_MODE", "service")
	t.Setenv("KEEL_MICRO_MODULES", "user,role,dept")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(cfg.Micro.Modules) != 3 || cfg.Micro.Modules[0] != "user" {
		t.Fatalf("模块列表解析失败: %+v", cfg.Micro.Modules)
	}
}
