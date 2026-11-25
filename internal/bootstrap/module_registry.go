package bootstrap

import (
	"fmt"
	"strings"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

// 模块清单由各模块在 init() 中自注册，发行版通过 internal/distribution
// 的空白导入决定编译哪些模块。本文件只负责三件事：解析启用范围、
// 补齐依赖、按依赖顺序排好装配次序。

// ModuleRegistry 解析后的模块注册表。
type ModuleRegistry struct {
	// Enabled 最终启用的模块名集合（含基础模块与自动补齐的依赖）。
	Enabled map[string]bool
	// Ordered 按依赖顺序排列的启用模块名：依赖在前，可直接用于装配。
	Ordered []string
	// Source 启用来源：all（全部）/ micro.modules / modules.enabled。
	Source string
	// specs 与 Ordered 一一对应的模块声明。
	specs []modulekit.Spec
}

// resolveModules 依据配置解析启用模块：
//  1. micro.mode=service 且 micro.modules 非空时，以 micro.modules 为准（兼容既有微服务拆分）；
//  2. modules.enabled 非空时，作为通用白名单（mono 亦可裁剪）；
//  3. 两者都为空时启用全部已注册模块。
//
// 基础模块强制常驻；显式启用的模块会自动补齐其依赖；引用未知模块直接报错（fail-fast）。
func resolveModules(cfg *config.Config) (*ModuleRegistry, error) {
	all := modulekit.All()
	if len(all) == 0 {
		return nil, fmt.Errorf("没有注册任何业务模块，检查 internal/distribution 的导入清单")
	}
	// 常驻模块必须被发行版导入：它们承载登录、公开配置、字典与公告，
	// 缺了任意一个，公开接口会在运行期空指针，不如启动时就说清楚。
	for _, name := range modulekit.AlwaysOnNames() {
		if _, ok := modulekit.Find(name); !ok {
			return nil, fmt.Errorf("发行版缺少常驻模块 %q，请在 internal/distribution 中导入它", name)
		}
	}

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
		for _, spec := range all {
			reg.Enabled[spec.Name] = true
		}
	} else {
		// 校验显式列出的名字，再递归补齐依赖
		var add func(name string)
		add = func(name string) {
			name = strings.ToLower(strings.TrimSpace(name))
			if reg.Enabled[name] {
				return
			}
			spec, ok := modulekit.Find(name)
			if !ok {
				return // 未知名字由下方显式校验处理
			}
			for _, dep := range spec.DependsOn {
				add(dep)
			}
			reg.Enabled[name] = true
		}
		for _, raw := range requested {
			name := strings.ToLower(strings.TrimSpace(raw))
			if _, ok := modulekit.Find(name); !ok {
				return nil, fmt.Errorf("未知模块 %q（当前发行版可用: %s）", raw, strings.Join(modulekit.Names(), ", "))
			}
			add(name)
		}
		// 基础模块常驻
		for _, name := range modulekit.AlwaysOnNames() {
			add(name)
		}
	}

	ordered, err := topoOrder(all, reg.Enabled)
	if err != nil {
		return nil, err
	}
	reg.Ordered = ordered
	for _, name := range ordered {
		spec, _ := modulekit.Find(name)
		reg.specs = append(reg.specs, spec)
	}
	return reg, nil
}

// topoOrder 按依赖关系排序：被依赖的模块排前面，其余保持注册顺序。
// 同时校验依赖名是否存在于当前发行版——写错名字或漏导入都会在这里暴露，
// 而不是等到某个模块的 Setup 拿不到服务时才发现。
func topoOrder(all []modulekit.Spec, enabled map[string]bool) ([]string, error) {
	index := make(map[string]int, len(all))
	local := make(map[string]modulekit.Spec, len(all))
	for i, s := range all {
		index[s.Name] = i
		local[s.Name] = s
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	state := make(map[string]int, len(all))
	var out []string

	var visit func(spec modulekit.Spec, path []string) error
	visit = func(spec modulekit.Spec, path []string) error {
		switch state[spec.Name] {
		case black:
			return nil
		case gray:
			return fmt.Errorf("模块依赖成环: %s → %s", strings.Join(path, " → "), spec.Name)
		}
		for _, dep := range spec.DependsOn {
			if _, ok := index[dep]; !ok {
				return fmt.Errorf("模块 %q 依赖的 %q 不在当前发行版中，请导入该模块或去掉这条依赖",
					spec.Name, dep)
			}
			if !enabled[dep] {
				// 依赖补齐已经保证启用集合闭包，走到这里说明依赖表本身有问题。
				return fmt.Errorf("模块 %q 依赖的 %q 未被启用", spec.Name, dep)
			}
		}
		state[spec.Name] = gray
		for _, dep := range spec.DependsOn {
			if err := visit(local[dep], append(path, spec.Name)); err != nil {
				return err
			}
		}
		state[spec.Name] = black
		out = append(out, spec.Name)
		return nil
	}

	for _, s := range all {
		if !enabled[s.Name] {
			continue
		}
		if err := visit(s, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// On 判断模块是否启用，供 seed 与定时任务使用。
func (r *ModuleRegistry) On(name string) bool {
	if r == nil || len(r.Enabled) == 0 {
		return true
	}
	return r.Enabled[name]
}

// asSet 转成 router 与配置中心使用的 map。
func (r *ModuleRegistry) asSet() map[string]bool {
	out := make(map[string]bool, len(r.Enabled))
	for k, v := range r.Enabled {
		out[k] = v
	}
	return out
}

// scopeModels 汇总需要建的表：底座表 + 已启用模块声明的表。
func (r *ModuleRegistry) scopeModels() []any {
	out := append([]any(nil), baseModels...)
	for _, spec := range r.specs {
		out = append(out, spec.Models...)
	}
	return out
}
