package route

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// TestConcurrentPublish 并发发布必须发出互不重复的版本号，且同一域最后只留
// 一个 active 发布。
//
// 版本号重复的后果不是"覆盖"这么简单：数据面按版本判断新旧，两个发布拿到
// 同一个号时，后一个的内容会被永久忽略，而且没有任何报错。
func TestConcurrentPublish(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, baseInput()); err != nil {
		t.Fatal(err)
	}

	const n = 12
	rels := make([]*model.GovRelease, n)
	errsOf := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rels[i], errsOf[i] = s.Publish(ctx, PublishInput{Note: fmt.Sprintf("并发 %d", i)})
		}(i)
	}
	wg.Wait()

	for i, err := range errsOf {
		if err != nil {
			t.Fatalf("第 %d 次并发发布失败: %v", i, err)
		}
	}
	revs := make([]int64, n)
	for i, rel := range rels {
		revs[i] = rel.Revision
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i] < revs[j] })
	for i := 1; i < len(revs); i++ {
		if revs[i] == revs[i-1] {
			t.Fatalf("发布版本号重复: %d（全部 %v）", revs[i], revs)
		}
	}

	var active int64
	if err := db.Model(&model.GovRelease{}).
		Where("domain = ? AND status = ?", "route", "active").Count(&active).Error; err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("同一域应只有 1 个 active 发布，实际 %d 个", active)
	}

	// 最新 active 必须是最新版本，否则数据面拿到的不是最后发布的内容。
	var live model.GovRelease
	if err := db.Where("domain = ? AND status = ?", "route", "active").First(&live).Error; err != nil {
		t.Fatal(err)
	}
	if live.Revision != revs[len(revs)-1] {
		t.Fatalf("active 发布不是最新版本：%d != %d", live.Revision, revs[len(revs)-1])
	}
}

// TestConcurrentWriteAndPublish 一边改规则一边发布，不能互相把状态写坏：
// 发布结束后，所有草稿要么被本次发布带上（published），要么留到下一次。
func TestConcurrentWriteAndPublish(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := baseInput()
			in.Prefix = fmt.Sprintf("/api/v1/w%d", i)
			in.Name = fmt.Sprintf("规则 %d", i)
			if _, err := s.Create(ctx, in); err != nil {
				t.Errorf("并发创建规则失败: %v", err)
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 没有规则时发布会报"没有可发布的规则"，属于预期内的竞态，允许。
			_, _ = s.Publish(ctx, PublishInput{})
		}()
	}
	wg.Wait()

	if _, err := s.Publish(ctx, PublishInput{}); err != nil {
		t.Fatalf("收尾发布失败: %v", err)
	}
	var drafts int64
	db.Model(&model.GovRoute{}).Where("status = ?", "draft").Count(&drafts)
	if drafts != 0 {
		t.Fatalf("发布后不应残留草稿，实际 %d 条", drafts)
	}
	var total int64
	db.Model(&model.GovRoute{}).Count(&total)
	if total != 6 {
		t.Fatalf("应有 6 条规则，实际 %d", total)
	}
}

// TestCreateRejectsInvalidStatus 状态只能由服务端流转，不能由调用方随手填。
//
// 直接创建一条 status=published 的规则，会让页面显示"已发布"，但发布记录里
// 根本没有它，网关也不会转发——这种"看起来生效了"的状态最难排查。
func TestCreateRejectsInvalidStatus(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	for _, status := range []string{"published", "bogus", "ACTIVE"} {
		in := baseInput()
		in.Status = status
		if _, err := s.Create(ctx, in); err == nil {
			t.Fatalf("状态 %q 应被拒绝", status)
		}
	}
	for i, status := range []string{"", "draft", "archived"} {
		in := baseInput()
		in.Status = status
		in.Prefix = fmt.Sprintf("/api/v1/status%d", i)
		in.Name = fmt.Sprintf("status-%d", i)
		if _, err := s.Create(ctx, in); err != nil {
			t.Fatalf("状态 %q 应被接受: %v", status, err)
		}
	}
}

// TestPrefixExtremeValues 前缀是 URL 路径，必须挡住空白、查询串与超长内容。
func TestPrefixExtremeValues(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	bad := []string{
		"/api v1",                      // 含空格
		"/api\tv1",                     // 含制表符
		"/api/v1?x=1",                  // 含查询串
		"/api/v1#frag",                 // 含片段
		"/api\\v1",                     // 含反斜杠
		"/api/\x00v1",                  // 含控制字符
		"/" + strings.Repeat("a", 200), // 超长
	}
	for _, p := range bad {
		in := baseInput()
		in.Prefix = p
		if _, err := s.Create(ctx, in); err == nil {
			t.Fatalf("前缀 %q 应被拒绝", p)
		}
	}

	// 合法写法要被规范化，而不是拒绝。
	good := []struct {
		in   string
		want string
	}{
		{"api/v1", "/api/v1"},
		{"/api/v1/", "/api/v1"},
		{"//api//v1//", "/api/v1"},
		{"  /api/v1  ", "/api/v1"},
	}
	for _, tc := range good {
		in := baseInput()
		in.Prefix = tc.in
		in.Name = tc.in
		row, err := s.Create(ctx, in)
		if err != nil {
			t.Fatalf("前缀 %q 应被接受: %v", tc.in, err)
		}
		if row.Prefix != tc.want {
			t.Fatalf("前缀 %q 应规范化为 %q，实际 %q", tc.in, tc.want, row.Prefix)
		}
	}
}

