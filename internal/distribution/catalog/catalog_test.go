package catalog

import (
	"testing"

	"github.com/keel-framework/keel/internal/platform/modulekit"
)

// catalog 是生成器的数据源。可选模块必须在这里可见，否则
// make dist MODULES="news" 会报"未知模块"——模块等于被悄悄删掉了。
func TestOptionalModulesRegistered(t *testing.T) {
	if len(Optional) == 0 {
		t.Fatal("可选模块清单不应为空")
	}
	for _, name := range Optional {
		if _, ok := modulekit.Find(name); !ok {
			t.Errorf("可选模块 %s 未注册：检查 catalog/catalog.go 的空白导入", name)
		}
	}
}

func TestNewsAssemblable(t *testing.T) {
	spec, ok := modulekit.Find("news")
	if !ok {
		t.Fatal("资讯中心必须能被组装：catalog 应导入 internal/modules/news")
	}
	if spec.AlwaysOn {
		t.Error("资讯中心是可选模块，不能标记为常驻")
	}
	if spec.Pkg == "" {
		t.Error("模块必须声明 Pkg，否则生成器无法把它翻译成导入清单")
	}
}

// 国内能力（注册/短信登录、会员充值）同样是可选模块：
// 默认版不装，但组装时必须能算出它们的依赖闭包。
func TestCNModulesAssemblable(t *testing.T) {
	deps := map[string]string{"cnauth": "auth", "recharge": "membership"}
	for name, dep := range deps {
		spec, ok := modulekit.Find(name)
		if !ok {
			t.Fatalf("%s 未注册：catalog 应导入对应包", name)
		}
		if spec.AlwaysOn {
			t.Errorf("%s 是可选模块，不能标记为常驻", name)
		}
		if spec.Pkg == "" {
			t.Errorf("%s 未声明 Pkg，生成器无法翻译成导入清单", name)
		}
		found := false
		for _, d := range spec.DependsOn {
			if d == dep {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 应声明依赖 %s，否则单独组装时装配顺序与服务查找会失败", name, dep)
		}
	}
}
