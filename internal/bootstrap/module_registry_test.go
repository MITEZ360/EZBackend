package bootstrap

import (
	"strings"
	"testing"

	"github.com/keel-framework/keel/internal/platform/config"
)

func TestResolveModulesAll(t *testing.T) {
	cfg := &config.Config{}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Source != "all" {
		t.Fatalf("source = %q, want all", reg.Source)
	}
	for _, name := range []string{"auth", "user", "news", "maintenance"} {
		if !reg.Enabled[name] {
			t.Errorf("all 模式应启用 %s", name)
		}
	}
	if len(reg.Enabled) != len(builtinModules) {
		t.Fatalf("启用数 %d != 模块总数 %d", len(reg.Enabled), len(builtinModules))
	}
}

func TestResolveModulesEnabledWithDependencies(t *testing.T) {
	// 只显式启用 news，应自动补齐 apikey（依赖）与四个基础模块
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{"news"}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Source != "modules.enabled" {
		t.Fatalf("source = %q", reg.Source)
	}
	want := []string{"auth", "config", "dict", "notice", "apikey", "news"}
	for _, name := range want {
		if !reg.Enabled[name] {
			t.Errorf("应启用 %s（依赖补齐或基础模块）", name)
		}
	}
	if reg.Enabled["user"] || reg.Enabled["monitor"] {
		t.Error("未显式启用且非依赖的模块不应被启用")
	}
	// 输出顺序稳定按声明排
	if len(reg.Ordered) == 0 || reg.Ordered[0] != "auth" {
		t.Fatalf("Ordered 应从 auth 开始: %v", reg.Ordered)
	}
}

func TestResolveModulesAlwaysOn(t *testing.T) {
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{"dashboard"}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth", "config", "dict", "notice"} {
		if !reg.Enabled[name] {
			t.Errorf("基础模块 %s 必须常驻", name)
		}
	}
}

func TestResolveModulesUnknown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{" nonexistent "}
	_, err := resolveModules(cfg)
	if err == nil || !strings.Contains(err.Error(), "未知模块") {
		t.Fatalf("未知模块应报错，got %v", err)
	}
}

func TestResolveModulesMicroService(t *testing.T) {
	cfg := &config.Config{}
	cfg.Micro.Mode = "service"
	cfg.Micro.Modules = []string{"file"}
	cfg.Modules.Enabled = []string{"news"} // service 模式下应被 micro.modules 覆盖
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Source != "micro.modules" {
		t.Fatalf("source = %q, want micro.modules", reg.Source)
	}
	if !reg.Enabled["file"] || reg.Enabled["news"] {
		t.Fatalf("service 模式应以 micro.modules 为准: %v", reg.Ordered)
	}
}
