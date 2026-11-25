package bootstrap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"

	// 测试进程同样需要发行版清单，否则注册表为空。
	_ "github.com/MITEZ360/tenon/internal/distribution"
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
	for _, name := range []string{"auth", "user", "message", "maintenance"} {
		if !reg.Enabled[name] {
			t.Errorf("all 模式应启用 %s", name)
		}
	}
	if len(reg.Enabled) != len(modulekit.All()) {
		t.Fatalf("启用数 %d != 模块总数 %d", len(reg.Enabled), len(modulekit.All()))
	}
	if len(reg.specs) != len(reg.Ordered) {
		t.Fatalf("声明数与顺序数不一致: %d vs %d", len(reg.specs), len(reg.Ordered))
	}
}

func TestResolveModulesEnabledWithDependencies(t *testing.T) {
	// 只显式启用 membership，应自动补齐 user → role/dept/menu 依赖与全部常驻模块。
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{"membership"}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Source != "modules.enabled" {
		t.Fatalf("source = %q", reg.Source)
	}
	for _, name := range []string{"auth", "menu", "config", "dict", "notice", "role", "dept", "user", "membership"} {
		if !reg.Enabled[name] {
			t.Errorf("应启用 %s（依赖补齐或基础模块）", name)
		}
	}
	if reg.Enabled["monitor"] || reg.Enabled["cluster"] {
		t.Error("未显式启用且非依赖的模块不应被启用")
	}
}

func TestResolveModulesAlwaysOn(t *testing.T) {
	cfg := &config.Config{}
	cfg.Modules.Enabled = []string{"dashboard"}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth", "menu", "config", "dict", "notice"} {
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
	cfg.Modules.Enabled = []string{"membership"} // service 模式下应被 micro.modules 覆盖
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Source != "micro.modules" {
		t.Fatalf("source = %q, want micro.modules", reg.Source)
	}
	if !reg.Enabled["file"] || reg.Enabled["membership"] {
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

func TestOrderedDependenciesFirst(t *testing.T) {
	cfg := &config.Config{}
	reg, err := resolveModules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pos := make(map[string]int, len(reg.Ordered))
	for i, name := range reg.Ordered {
		pos[name] = i
	}
	// 装配顺序必须保证被依赖者先 Setup，否则 Setup 里 Lookup 拿不到服务。
	for _, spec := range modulekit.All() {
		for _, dep := range spec.DependsOn {
			if pos[dep] > pos[spec.Name] {
				t.Errorf("%s 依赖 %s，但 %s 排在前面", spec.Name, dep, spec.Name)
			}
		}
	}
}

func TestTopoOrderRejectsMissingDependency(t *testing.T) {
	specs := []modulekit.Spec{
		{Name: "a", DependsOn: []string{"ghost"}},
	}
	_, err := topoOrder(specs, map[string]bool{"a": true})
	if err == nil || !strings.Contains(err.Error(), "不在当前发行版中") {
		t.Fatalf("缺失依赖应在解析阶段报错，got %v", err)
	}
}

func TestTopoOrderRejectsCycle(t *testing.T) {
	specs := []modulekit.Spec{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	}
	_, err := topoOrder(specs, map[string]bool{"a": true, "b": true})
	if err == nil || !strings.Contains(err.Error(), "成环") {
		t.Fatalf("循环依赖应报错，got %v", err)
	}
}

func TestMenuTreeFollowsModuleSwitch(t *testing.T) {
	all := map[string]bool{}
	for _, spec := range modulekit.All() {
		all[spec.Name] = true
	}
	full := collectMenuIDs(modulekit.Menus(all))
	for _, id := range []string{"m_ops", "m_file", "m_file_del", "m_system", "m_user"} {
		if !full[id] {
			t.Errorf("全部启用时应包含菜单 %s", id)
		}
	}

	// 只留常驻模块：业务菜单与其私有目录都应消失。
	base := map[string]bool{"auth": true, "menu": true, "dict": true, "config": true, "notice": true}
	trimmed := collectMenuIDs(modulekit.Menus(base))
	if trimmed["m_file"] || trimmed["m_ops"] {
		t.Error("关闭 file 后其页面与私有目录应一并消失")
	}
	if !trimmed["m_dict"] {
		t.Error("常驻模块的菜单应保留")
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
	var hasUser, hasFile bool
	for _, m := range got {
		name := fmt.Sprintf("%T", m)
		if strings.Contains(name, "model.User") {
			hasUser = true
		}
		if strings.Contains(name, "model.File") {
			hasFile = true
		}
	}
	if !hasUser {
		t.Error("底座表 User 必须始终参与建表")
	}
	if hasFile {
		t.Error("未启用的 file 模块不应参与建表")
	}
}

func TestOnDefaultsToEnabled(t *testing.T) {
	var nilReg *ModuleRegistry
	if !nilReg.On("file") {
		t.Error("空注册表应视为全部启用")
	}
	reg := &ModuleRegistry{Enabled: map[string]bool{"file": true}}
	if !reg.On("file") || reg.On("message") {
		t.Error("On 应严格反映启用集合")
	}
}

func collectMenuIDs(nodes []modulekit.MenuNode) map[string]bool {
	out := map[string]bool{}
	modulekit.WalkMenus(nodes, func(m modulekit.Menu) { out[m.ID] = true })
	return out
}
