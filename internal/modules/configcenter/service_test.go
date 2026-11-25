package configcenter

import (
	"testing"
	"time"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/internal/platform/modulekit"
)

// 测试进程不加载发行版清单（导入 distribution 会与本包形成循环），
// 这里手动注册几个模块，供候选项生成与白名单校验使用。
func init() {
	modulekit.Register(modulekit.Spec{Name: "alpha", Label: "Alpha"})
	modulekit.Register(modulekit.Spec{Name: "beta", Label: "Beta", DependsOn: []string{"alpha"}})
	modulekit.Register(modulekit.Spec{Name: "gamma", Label: "Gamma", AlwaysOn: true})
}

func TestLookupReturnsCurrentValues(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.CaptchaEnabled = true
	cfg.Security.LockDuration = 15 * time.Minute
	cfg.Storage.MaxSize = 20 << 20
	cfg.JWT.AccessTTL = 2 * time.Hour
	cfg.Storage.AllowExt = []string{"png", "jpg"}

	cases := map[string]string{
		"security.captcha_enabled": "true",
		"security.lock_duration":   "15m",
		"storage.max_size":         "20971520",
		"jwt.access_ttl":           "2h",
		"storage.allow_ext":        "png,jpg",
	}
	for key, want := range cases {
		got, ok := lookup(cfg, key)
		if !ok {
			t.Fatalf("lookup(%s) 未命中", key)
		}
		if got != want {
			t.Errorf("lookup(%s) = %q, want %q", key, got, want)
		}
	}
}

func TestLookupMissingKey(t *testing.T) {
	// 部署类参数不在可编辑白名单内：lookup 能取到值，但保存入口必须拒绝。
	if _, ok := itemIndex["db.dsn"]; ok {
		t.Fatal("db.dsn 不应出现在可管理清单中")
	}
	cfg := &config.Config{}
	if _, ok := lookup(cfg, "nope.unknown"); ok {
		t.Fatal("未知键不应命中")
	}
}

func TestParseValueByType(t *testing.T) {
	if v, err := parseValue(Item{Type: "bool"}, "true"); err != nil || v != true {
		t.Fatalf("bool 解析失败: %v %v", v, err)
	}
	if v, err := parseValue(Item{Type: "int"}, "20"); err != nil || v != 20 {
		t.Fatalf("int 解析失败: %v %v", v, err)
	}
	if v, err := parseValue(Item{Type: "duration"}, "15m"); err != nil || v != 15*time.Minute {
		t.Fatalf("duration 解析失败: %v %v", v, err)
	}
	v, err := parseValue(Item{Type: "list"}, "png, jpg ,,")
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]string)
	if !ok || len(list) != 2 || list[0] != "png" || list[1] != "jpg" {
		t.Fatalf("list 解析结果异常: %#v", v)
	}
}

func TestValidateRejectsInvalidValues(t *testing.T) {
	min := 1.0
	max := 10.0
	cases := []struct {
		item Item
		raw  string
	}{
		{Item{Name: "开关", Type: "bool"}, "yes"},
		{Item{Name: "数量", Type: "int", Min: &min, Max: &max}, "0"},
		{Item{Name: "数量", Type: "int", Min: &min, Max: &max}, "11"},
		{Item{Name: "间隔", Type: "duration"}, "-1m"},
		{Item{Name: "级别", Type: "string", Options: []string{"info", "warn"}}, "debug"},
	}
	for _, c := range cases {
		if err := validate(c.item, c.raw); err == nil {
			t.Errorf("validate(%s, %q) 应该失败", c.item.Name, c.raw)
		}
	}
	if err := validate(Item{Name: "数量", Type: "int", Min: &min, Max: &max}, "5"); err != nil {
		t.Errorf("合法值被拒绝: %v", err)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		15 * time.Minute: "15m",
		2 * time.Hour:    "2h",
		168 * time.Hour:  "168h",
		90 * time.Second: "90s",
		90 * time.Minute: "90m",
	}
	for d, want := range cases {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestEveryItemHasGroup(t *testing.T) {
	groups := make(map[string]bool, len(groupMeta))
	for _, g := range groupMeta {
		groups[g.Name] = true
	}
	for _, it := range items {
		if !groups[it.Group] {
			t.Errorf("配置项 %s 的分组 %s 未在 groupMeta 中声明", it.Key, it.Group)
		}
		if it.Hot && it.Restart {
			t.Errorf("配置项 %s 不能同时标记热生效与需重启", it.Key)
		}
	}
}

func TestModuleSwitchItem(t *testing.T) {
	it, ok := itemIndex["modules.enabled"]
	if !ok {
		t.Fatal("模块开关必须可在管理端修改")
	}
	if !it.Restart || it.Hot {
		t.Error("模块开关决定装配范围，只能标为需重启")
	}
	// 候选项直接来自注册表，两边漂移会导致写进白名单的名字解析不了，
	// 下次启动 fail-fast 起不来。候选项是动态生成的，静态字段应为空。
	if len(it.Choices) != 0 {
		t.Error("模块候选项应动态生成，不写成包级常量")
	}
	choices := moduleChoices()
	if len(choices) != len(modulekit.All()) {
		t.Fatalf("候选项 %d 个，模块声明 %d 个", len(choices), len(modulekit.All()))
	}
	for i, spec := range modulekit.All() {
		c := choices[i]
		if c.Value != spec.Name || c.Locked != spec.AlwaysOn {
			t.Errorf("第 %d 项不一致: %+v vs %+v", i, c, spec)
		}
	}
}

func TestValidateModuleList(t *testing.T) {
	it := itemIndex[KeyModules]
	if err := validate(it, "alpha,beta"); err != nil {
		t.Fatalf("合法模块名应通过: %v", err)
	}
	// 写错名字必须当场拒绝，而不是留到重启时 fail-fast。
	if err := validate(it, "alpha,nosuchmodule"); err == nil {
		t.Fatal("未知模块名应被拒绝")
	}
}
