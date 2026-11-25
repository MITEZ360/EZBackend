package bootstrap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/modulekit"
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
	if len(reg.Enabled) != len(modulekit.Specs) {
		t.Fatalf("启用数 %d != 模块总数 %d", len(reg.Enabled), len(modulekit.Specs))
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

func TestResolveModulesClusterNeedsConfigCenter(t *testing.T) {
	// 集群的配置下发依赖配置中心，单独启用 cluster 必须自动带上 configcenter。
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{"cluster"}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Enabled["configcenter"] {
		t.Fatal("启用 cluster 应自动补齐 configcenter 依赖")
	}
}

func TestFilterMenusDropsDisabledModule(t *testing.T) {
	enabled := map[string]bool{"dashboard": true, "config": true}
	got := filterMenus(menuSeeds, enabled)

	kept := map[string]bool{}
	var walk func([]menuSeed)
	walk = func(items []menuSeed) {
		for _, it := range items {
			kept[it.ID] = true
			walk(it.Children)
		}
	}
	walk(got)

	if !kept["m_dashboard"] {
		t.Error("启用的 dashboard 菜单应保留")
	}
	if kept["m_news"] || kept["m_news_del"] {
		t.Error("关闭的 news 菜单及其按钮应被裁掉")
	}
	if kept["m_content"] {
		t.Error("子项被裁光的资讯中心目录应一并消失")
	}
	if !kept["m_config"] {
		t.Error("常驻的底座菜单应保留")
	}
}

func TestFilterMenusKeepsAllWhenEnabled(t *testing.T) {
	all := map[string]bool{}
	for _, spec := range modulekit.Specs {
		all[spec.Name] = true
	}
	var countAll, countFiltered int
	var walk func([]menuSeed, *int)
	walk = func(items []menuSeed, n *int) {
		for _, it := range items {
			*n++
			walk(it.Children, n)
		}
	}
	walk(menuSeeds, &countAll)
	walk(filterMenus(menuSeeds, all), &countFiltered)
	if countAll != countFiltered {
		t.Fatalf("全部启用时菜单数应不变: %d -> %d", countAll, countFiltered)
	}
}

func TestScopeModels(t *testing.T) {
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{"dashboard"}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.scopeModels()

	// 底座表必须在：dashboard 单独启用时仍然要能登录、建角色。
	var hasUser, hasNews bool
	for _, m := range got {
		name := fmt.Sprintf("%T", m)
		if strings.Contains(name, "model.User") {
			hasUser = true
		}
		if strings.Contains(name, "model.NewsArticle") {
			hasNews = true
		}
	}
	if !hasUser {
		t.Error("底座表 User 必须始终参与建表")
	}
	if hasNews {
		t.Error("未启用的 news 模块不应参与建表")
	}
}

func TestOnDefaultsToEnabled(t *testing.T) {
	var nilReg *ModuleRegistry
	if !nilReg.On("news") {
		t.Error("空注册表应视为全部启用")
	}
	reg := &ModuleRegistry{Enabled: map[string]bool{"news": true}}
	if reg.On("news") != true || reg.On("message") != false {
		t.Error("On 应严格反映启用集合")
	}
}
