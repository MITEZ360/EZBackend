// Package event 提供一个轻量的进程内事件总线，用于解耦"业务写完"之后的副作用
// （指标统计、通知、缓存刷新、外部同步等）。
//
// 定位说明：它只解决单进程内的解耦，不替代消息队列。跨进程、需要可靠投递的场景
// 请使用 Kafka / RabbitMQ / RocketMQ 等成熟中间件。
package event

import (
	"context"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Event 是总线中流转的事件。
type Event struct {
	Topic string    `json:"topic"`
	Time  time.Time `json:"time"`
	Data  any       `json:"data,omitempty"`
}

// Handler 处理事件；异步处理器在独立 worker 中执行。
type Handler func(ctx context.Context, e Event)

// Stats 是总线的运行统计，可暴露到监控。
type Stats struct {
	Published uint64 `json:"published"`
	Delivered uint64 `json:"delivered"`
	Dropped   uint64 `json:"dropped"`
	Failed    uint64 `json:"failed"`
	QueueLen  int    `json:"queueLen"`
}

// Options 总线参数。
type Options struct {
	QueueSize int // 异步队列长度，默认 1024
	Workers   int // 异步 worker 数，默认 4
	// OnError 处理器 panic 或出错时回调，便于记录日志。
	OnError func(topic string, err any, stack []byte)
}

type subscriber struct {
	handler Handler
	async   bool
}

// Bus 是并发安全的事件总线。
type Bus struct {
	mu     sync.RWMutex
	subs   map[string][]subscriber // key 为 topic 或 *
	queue  chan Event
	closed chan struct{}
	once   sync.Once
	opts   Options

	published atomic.Uint64
	delivered atomic.Uint64
	dropped   atomic.Uint64
	failed    atomic.Uint64
	wg        sync.WaitGroup
}

// New 创建并启动总线。
func New(opts Options) *Bus {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1024
	}
	if opts.Workers <= 0 {
		opts.Workers = 4
	}
	b := &Bus{
		subs:   make(map[string][]subscriber),
		queue:  make(chan Event, opts.QueueSize),
		closed: make(chan struct{}),
		opts:   opts,
	}
	b.wg.Add(opts.Workers)
	for i := 0; i < opts.Workers; i++ {
		go b.worker()
	}
	return b
}

// Subscribe 注册异步处理器；topic 传 "*" 表示订阅全部事件。
func (b *Bus) Subscribe(topic string, h Handler) {
	b.add(topic, subscriber{handler: h, async: true})
}

// SubscribeSync 注册同步处理器，在 PublishSync 时按注册顺序执行。
func (b *Bus) SubscribeSync(topic string, h Handler) {
	b.add(topic, subscriber{handler: h, async: false})
}

func (b *Bus) add(topic string, s subscriber) {
	if b == nil || s.handler == nil {
		return
	}
	topic = normalize(topic)
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], s)
	b.mu.Unlock()
}

// Publish 异步投递事件：队列满时丢弃并计数，绝不阻塞调用方。
func (b *Bus) Publish(ctx context.Context, topic string, data any) {
	if b == nil {
		return
	}
	e := Event{Topic: normalize(topic), Time: time.Now(), Data: data}
	b.published.Add(1)

	// 同步订阅者立即执行，保证"落库后立刻可见"的语义
	for _, s := range b.match(e.Topic) {
		if !s.async {
			b.run(ctx, s.handler, e)
		}
	}

	select {
	case <-b.closed:
		b.dropped.Add(1)
	case b.queue <- e:
	default:
		b.dropped.Add(1)
	}
}

// PublishSync 同步执行所有订阅者（含异步订阅者），用于测试或强一致场景。
func (b *Bus) PublishSync(ctx context.Context, topic string, data any) {
	if b == nil {
		return
	}
	e := Event{Topic: normalize(topic), Time: time.Now(), Data: data}
	b.published.Add(1)
	for _, s := range b.match(e.Topic) {
		b.run(ctx, s.handler, e)
	}
}

func (b *Bus) match(topic string) []subscriber {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]subscriber, 0, len(b.subs[topic])+len(b.subs["*"]))
	out = append(out, b.subs[topic]...)
	if topic != "*" {
		out = append(out, b.subs["*"]...)
	}
	return out
}

func (b *Bus) worker() {
	defer b.wg.Done()
	for {
		select {
		case <-b.closed:
			// 关闭前把队列中剩余事件处理完
			for {
				select {
				case e := <-b.queue:
					b.dispatch(e)
				default:
					return
				}
			}
		case e := <-b.queue:
			b.dispatch(e)
		}
	}
}

func (b *Bus) dispatch(e Event) {
	for _, s := range b.match(e.Topic) {
		if s.async {
			b.run(context.Background(), s.handler, e)
		}
	}
}

// run 执行单个处理器并隔离 panic，保证一个坏订阅者不会拖垮总线。
func (b *Bus) run(ctx context.Context, h Handler, e Event) {
	defer func() {
		if r := recover(); r != nil {
			b.failed.Add(1)
			if b.opts.OnError != nil {
				b.opts.OnError(e.Topic, r, debug.Stack())
			}
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	h(ctx, e)
	b.delivered.Add(1)
}

// Stats 返回运行统计。
func (b *Bus) Stats() Stats {
	if b == nil {
		return Stats{}
	}
	return Stats{
		Published: b.published.Load(),
		Delivered: b.delivered.Load(),
		Dropped:   b.dropped.Load(),
		Failed:    b.failed.Load(),
		QueueLen:  len(b.queue),
	}
}

// Close 停止接收新事件，处理完队列后退出（幂等）。
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		close(b.closed)
		b.wg.Wait()
	})
}

func normalize(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return "unknown"
	}
	return topic
}
