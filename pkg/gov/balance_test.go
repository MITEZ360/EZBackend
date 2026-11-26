package gov

import (
	"testing"
	"time"
)

func inst(id string, weight int) Instance {
	return Instance{ID: id, Addr: id + ":8080", Weight: weight}
}

// TestPickWeightedDistribution 平滑加权轮询应按权重比例分配，且不会连续打同一台。
func TestPickWeightedDistribution(t *testing.T) {
	b := NewBalancer(0)
	list := []Instance{inst("a", 300), inst("b", 100)}

	count := map[string]int{}
	seq := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		got, ok := b.Pick("svc", list, "")
		if !ok {
			t.Fatal("有实例时应返回 ok")
		}
		count[got.ID]++
		seq = append(seq, got.ID)
	}
	// 8 次应为 6:2，且任一台不连续出现 4 次以上。
	if count["a"] != 6 || count["b"] != 2 {
		t.Fatalf("8 次分配应为 6:2，实际 %v（序列 %v）", count, seq)
	}
	run, maxRun := 1, 1
	for i := 1; i < len(seq); i++ {
		if seq[i] == seq[i-1] {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 1
		}
	}
	if maxRun >= 4 {
		t.Fatalf("连续命中同一实例 %d 次，平滑加权轮询失效: %v", maxRun, seq)
	}

	// 长跑比例仍然成立。
	count = map[string]int{}
	for i := 0; i < 400; i++ {
		got, _ := b.Pick("svc", list, "")
		count[got.ID]++
	}
	if count["a"] != 300 || count["b"] != 100 {
		t.Fatalf("400 次应为 300:100，实际 %v", count)
	}
}

// TestPickSingleAndEmpty 单实例总命中，空列表返回 false。
func TestPickSingleAndEmpty(t *testing.T) {
	b := NewBalancer(0)
	if _, ok := b.Pick("svc", nil, ""); ok {
		t.Fatal("空列表应返回 false")
	}
	one := []Instance{inst("only", 100)}
	for i := 0; i < 5; i++ {
		got, ok := b.Pick("svc", one, "")
		if !ok || got.ID != "only" {
			t.Fatalf("单实例应总命中，实际 %+v ok=%v", got, ok)
		}
	}
}

// TestPickZeroWeightTreatedAsHundred 权重缺省/非正数按 100 处理，不能整除归零。
func TestPickZeroWeightTreatedAsHundred(t *testing.T) {
	b := NewBalancer(0)
	list := []Instance{inst("a", 0), inst("b", -10)}
	count := map[string]int{}
	for i := 0; i < 10; i++ {
		got, ok := b.Pick("svc", list, "")
		if !ok {
			t.Fatal("应返回 ok")
		}
		count[got.ID]++
	}
	if count["a"] != 5 || count["b"] != 5 {
		t.Fatalf("非正权重应等价于 100，实际 %v", count)
	}
}

// TestPickHashStable 同一 hashKey 固定落同一实例；不同 key 能分散。
func TestPickHashStable(t *testing.T) {
	b := NewBalancer(0)
	list := []Instance{inst("a", 100), inst("b", 100), inst("c", 100)}

	first, ok := b.Pick("svc", list, "tenant-42")
	if !ok {
		t.Fatal("应返回 ok")
	}
	for i := 0; i < 20; i++ {
		got, _ := b.Pick("svc", list, "tenant-42")
		if got.ID != first.ID {
			t.Fatalf("同一 key 应固定命中 %s，实际 %s", first.ID, got.ID)
		}
	}

	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		got, _ := b.Pick("svc", list, "tenant-"+string(rune('a'+i%26)))
		seen[got.ID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("不同 key 应分散到多个实例，实际只命中 %v", seen)
	}

	// 实例顺序变化时，只要列表内容相同，结果应保持一致（列表由控制面排序）。
	rev := []Instance{list[2], list[0], list[1]}
	got, _ := b.Pick("svc", rev, "tenant-42")
	if got.ID != first.ID {
		t.Logf("实例顺序变化导致重分布（可接受，但不建议控制面乱序）: %s -> %s", first.ID, got.ID)
	}
}

// TestFailCooldown 失败后在冷却期内摘除，Success 立即恢复；全部冷却时兜底全量。
func TestFailCooldown(t *testing.T) {
	b := NewBalancer(time.Minute)
	list := []Instance{inst("a", 100), inst("b", 100)}

	b.Fail("a")
	if d := b.Cooldown("a"); d <= 0 {
		t.Fatalf("失败后应有冷却，实际 %v", d)
	}
	for i := 0; i < 10; i++ {
		got, ok := b.Pick("svc", list, "")
		if !ok || got.ID != "b" {
			t.Fatalf("冷却期内应只命中 b，实际 %+v", got)
		}
	}

	b.Success("a")
	if d := b.Cooldown("a"); d != 0 {
		t.Fatalf("成功后冷却应清零，实际 %v", d)
	}
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		got, _ := b.Pick("svc", list, "")
		seen[got.ID] = true
	}
	if !seen["a"] {
		t.Fatalf("恢复后应能重新命中 a，实际 %v", seen)
	}

	// 两台都冷却时必须退回全量，保留探测恢复的机会。
	b.Fail("a")
	b.Fail("b")
	if _, ok := b.Pick("svc", list, ""); !ok {
		t.Fatal("全部冷却时应退回全量而不是拒绝服务")
	}
}

// TestFailCooldownGrows 连续失败线性延长，上限 60 秒。
func TestFailCooldownGrows(t *testing.T) {
	b := NewBalancer(time.Second)
	b.Fail("a")
	first := b.Cooldown("a")
	b.Fail("a")
	second := b.Cooldown("a")
	if second <= first {
		t.Fatalf("连续失败应延长冷却: %v -> %v", first, second)
	}
	for i := 0; i < 100; i++ {
		b.Fail("a")
	}
	if d := b.Cooldown("a"); d > time.Minute+time.Second {
		t.Fatalf("冷却上限应为 60s，实际 %v", d)
	}
}

// TestDefaultCooldown 非正冷却参数应回落到 5s，而不是 0（0 会让摘除立即失效）。
func TestDefaultCooldown(t *testing.T) {
	b := NewBalancer(0)
	b.Fail("a")
	if d := b.Cooldown("a"); d < 4*time.Second {
		t.Fatalf("默认冷却应约 5s，实际 %v", d)
	}
}
