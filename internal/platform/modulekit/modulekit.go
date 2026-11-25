// Package modulekit 是业务模块的自描述注册表。
//
// 一个模块的全部装配信息集中在自己包的 module.go 里，通过 init() 注册：
//
//	func init() {
//	    modulekit.Register(modulekit.Spec{
//	        Name:   "news",
//	        Label:  "资讯聚合",
//	        Models: []any{&model.NewsArticle{}, ...},
//	        Menus:  []modulekit.Menu{...},
//	        Jobs:   []modulekit.Job{...},
//	        Setup:  setup,
//	    })
//	}
//
// 发行版通过 internal/distribution 里的空白导入决定编译哪些模块，
// 删掉一行就少一个模块的全部代码、路由、菜单、表与任务。
//
// 为什么不把清单集中写在一处（旧的 Specs 数组）：集中清单意味着加模块仍然
// 要改框架代码，裁模块也要改框架代码，"独立模块"只是目录分开了。自注册
// 让框架只依赖注册结果，不依赖任何具体模块。
package modulekit

import (
	"context"
	"sort"
	"sync"

	"gorm.io/gorm"
)

// Spec 是一个业务模块的完整声明。
type Spec struct {
	// Name 模块标识，与 modules.enabled 白名单、依赖声明中的名字一致。
	Name string
	// Pkg 模块所属的 Go 包导入路径。发行版生成器据此把"要哪些模块"
	// 翻译成"导入哪些包"；一个包可以注册多个模块（如字典/参数/公告同属
	// system 包），此时按包裁剪会把整包带进来，模块级差异交给配置白名单。
	Pkg string
	// Label 管理端展示名。
	Label string
	// Desc 管理端说明：这个模块负责什么，关掉会少什么。
	Desc string
	// DependsOn 依赖的模块标识；启用本模块时会自动补齐依赖。
	// 依赖只表达"启动顺序与启用范围"，不传递服务实例。
	DependsOn []string
	// AlwaysOn 基础模块：公开接口（登录、公开配置、字典、公告）所依赖，不能裁剪。
	// 发行版必须导入全部 AlwaysOn 模块，否则启动即失败。
	AlwaysOn bool

	// Models 模块独占的数据表。关闭模块时不建表；重新启用时幂等补建。
	// 底座表（用户、角色、菜单、字典等）由框架维护，不在这里声明。
	Models []any
	// Menus 侧边栏菜单与按钮权限点。模块被裁掉后这些菜单不会出现；
	// 数据库里已有的记录会被置为停用而不是删除。
	Menus []Menu
	// Jobs 内置定时任务声明。关闭模块后不再补种，也不再执行。
	Jobs []Job

	// Setup 装配模块：构造 service/handler，声明路由、任务实现与服务供给。
	// 为 nil 表示该模块只有声明、没有运行期装配（纯数据模块）。
	Setup func(*Context) error
	// Sync 每次启动时执行的幂等数据补齐（内置等级、分类这类初始数据）。
	// 只在开启数据库种子时执行；实现必须自己判重，不能覆盖管理员的修改。
	Sync func(ctx context.Context, db *gorm.DB) error
}

// Menu 是一条内置菜单声明。父子关系通过 Parent 扁平表达，
// 不用嵌套结构，模块之间才可能共享同一个目录节点。
type Menu struct {
	ID     string
	Parent string
	Name   string
	// Type 取 model.MenuDir / MenuPage / MenuButton；留空按 MenuPage 处理。
	Type string
	Path string
	Icon string
	Perm string
	Sort int
	// Roles 除内置超管外还需要授权的内置角色，如 r_user。
	// 只在菜单首次创建时生效，管理员后续的调整不会被覆盖。
	Roles []string
}

// Job 是一条内置定时任务声明。Handler 是任务实现标识，
// 由 Setup 里的 Context.Job 注册同名实现。
type Job struct {
	Name    string
	Cron    string
	Handler string
	Params  string
	Remark  string
}

// 注册表。init() 阶段单线程写入，运行期只读，读写锁只是防御。
var (
	mu     sync.RWMutex
	specs  []Spec
	byName = map[string]int{}
)

// Register 注册一个模块。只应在包的 init() 中调用；名字重复直接 panic，
// 因为这一定是复制粘贴留下的错误，静默覆盖会让某次发版少掉一个模块。
func Register(s Spec) {
	if s.Name == "" {
		panic("modulekit: 模块名不能为空")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := byName[s.Name]; ok {
		panic("modulekit: 模块重复注册: " + s.Name)
	}
	byName[s.Name] = len(specs)
	specs = append(specs, s)
}

// All 返回全部已注册模块，顺序为注册（即导入）顺序。
func All() []Spec {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Spec, len(specs))
	copy(out, specs)
	return out
}

// Find 按标识查找模块。
func Find(name string) (Spec, bool) {
	mu.RLock()
	defer mu.RUnlock()
	i, ok := byName[name]
	if !ok {
		return Spec{}, false
	}
	return specs[i], true
}

// Names 返回全部模块标识（排序后），用于错误提示。
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// AlwaysOnNames 返回不可裁剪的基础模块标识。
func AlwaysOnNames() []string {
	mu.RLock()
	defer mu.RUnlock()
	var out []string
	for _, s := range specs {
		if s.AlwaysOn {
			out = append(out, s.Name)
		}
	}
	return out
}

// ModelsOf 汇总全部模块声明的表，供迁移使用。
func ModelsOf() []any {
	mu.RLock()
	defer mu.RUnlock()
	var out []any
	for _, s := range specs {
		out = append(out, s.Models...)
	}
	return out
}

// ---- 早期动作 ----
//
// 有些事必须在"解析模块开关"之前完成，最典型的是把管理端保存的配置覆盖值
// 合并进运行配置——modules.enabled 本身就存在覆盖表里。但框架不应该为此
// import 配置中心（那会把它强绑进每个发行版），所以由需要它的模块在 init()
// 里注册一个早期动作，框架按注册顺序执行。

var bootFns []func(context.Context, *gorm.DB) error

// RegisterBootstrap 注册开库之后、模块解析之前执行的动作。
// 动作失败只记录不阻断——配置读不出来时用文件里的值继续启动更合理。
func RegisterBootstrap(fn func(context.Context, *gorm.DB) error) {
	bootFns = append(bootFns, fn)
}

// RunBootstrap 执行全部早期动作，返回逐个的错误供调用方记日志。
func RunBootstrap(ctx context.Context, db *gorm.DB) []error {
	var errs []error
	for _, fn := range bootFns {
		if err := fn(ctx, db); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
