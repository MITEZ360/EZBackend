package gov

import (
	"encoding/json"
	"sync"
	"testing"
)

// TestChecksum 校验和对内容敏感、对长度稳定。
func TestChecksum(t *testing.T) {
	a := Checksum([]byte(`{"rev":1}`))
	b := Checksum([]byte(`{"rev":2}`))
	if a == b {
		t.Fatal("不同内容不应得到相同校验和")
	}
	if a != Checksum([]byte(`{"rev":1}`)) {
		t.Fatal("相同内容应得到相同校验和")
	}
	if len(a) != 64 {
		t.Fatalf("SHA-256 十六进制应为 64 字符，实际 %d", len(a))
	}
	if Checksum(nil) != Checksum([]byte{}) {
		t.Fatal("nil 与空切片应等价")
	}
}

// TestStoreDefaultEmpty 未加载时返回空快照而非 nil，调用方无需判空。
func TestStoreDefaultEmpty(t *testing.T) {
	var s Store
	snap := s.Load()
	if snap == nil {
		t.Fatal("空 store 不应返回 nil")
	}
	if len(snap.Parts) != 0 || snap.GlobalRev != 0 {
		t.Fatalf("空快照应无分区: %+v", snap)
	}
	// 对空快照取分区必须安全。
	if _, ok := snap.Part(PartRoute); ok {
		t.Fatal("空快照不应有 route 分区")
	}
	if _, ok := snap.Routes(); ok {
		t.Fatal("空快照解码 route 应失败")
	}
}

// TestStoreReplace 替换后旧快照不被原地修改（不可变约定）。
func TestStoreReplace(t *testing.T) {
	var s Store
	first := &Snapshot{GlobalRev: 1, Parts: map[string]Part{PartRoute: {Rev: 1, Payload: json.RawMessage(`{"rev":1,"routes":[]}`)}}}
	s.Store(first)
	second := &Snapshot{GlobalRev: 2, Parts: map[string]Part{}}
	s.Store(second)

	if got := s.Load().GlobalRev; got != 2 {
		t.Fatalf("应加载新快照，实际 rev=%d", got)
	}
	if first.GlobalRev != 1 || len(first.Parts) != 1 {
		t.Fatal("旧快照被修改，违反不可变约定")
	}
	// Store(nil) 是空操作，不能把已有快照清掉。
	s.Store(nil)
	if got := s.Load().GlobalRev; got != 2 {
		t.Fatalf("Store(nil) 不应清空快照，实际 rev=%d", got)
	}
	// Parts 为 nil 时补成空 map，避免调用方对 nil map 写入 panic。
	s.Store(&Snapshot{GlobalRev: 3})
	if s.Load().Parts == nil {
		t.Fatal("Parts 应被补成非 nil map")
	}
}

// TestStoreConcurrentReadWrite 并发读写不应出现数据竞争或读到半成品。
func TestStoreConcurrentReadWrite(t *testing.T) {
	var s Store
	s.Store(&Snapshot{GlobalRev: 0, Parts: map[string]Part{}})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				snap := s.Load()
				if snap == nil {
					t.Error("读到 nil 快照")
					return
				}
				_ = snap.GlobalRev
				_, _ = snap.Part(PartRoute)
			}
		}()
	}
	for i := 1; i <= 200; i++ {
		s.Store(&Snapshot{GlobalRev: int64(i), Parts: map[string]Part{}})
	}
	wg.Wait()
}

// TestSnapshotDecode 三个分区解码：成功、缺分区、载荷损坏。
func TestSnapshotDecode(t *testing.T) {
	routePayload := RoutePayload{Rev: 7, Routes: []Route{{ID: "r1", Prefix: "/api", Service: "core"}}}
	regPayload := RegistryPayload{Rev: 7, Services: map[string]Service{
		"core": {Name: "core", Instances: []Instance{{ID: "i1", Addr: "10.0.0.1:8080"}}},
	}}
	cfgPayload := ConfigPayload{Rev: 7, Namespace: "app", Items: map[string]ConfigItem{
		"db.password": {Value: "s3cret", Type: "string", Encrypted: true},
	}}

	part := func(v any) Part {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return Part{Rev: 7, Checksum: Checksum(b), Payload: b}
	}

	snap := &Snapshot{GlobalRev: 7, Parts: map[string]Part{
		PartRoute:                part(routePayload),
		PartRegistry:             part(regPayload),
		PartConfigPrefix + "app": part(cfgPayload),
	}}

	if rp, ok := snap.Routes(); !ok || len(rp.Routes) != 1 || rp.Routes[0].Service != "core" {
		t.Fatalf("route 分区解码异常: ok=%v %+v", ok, rp)
	}
	if reg, ok := snap.Registry(); !ok || reg.Services["core"].Instances[0].Addr != "10.0.0.1:8080" {
		t.Fatalf("registry 分区解码异常: ok=%v %+v", ok, reg)
	}
	if cfg, ok := snap.Config("app"); !ok || cfg.Items["db.password"].Value != "s3cret" {
		t.Fatalf("config 分区解码异常: ok=%v %+v", ok, cfg)
	}
	if _, ok := snap.Config("missing"); ok {
		t.Fatal("不存在的命名空间应返回 false")
	}

	// 载荷损坏必须解码失败，让调用方保留旧视图。
	snap.Parts[PartRoute] = Part{Rev: 8, Payload: json.RawMessage(`{oops`)}
	if _, ok := snap.Routes(); ok {
		t.Fatal("损坏的 route 载荷应解码失败")
	}
	// nil 快照上取分区不能 panic。
	var nilSnap *Snapshot
	if _, ok := nilSnap.Part(PartRoute); ok {
		t.Fatal("nil 快照不应有分区")
	}
}
