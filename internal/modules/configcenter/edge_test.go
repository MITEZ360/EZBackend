package configcenter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// TestConcurrentSaveItemsSameKey 并发写同一个键：不能报错，也不能写坏行，
// 最终值必须是某一次写入的完整内容（不能出现两次写入拼在一起的中间态）。
func TestConcurrentSaveItemsSameKey(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	const n = 16
	values := make([]string, n)
	errsOf := make([]error, n)
	for i := 0; i < n; i++ {
		values[i] = fmt.Sprintf("value-%02d", i)
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errsOf[i] = s.SaveItems(ctx, "app", []ItemInput{{Key: "rate", Value: values[i]}}, "tester")
		}(i)
	}
	wg.Wait()
	for i, err := range errsOf {
		if err != nil {
			t.Fatalf("第 %d 次并发写入失败: %v", i, err)
		}
	}

	var rows []model.GovConfigItem
	if err := db.Where("namespace = ? AND key = ?", "app", "rate").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("同一键应只有 1 行，实际 %d", len(rows))
	}
	ok := false
	for _, v := range values {
		if rows[0].Value == v {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("最终值不是任何一次写入的完整内容: %q", rows[0].Value)
	}
}

// TestConcurrentSaveDistinctKeys 并发写不同键：一个都不能丢。
func TestConcurrentSaveDistinctKeys(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	const n = 40
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.SaveItems(ctx, "app",
				[]ItemInput{{Key: fmt.Sprintf("k%02d", i), Value: fmt.Sprint(i)}}, "tester"); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发写入失败: %v", err)
	}
	var count int64
	db.Model(&model.GovConfigItem{}).Where("namespace = ?", "app").Count(&count)
	if count != n {
		t.Fatalf("应写入 %d 个键，实际 %d", n, count)
	}
}

// TestSaveItemsAtomic 批量保存必须整体成功或整体失败。
//
// 逐条 upsert 时，第 3 条校验失败会把前两条留在库里，调用方收到 400 却以为
// 什么都没改；下一次他把整批再提交一次，前两条就变成了"被确认过的旧值"。
func TestSaveItemsAtomic(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	items := []ItemInput{
		{Key: "k1", Value: "v1"},
		{Key: "k2", Value: "v2"},
		{Key: "k3", Value: "v3", Scope: "service"}, // 缺 service，必然失败
	}
	if err := s.SaveItems(ctx, "app", items, "tester"); err == nil {
		t.Fatal("含非法项的批量保存应整体失败")
	}
	var count int64
	db.Model(&model.GovConfigItem{}).Where("namespace = ?", "app").Count(&count)
	if count != 0 {
		t.Fatalf("失败的批量保存不应留下部分数据，实际 %d 条", count)
	}

	// 修正后整批成功。
	items[2].Service = "order-svc"
	if err := s.SaveItems(ctx, "app", items, "tester"); err != nil {
		t.Fatalf("修正后的批量保存应成功: %v", err)
	}
	db.Model(&model.GovConfigItem{}).Where("namespace = ?", "app").Count(&count)
	if count != 3 {
		t.Fatalf("应写入 3 条，实际 %d", count)
	}
}

// TestConcurrentCreateNamespaceSameName 同名并发创建：一个成功，其余必须是
// 冲突（409），不能把唯一索引错误直接抛成 500。
func TestConcurrentCreateNamespaceSameName(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	var okCount int
	var mu sync.Mutex
	errsOf := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.CreateNamespace(ctx, NamespaceInput{Namespace: "race-ns", Env: "dev"})
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
		if !asErr(err, &e) || e.Status != 409 {
			t.Fatalf("第 %d 个错误应为 409 冲突，实际 %v", i, err)
		}
	}
}

// TestNamespaceValidation 命名空间是分区键的一部分（config:<ns>），
// 允许空格或斜杠会让快照分区名产生歧义。
func TestNamespaceValidation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	bad := []string{
		"", "   ", "a b", "a/b", "a\\b", "a\tb", "\x00x",
		strings.Repeat("n", 65),
		"-leading-dash", ".dot", "中文空间",
	}
	for _, ns := range bad {
		if _, err := s.CreateNamespace(ctx, NamespaceInput{Namespace: ns}); err == nil {
			t.Fatalf("命名空间 %q 应被拒绝", ns)
		}
	}
	for _, ns := range []string{"app", "app-v1", "app_v1", "app.v1", "prod2"} {
		if _, err := s.CreateNamespace(ctx, NamespaceInput{Namespace: ns}); err != nil {
			t.Fatalf("命名空间 %q 应被接受: %v", ns, err)
		}
	}
}

