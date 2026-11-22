package bootstrap

import (
	"fmt"
	"sort"
	"strings"

	"github.com/keel-framework/keel/internal/platform/config"
)

// Module 是业务模块的声明契约。
//
// 本框架当前由 app.go 统一构造所有服务与处理器、router 按 on(module) 挂载路由，
// 因此 Setup/Teardown 在单体装配里是轻量钩子；契约的存在让「按配置启停模块」和
// 「依赖自检」有统一入口，为后续按需裁剪或微服务拆分提供稳定边界。
type Module interface {
	// Name 模块标识，与 router 的 d.on(name) 一致。
	Name() string
	// DependsOn 依赖的模块标识；启用本模块时会自动补齐依赖。
	DependsOn() []string
	// AlwaysOn 基础模块：公开路由无条件挂载，不能裁剪。
	AlwaysOn() bool
}

// moduleSpec 是 Module 的数据实现。
type moduleSpec struct {
	name      string
	dependsOn []string
	alwaysOn  bool
}

func (m moduleSpec) Name() string        { return m.name }
func (m moduleSpec) DependsOn() []string { return append([]string(nil), m.dependsOn...) }
func (m moduleSpec) AlwaysOn() bool      { return m.alwaysOn }

// builtinModules 声明全部可挂载业务模块及其依赖关系。
// alwaysOn 的四项是公开接口（登录、公开配置、字典、公告）所依赖，必须常驻。
var builtinModules = []moduleSpec{
	{name: "auth", alwaysOn: true},
	{name: "config", alwaysOn: true},
	{name: "dict", alwaysOn: true},
	{name: "notice", alwaysOn: true},
	{name: "dashboard"},
	{name: "user", dependsOn: []string{"role", "dept"}},
	{name: "membership", dependsOn: []string{"user"}},
	{name: "apikey"},
	{name: "news", dependsOn: []string{"apikey"}},
	{name: "role", dependsOn: []string{"menu"}},
	{name: "menu"},
	{name: "dept"},
	{name: "message", dependsOn: []string{"notice"}},
	{name: "audit"},
	{name: "job"},
	{name: "file"},
	{name: "monitor"},
	{name: "maintenance"},
}

var moduleIndex = func() map[string]moduleSpec {
	m := make(map[string]moduleSpec, len(builtinModules))
	for _, spec := range builtinModules {
		m[spec.name] = spec
	}
	return m
}()

// ModuleRegistry 解析后的模块注册表。
type ModuleRegistry struct {
	// Enabled 最终启用的模块名集合（含基础模块与自动补齐的依赖）。
	Enabled map[string]bool
	// Ordered 按声明顺序排列的启用模块名，用于日志输出。
	Ordered []string
	// Source 启用来源：all（全部）/ micro.modules / modules.enabled。
	Source string
}

// resolveModules 依据配置解析启用模块：
//  1. micro.mode=service 且 micro.modules 非空时，以 micro.modules 为准（兼容既有微服务拆分）；
//  2. modules.enabled 非空时，作为通用白名单（mono 亦可裁剪）；
//  3. 两者都为空时启用全部模块。
//
// 基础模块强制常驻；显式启用的模块会自动补齐其依赖；引用未知模块直接报错（fail-fast）。
func resolveModules(cfg *config.Config) (*ModuleRegistry, error) {
	var requested []string
	source := "all"
	if strings.EqualFold(cfg.Micro.Mode, "service") && len(cfg.Micro.Modules) > 0 {
		requested = cfg.Micro.Modules
		source = "micro.modules"
	} else if len(cfg.Modules.Enabled) > 0 {
		requested = cfg.Modules.Enabled
		source = "modules.enabled"
	}

	reg := &ModuleRegistry{Enabled: map[string]bool{}, Source: source}
	if len(requested) == 0 {
		for _, spec := range builtinModules {
			reg.Enabled[spec.name] = true
			reg.Ordered = append(reg.Ordered, spec.name)
		}
		return reg, nil
	}

	// 校验显式列出的名字，再递归补齐依赖
	var add func(name string)
	add = func(name string) {
		name = strings.ToLower(strings.TrimSpace(name))
		if reg.Enabled[name] {
			return
		}
		spec, ok := moduleIndex[name]
		if !ok {
			return // 未知名字由下方显式校验处理
		}
		for _, dep := range spec.dependsOn {
			add(dep)
		}
		reg.Enabled[name] = true
	}
	for _, raw := range requested {
		name := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := moduleIndex[name]; !ok {
			return nil, fmt.Errorf("未知模块 %q（可用: %s）", raw, strings.Join(moduleNames(), ", "))
		}
		add(name)
	}
	// 基础模块常驻
	for _, spec := range builtinModules {
		if spec.alwaysOn {
			add(spec.name)
		}
	}
	// 按声明顺序输出，保证日志稳定
	for _, spec := range builtinModules {
		if reg.Enabled[spec.name] {
			reg.Ordered = append(reg.Ordered, spec.name)
		}
	}
	return reg, nil
}

func moduleNames() []string {
	out := make([]string, 0, len(builtinModules))
	for _, spec := range builtinModules {
		out = append(out, spec.name)
	}
	sort.Strings(out)
	return out
}

// asSet 转成 router.Deps.Modules 使用的 map。
func (r *ModuleRegistry) asSet() map[string]bool {
	out := make(map[string]bool, len(r.Enabled))
	for k, v := range r.Enabled {
		out[k] = v
	}
	return out
}
