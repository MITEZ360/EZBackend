package micro

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// snapshotOf 组装一份最小快照；routes 为 nil 时也写出空数组，模拟"显式下线全部规则"。
func snapshotOf(t *testing.T, routes []gov.Route, services map[string][]gov.Instance) *gov.Snapshot {
	t.Helper()
	if routes == nil {
		routes = []gov.Route{}
	}
	if services == nil {
		services = map[string][]gov.Instance{}
	}
	reg := map[string]gov.Service{}
	for name, list := range services {
		if list == nil {
			list = []gov.Instance{}
		}
		reg[name] = gov.Service{Name: name, Instances: list}
	}
	rp, err := json.Marshal(gov.RoutePayload{Rev: 2, Routes: routes})
	if err != nil {
		t.Fatal(err)
	}
	rpReg, err := json.Marshal(gov.RegistryPayload{Rev: 2, Services: reg})
	if err != nil {
		t.Fatal(err)
	}
	return &gov.Snapshot{GlobalRev: 2, Parts: map[string]gov.Part{
		gov.PartRoute:    {Rev: 2, Checksum: gov.Checksum(rp), Payload: rp},
		gov.PartRegistry: {Rev: 2, Checksum: gov.Checksum(rpReg), Payload: rpReg},
	}}
}

func addrOf(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

// upstream 记录收到的路径与请求头，便于断言 strip 与注入行为。
type upstream struct {
	srv    *httptest.Server
	paths  atomic.Value // string
	calls  atomic.Int64
	header atomic.Value // http.Header
}

func newUpstream(t *testing.T, delay time.Duration) *upstream {
	t.Helper()
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		u.paths.Store(r.URL.Path)
		u.header.Store(r.Header.Clone())
		if delay > 0 {
			time.Sleep(delay)
		}
		_, _ = io.WriteString(w, "upstream:"+r.URL.Path)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// TestSnapshotTakesOverFromConfig 快照就位后必须压过配置文件里的同名前缀。
func TestSnapshotTakesOverFromConfig(t *testing.T) {
	live := newUpstream(t, 0)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "should-not-be-called", http.StatusTeapot)
	}))
	defer dead.Close()

	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{
		{Prefix: "/api", Service: "legacy"},
	}})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Name: "dynamic", Prefix: "/api", Service: "svc", Weight: 100, Strip: true}},
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(live.srv), Weight: 100}}},
	)); err != nil {
		t.Fatalf("应用快照失败: %v", err)
	}

	resp, body := do(t, gw, "/api/hello")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("应转发成功，实际 %d %s", resp.StatusCode, body)
	}
	if got := live.paths.Load(); got != "/hello" {
		t.Fatalf("strip 后上游应收到 /hello，实际 %v", got)
	}
	if !strings.Contains(body, "upstream:/hello") {
		t.Fatalf("响应应来自快照指向的上游: %s", body)
	}
	h, _ := live.header.Load().(http.Header)
	if h.Get("X-Internal-Token") != "internal-token" {
		t.Fatalf("应注入内部令牌，实际 %q", h.Get("X-Internal-Token"))
	}
	if h.Get("X-Caller-Service") != "tenon-gateway" {
		t.Fatalf("应注入调用方标识，实际 %q", h.Get("X-Caller-Service"))
	}
}

// TestSnapshotDoesNotFallBackToConfig 快照没有命中时直接 404，不能回落配置文件。
func TestSnapshotDoesNotFallBackToConfig(t *testing.T) {
	live := newUpstream(t, 0)
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{
		{Prefix: "/api", Service: "legacy"},
	}})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "other", Prefix: "/other", Service: "svc", Weight: 100}},
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(live.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}

	resp, body := do(t, gw, "/api/hello")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未命中快照规则应 404，实际 %d %s", resp.StatusCode, body)
	}
	if live.calls.Load() != 0 {
		t.Fatal("不应转发到任何上游")
	}
}

// TestSnapshotEmptyRoutesAreAuthoritative 空路由表表示"全部下线"，而不是"退回配置"。
func TestSnapshotEmptyRoutesAreAuthoritative(t *testing.T) {
	live := newUpstream(t, 0)
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{
		{Prefix: "/api", Service: "legacy"},
	}})
	if err := gw.ApplySnapshot(snapshotOf(t, nil,
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(live.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}
	resp, body := do(t, gw, "/api/hello")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("空快照应返回 404，实际 %d %s", resp.StatusCode, body)
	}
	if live.calls.Load() != 0 {
		t.Fatal("空快照不应转发")
	}
}

