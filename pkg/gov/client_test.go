package gov

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeControl 是一个最小可用的控制面：按内容指纹返回快照或 304，并记录收到的注册心跳。
type fakeControl struct {
	mu         sync.Mutex
	rev        int64
	payload    []byte // route 分区载荷，改它就等于改内容
	broken     bool   // 返回 500
	badChecks  bool   // 破坏分区校验和
	fetches    atomic.Int64
	registers  atomic.Int64
	heartbeats atomic.Int64
	lastBody   map[string]any
}

func (f *fakeControl) setRev(rev int64) {
	f.mu.Lock()
	f.rev = rev
	f.mu.Unlock()
}

// setContent 换掉 route 分区的内容；只改版本号不改内容时指纹不变，数据面应当收不到更新。
func (f *fakeControl) setContent(rev int64, payload string) {
	f.mu.Lock()
	f.rev = rev
	f.payload = []byte(payload)
	f.mu.Unlock()
}

// build 组装当前快照；badChecks 为真时把校验和打乱，模拟传输损坏。
func (f *fakeControl) build() Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := Checksum(f.payload)
	if f.badChecks {
		sum = Checksum([]byte("tampered"))
	}
	return Snapshot{GlobalRev: f.rev, Parts: map[string]Part{
		PartRoute: {Rev: 1, Checksum: sum, Payload: f.payload},
	}}
}

func (f *fakeControl) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/gov/snapshot", func(w http.ResponseWriter, r *http.Request) {
		f.fetches.Add(1)
		f.mu.Lock()
		broken := f.broken
		f.mu.Unlock()

		if broken {
			http.Error(w, "control plane down", http.StatusInternalServerError)
			return
		}
		out := f.build()
		if q := r.URL.Query().Get("fp"); q != "" && q == out.Fingerprint() {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	record := func(counter *atomic.Int64) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.lastBody = body
			f.mu.Unlock()
			// 计数放在记录之后：测试靠它判断"这次请求已经记下来了"，
			// 顺序反了就会读到上一次请求的 body。
			counter.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
		}
	}
	mux.HandleFunc("/api/v1/gov/instances/register", record(&f.registers))
	mux.HandleFunc("/api/v1/gov/instances/heartbeat", record(&f.heartbeats))
	return mux
}

func newFakeControl(t *testing.T, rev int64) (*fakeControl, *httptest.Server) {
	t.Helper()
	f := &fakeControl{
		rev:     rev,
		payload: []byte(`{"rev":1,"routes":[{"id":"r1","prefix":"/api","service":"core","weight":100}]}`),
	}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return f, srv
}

