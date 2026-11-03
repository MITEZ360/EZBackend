package discovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewStaticParse(t *testing.T) {
	reg, err := NewStatic([]string{"keel-core=127.0.0.1:9000,127.0.0.1:9002", " keel-auth=10.0.0.1:9001 "})
	if err != nil {
		t.Fatalf("解析静态注册表失败: %v", err)
	}
	list, err := reg.Resolve(context.Background(), "keel-core")
	if err != nil {
		t.Fatalf("解析实例失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个实例，实际 %d", len(list))
	}
	if list[0].URL("http") != "http://127.0.0.1:9000" {
		t.Fatalf("URL 拼接错误: %s", list[0].URL("http"))
	}
	if _, err := reg.Resolve(context.Background(), "not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未注册服务应返回 ErrNotFound，实际 %v", err)
	}
}

func TestNewStaticInvalid(t *testing.T) {
	cases := [][]string{
		{"keel-core"},           // 缺少等号
		{"keel-core=127.0.0.1"}, // 缺少端口
		{"keel-core="},          // 无实例
	}
	for _, c := range cases {
		if _, err := NewStatic(c); err == nil {
			t.Fatalf("非法配置应报错: %v", c)
		}
	}
}

func TestNewFactory(t *testing.T) {
	r, err := New("static", []string{"svc=127.0.0.1:1"}, "", 0)
	if err != nil || r.Name() != "static" {
		t.Fatalf("static 工厂异常: %v %v", r, err)
	}
	if _, err := New("dns", nil, "default.svc.local:9000", time.Second); err != nil {
		t.Fatalf("dns 工厂异常: %v", err)
	}
	if _, err := New("consul", nil, "", 0); err == nil {
		t.Fatal("不支持的类型应报错")
	}
}

func TestBalancerRoundRobin(t *testing.T) {
	b := NewBalancer(time.Minute)
	list := []Instance{{ID: "a", Addr: "1:1"}, {ID: "b", Addr: "2:2"}}
	counts := map[string]int{}
	for i := 0; i < 10; i++ {
		ins, ok := b.Pick("svc", list)
		if !ok {
			t.Fatal("应能选出实例")
		}
		counts[ins.ID]++
	}
	if counts["a"] != 5 || counts["b"] != 5 {
		t.Fatalf("轮询不均匀: %v", counts)
	}
}

func TestBalancerFailover(t *testing.T) {
	b := NewBalancer(50 * time.Millisecond)
	list := []Instance{{ID: "a", Addr: "1:1"}, {ID: "b", Addr: "2:2"}}
	b.Fail("a")
	for i := 0; i < 5; i++ {
		ins, _ := b.Pick("svc", list)
		if ins.ID != "b" {
			t.Fatalf("故障实例应被摘除，实际选中 %s", ins.ID)
		}
	}
	if len(b.Stats()) != 1 {
		t.Fatalf("冷却实例数应为 1，实际 %v", b.Stats())
	}
	time.Sleep(80 * time.Millisecond)
	if len(b.Stats()) != 0 {
		t.Fatal("冷却结束后应自动恢复")
	}
}

func TestBalancerEmpty(t *testing.T) {
	b := NewBalancer(time.Second)
	if _, ok := b.Pick("svc", nil); ok {
		t.Fatal("空列表不应选出实例")
	}
}

func TestDNSResolveLocalhost(t *testing.T) {
	// 使用本机回环地址验证 DNS 实现的可解析路径
	d, err := NewDNS("localhost:9000", time.Minute)
	if err != nil {
		t.Fatalf("创建 DNS 注册表失败: %v", err)
	}
	list, err := d.Resolve(context.Background(), "localhost")
	if err != nil {
		t.Skipf("当前环境无法解析 localhost: %v", err)
	}
	if len(list) == 0 || list[0].Addr == "" {
		t.Fatal("解析结果为空")
	}
}
