package govserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// TestNextRevConcurrentUnique 并发分配版本号必须唯一且严格递增。
//
// 版本号是数据面判断"新旧"的唯一依据。两个并发发布拿到同一个号，后写入的
// 那一个在数据面看来就是"和手上的一样"，永远不会被应用——路由改了但全网
// 不生效，且没有任何报错。
func TestNextRevConcurrentUnique(t *testing.T) {
	_, db := newTestServer(t) // 初始 global_rev = 1
	const n = 40

	revs := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = db.Transaction(func(tx *gorm.DB) error {
				rev, err := NextRev(tx)
				if err != nil {
					return err
				}
				revs[i] = rev
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个事务分配版本失败: %v", i, err)
		}
	}
	sorted := append([]int64(nil), revs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Fatalf("版本号重复: %d（全部：%v）", sorted[i], sorted)
		}
	}
	if sorted[0] != 2 || sorted[len(sorted)-1] != n+1 {
		t.Fatalf("版本号应连续落在 2..%d，实际 %v", n+1, sorted)
	}
}

// TestBuildConcurrentWithWriters 并发构建快照时读写互不干扰。
//
// 控制面一边被数据面轮询、一边被运维写：任何一次 Build 都不能 500，
// 且每个分区的校验和必须与载荷一致，否则数据面会整体拒收。
func TestBuildConcurrentWithWriters(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()

	// 先造一点基础数据：一个服务、两个实例、一条已发布路由。
	if err := db.Create(&model.GovService{Name: "svc", Status: "enabled"}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Create(&model.GovInstance{
			ServiceID: "svc", InstanceID: fmt.Sprintf("i%d", i),
			Host: "10.0.0.1", Port: 8080 + i, Status: "online", Weight: 100,
			LastSeen: time.Now(),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	publishRoute(t, db, s, 2, "r1", "/api/v1")

	var stop sync.WaitGroup
	done := make(chan struct{})
	var buildErr, invalid int
	var mu sync.Mutex

	for i := 0; i < 8; i++ {
		stop.Add(1)
		go func(i int) {
			defer stop.Done()
			inst := []string{"", "i0", "i1", "unknown"}[i%4]
			for {
				select {
				case <-done:
					return
				default:
				}
				snap, err := s.Build(ctx, inst)
				mu.Lock()
				if err != nil {
					buildErr++
				} else {
					if _, ok := snap.Parts[gov.PartRoute]; !ok {
						invalid++
					}
					for key, p := range snap.Parts {
						if p.Checksum != gov.Checksum(p.Payload) {
							invalid++
							_ = key
						}
					}
				}
				mu.Unlock()
			}
		}(i)
	}

	// 写侧：持续注册实例、发布新版本。
	for round := 3; round <= 8; round++ {
		if err := db.Create(&model.GovInstance{
			ServiceID: "svc", InstanceID: fmt.Sprintf("n%d", round),
			Host: "10.0.0.2", Port: 9000 + round, Status: "online", Weight: 100,
			LastSeen: time.Now(),
		}).Error; err != nil {
			t.Fatalf("并发写入实例失败: %v", err)
		}
		publishRoute(t, db, s, int64(round), fmt.Sprintf("r%d", round), fmt.Sprintf("/api/v%d", round))
	}
	close(done)
	stop.Wait()

	if buildErr != 0 {
		t.Fatalf("并发构建出现 %d 次错误", buildErr)
	}
	if invalid != 0 {
		t.Fatalf("并发构建出现 %d 次非法快照", invalid)
	}
	// 最后一次构建必须看到最新发布。
	snap, err := s.Build(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	rp, ok := snap.Routes()
	if !ok || len(rp.Routes) != 1 || rp.Routes[0].ID != "r8" {
		t.Fatalf("最终路由不是 r8: ok=%v %+v", ok, rp.Routes)
	}
}

// TestCachePutRejectsStaleGeneration 慢构建不能把过期快照塞回缓存。
//
// 场景：Build 已经开始拼装（读到的是旧数据），此时一次发布把缓存清掉，
// 随后那个慢 Build 才写缓存。若不做代际校验，接下来整整一秒（缓存 TTL）
// 数据面拿到的都是发布前的旧快照，表现为"发布了但网关没反应"。
func TestCachePutRejectsStaleGeneration(t *testing.T) {
	s, _ := newTestServer(t)
	snap := &gov.Snapshot{GlobalRev: 1, Parts: map[string]gov.Part{}}

	gen := s.generation()
	// 模拟：慢构建拿到 gen 之后，发生了发布。
	s.Bump()
	s.cachePut("inst", gen, snap)

	s.cacheMu.Lock()
	_, cached := s.cache["inst"]
	s.cacheMu.Unlock()
	if cached {
		t.Fatal("发布之后不允许用旧代际的构建结果回填缓存")
	}

	// 代际未变时正常缓存。
	gen = s.generation()
	s.cachePut("inst", gen, snap)
	s.cacheMu.Lock()
	_, cached = s.cache["inst"]
	s.cacheMu.Unlock()
	if !cached {
		t.Fatal("代际未变时应正常写入缓存")
	}
}

// TestBumpConcurrentSubscribers 并发广播不能阻塞、不能丢订阅者。
func TestBumpConcurrentSubscribers(t *testing.T) {
	s, _ := newTestServer(t)
	s.Bump() // 先建好版本行

	const subs = 20
	chans := make([]chan int64, subs)
	for i := range chans {
		ch := make(chan int64, 8)
		chans[i] = ch
		s.subMu.Lock()
		s.subs[ch] = struct{}{}
		s.subMu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				s.Bump()
			}
		}()
	}
	wg.Wait()

	for i, ch := range chans {
		select {
		case <-ch:
		default:
			t.Fatalf("第 %d 个订阅者没有收到任何变更信号", i)
		}
	}
	s.subMu.Lock()
	if len(s.subs) != subs {
		t.Fatalf("订阅者数量被改坏: %d", len(s.subs))
	}
	s.subMu.Unlock()
}

// TestBuildCacheIsolatedPerInstance 灰度按实例缓存，不能把某个实例的视图
// 泄漏给另一个实例。
func TestBuildCacheIsolatedPerInstance(t *testing.T) {
	s, db := newTestServer(t)
	ctx := context.Background()
	if err := db.Create(&model.GovService{Name: "svc", Status: "enabled"}).Error; err != nil {
		t.Fatal(err)
	}
	// canary 只给 i0 看 r1，全量是 r0。
	publishRouteTo(t, db, s, 2, "r0", "/api/v1", "")
	publishRouteTo(t, db, s, 3, "r1", "/api/v2", "i0")

	canary, err := s.Build(ctx, "i0")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Build(ctx, "i1")
	if err != nil {
		t.Fatal(err)
	}
	got := func(snap *gov.Snapshot) string {
		rp, _ := snap.Routes()
		if len(rp.Routes) != 1 {
			return "?"
		}
		return rp.Routes[0].ID
	}
	if got(canary) != "r1" {
		t.Fatalf("灰度实例应看到 r1，实际 %s", got(canary))
	}
	if got(other) != "r0" {
		t.Fatalf("名单外实例应看到 r0，实际 %s", got(other))
	}
}

// publishRoute 直接把一条路由写成 active 的全量发布。
func publishRoute(t *testing.T, db *gorm.DB, s *Server, rev int64, id, prefix string) {
	t.Helper()
	publishRouteTo(t, db, s, rev, id, prefix, "")
}

func publishRouteTo(t *testing.T, db *gorm.DB, s *Server, rev int64, id, prefix, targets string) {
	t.Helper()
	payload, err := json.Marshal(gov.RoutePayload{Rev: rev, Routes: []gov.Route{
		{ID: id, Name: id, Prefix: prefix, Service: "svc", Weight: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	stage := "full"
	if targets != "" {
		stage = "canary"
	}
	targetsJSON := "[]"
	if targets != "" {
		targetsJSON = `["` + targets + `"]`
	}
	// 同一域旧的 active 收口为 done。
	if err := db.Model(&model.GovRelease{}).
		Where("domain = ? AND status = ?", "route", "active").
		Update("status", "done").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.GovRelease{
		Domain: "route", Revision: rev, Payload: payload, Checksum: gov.Checksum(payload),
		Targets: targetsJSON, Stage: stage, Status: "active",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.GovRevision{}).Where("id = 1").Update("global_rev", rev).Error; err != nil {
		t.Fatal(err)
	}
	s.Bump()
}