// TestWeightAndConditionsBounds 权重、条件 map 的边界值。
func TestWeightAndConditionsBounds(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()

	for _, w := range []int{-1, -100, 10001} {
		in := baseInput()
		in.Weight = w
		if _, err := s.Create(ctx, in); err == nil {
			t.Fatalf("权重 %d 应被拒绝", w)
		}
	}
	in := baseInput()
	in.Weight = 0 // 0 表示未填，取默认 100
	row, err := s.Create(ctx, in)
	if err != nil {
		t.Fatalf("权重 0 应回落到默认值: %v", err)
	}
	if row.Weight != 100 {
		t.Fatalf("权重 0 应回落到 100，实际 %d", row.Weight)
	}

	// Header/Query 的条件键为空没有意义，应当拒绝而不是静默忽略。
	for _, mutate := range []func(*RouteInput){
		func(in *RouteInput) { in.Headers = map[string]string{"": "x"} },
		func(in *RouteInput) { in.Query = map[string]string{"": "x"} },
		func(in *RouteInput) { in.Headers = map[string]string{"X-Tenant": strings.Repeat("v", 600)} },
	} {
		bad := baseInput()
		bad.Prefix = "/api/v1/cond"
		mutate(&bad)
		if _, err := s.Create(ctx, bad); err == nil {
			t.Fatal("非法条件应被拒绝")
		}
	}

	// 方法大小写与重复要归一化，避免 GET / get 被当成两条不同的规则。
	ok := baseInput()
	ok.Prefix = "/api/v1/method"
	ok.Methods = []string{"get", "GET", " Post "}
	row, err = s.Create(ctx, ok)
	if err != nil {
		t.Fatalf("方法归一化失败: %v", err)
	}
	if row.Methods != "GET,POST" {
		t.Fatalf("方法应归一化为 GET,POST，实际 %q", row.Methods)
	}
}

// TestRollbackCorruptedPayload 发布记录被外部改坏时回滚必须报错，不能
// 悄悄生成一个空路由版本——那会把整站打挂。
func TestRollbackCorruptedPayload(t *testing.T) {
	s, db := newTestService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, baseInput()); err != nil {
		t.Fatal(err)
	}
	rel, err := s.Publish(ctx, PublishInput{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.GovRelease{}).Where("id = ?", rel.ID).
		Update("payload", []byte("{not json")).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rollback(ctx, rel.ID, "tester"); err == nil {
		t.Fatal("损坏的发布内容应拒绝回滚")
	}
	var live model.GovRelease
	if err := db.Where("domain = ? AND status = ?", "route", "active").First(&live).Error; err != nil {
		t.Fatal(err)
	}
	if live.ID != rel.ID {
		t.Fatalf("回滚失败不应改变当前 active 发布，实际 %s", live.ID)
	}
}

// TestPublishedRouteCanReachGatewayCompile 发布出来的载荷必须能被数据面
// 直接编译——两边共用同一套规则结构，格式不一致会在运行时才暴露。
func TestPublishedRouteCanReachGatewayCompile(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	in := baseInput()
	in.Methods = []string{"GET"}
	in.Headers = map[string]string{"X-Tenant": "t1"}
	in.Query = map[string]string{"debug": "1"}
	if _, err := s.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	rel, err := s.Publish(ctx, PublishInput{})
	if err != nil {
		t.Fatal(err)
	}
	var payload gov.RoutePayload
	if err := json.Unmarshal(rel.Payload, &payload); err != nil {
		t.Fatalf("发布载荷无法解码: %v", err)
	}
	c := gov.Compile(payload.Routes)
	if c.Len() != 1 {
		t.Fatalf("数据面应编译出 1 条规则，实际 %d", c.Len())
	}
	hit := c.Match("GET", "/api/v1/order/1",
		map[string][]string{"X-Tenant": {"t1"}}, map[string][]string{"debug": {"1"}})
	if hit == nil {
		t.Fatal("按发布内容应能命中")
	}
	if c.Match("POST", "/api/v1/order/1",
		map[string][]string{"X-Tenant": {"t1"}}, map[string][]string{"debug": {"1"}}) != nil {
		t.Fatal("方法不匹配时不应命中")
	}
}

// asErr 判断错误是否为业务错误并取出状态码。
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
