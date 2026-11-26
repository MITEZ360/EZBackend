package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
)

// TestConcurrentRegisterSameInstance 同一实例被并发注册（进程重启、双发都
// 可能发生）必须幂等：不能报错，也不能写出两条记录。
func TestConcurrentRegisterSameInstance(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	const n = 32
	ids := make([]string, n)
	errsOf := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			row, err := s.Register(ctx, RegisterInput{
				Service: "same-svc", Instance: "same-1", Host: "10.0.0.8", Port: 8080,
				Version: "1.0.0", Weight: 100,
			})
			errsOf[i] = err
			if row != nil {
				ids[i] = row.ID
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errsOf {
		if err != nil {
			t.Fatalf("第 %d 次并发注册失败: %v", i, err)
		}
	}
	first := ids[0]
	for i, id := range ids {
		if id != first {
			t.Fatalf("同一实例返回了不同的主键: [0]=%s [%d]=%s", first, i, id)
		}
	}

	var count int64
	if err := s.db.Model(&model.GovInstance{}).Where("service_id = ? AND instance_id = ?", "same-svc", "same-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("同一实例应只有 1 条记录，实际 %d", count)
	}
}

// TestConcurrentRegisterNewService 一组新实例同时上线时，服务建档必须是幂等的。
//
// 逐个注册都有"查不到就建"的判断；并发下多个 goroutine 会同时判定"服务不存在"
// 然后一起插入。唯一索引挡住重复，但如果把数据库错误直接抛给调用方，扩容时
// 就有一部分实例注册失败——这正是最不该失败的时刻。
func TestConcurrentRegisterNewService(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Register(ctx, RegisterInput{
				Service: "burst-svc", Instance: fmt.Sprintf("b%d", i),
				Host: "10.0.1.1", Port: 8000 + i, Version: "1.0.0", Weight: 100,
			})
			if err != nil {
				errCh <- fmt.Errorf("第 %d 个实例: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发注册新服务失败: %v", err)
	}

	var svcCount, insCount int64
	s.db.Model(&model.GovService{}).Where("name = ?", "burst-svc").Count(&svcCount)
	s.db.Model(&model.GovInstance{}).Where("service_id = ?", "burst-svc").Count(&insCount)
	if svcCount != 1 {
		t.Fatalf("服务应只建档 1 次，实际 %d", svcCount)
	}
	if insCount != n {
		t.Fatalf("应有 %d 个实例，实际 %d", n, insCount)
	}
}

// TestConcurrentCreateServiceSameName 同名并发创建：一个成功，其余必须是
// 冲突（409），不能是 500——500 会让人以为是服务端故障，实际只是重名。
func TestConcurrentCreateServiceSameName(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	errsOf := make([]error, n)
	var okCount int
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateService(ctx, ServiceInput{Name: "race-svc"})
			errsOf[i] = err
			if err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if okCount != 1 {
		t.Fatalf("同名并发创建应恰好成功 1 次，实际 %d", okCount)
	}
	for i, err := range errsOf {
		if err == nil {
			continue
		}
		var e *errs.Error
		if !asErr(err, &e) {
			t.Fatalf("第 %d 个错误不是业务错误: %v", i, err)
		}
		if e.Status != 409 {
			t.Fatalf("第 %d 个错误应为冲突(409)，实际 %d: %v", i, e.Status, err)
		}
	}
}

// TestRegisterInvalidParams 极端参数必须被挡在入库之前。
//
// 校验只做 "port<=0" 是不够的：70000 这样的端口能存进库，还会被下发到网关，
// 每次转发都拨号失败，而服务目录里看着是健康的。
func TestRegisterInvalidParams(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		in   RegisterInput
	}{
		{"服务为空", RegisterInput{Host: "10.0.0.1", Port: 8080}},
		{"主机为空", RegisterInput{Service: "svc", Port: 8080}},
		{"端口为 0", RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 0}},
		{"端口为负", RegisterInput{Service: "svc", Host: "10.0.0.1", Port: -1}},
		{"端口超界", RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 70000}},
		{"主机含空格", RegisterInput{Service: "svc", Host: "10.0.0.1 8080", Port: 8080}},
		{"服务名含空格", RegisterInput{Service: "bad svc", Host: "10.0.0.1", Port: 8080}},
		{"服务名含斜杠", RegisterInput{Service: "bad/svc", Host: "10.0.0.1", Port: 8080}},
		{"服务名过长", RegisterInput{Service: strings.Repeat("a", 65), Host: "10.0.0.1", Port: 8080}},
		{"主机过长", RegisterInput{Service: "svc", Host: strings.Repeat("h", 256), Port: 8080}},
		{"负权重", RegisterInput{Service: "svc", Host: "10.0.0.1", Port: 8080, Weight: -5}},
	}
	for _, tc := range cases {
		if _, err := s.Register(ctx, tc.in); err == nil {
			t.Fatalf("%s 应被拒绝，实际通过", tc.name)
		}
	}

	var count int64
	s.db.Model(&model.GovInstance{}).Count(&count)
	if count != 0 {
		t.Fatalf("非法注册不应留下记录，实际 %d 条", count)
	}
}

