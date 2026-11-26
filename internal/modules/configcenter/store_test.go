package configcenter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/gov"
)

const testMasterKey = "configstore-test-key"

func newStore(t *testing.T) (*ConfigStore, *gorm.DB) {
	t.Helper()
	t.Setenv("TENON_MASTER_KEY", testMasterKey)
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/gov.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(
		&model.GovRevision{}, &model.GovService{}, &model.GovInstance{},
		&model.GovRoute{}, &model.GovRelease{},
		&model.GovConfigNamespace{}, &model.GovConfigItem{},
	); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if err := db.Create(&model.GovRevision{ID: 1, GlobalRev: 1}).Error; err != nil {
		t.Fatalf("初始化版本号失败: %v", err)
	}
	return NewConfigStore(db, zap.NewNop(), govserver.New(db, "test-token", zap.NewNop())), db
}

func mustNamespace(t *testing.T, s *ConfigStore, name string) {
	t.Helper()
	if _, err := s.CreateNamespace(context.Background(), NamespaceInput{Namespace: name, Env: "dev", Desc: "测试"}); err != nil {
		t.Fatalf("创建命名空间 %s 失败: %v", name, err)
	}
}

// TestCreateNamespaceValidation 命名空间必填、唯一，删除后可重建。
func TestCreateNamespaceValidation(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()

	if _, err := s.CreateNamespace(ctx, NamespaceInput{Namespace: "  "}); err == nil {
		t.Fatal("空命名空间应被拒绝")
	}
	mustNamespace(t, s, "app")
	if _, err := s.CreateNamespace(ctx, NamespaceInput{Namespace: "app"}); err == nil {
		t.Fatal("重复命名空间应被拒绝")
	}

	ns, err := s.Namespaces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Namespace != "app" {
		t.Fatalf("命名空间列表异常: %+v", ns)
	}

	if err := s.DeleteNamespace(ctx, ns[0].ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Unscoped().Model(&model.GovConfigNamespace{}).Where("namespace = ?", "app").Count(&count)
	if count != 0 {
		t.Fatalf("命名空间应被物理删除，实际剩 %d", count)
	}
	mustNamespace(t, s, "app") // 同名必须能重建
	if err := s.DeleteNamespace(ctx, "not-exist"); err == nil {
		t.Fatal("删除不存在的命名空间应报错")
	}
}

// TestSaveItemsValidation 配置键必填，service 作用域必须带服务名，命名空间必须存在。
func TestSaveItemsValidation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.SaveItems(ctx, "missing", []ItemInput{{Key: "k", Value: "v"}}, "t"); err == nil {
		t.Fatal("不存在的命名空间应拒绝保存")
	}
	mustNamespace(t, s, "app")
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "  ", Value: "v"}}, "t"); err == nil {
		t.Fatal("空键应被拒绝")
	}
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "k", Scope: "service"}}, "t"); err == nil {
		t.Fatal("service 作用域缺服务名应被拒绝")
	}
	if _, err := s.Items(ctx, ""); err == nil {
		t.Fatal("查询缺命名空间应报错")
	}
}

// TestSaveItemsEncryptsAndMasks 加密项以 enc: 密文落库，接口只回显掩码。
func TestSaveItemsEncryptsAndMasks(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")

	if err := s.SaveItems(ctx, "app", []ItemInput{
		{Key: "db.password", Value: "S3cret-2026", Encrypted: true},
		{Key: "feature.flag", Value: "on"},
	}, "alice"); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	var row model.GovConfigItem
	db.Where("namespace = ? AND key = ?", "app", "db.password").First(&row)
	if !strings.HasPrefix(row.Value, gov.EncPrefix) {
		t.Fatalf("加密项应以密文落库，实际 %q", row.Value)
	}
	plain, err := gov.Decrypt(row.Value, testMasterKey)
	if err != nil || plain != "S3cret-2026" {
		t.Fatalf("密文应可解回原值: %q %v", plain, err)
	}
	if row.UpdatedBy != "alice" {
		t.Fatalf("应记录操作人，实际 %q", row.UpdatedBy)
	}

	items, err := s.Items(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.Key] = it.Value
	}
	if got["db.password"] != "******" {
		t.Fatalf("加密项应显示掩码，实际 %q", got["db.password"])
	}
	if got["feature.flag"] != "on" {
		t.Fatalf("明文项应原样返回，实际 %q", got["feature.flag"])
	}
}

// TestSaveItemsKeepsSecretWhenMasked 前端回传空值或掩码时保留原密文，避免误清密钥。
func TestSaveItemsKeepsSecretWhenMasked(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "token", Value: "origin", Encrypted: true}}, "t"); err != nil {
		t.Fatal(err)
	}
	var before model.GovConfigItem
	db.Where("namespace = ? AND key = ?", "app", "token").First(&before)

	for _, v := range []string{"", "******"} {
		if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "token", Value: v, Encrypted: true}}, "t"); err != nil {
			t.Fatalf("保存 %q 失败: %v", v, err)
		}
		var after model.GovConfigItem
		db.Where("namespace = ? AND key = ?", "app", "token").First(&after)
		if after.Value != before.Value {
			t.Fatalf("回传 %q 时不应覆盖原密文: %q -> %q", v, before.Value, after.Value)
		}
	}

	// 传新的明文则必须替换。
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "token", Value: "rotated", Encrypted: true}}, "t"); err != nil {
		t.Fatal(err)
	}
	var rotated model.GovConfigItem
	db.Where("namespace = ? AND key = ?", "app", "token").First(&rotated)
	if rotated.Value == before.Value {
		t.Fatal("新明文应覆盖旧密文")
	}
	if plain, _ := gov.Decrypt(rotated.Value, testMasterKey); plain != "rotated" {
		t.Fatalf("新密文应解出 rotated，实际 %q", plain)
	}
}