// TestApplySnapshotRejectsBrokenPayload 缺分区/载荷损坏时保留旧视图，网关继续可用。
func TestApplySnapshotRejectsBrokenPayload(t *testing.T) {
	live := newUpstream(t, 0)
	gw := newGW(t, config.Micro{})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Weight: 100}},
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(live.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}
	if resp, _ := do(t, gw, "/api/x"); resp.StatusCode != http.StatusOK {
		t.Fatalf("前置快照应生效，实际 %d", resp.StatusCode)
	}

	// 缺 route 分区。
	if err := gw.ApplySnapshot(&gov.Snapshot{GlobalRev: 3, Parts: map[string]gov.Part{}}); err == nil {
		t.Fatal("缺 route 分区应报错")
	}
	// 载荷损坏。
	if err := gw.ApplySnapshot(&gov.Snapshot{GlobalRev: 4, Parts: map[string]gov.Part{
		gov.PartRoute: {Rev: 4, Payload: json.RawMessage(`{broken`)},
	}}); err == nil {
		t.Fatal("损坏的 route 载荷应报错")
	}
	// 两次失败都不能破坏已有视图。
	if resp, body := do(t, gw, "/api/x"); resp.StatusCode != http.StatusOK {
		t.Fatalf("失败的应用不应影响旧视图，实际 %d %s", resp.StatusCode, body)
	}
	// nil 快照是空操作。
	if err := gw.ApplySnapshot(nil); err != nil {
		t.Fatalf("nil 快照应被忽略: %v", err)
	}
}

// TestSnapshotVersionFilter 路由指定版本时只转发到该版本的实例。
func TestSnapshotVersionFilter(t *testing.T) {
	v1 := newUpstream(t, 0)
	v2 := newUpstream(t, 0)
	gw := newGW(t, config.Micro{})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Version: "2.0.0", Weight: 100}},
		map[string][]gov.Instance{"svc": {
			{ID: "i1", Addr: addrOf(v1.srv), Version: "1.0.0", Weight: 100},
			{ID: "i2", Addr: addrOf(v2.srv), Version: "2.0.0", Weight: 100},
		}},
	)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if resp, body := do(t, gw, "/api/x"); resp.StatusCode != http.StatusOK {
			t.Fatalf("应转发成功，实际 %d %s", resp.StatusCode, body)
		}
	}
	if v1.calls.Load() != 0 {
		t.Fatalf("版本不符的实例不应被调用，实际 %d 次", v1.calls.Load())
	}
	if v2.calls.Load() != 4 {
		t.Fatalf("应全部命中 2.0.0 实例，实际 %d 次", v2.calls.Load())
	}
}

// TestSnapshotNoInstances 服务没有实例时返回 503，而不是空指针或 502。
func TestSnapshotNoInstances(t *testing.T) {
	gw := newGW(t, config.Micro{})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "ghost", Weight: 100}},
		map[string][]gov.Instance{"ghost": {}},
	)); err != nil {
		t.Fatal(err)
	}
	resp, body := do(t, gw, "/api/x")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("无可用实例应 503，实际 %d %s", resp.StatusCode, body)
	}
}

// TestSnapshotRetryOnFailure 配了重试后，第一个实例连不上要换下一个实例再试。
func TestSnapshotRetryOnFailure(t *testing.T) {
	live := newUpstream(t, 0)
	// 127.0.0.1:1 必然连不上。
	gw := newGW(t, config.Micro{})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Weight: 100, Retries: 2}},
		map[string][]gov.Instance{"svc": {
			{ID: "dead", Addr: "127.0.0.1:1", Weight: 100},
			{ID: "live", Addr: addrOf(live.srv), Weight: 100},
		}},
	)); err != nil {
		t.Fatal(err)
	}

	// 轮询会先打到 dead，失败后重试；只要最终能成功即可。
	resp, body := do(t, gw, "/api/x")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("重试后应成功，实际 %d %s", resp.StatusCode, body)
	}
	if live.calls.Load() == 0 {
		t.Fatal("应至少命中一次可用实例")
	}
}

// TestSnapshotTimeout 路由级超时生效：上游慢于超时时间应报错而不是一直等。
func TestSnapshotTimeout(t *testing.T) {
	slow := newUpstream(t, 300*time.Millisecond)
	gw := newGW(t, config.Micro{})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Weight: 100, TimeoutMs: 30}},
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(slow.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	resp, body := do(t, gw, "/api/x")
	elapsed := time.Since(start)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("超时不应返回 200: %s", body)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("超时未生效，耗时 %v", elapsed)
	}
}

// TestSnapshotBalanceAcrossInstances 两个等价实例应被轮流使用，而不是只打一台。
func TestSnapshotBalanceAcrossInstances(t *testing.T) {
	a := newUpstream(t, 0)
	b := newUpstream(t, 0)
	gw := newGW(t, config.Micro{})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Weight: 100}},
		map[string][]gov.Instance{"svc": {
			{ID: "a", Addr: addrOf(a.srv), Weight: 100},
			{ID: "b", Addr: addrOf(b.srv), Weight: 100},
		}},
	)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if resp, body := do(t, gw, "/api/x"); resp.StatusCode != http.StatusOK {
			t.Fatalf("第 %d 次请求失败: %d %s", i+1, resp.StatusCode, body)
		}
	}
	if a.calls.Load() != 5 || b.calls.Load() != 5 {
		t.Fatalf("应各命中 5 次，实际 a=%d b=%d", a.calls.Load(), b.calls.Load())
	}
}
