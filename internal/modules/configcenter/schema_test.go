package configcenter

import "testing"

// 配置项跟着模块走：模块没编进当前发行版时，它的配置项必须隐藏。
//
// 测试进程不加载发行版清单（会与本包形成循环），只手动注册了 alpha/beta/gamma，
// 所以 news 在这里等价于"默认发行版不含的独立模块"。
func TestModuleScopedItemsFollowDistribution(t *testing.T) {
	it, ok := itemIndex["news.retention_days"]
	if !ok {
		t.Fatal("资讯保留天数应留在配置清单里，组装 news 模块后需要它")
	}
	if it.Module != "news" {
		t.Fatalf("资讯保留天数应归属 news 模块，实际 %q", it.Module)
	}
	if it.available() {
		t.Error("news 不在当前发行版，该配置项应隐藏")
	}
	if got := len(itemsOf("news")); got != 0 {
		t.Errorf("资讯分组应整组隐藏，实际仍有 %d 项", got)
	}

	if !(Item{Module: "alpha"}).available() {
		t.Error("已注册模块的配置项应展示")
	}
	if !(Item{}).available() {
		t.Error("基础配置项不属于任何模块，任何时候都应展示")
	}
	if got := len(itemsOf("basic")); got == 0 {
		t.Error("基础分组不能被模块过滤误伤")
	}
}
