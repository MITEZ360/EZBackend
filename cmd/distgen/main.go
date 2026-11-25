// Command distgen 按模块名生成定制发行版的导入清单。
//
// 用法：
//
//	make dist MODULES="user configcenter cluster"
//	go run ./cmd/distgen -modules "user,configcenter,cluster"
//	go run ./cmd/distgen -list
//	go run ./cmd/distgen -modules all      # 全量（含默认不装的演示业务模块）
//	go run ./cmd/distgen -modules default  # 删除定制清单，回到默认发行版
//
// 它做的事很窄：把"我要哪些功能模块"翻译成"要导入哪些包"。
//
//  1. 读 catalog 拿到完整的模块注册表（含依赖关系），catalog 包含默认
//     发行版清单以及未默认收录的可选模块；
//  2. 以给定模块为起点递归补齐依赖，并强制带上 AlwaysOn 基础模块；
//  3. 按依赖拓扑序输出 internal/distribution/profile.go。
//
// 生成的 profile.go 带 profile 构建标签，与 default.go（!profile）互斥，
// 因此同一份源码可以产出多个发行版：默认构建只含通用后台能力，`make dist`
// 才按需把资讯中心这类独立模块装进去。
//
// 为什么不直接让用户在配置文件里写 modules.enabled：
// 那是运行期开关，代码仍在二进制里。这里做的是编译期裁剪，被裁掉的模块
// 连代码都不会进二进制。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	// 触发完整模块注册（默认发行版 + 可选模块），拿到依赖图。
	"github.com/MITEZ360/tenon/internal/distribution/catalog"
	"github.com/MITEZ360/tenon/internal/platform/modulekit"
)

const defaultOutput = "internal/distribution/profile.go"

func main() {
	var (
		modules = flag.String("modules", "", `要包含的模块，逗号或空格分隔；all 全量，default 回到默认发行版`)
		list    = flag.Bool("list", false, "列出可组合的模块与依赖")
		printM  = flag.Bool("print", false, "只输出最终装配的模块名（空格分隔），供构建脚本传给前端裁剪")
		output  = flag.String("o", defaultOutput, "生成文件路径")
		name    = flag.String("name", "Custom", "发行版名称，写入 distribution.Name，会出现在启动日志与 -version 输出里")
	)
	flag.Parse()

	if *list || (*modules == "" && flag.NArg() == 0) {
		printList()
		return
	}

	// 也支持把模块名直接当参数传：distgen user configcenter cluster
	raw := *modules
	if raw == "" {
		raw = strings.Join(flag.Args(), " ")
	}
	want := splitNames(raw)

	// default 表示"回到默认发行版"：删掉定制清单，后续 make build 走 default.go。
	if len(want) == 1 && want[0] == "default" {
		if err := os.Remove(*output); err != nil && !os.IsNotExist(err) {
			fail("删除 %s 失败: %v", *output, err)
		}
		fmt.Printf("已移除定制清单，后续构建回到默认发行版（make build）。\n")
		return
	}

	// all 表示"全部模块都要"，包括默认不装的演示业务模块（资讯中心等）。
	if len(want) == 1 && want[0] == "all" {
		want = modulekit.Names()
	}

	selected, err := resolve(want)
	if err != nil {
		fail("%v", err)
	}

	// -print 供构建脚本取"实际装配集合"（含依赖补齐与常驻模块），
	// 前端据此裁剪页面，避免前后端两份清单各自维护而错位。
	if *printM {
		fmt.Println(strings.Join(namesOf(selected), " "))
		return
	}

	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fail("创建目录失败: %v", err)
	}
	if err := os.WriteFile(*output, []byte(render(*output, *name, want, selected)), 0o644); err != nil {
		fail("写入 %s 失败: %v", *output, err)
	}

	fmt.Printf("已生成 %s（发行版 %s）\n", *output, *name)
	fmt.Printf("  显式选择：%s\n", strings.Join(dedup(want), " "))
	fmt.Printf("  最终装配 %d 个：%s\n", len(selected), strings.Join(namesOf(selected), " "))
	if extra := added(want, selected); len(extra) > 0 {
		fmt.Printf("  自动补齐 %d 个：%s\n", len(extra), strings.Join(extra, " "))
	}
	fmt.Printf("  涉及代码包 %d 个\n", len(pkgsOf(selected)))
	fmt.Println()
	fmt.Println("下一步：make dist MODULES=\"" + strings.Join(want, " ") + "\"（或 go build -tags profile ./cmd/tenon）")
}