// TestSaveItemsWithoutMasterKey 未配置主密钥时保存加密项必须失败，不能落明文。
func TestSaveItemsWithoutMasterKey(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")
	t.Setenv("TENON_MASTER_KEY", "")

	err := s.SaveItems(ctx, "app", []ItemInput{{Key: "secret", Value: "x", Encrypted: true}}, "t")
	if err == nil {
		t.Fatal("无主密钥时应拒绝保存加密项")
	}
	if !strings.Contains(err.Error(), "主密钥") {
		t.Fatalf("错误信息应提示主密钥，实际: %v", err)
	}
	// 明文项不受影响。
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "plain", Value: "v"}}, "t"); err != nil {
		t.Fatalf("明文项应可正常保存: %v", err)
	}
}

// TestPublishKeepsCiphertext 发布载荷必须是密文，发布记录不能成为第二个明文出口。
func TestPublishKeepsCiphertext(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")
	if err := s.SaveItems(ctx, "app", []ItemInput{
		{Key: "db.password", Value: "top-secret", Encrypted: true},
		{Key: "flag", Value: "on"},
	}, "t"); err != nil {
		t.Fatal(err)
	}

	rel, err := s.Publish(ctx, "app", "tester", "首发")
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if rel.Domain != gov.PartConfigPrefix+"app" {
		t.Fatalf("发布域应为 config:app，实际 %q", rel.Domain)
	}
	if rel.Checksum != gov.Checksum(rel.Payload) {
		t.Fatal("校验和与载荷不一致")
	}
	if strings.Contains(string(rel.Payload), "top-secret") {
		t.Fatal("发布载荷里不能出现明文密钥")
	}
	var payload gov.ConfigPayload
	if err := json.Unmarshal(rel.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Namespace != "app" || payload.Rev != rel.Revision {
		t.Fatalf("载荷元信息异常: %+v", payload)
	}
	if !strings.HasPrefix(payload.Items["db.password"].Value, gov.EncPrefix) {
		t.Fatalf("载荷内应是密文，实际 %q", payload.Items["db.password"].Value)
	}
	if payload.Items["flag"].Value != "on" || payload.Items["flag"].Encrypted {
		t.Fatalf("明文项异常: %+v", payload.Items["flag"])
	}

	if _, err := s.Publish(ctx, "", "t", ""); err == nil {
		t.Fatal("缺命名空间应拒绝发布")
	}
}

// TestRollbackRestoresItemValues 回滚生成新版本，并把配置项本身写回旧值。
func TestRollbackRestoresItemValues(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "flag", Value: "on"}}, "t"); err != nil {
		t.Fatal(err)
	}
	r1, err := s.Publish(ctx, "app", "t", "v1")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "flag", Value: "off"}}, "t"); err != nil {
		t.Fatal(err)
	}
	r2, err := s.Publish(ctx, "app", "t", "v2")
	if err != nil {
		t.Fatal(err)
	}

	rolled, err := s.Rollback(ctx, r1.ID, "carol")
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if rolled.Revision <= r2.Revision {
		t.Fatalf("回滚应分配更大版本号: %d <= %d", rolled.Revision, r2.Revision)
	}
	if !strings.Contains(rolled.Note, "回滚到 r") {
		t.Fatalf("回滚备注应可追溯: %q", rolled.Note)
	}

	var item model.GovConfigItem
	db.Where("namespace = ? AND key = ?", "app", "flag").First(&item)
	if item.Value != "on" {
		t.Fatalf("配置项应回写到 r1 的值 on，实际 %q", item.Value)
	}

	// 发布历史按版本倒序，旧的收口为 done。
	list, err := s.Releases(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Status != "active" || list[1].Status != "done" {
		t.Fatalf("发布历史异常: %+v", list)
	}

	// 不能拿路由发布记录来回滚配置。
	routeRel := model.GovRelease{Domain: "route", Revision: 99, Payload: []byte(`{"rev":99}`), Status: "active"}
	db.Create(&routeRel)
	if _, err := s.Rollback(ctx, routeRel.ID, "t"); err == nil {
		t.Fatal("非配置发布记录应拒绝回滚")
	}
}

// TestDeleteItemRecreate 配置项物理删除后同名可以重新创建。
func TestDeleteItemRecreate(t *testing.T) {
	s, db := newStore(t)
	ctx := context.Background()
	mustNamespace(t, s, "app")
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "dup", Value: "1"}}, "t"); err != nil {
		t.Fatal(err)
	}
	var item model.GovConfigItem
	db.Where("namespace = ? AND key = ?", "app", "dup").First(&item)

	if err := s.DeleteItem(ctx, item.ID); err != nil {
		t.Fatalf("删除配置项失败: %v", err)
	}
	var count int64
	db.Unscoped().Model(&model.GovConfigItem{}).Where("namespace = ? AND key = ?", "app", "dup").Count(&count)
	if count != 0 {
		t.Fatalf("配置项应被物理删除，实际剩 %d", count)
	}
	if err := s.SaveItems(ctx, "app", []ItemInput{{Key: "dup", Value: "2"}}, "t"); err != nil {
		t.Fatalf("同名配置应能重建: %v", err)
	}
	if err := s.DeleteItem(ctx, "not-exist"); err == nil {
		t.Fatal("删除不存在的配置项应报错")
	}
}
