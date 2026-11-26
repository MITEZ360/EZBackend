package gov

import (
	"hash/fnv"
	"sync"
	"time"
)

// failureMark 记录实例的连续失败与冷却截止时间。
type failureMark struct {
	count int
	until time.Time
}

// Balancer 做进程内负载均衡：平滑加权轮询 + 一致性哈希 + 失败冷却摘除。
//
// 它只认 Instance ID，不关心实例来自注册中心还是配置文件，因此控制面
// 切换注册源时不需要改数据面。
type Balancer struct {
	mu       sync.Mutex
	cooldown time.Duration
	failures map[string]failureMark
	weights  map[string]map[string]int // service -> instanceID -> 当前权重
}

// NewBalancer 创建均衡器；cooldown 为首次失败的摘除时长。
func NewBalancer(cooldown time.Duration) *Balancer {
	if cooldown <= 0 {
		cooldown = 5 * time.Second
	}
	return &Balancer{
		cooldown: cooldown,
		failures: map[string]failureMark{},
		weights:  map[string]map[string]int{},
	}
}

// Pick 选择一个实例。
//
// hashKey 非空时走一致性哈希（同一租户/用户固定落到同一实例）；
// 为空时走平滑加权轮询。所有实例都在冷却时退回全量，保留探测恢复的机会。
func (b *Balancer) Pick(service string, list []Instance, hashKey string) (Instance, bool) {
	if len(list) == 0 {
		return Instance{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	alive := make([]Instance, 0, len(list))
	for _, ins := range list {
		if m, ok := b.failures[ins.ID]; ok && now.Before(m.until) {
			continue
		}
		alive = append(alive, ins)
	}
	if len(alive) == 0 {
		alive = list
	}
	if hashKey != "" {
		return pickHash(alive, hashKey), true
	}
	return b.pickWeighted(service, alive), true
}

// pickWeighted 是 nginx 的平滑加权轮询：每次给所有实例加自身权重，
// 选当前权重最大的，再把它减去总权重。长尾分布均匀，不会出现连续打一台。
func (b *Balancer) pickWeighted(service string, list []Instance) Instance {
	state := b.weights[service]
	if state == nil {
		state = map[string]int{}
		b.weights[service] = state
	}
	// 清掉已经下线的实例，避免状态表随时间膨胀。
	live := make(map[string]bool, len(list))
	total := 0
	best := 0
	for i, ins := range list {
		live[ins.ID] = true
		w := ins.Weight
		if w <= 0 {
			w = 100
		}
		state[ins.ID] += w
		total += w
		if state[ins.ID] > state[list[best].ID] {
			best = i
		}
	}
	for id := range state {
		if !live[id] {
			delete(state, id)
		}
	}
	state[list[best].ID] -= total
	return list[best]
}

// pickHash 按 key 做加权一致性哈希，实例顺序变化时影响面尽量小。
func pickHash(list []Instance, key string) Instance {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	pos := int(h.Sum32())
	total := 0
	for _, ins := range list {
		w := ins.Weight
		if w <= 0 {
			w = 100
		}
		total += w
	}
	if total <= 0 {
		return list[0]
	}
	point := pos % total
	acc := 0
	for _, ins := range list {
		w := ins.Weight
		if w <= 0 {
			w = 100
		}
		acc += w
		if point < acc {
			return ins
		}
	}
	return list[len(list)-1]
}

// Success 标记实例恢复，清除冷却与失败计数。
func (b *Balancer) Success(id string) {
	b.mu.Lock()
	delete(b.failures, id)
	b.mu.Unlock()
}

// Fail 记录一次失败；连续失败线性延长冷却，上限 60 秒。
func (b *Balancer) Fail(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.failures[id]
	m.count++
	d := b.cooldown * time.Duration(m.count)
	if d > time.Minute {
		d = time.Minute
	}
	m.until = time.Now().Add(d)
	b.failures[id] = m
}

// Cooldown 返回实例剩余冷却时长，仅供管理端展示。
func (b *Balancer) Cooldown(id string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, ok := b.failures[id]
	if !ok {
		return 0
	}
	if d := time.Until(m.until); d > 0 {
		return d
	}
	return 0
}