// splitNames 支持逗号、空格、换行混写。
func splitNames(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// resolve 计算最终模块集合并按依赖拓扑排序：依赖在前，可直接用于装配。
func resolve(want []string) ([]modulekit.Spec, error) {
	// 基础模块无条件加入：登录、公开配置、字典、公告是公开接口的前提，
	// 缺任意一个，服务启动后会在运行期空指针。
	target := map[string]bool{}
	for _, name := range modulekit.AlwaysOnNames() {
		target[name] = true
	}
	var unknown []string
	for _, name := range want {
		if _, ok := modulekit.Find(name); ok {
			target[name] = true
		} else {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("未知模块 %s；可用模块：%s",
			strings.Join(unknown, " "), strings.Join(modulekit.Names(), " "))
	}

	var out []modulekit.Spec
	state := map[string]int{} // 1=访问中 2=完成
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("模块依赖成环：%s", strings.Join(append(path, name), " → "))
		}
		spec, ok := modulekit.Find(name)
		if !ok {
			return fmt.Errorf("模块 %s 声明了不存在的依赖", name)
		}
		state[name] = 1
		for _, dep := range spec.DependsOn {
			if err := visit(dep, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = 2
		out = append(out, spec)
		return nil
	}

	names := make([]string, 0, len(target))
	for name := range target {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// pkgsOf 把模块集合映射成去重后的包导入路径，保持拓扑序。
func pkgsOf(specs []modulekit.Spec) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range specs {
		pkg := s.Pkg
		if pkg == "" {
			// 缺 Pkg 时按目录约定兜底，但更希望模块自己声明准确路径。
			pkg = "github.com/MITEZ360/tenon/internal/modules/" + s.Name
		}
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out
}

func namesOf(specs []modulekit.Spec) []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	return out
}

// added 返回"因依赖而被带进来"的模块。
func added(want []string, specs []modulekit.Spec) []string {
	keep := map[string]bool{}
	for _, w := range dedup(want) {
		keep[w] = true
	}
	var out []string
	for _, s := range specs {
		if !keep[s.Name] {
			out = append(out, s.Name)
		}
	}
	return out
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func render(path, name string, want []string, specs []modulekit.Spec) string {
	var b strings.Builder
	b.WriteString("//go:build profile\n\n")
	b.WriteString("// Code generated by cmd/distgen; DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// 定制发行版清单：" + strings.Join(dedup(want), " ") + "\n")
	b.WriteString("// 实际装配模块：" + strings.Join(namesOf(specs), " ") + "\n")
	b.WriteString("//\n")
	b.WriteString("// 重新生成：make dist MODULES=\"" + strings.Join(dedup(want), " ") + "\" NAME=" + name + "\n")
	b.WriteString("// 回到默认发行版：go run ./cmd/distgen -modules default，然后 make build\n\n")
	b.WriteString("package distribution\n\n")
	b.WriteString("import (\n")
	for _, pkg := range pkgsOf(specs) {
		b.WriteString("\t_ \"" + pkg + "\"\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("// Name 是当前发行版的名称，由 distgen -name 写入。\n")
	b.WriteString("const Name = " + strconv.Quote(name) + "\n")
	return b.String()
}

func printList() {
	fmt.Println("可组合的模块（* 常驻不可裁剪，+ 默认发行版不含、需显式组装）：")
	optional := map[string]bool{}
	for _, name := range catalog.Optional {
		optional[name] = true
	}
	all := modulekit.All()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	for _, s := range all {
		mark := " "
		switch {
		case s.AlwaysOn:
			mark = "*"
		case optional[s.Name]:
			mark = "+"
		}
		dep := ""
		if len(s.DependsOn) > 0 {
			dep = "  依赖: " + strings.Join(s.DependsOn, " ")
		}
		fmt.Printf("  %s %-14s %s%s\n", mark, s.Name, s.Label, dep)
	}
	fmt.Println()
	fmt.Println("示例：make dist MODULES=\"user configcenter cluster\"")
	fmt.Println("Base：make dist-base（仅后台管理基础功能）")
	fmt.Println("全量：make dist MODULES=all      回到默认：make dist-default")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "distgen: "+format+"\n", args...)
	os.Exit(1)
}
