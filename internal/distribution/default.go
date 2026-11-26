//go:build !profile

// Package distribution 是发行版的模块清单。
//
// 本文件是「默认发行版」：不带构建标签的构建（make build）走这里，
// 收录一个通用后台该有的东西——登录、RBAC、组织、运维控制台，
// 以及消息、会员、API 密钥这类通用业务能力。
//
// 演示业务模块（资讯中心）不在默认清单里。它是独立模块，需要时显式组装：
//
//	make dist MODULES="user news"
//
// 那会生成 profile.go（profile 构建标签），与本文件的 !profile 互斥，
// 因此同一份源码可以产出多个发行版，互不干扰：默认版干净，需要的项目
// 自己把模块加进去。完整可组合清单由 catalog 包汇总，供生成器读取。
//
// 增删模块在这两个清单里都只是一行空白导入：空白导入触发各模块 module.go
// 里的 init()，注册自己的声明与装配函数。其余代码——装配、路由、建表、
// 菜单、定时任务、配置中心——都只依赖 modulekit 的注册结果，不认识具体模块。
// 没有导入的模块不会进入二进制：不存在的代码谈不上"关掉"，比运行期开关
// 更彻底，也更省体积。
package distribution

import (
	// 常驻模块：公开接口依赖，不可裁剪。全部发行版都必须保留。
	_ "github.com/MITEZ360/tenon/internal/modules/auth"
	_ "github.com/MITEZ360/tenon/internal/modules/menu"
	_ "github.com/MITEZ360/tenon/internal/modules/system"

	// 后台管理基础能力。
	_ "github.com/MITEZ360/tenon/internal/modules/dashboard"
	_ "github.com/MITEZ360/tenon/internal/modules/dept"
	_ "github.com/MITEZ360/tenon/internal/modules/role"
	_ "github.com/MITEZ360/tenon/internal/modules/user"

	// 运维控制台。
	_ "github.com/MITEZ360/tenon/internal/modules/audit"
	_ "github.com/MITEZ360/tenon/internal/modules/cacheadmin"
	_ "github.com/MITEZ360/tenon/internal/modules/configcenter"
	_ "github.com/MITEZ360/tenon/internal/modules/file"
	_ "github.com/MITEZ360/tenon/internal/modules/job"
	_ "github.com/MITEZ360/tenon/internal/modules/maintenance"
	_ "github.com/MITEZ360/tenon/internal/modules/monitor"
	_ "github.com/MITEZ360/tenon/internal/modules/routemanage"

	// 集群协同，依赖配置中心。
	_ "github.com/MITEZ360/tenon/internal/modules/cluster"

	// 服务治理：服务目录、实例健康与动态路由。路由依赖 registry。
	_ "github.com/MITEZ360/tenon/internal/modules/registry"
	_ "github.com/MITEZ360/tenon/internal/modules/route"

	// 通用业务能力：与具体行业无关，默认保留。
	_ "github.com/MITEZ360/tenon/internal/modules/apikey"
	_ "github.com/MITEZ360/tenon/internal/modules/flow"
	_ "github.com/MITEZ360/tenon/internal/modules/membership"
	_ "github.com/MITEZ360/tenon/internal/modules/message"
	// 资讯中心（news）是独立模块，不在默认发行版；需要时
	// make dist MODULES="user news"，或在本文件加一行导入。
)

// Name 是当前发行版的名称。默认发行版（make build）叫 Default，
// 定制发行版由 distgen -name 写进 profile.go，二者构建标签互斥。
const Name = "Default"