// TestItemValidation 配置项的键、类型、作用域都要有边界。
func TestItemValidation(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	bad := []struct {
		name string
		item ItemInput
	}{
		{"空键", ItemInput{Key: "   ", Value: "v"}},
		{"键过长", ItemInput{Key: strings.Repeat("k", 129), Value: "v"}},
		{"键含控制字符", ItemInput{Key: "a\x00b", Value: "v"}},
		{"键含空格", ItemInput{Key: "a b", Value: "v"}},
		{"作用域非法", ItemInput{Key: "k", Value: "v", Scope: "cluster"}},
		{"service 作用域缺服务", ItemInput{Key: "k", Value: "v", Scope: "service"}},
		{"类型过长", ItemInput{Key: "k", Value: "v", Type: strings.Repeat("t", 33)}},
		{"值过大", ItemInput{Key: "k", Value: strings.Repeat("v", 64*1024+1)}},
	}
	for _, tc := range bad {
		if err := s.SaveItems(ctx, "app", []ItemInput{tc.item}, "tester"); err == nil {
			t.Fatalf("%s 应被拒绝", tc.name)
		}
	}
	var count int64
	db.Model(&model.GovConfigItem{}).Count(&count)
	if count != 0 {
		t.Fatalf("非法配置不应入库，实际 %d 条", count)
	}

	// 缺省值：scope=global，type=string。
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "plain", Value: "v"}}, "tester"); err != nil {
		t.Fatalf("正常配置应写入: %v", err)
	}
	var row model.GovConfigItem
	if err := db.Where("namespace = ? AND key = ?", "app", "plain").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Scope != "global" || row.Type != "string" {
		t.Fatalf("缺省值不对: scope=%q type=%q", row.Scope, row.Type)
	}
}

// TestSaveItemsDuplicateKeyLastWins 同一批次里出现重复键时，以最后一条为准，
// 而不是把它们当成两条独立配置。
func TestSaveItemsDuplicateKeyLastWins(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	if err := s.SaveItems(ctx, "app", []ItemInput{
		{Key: "dup", Value: "first"},
		{Key: "dup", Value: "second"},
	}, "tester"); err != nil {
		t.Fatal(err)
	}
	var rows []model.GovConfigItem
	db.Where("namespace = ? AND key = ?", "app", "dup").Find(&rows)
	if len(rows) != 1 || rows[0].Value != "second" {
		t.Fatalf("重复键应只留一行且取最后一次，实际 %+v", rows)
	}
}

// TestConcurrentPublishSameNamespace 并发发布同一命名空间：版本号不重复，
// 最终只有一个 active。
func TestConcurrentPublishSameNamespace(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "k", Value: "v"}}, "tester"); err != nil {
		t.Fatal(err)
	}

	const n = 10
	revs := make([]int64, n)
	errsOf := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rel, err := s.Publish(ctx, "app", "tester", "")
			errsOf[i] = err
			if rel != nil {
				revs[i] = rel.Revision
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errsOf {
		if err != nil {
			t.Fatalf("第 %d 次并发发布失败: %v", i, err)
		}
	}
	seen := map[int64]bool{}
	for _, r := range revs {
		if seen[r] {
			t.Fatalf("发布版本号重复: %d（全部 %v）", r, revs)
		}
		seen[r] = true
	}
	var active int64
	db.Model(&model.GovRelease{}).
		Where("domain = ? AND status = ?", gov.PartConfigPrefix+"app", "active").Count(&active)
	if active != 1 {
		t.Fatalf("同一命名空间应只有 1 个 active 发布，实际 %d", active)
	}
}

// TestSaveEncryptedWithoutMasterKey 未配置主密钥时必须拒绝，而不是把明文
// 当成"加密项"存进去——后者会在页面上显示掩码，实际库里是明文。
func TestSaveEncryptedWithoutMasterKey(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	t.Setenv("TENON_MASTER_KEY", "")
	err := s.SaveItems(ctx, "app", []ItemInput{{Key: "secret", Value: "plain", Encrypted: true}}, "tester")
	if err == nil {
		t.Fatal("缺少主密钥时应拒绝加密写入")
	}
	var count int64
	db.Model(&model.GovConfigItem{}).Where("namespace = ?", "app").Count(&count)
	if count != 0 {
		t.Fatalf("失败时不应留下数据，实际 %d 条", count)
	}
}

// TestConcurrentSaveItemsEncrypted 并发写加密项：最终必须是合法密文，
// 且能被正确解密回某一次写入的明文。
func TestConcurrentSaveItemsEncrypted(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	const n = 12
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("secret-%02d", i)
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.SaveItems(ctx, "app", []ItemInput{
				{Key: "token", Value: values[i], Encrypted: true},
			}, "tester"); err != nil {
				t.Errorf("并发写加密项失败: %v", err)
			}
		}(i)
	}
	wg.Wait()

	var row model.GovConfigItem
	if err := db.Where("namespace = ? AND key = ?", "app", "token").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(row.Value, gov.EncPrefix) {
		t.Fatalf("落库内容不是密文: %q", row.Value)
	}
	plain, err := gov.Decrypt(row.Value, gov.MasterKey())
	if err != nil {
		t.Fatalf("密文无法解密: %v", err)
	}
	ok := false
	for _, v := range values {
		if v == plain {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("解密结果不是任何一次写入的值: %q", plain)
	}
}

// asErr 判断错误是否为业务错误。
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