func newTestClient(t *testing.T, srv *httptest.Server, opts ...func(*ClientConfig)) *Client {
	t.Helper()
	cfg := ClientConfig{
		BaseURL:      srv.URL,
		Token:        "test-token",
		InstanceID:   "test-1",
		PollInterval: time.Hour, // 测试里手动驱动，不让后台轮询干扰
		SnapshotFile: filepath.Join(t.TempDir(), "snapshot.json"),
		Logger:       zap.NewNop(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return NewClient(cfg)
}

// TestClientApply 首次拉取应用快照，并落本地快照文件。
func TestClientApply(t *testing.T) {
	_, srv := newFakeControl(t, 5)
	var applied []int64
	c := newTestClient(t, srv, func(cfg *ClientConfig) {
		cfg.OnApply = func(s *Snapshot) { applied = append(applied, s.GlobalRev) }
	})

	if err := c.Sync(); err != nil {
		t.Fatalf("首次拉取失败: %v", err)
	}
	if got := c.Snapshot().GlobalRev; got != 5 {
		t.Fatalf("快照版本应为 5，实际 %d", got)
	}
	if len(applied) != 1 || applied[0] != 5 {
		t.Fatalf("OnApply 应恰好回调一次且为 5，实际 %v", applied)
	}
	rp, ok := c.Snapshot().Routes()
	if !ok || len(rp.Routes) != 1 || rp.Routes[0].Service != "core" {
		t.Fatalf("路由分区异常: ok=%v %+v", ok, rp)
	}
	// 本地快照必须落盘，供下次启动兜底。
	if _, err := os.Stat(c.cfg.SnapshotFile); err != nil {
		t.Fatalf("本地快照未写入: %v", err)
	}

	// 幂等：内容没变时控制面回 304，不重复回调。
	if err := c.Sync(); err != nil {
		t.Fatalf("重复拉取应无错: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("内容未变不应重复回调，实际 %v", applied)
	}
}

// TestClientRevisionOnlyChangeIsNoop 只推进版本号、内容不变时数据面不应重建视图。
//
// 这是把去重判据从版本号换成内容指纹的直接理由：版本号会被无关域的发布推高，
// 拿它去重等于替每个域都做了一次全量重建。
func TestClientRevisionOnlyChangeIsNoop(t *testing.T) {
	f, srv := newFakeControl(t, 5)
	c := newTestClient(t, srv)
	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}

	// 只动版本号，载荷一个字节都不变。
	f.setRev(9)
	if err := c.Sync(); err != nil {
		t.Fatalf("轮询失败: %v", err)
	}
	if got := c.Snapshot().GlobalRev; got != 5 {
		t.Fatalf("内容未变时不应替换快照，实际版本变成了 %d", got)
	}
}

// TestClientApplyNewerRevision 内容变化时更新快照；304 视为无变化。
func TestClientApplyNewerRevision(t *testing.T) {
	f, srv := newFakeControl(t, 5)
	c := newTestClient(t, srv)

	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}
	f.setContent(6, `{"rev":6,"routes":[{"id":"r1","prefix":"/api/v2","service":"core","weight":100}]}`)
	if err := c.Sync(); err != nil {
		t.Fatalf("拉取新版失败: %v", err)
	}
	if got := c.Snapshot().GlobalRev; got != 6 {
		t.Fatalf("应更新到 6，实际 %d", got)
	}
	if rp, _ := c.Snapshot().Routes(); len(rp.Routes) != 1 || rp.Routes[0].Prefix != "/api/v2" {
		t.Fatalf("应换成新内容，实际 %+v", rp.Routes)
	}

	// 内容不变时回 304，快照保持不变也不报错。
	if err := c.Sync(); err != nil {
		t.Fatalf("304 不应报错: %v", err)
	}
	if got := c.Snapshot().GlobalRev; got != 6 {
		t.Fatalf("304 不应改变快照，实际 %d", got)
	}

	// 内容继续变化时能追上。
	f.setContent(9, `{"rev":9,"routes":[]}`)
	if err := c.Sync(); err != nil {
		t.Fatalf("轮询新内容失败: %v", err)
	}
	if got := c.Snapshot().GlobalRev; got != 9 {
		t.Fatalf("应追上 9，实际 %d", got)
	}

	// Reload 不带指纹，即使内容一样也强制应用。
	var reloaded int
	c.cfg.OnApply = func(*Snapshot) { reloaded++ }
	if err := c.Reload(); err != nil {
		t.Fatalf("强制全量失败: %v", err)
	}
	if reloaded != 1 {
		t.Fatalf("Reload 应强制回调一次，实际 %d", reloaded)
	}
}

// TestClientKeepsOldSnapshotOnError 控制面故障或校验失败时保留旧快照。
func TestClientKeepsOldSnapshotOnError(t *testing.T) {
	f, srv := newFakeControl(t, 5)
	c := newTestClient(t, srv)
	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	f.broken = true
	f.rev = 6
	f.mu.Unlock()
	if err := c.Sync(); err == nil {
		t.Fatal("控制面 500 应返回错误")
	}
	if got := c.Snapshot().GlobalRev; got != 5 {
		t.Fatalf("失败时应保留旧快照 5，实际 %d", got)
	}

	f.mu.Lock()
	f.broken = false
	f.badChecks = true
	f.mu.Unlock()
	if err := c.Sync(); err == nil {
		t.Fatal("校验和不匹配应返回错误")
	}
	if got := c.Snapshot().GlobalRev; got != 5 {
		t.Fatalf("校验失败时应保留旧快照 5，实际 %d", got)
	}
}

// TestClientStartsFromLocalSnapshot 控制面不可达时靠本地快照启动。
func TestClientStartsFromLocalSnapshot(t *testing.T) {
	f, srv := newFakeControl(t, 5)
	dir := t.TempDir()
	snapshotFile := filepath.Join(dir, "snapshot.json")
	c := newTestClient(t, srv, func(cfg *ClientConfig) { cfg.SnapshotFile = snapshotFile })
	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = f

	// 换一个指向不可达地址的新客户端，复用同一份本地快照。
	dead := NewClient(ClientConfig{
		BaseURL:      "http://127.0.0.1:1", // 必然连不上
		SnapshotFile: snapshotFile,
		Logger:       zap.NewNop(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dead.Start(ctx)

	if got := dead.Snapshot().GlobalRev; got != 5 {
		t.Fatalf("应从本地快照启动到 rev=5，实际 %d", got)
	}
	if _, ok := dead.Snapshot().Routes(); !ok {
		t.Fatal("本地快照的路由分区应可用")
	}
}

// TestClientRegisterAndHeartbeat 自注册与心跳上报的字段必须能被控制面直接落库。
func TestClientRegisterAndHeartbeat(t *testing.T) {
	f, srv := newFakeControl(t, 3)
	c := newTestClient(t, srv, func(cfg *ClientConfig) {
		cfg.Announce = &AnnounceConfig{
			Service: "tenon-gateway", InstanceID: "gw-1",
			Host: "10.0.0.9", Port: 9100, Version: "1.0.0", Zone: "cn-a",
			Labels: map[string]string{"zone-tag": "edge"}, Interval: time.Hour,
		}
	})
	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := c.register(); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if f.registers.Load() != 1 {
		t.Fatalf("应收到 1 次注册，实际 %d", f.registers.Load())
	}

	f.mu.Lock()
	body := f.lastBody
	f.mu.Unlock()
	wants := map[string]any{
		"service": "tenon-gateway", "instanceId": "gw-1", "host": "10.0.0.9",
		"port": float64(9100), "version": "1.0.0", "zone": "cn-a",
		"configVersion": float64(3),
	}
	for k, want := range wants {
		if body[k] != want {
			t.Fatalf("注册体 %s 应为 %v，实际 %v（整体 %v）", k, want, body[k], body)
		}
	}
	// 自定义 label 原样保留，实现会额外补一个 role=data-plane 标记。
	labels, _ := body["labels"].(map[string]any)
	if labels["zone-tag"] != "edge" {
		t.Fatalf("自定义 label 应保留，实际 %v", body["labels"])
	}
	if labels["role"] != "data-plane" {
		t.Fatalf("应标注数据面角色，实际 %v", body["labels"])
	}

	c.reportApplied(4)
	// reportApplied 是异步的：等到心跳里的版本真的变成 4 为止，
	// 只等"心跳计数非 0"会在慢机器上读到注册那次的 body。
	var beatRev any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		beatRev = f.lastBody["configVersion"]
		f.mu.Unlock()
		if beatRev == float64(4) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if f.heartbeats.Load() == 0 {
		t.Fatal("应用快照后应回报版本")
	}
	if beatRev != float64(4) {
		t.Fatalf("心跳应带最新 rev=4，实际 %v", beatRev)
	}
}

// TestClientNoBaseURL 未配置控制面地址时报错而不是静默成功。
func TestClientNoBaseURL(t *testing.T) {
	c := NewClient(ClientConfig{Logger: zap.NewNop()})
	if err := c.Sync(); err == nil {
		t.Fatal("缺少控制面地址应报错")
	}
}

// TestClientAnnounceDefaultInstanceID 未显式给 instanceId 时回落到客户端标识。
func TestClientAnnounceDefaultInstanceID(t *testing.T) {
	f, srv := newFakeControl(t, 1)
	c := newTestClient(t, srv, func(cfg *ClientConfig) {
		cfg.InstanceID = "fallback-1"
		cfg.Announce = &AnnounceConfig{Service: "svc", Host: "127.0.0.1", Port: 80, Interval: time.Hour}
	})
	if got := c.announceInstanceID(); got != "fallback-1" {
		t.Fatalf("应回落到客户端 InstanceID，实际 %q", got)
	}
	if err := c.register(); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	f.mu.Lock()
	body := f.lastBody
	f.mu.Unlock()
	if body["instanceId"] != "fallback-1" {
		t.Fatalf("注册体应带回落 ID，实际 %v", body["instanceId"])
	}
}
