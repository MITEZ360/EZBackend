package gov

import (
	"sync"
	"time"
)

// Breaker 是进程内熔断器，按目标（服务或实例）独立计数。
//
// 三态：closed 正常放行；open 直接拒绝；half-open 放一个探测请求，
// 成功回 closed，失败重新计时。它保护的是调用方，不是被调方。
type Breaker struct {
	mu        sync.Mutex
	threshold int
	openFor   time.Duration
	states    map[string]*breakerState
}

type breakerState struct {
	failures int
	openedAt time.Time
	probing  bool
}

// NewBreaker 创建熔断器；threshold 为连续失败阈值，openFor 为熔断时长。
func NewBreaker(threshold int, openFor time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if openFor <= 0 {
		openFor = 10 * time.Second
	}
	return &Breaker{threshold: threshold, openFor: openFor, states: map[string]*breakerState{}}
}

// Allow 判断当前是否放行。
func (b *Breaker) Allow(key string) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.states[key]
	if st == nil || st.failures < b.threshold {
		return true
	}
	if time.Since(st.openedAt) < b.openFor {
		return false
	}
	// 熔断窗口已过，只放一个探测请求。
	if st.probing {
		return false
	}
	st.probing = true
	return true
}

// Success 关闭熔断并清空计数。
func (b *Breaker) Success(key string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.states, key)
	b.mu.Unlock()
}

// Fail 记录失败；达到阈值后打开熔断。
func (b *Breaker) Fail(key string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.states[key]
	if st == nil {
		st = &breakerState{}
		b.states[key] = st
	}
	st.failures++
	st.probing = false
	if st.failures >= b.threshold {
		st.openedAt = time.Now()
	}
}
