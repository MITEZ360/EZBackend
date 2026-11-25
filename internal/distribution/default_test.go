package distribution

import (
	"testing"

	"github.com/keel-framework/keel/internal/platform/modulekit"
)

// 默认发行版的边界要可断言：谁把可选业务模块加回 default.go，这条会红。
//
// 资讯中心（news）、自助注册（cnauth）、会员充值（recharge）都是独立模块，
// 只在 make dist MODULES="... " 生成的 profile.go 里出现；默认构建里连代码都没有。
func TestDefaultDistributionExcludesOptional(t *testing.T) {
	for _, name := range []string{"news", "cnauth", "recharge"} {
		if _, ok := modulekit.Find(name); ok {
			t.Fatalf("%s 不应出现在默认发行版；需要时用 make dist MODULES=\"user %s\" 组装", name, name)
		}
	}
}

// 通用后台能力必须默认就有，否则"开箱可用"的承诺不成立。
func TestDefaultDistributionKeepsCore(t *testing.T) {
	for _, name := range []string{
		"auth", "menu", "config", "dict", "notice", // 常驻
		"user", "role", "dept", "dashboard", // 后台管理
		"audit", "job", "file", "monitor", "configcenter", "cluster", // 运维
	} {
		if _, ok := modulekit.Find(name); !ok {
			t.Errorf("默认发行版缺少 %s", name)
		}
	}
}
