package discovery

import (
	"sync"
	"sync/atomic"
	"time"
)

// Balancer 在实例列表上做平滑轮询，并对失败实例做短时熔断（冷却）。
// 冷却期内不参与轮询，冷却结束后自动恢复，避免"一次抖动即永久摘除"。
type Balancer struct {
	mu       sync.RWMutex
	rr       map[string]*uint64
	cooldown time.Duration
	failures map[string]failureMark
}

type failureMark struct {
	count int
	until time.Time
}

// NewBalancer 创建负载均衡器，cooldown 为单次失败的摘除时长。
func NewBalancer(cooldown time.Duration) *Balancer {
	if cooldown <= 0 {
		cooldown = 5 * time.Second
	}
	return &Balancer{rr: make(map[string]*uint64), cooldown: cooldown, failures: make(map[string]failureMark)}
}

// Pick 从列表中选择一个实例；全部处于冷却时退回全量轮询，保证仍有流量探测。
func (b *Balancer) Pick(service string, list []Instance) (Instance, bool) {
	if len(list) == 0 {
		return Instance{}, false
	}
	b.mu.RLock()
	counter, ok := b.rr[service]
	now := time.Now()
	alive := make([]Instance, 0, len(list))
	for _, ins := range list {
		if m, found := b.failures[ins.ID]; found && now.Before(m.until) {
			continue
		}
		alive = append(alive, ins)
	}
	b.mu.RUnlock()
	if len(alive) == 0 {
		alive = list
	}
	if !ok {
		b.mu.Lock()
		if counter, ok = b.rr[service]; !ok {
			var c uint64
			counter = &c
			b.rr[service] = counter
		}
		b.mu.Unlock()
	}
	n := atomic.AddUint64(counter, 1) - 1
	return alive[n%uint64(len(alive))], true
}

// Success 标记实例恢复健康。
func (b *Balancer) Success(instanceID string) {
	b.mu.Lock()
	delete(b.failures, instanceID)
	b.mu.Unlock()
}

// Fail 记录一次失败，触发冷却；连续失败会线性延长冷却时间（上限 1 分钟）。
func (b *Balancer) Fail(instanceID string) {
	b.mu.Lock()
	m := b.failures[instanceID]
	m.count++
	d := b.cooldown * time.Duration(m.count)
	if d > time.Minute {
		d = time.Minute
	}
	m.until = time.Now().Add(d)
	b.failures[instanceID] = m
	b.mu.Unlock()
}

// Stats 返回当前被冷却的实例数量，便于监控。
func (b *Balancer) Stats() map[string]int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	now := time.Now()
	out := make(map[string]int)
	for id, m := range b.failures {
		if now.Before(m.until) {
			out[id] = int(m.until.Sub(now).Seconds())
		}
	}
	return out
}
