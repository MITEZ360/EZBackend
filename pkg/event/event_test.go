package event

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncSubscribe(t *testing.T) {
	b := New(Options{QueueSize: 8, Workers: 2})
	defer b.Close()

	var got atomic.Value
	b.Subscribe("user.created", func(_ context.Context, e Event) {
		got.Store(e.Data.(string))
	})
	b.Publish(context.Background(), "user.created", "u1")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if v, ok := got.Load().(string); ok && v == "u1" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("异步订阅未收到事件")
}

func TestSyncSubscribe(t *testing.T) {
	b := New(Options{})
	defer b.Close()

	var called bool
	b.SubscribeSync("order.paid", func(context.Context, Event) { called = true })
	b.Publish(context.Background(), "order.paid", nil)
	if !called {
		t.Fatal("同步订阅应在 Publish 返回前执行")
	}
}

func TestWildcard(t *testing.T) {
	b := New(Options{})
	defer b.Close()

	var count atomic.Int32
	b.Subscribe("*", func(context.Context, Event) { count.Add(1) })
	b.PublishSync(context.Background(), "a.b", nil)
	b.PublishSync(context.Background(), "c.d", nil)
	if count.Load() != 2 {
		t.Fatalf("通配订阅应收到 2 个事件，实际 %d", count.Load())
	}
}

func TestPanicIsolated(t *testing.T) {
	var errs atomic.Int32
	b := New(Options{OnError: func(string, any, []byte) { errs.Add(1) }})
	defer b.Close()

	b.Subscribe("boom", func(context.Context, Event) { panic("handler panic") })
	var ok atomic.Bool
	b.Subscribe("boom", func(context.Context, Event) { ok.Store(true) })

	b.PublishSync(context.Background(), "boom", nil)
	if errs.Load() != 1 {
		t.Fatalf("panic 应被捕获并回调 1 次，实际 %d", errs.Load())
	}
	if !ok.Load() {
		t.Fatal("panic 不应影响后续订阅者")
	}
}

func TestQueueOverflowDrops(t *testing.T) {
	// 不启动消费：Workers 会在后台消费，因此用一个阻塞的订阅者占满 worker
	block := make(chan struct{})
	b := New(Options{QueueSize: 1, Workers: 1})
	defer func() { close(block); b.Close() }()

	b.Subscribe("slow", func(context.Context, Event) { <-block })
	for i := 0; i < 50; i++ {
		b.Publish(context.Background(), "slow", i)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && b.Stats().Dropped == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if b.Stats().Dropped == 0 {
		t.Fatal("队列满时应丢弃事件而不是阻塞")
	}
}

func TestConcurrentPublish(t *testing.T) {
	b := New(Options{QueueSize: 1024, Workers: 8})
	defer b.Close()

	var count atomic.Int64
	b.Subscribe("counter", func(context.Context, Event) { count.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				b.Publish(context.Background(), "counter", j)
			}
		}()
	}
	wg.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && count.Load() < 800 {
		time.Sleep(10 * time.Millisecond)
	}
	if count.Load() != 800 {
		t.Fatalf("期望处理 800 个事件，实际 %d（stats=%+v）", count.Load(), b.Stats())
	}
}

func TestNilBusSafe(t *testing.T) {
	var b *Bus
	b.Publish(context.Background(), "x", nil) // 不应 panic
	b.Subscribe("x", func(context.Context, Event) {})
	b.Close()
	if b.Stats() != (Stats{}) {
		t.Fatal("空总线的统计应为零值")
	}
}

func TestCloseDrainsQueue(t *testing.T) {
	b := New(Options{QueueSize: 16, Workers: 2})
	var count atomic.Int32
	b.Subscribe("drain", func(context.Context, Event) { count.Add(1) })
	for i := 0; i < 10; i++ {
		b.Publish(context.Background(), "drain", i)
	}
	b.Close()
	if count.Load() != 10 {
		t.Fatalf("关闭时应处理完队列，实际 %d", count.Load())
	}
}
