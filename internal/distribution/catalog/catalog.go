// Package catalog 汇总代码库里全部可装配的模块，只供工具与测试读取。
//
// 本包 = 默认发行版清单（internal/distribution） + 未默认收录的可选模块。
// 不重复罗列默认模块，是为了让默认清单变化时这里自动跟随，少一处漂移点。
//
// 应用二进制不导入本包，因此这里列出的可选模块不会混进 `make build`；
// 只有 `make dist MODULES="news"` 生成的 profile.go 会真正把它们编进去。
package catalog

import (
	// 默认发行版的全部模块。
	_ "github.com/keel-framework/keel/internal/distribution"

	// 可选业务模块：默认发行版不含，按需组装时才进二进制。
	_ "github.com/keel-framework/keel/internal/modules/cnauth"
	_ "github.com/keel-framework/keel/internal/modules/news"
	_ "github.com/keel-framework/keel/internal/modules/recharge"
)

// Optional 列出不属于默认发行版的模块名。
//
// distgen 用它给 `make dist-list` 打标记，避免用户以为默认构建里已经有它；
// 同时用测试校验这些名字确实已注册（写错名字会让可选模块悄悄消失）。
var Optional = []string{"cnauth", "news", "recharge"}
