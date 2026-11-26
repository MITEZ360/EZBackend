package gov

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStoreConcurrentSwapAndRead 读写并发下快照必须始终自洽。
//
// 网关的请求路径只做一次 atomic load，没有锁；这条用例保证"整体替换"
// 这个约定被守住：读者要么看到旧版本，要么看到新版本，不会读到写了一半
// 的分区表，也不会读到被原地修改的载荷。
func TestStoreConcurrentSwapAndRead(t *testing.T) {
	var st Store
	mk := func(gen int) *Snapshot {
		payload := []byte(`{"rev":` + itoa(gen) + `,"routes":[]}`)
		return &Snapshot{GlobalRev: int64(gen), Parts: map[string]Part{
			PartRoute: {Rev: int64(gen), Checksum: Checksum(payload), Payload: payload},
		}}
	}
	st.Store(mk(0))

	const gens = 200
	valid := make(map[string]bool, gens+1)
	for i := 0; i <= gens; i++ {
		valid[mk(i).Fingerprint()] = true
	}

	var stop atomic.Bool
	var bad atomic.Int64
	var reads atomic.Int64
	var wg sync.WaitGroup

	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := offset; i <= gens; i += 4 {
				st.Store(mk(i))
			}
		}(w)
	}
	for r := 0; r < 16; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				snap := st.Load()
				reads.Add(1)
				if snap == nil || len(snap.Parts) != 1 {
					bad.Add(1)
					continue
				}
				p, ok := snap.Part(PartRoute)
				if !ok || p.Checksum != Checksum(p.Payload) {
					bad.Add(1)
					continue
				}
				if _, ok := snap.Routes(); !ok {
					bad.Add(1)
					continue
				}
				if !valid[snap.Fingerprint()] {
					bad.Add(1)
				}
			}
		}()
	}

	time.Sleep(120 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if bad.Load() != 0 {
		t.Fatalf("并发读写出现 %d 次不一致快照（共 %d 次读取）", bad.Load(), reads.Load())
	}
	if reads.Load() == 0 {
		t.Fatal("读者没有跑到，用例无效")
	}
}

// TestCompiledRoutesConcurrentMatch 并发匹配必须无锁竞争且结果稳定。
func TestCompiledRoutesConcurrentMatch(t *testing.T) {
	routes := []Route{
		{ID: "r1", Prefix: "/api/v1/order", Service: "order"},
		{ID: "r2", Prefix: "/api/v1", Service: "v1"},
		{ID: "r3", Prefix: "/api", Service: "api"},
		{ID: "r4", Prefix: "/api/v1/order", Methods: []string{"POST"}, Service: "order-post"},
	}
	c := Compile(routes)

	cases := []struct {
		method, path, want string
	}{
		{"GET", "/api/v1/order/1", "order"},
		{"POST", "/api/v1/order", "order-post"},
		{"GET", "/api/v1/user", "v1"},
		{"GET", "/api/other", "api"},
		{"GET", "/nope", ""},
	}
	const workers, rounds = 32, 300
	var wg sync.WaitGroup
	var mismatches atomic.Int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				for _, tc := range cases {
					got := ""
					if r := c.Match(tc.method, tc.path, http.Header{}, url.Values{}); r != nil {
						got = r.Service
					}
					if got != tc.want {
						mismatches.Add(1)
					}
				}
			}
		}()
	}
	wg.Wait()
	if mismatches.Load() != 0 {
		t.Fatalf("并发匹配出现 %d 次不一致", mismatches.Load())
	}
	if c.Len() != len(routes) {
		t.Fatalf("规则数应为 %d，实际 %d", len(routes), c.Len())
	}
}

// TestBalancerConcurrentWeighted 并发下单点决策必须精确：等权重实例各拿
// 相同的份额，不能因为竞争丢掉或重复计数。
func TestBalancerConcurrentWeighted(t *testing.T) {
	list := []Instance{
		{ID: "a", Weight: 10}, {ID: "b", Weight: 10}, {ID: "c", Weight: 10},
		{ID: "d", Weight: 10}, {ID: "e", Weight: 10},
	}
	b := NewBalancer(time.Second)
	counts := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	const workers, perWorker = 32, 100
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := map[string]int{}
			for i := 0; i < perWorker; i++ {
				ins, ok := b.Pick("svc", list, "")
				if !ok {
					t.Error("等权重实例不应选不出来")
					return
				}
				local[ins.ID]++
			}
			mu.Lock()
			for k, v := range local {
				counts[k] += v
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	total := workers * perWorker
	if got := len(counts); got != len(list) {
		t.Fatalf("应覆盖 %d 个实例，实际 %d", len(list), got)
	}
	sum := 0
	for _, n := range counts {
		sum += n
	}
	if sum != total {
		t.Fatalf("决策总数应为 %d，实际 %d", total, sum)
	}
	want := total / len(list)
	for id, n := range counts {
		if n < want-1 || n > want+1 {
			t.Fatalf("实例 %s 份额异常：%d（期望约 %d），分布 %v", id, n, want, counts)
		}
	}
}

// TestBalancerConcurrentFailSuccess 并发上报失败/恢复不能把冷却表写坏。
func TestBalancerConcurrentFailSuccess(t *testing.T) {
	b := NewBalancer(50 * time.Millisecond)
	list := []Instance{{ID: "a"}, {ID: "b"}}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				b.Fail("a")
			} else {
				b.Success("a")
			}
			if _, ok := b.Pick("svc", list, ""); !ok {
				t.Error("实例全在冷却时应退回全量而不是选不出来")
			}
			_ = b.Cooldown("a")
		}(i)
	}
	wg.Wait()
	if d := b.Cooldown("a"); d < 0 || d > time.Minute {
		t.Fatalf("冷却时长越界: %v", d)
	}
}

