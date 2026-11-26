package micro

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// TestGatewayConcurrentSnapshotSwap 请求热路径与快照替换并发进行必须安全。
//
// 数据面每收到一次新快照就整体换表。这条用例让 32 个 goroutine 持续打请求、
// 同时另一个 goroutine 反复切换上游，验证：不 panic、不出现 5xx、
// 每个请求都恰好被转发一次（既不能丢，也不能被重复计到两台）。
func TestGatewayConcurrentSnapshotSwap(t *testing.T) {
	a := newUpstream(t, 0)
	b := newUpstream(t, 0)
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{{Prefix: "/api", Service: "svc"}}})

	mk := func(addr string) *gov.Snapshot {
		return snapshotOf(t,
			[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Weight: 100}},
			map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addr, Weight: 100}}})
	}
	if err := gw.ApplySnapshot(mk(addrOf(a.srv))); err != nil {
		t.Fatalf("首次应用快照失败: %v", err)
	}

	engine := gin.New()
	engine.Any("/api/*path", gw.Handler())
	srv := httptest.NewServer(engine)
	defer srv.Close()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64}}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var bad atomic.Int64
	var ok atomic.Int64
	const workers = 32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := client.Get(srv.URL + "/api/ping") //nolint:noctx // 测试代码
				if err != nil {
					bad.Add(1)
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					bad.Add(1)
					continue
				}
				if string(body) != "upstream:/api/ping" && string(body) != "upstream:/ping" {
					bad.Add(1)
					continue
				}
				ok.Add(1)
			}
		}()
	}

	// 写侧：A/B 交替切换。
	for i := 0; i < 200; i++ {
		target := addrOf(a.srv)
		if i%2 == 0 {
			target = addrOf(b.srv)
		}
		if err := gw.ApplySnapshot(mk(target)); err != nil {
			t.Errorf("切换快照失败: %v", err)
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()

	if bad.Load() != 0 {
		t.Fatalf("并发请求出现 %d 次异常（成功 %d 次）", bad.Load(), ok.Load())
	}
	if got, want := ok.Load(), a.calls.Load()+b.calls.Load(); got != want {
		t.Fatalf("成功响应数与上游收到的请求数不一致：%d != %d", got, want)
	}
}

// TestApplySnapshotAtomicOnBadPayload 坏快照不能把正在服务的路由表清空。
func TestApplySnapshotAtomicOnBadPayload(t *testing.T) {
	a := newUpstream(t, 0)
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{{Prefix: "/api", Service: "svc"}}})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "r1", Prefix: "/api", Service: "svc", Weight: 100}},
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(a.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}

	// 只有 registry 分区、没有 route 分区：必须报错并保留旧表。
	regPayload := []byte(`{"rev":9,"services":{}}`)
	if err := gw.ApplySnapshot(&gov.Snapshot{GlobalRev: 9, Parts: map[string]gov.Part{
		gov.PartRegistry: {Rev: 9, Checksum: gov.Checksum(regPayload), Payload: regPayload},
	}}); err == nil {
		t.Fatal("缺少路由分区的快照应被拒绝")
	}
	resp, body := do(t, gw, "/api/hello")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("坏快照不应影响现有路由，实际 %d %s", resp.StatusCode, body)
	}

	// nil 快照直接忽略，不 panic。
	if err := gw.ApplySnapshot(nil); err != nil {
		t.Fatalf("nil 快照应被忽略: %v", err)
	}
}

// TestGatewayTimeoutPerRoute 路由级超时对上游慢响应生效，且不会污染后续请求。
func TestGatewayTimeoutPerRoute(t *testing.T) {
	slow := newUpstream(t, 200*time.Millisecond)
	fast := newUpstream(t, 0)
	gw := newGW(t, config.Micro{Routes: []config.ProxyRoute{{Prefix: "/api", Service: "svc"}}})
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{
			{ID: "slow", Prefix: "/api/slow", Service: "svc", Weight: 100, TimeoutMs: 50},
			{ID: "fast", Prefix: "/api", Service: "svc", Weight: 100},
		},
		map[string][]gov.Instance{"svc": {{ID: "i1", Addr: addrOf(slow.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	resp, _ := do(t, gw, "/api/slow")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("超时应返回 503，实际 %d", resp.StatusCode)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("超时未生效，耗时 %v", d)
	}

	// 换到快的上游后，同一个前缀应立刻恢复正常——超时不应被固化成熔断。
	if err := gw.ApplySnapshot(snapshotOf(t,
		[]gov.Route{{ID: "fast", Prefix: "/api", Service: "svc", Weight: 100}},
		map[string][]gov.Instance{"svc": {{ID: "i2", Addr: addrOf(fast.srv), Weight: 100}}},
	)); err != nil {
		t.Fatal(err)
	}
	resp, body := do(t, gw, "/api/ping")
	if resp.StatusCode != http.StatusOK || body != "upstream:/api/ping" {
		t.Fatalf("换上游后应恢复，实际 %d %s", resp.StatusCode, body)
	}
}