// TestCreateServiceNameValidation 服务名是路由与实例的主键，必须可作标识符。
func TestCreateServiceNameValidation(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	for _, name := range []string{
		"bad name", "bad/svc", "\u0000null", strings.Repeat("x", 65),
	} {
		if _, err := s.CreateService(ctx, ServiceInput{Name: name}); err == nil {
			t.Fatalf("服务名 %q 应被拒绝", name)
		}
	}
	// 正常名字照常通过，包括中划线、下划线、点与大小写。
	for _, name := range []string{"order-svc", "order_svc", "order.svc", "OrderSvc"} {
		if _, err := s.CreateService(ctx, ServiceInput{Name: name}); err != nil {
			t.Fatalf("服务名 %q 应被接受: %v", name, err)
		}
	}
}

// TestDeregisterScopedByService 同名 instanceId 分属两个服务时，注销必须
// 只影响指定的那个。
//
// instanceId 默认取 host:port，两个服务监听同一地址（比如都通过 sidecar）
// 时会撞号。不带服务名注销会把两个服务一起摘掉，属于静默误伤。
func TestDeregisterScopedByService(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	for _, svc := range []string{"svc-a", "svc-b"} {
		if _, err := s.Register(ctx, RegisterInput{
			Service: svc, Instance: "shared-1", Host: "10.0.0.1", Port: 8080,
		}); err != nil {
			t.Fatalf("注册 %s 失败: %v", svc, err)
		}
	}

	if err := s.DeregisterScoped(ctx, "svc-a", "shared-1"); err != nil {
		t.Fatalf("按服务注销失败: %v", err)
	}
	var a, b model.GovInstance
	if err := s.db.Where("service_id = ? AND instance_id = ?", "svc-a", "shared-1").First(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Where("service_id = ? AND instance_id = ?", "svc-b", "shared-1").First(&b).Error; err != nil {
		t.Fatal(err)
	}
	if a.Status != "offline" {
		t.Fatalf("svc-a 应被置为 offline，实际 %s", a.Status)
	}
	if b.Status != "online" {
		t.Fatalf("svc-b 不应被误摘，实际 %s", b.Status)
	}
}

// TestHeartbeatRejectsUnknownInstance 未注册的实例心跳应报"未注册"，
// 而不是静默成功——后者会让接入方以为注册成功。
func TestHeartbeatRejectsUnknownInstance(t *testing.T) {
	s, _ := newTestService(t)
	err := s.Heartbeat(context.Background(), RegisterInput{Service: "ghost", Instance: "nope"})
	if err == nil {
		t.Fatal("未注册实例的心跳应失败")
	}
	var e *errs.Error
	if !asErr(err, &e) || e.Status != 404 {
		t.Fatalf("应为 404，实际 %v", err)
	}
}

// TestRegisterPortBoundaries 合法边界值必须接受（1 与 65535）。
func TestRegisterPortBoundaries(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	for _, port := range []int{1, 65535} {
		if _, err := s.Register(ctx, RegisterInput{
			Service: "edge-svc", Instance: fmt.Sprintf("p%d", port), Host: "127.0.0.1", Port: port,
		}); err != nil {
			t.Fatalf("端口 %d 应被接受: %v", port, err)
		}
	}
}

// TestHeartbeatStatusContract 心跳与人工状态的交互必须明确：
//   - draining（运维摘流）必须粘住，不能因为实例还活着就被心跳改回 online；
//   - offline 且仍在心跳，说明进程活着，应恢复为 online（否则一次误摘除
//     会让实例永远接不到流量，只能人工干预）。
func TestHeartbeatStatusContract(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	in := RegisterInput{Service: "hb-svc", Instance: "hb-1", Host: "10.0.0.5", Port: 8080}
	if _, err := s.Register(ctx, in); err != nil {
		t.Fatal(err)
	}

	if err := s.SetStatus(ctx, "hb-1", "draining"); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, in); err != nil {
		t.Fatal(err)
	}
	got := instanceStatus(t, s, "hb-svc", "hb-1")
	if got != "draining" {
		t.Fatalf("draining 应被心跳保留，实际 %s", got)
	}

	if err := s.SetStatus(ctx, "hb-1", "offline"); err != nil {
		t.Fatal(err)
	}
	if err := s.Heartbeat(ctx, in); err != nil {
		t.Fatal(err)
	}
	if got := instanceStatus(t, s, "hb-svc", "hb-1"); got != "online" {
		t.Fatalf("offline 实例心跳后应恢复 online，实际 %s", got)
	}
}

func instanceStatus(t *testing.T, s *Service, service, instance string) string {
	t.Helper()
	var row model.GovInstance
	if err := s.db.Where("service_id = ? AND instance_id = ?", service, instance).First(&row).Error; err != nil {
		t.Fatalf("读取实例失败: %v", err)
	}
	return row.Status
}

// asErr 是 errors.As 的小包装，避免每处都写类型断言样板。
func asErr(err error, target **errs.Error) bool {
	for err != nil {
		if e, ok := err.(*errs.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