// TestLimiterConcurrentPerKey 每个 key 的令牌桶独立且精确：容量 10 的桶在
// 200 个并发请求下恰好放行 10 个，多放一个就说明有竞争。
func TestLimiterConcurrentPerKey(t *testing.T) {
	l := NewLimiter(0.0001, 10) // 补充速率近似为 0，避免测试期间回填干扰
	const keys, workers = 25, 200
	var wg sync.WaitGroup
	var allowed atomic.Int64
	for k := 0; k < keys; k++ {
		key := "key-" + itoa(k)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if l.Allow(key) {
					allowed.Add(1)
				}
			}()
		}
	}
	wg.Wait()
	if got, want := allowed.Load(), int64(keys*10); got != want {
		t.Fatalf("应放行 %d 次，实际 %d 次", want, got)
	}
}

// TestClientConcurrentSync 并发 Sync 不能把快照推回旧版本。
//
// Apply 内部串行化，但对外暴露的 Sync 会被 SSE、轮询、手动刷新三个来源同时
// 调用；这条用例确认无论谁先拿到锁，最终都收敛到最新内容。
func TestClientConcurrentSync(t *testing.T) {
	f, srv := newFakeControl(t, 1)
	var lastRev atomic.Int64
	c := newTestClient(t, srv, func(cfg *ClientConfig) {
		cfg.OnApply = func(s *Snapshot) {
			r := s.GlobalRev
			for {
				prev := lastRev.Load()
				if r < prev {
					t.Errorf("快照版本回退: %d -> %d", prev, r)
					return
				}
				if lastRev.CompareAndSwap(prev, r) {
					return
				}
			}
		}
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if err := c.Sync(); err != nil {
						t.Errorf("并发 Sync 失败: %v", err)
						return
					}
				}
			}
		}()
	}
	// 控制面持续推出新内容，版本与载荷同步前进。
	for rev := int64(2); rev <= 12; rev++ {
		f.setContent(rev, `{"rev":`+itoa(int(rev))+`,"routes":[{"id":"r`+itoa(int(rev))+`","prefix":"/api","service":"core"}]}`)
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
	wg.Wait()

	if err := c.Sync(); err != nil {
		t.Fatalf("收尾同步失败: %v", err)
	}
	if got := c.Snapshot().GlobalRev; got != 12 {
		t.Fatalf("最终应收敛到 12，实际 %d", got)
	}
	rp, ok := c.Snapshot().Routes()
	if !ok || len(rp.Routes) != 1 || rp.Routes[0].ID != "r12" {
		t.Fatalf("最终路由内容不对: ok=%v %+v", ok, rp.Routes)
	}
}

// TestSnapshotDecodeConcurrent 同一份快照被多个消费者同时解码时必须互不影响。
func TestSnapshotDecodeConcurrent(t *testing.T) {
	routePayload, _ := json.Marshal(RoutePayload{Rev: 3, Routes: []Route{{ID: "r1", Prefix: "/api", Service: "core"}}})
	regPayload, _ := json.Marshal(RegistryPayload{Rev: 3, Services: map[string]Service{
		"core": {Name: "core", Instances: []Instance{{ID: "i1", Addr: "127.0.0.1:9000", Weight: 100}}},
	}})
	snap := &Snapshot{GlobalRev: 3, Parts: map[string]Part{
		PartRoute:    {Rev: 3, Checksum: Checksum(routePayload), Payload: routePayload},
		PartRegistry: {Rev: 3, Checksum: Checksum(regPayload), Payload: regPayload},
	}}
	var wg sync.WaitGroup
	var bad atomic.Int64
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if rp, ok := snap.Routes(); !ok || len(rp.Routes) != 1 {
					bad.Add(1)
				}
				if reg, ok := snap.Registry(); !ok || len(reg.Services) != 1 {
					bad.Add(1)
				}
				if snap.Fingerprint() == "" {
					bad.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("并发解码出现 %d 次异常", bad.Load())
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestLimiterSweepsIdleBuckets 长期运行下空闲 key 必须被清理，
// 否则按用户/租户限流会让 map 无限增长，最终 OOM。
func TestLimiterSweepsIdleBuckets(t *testing.T) {
	l := NewLimiter(1, 1)
	if !l.Allow("idle-key") {
		t.Fatal("首次请求应放行")
	}
	// 把桶的活跃时间拨到 10 分钟前，并把上次清理时间拨到 2 分钟前，
	// 触发下一次 Allow 里的 sweep。
	l.mu.Lock()
	l.buckets["idle-key"].last = time.Now().Add(-10 * time.Minute)
	l.sweptAt = time.Now().Add(-2 * time.Minute)
	l.mu.Unlock()

	if !l.Allow("fresh-key") {
		t.Fatal("新 key 应放行")
	}
	l.mu.Lock()
	_, idle := l.buckets["idle-key"]
	_, fresh := l.buckets["fresh-key"]
	l.mu.Unlock()
	if idle {
		t.Fatal("空闲 10 分钟的桶应被清理")
	}
	if !fresh {
		t.Fatal("活跃的桶不应被清理")
	}
}
