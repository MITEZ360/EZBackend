package bootstrap

import (
	"fmt"
	"strings"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

// 模块清单与依赖关系声明在 platform/modulekit：bootstrap 用它解析启停、
// 决定装配范围，配置中心用它渲染管理端的模块开关，两边共用一份数据。

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
		for _, spec := range modulekit.Specs {
			reg.Enabled[spec.Name] = true
			reg.Ordered = append(reg.Ordered, spec.Name)
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
			return nil, fmt.Errorf("未知模块 %q（可用: %s）", raw, strings.Join(modulekit.Names(), ", "))
		}
		add(name)
	}
	// 基础模块常驻
	for _, spec := range modulekit.Specs {
		if spec.AlwaysOn {
			add(spec.Name)
		}
	}
	// 按声明顺序输出，保证日志稳定
	for _, spec := range modulekit.Specs {
		if reg.Enabled[spec.Name] {
			reg.Ordered = append(reg.Ordered, spec.Name)
		}
	}
	return reg, nil
}

// asSet 转成 router.Deps.Modules 使用的 map。
func (r *ModuleRegistry) asSet() map[string]bool {
	out := make(map[string]bool, len(r.Enabled))
	for k, v := range r.Enabled {
		out[k] = v
	}
	return out
}
