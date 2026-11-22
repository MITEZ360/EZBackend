package migrate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/model"
	"github.com/keel-framework/keel/internal/platform/config"
)

func newTestRunner(t *testing.T) (*Runner, *gorm.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "migrate.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(db, config.Migration{Mode: "versioned"}, "sqlite", zap.NewNop(),
		[]any{&model.User{}, &model.UserOAuthBinding{}})
	if err != nil {
		t.Fatal(err)
	}
	return r, db
}

func TestRunnerUpDownCycle(t *testing.T) {
	r, db := newTestRunner(t)
	ctx := context.Background()

	st, err := r.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != 0 || st.Pending == 0 {
		t.Fatalf("初始状态异常: current=%d pending=%d", st.Current, st.Pending)
	}

	applied, err := r.Up(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 4 {
		t.Fatalf("应应用 4 个版本, got %d", len(applied))
	}
	st, _ = r.Status(ctx)
	if st.Current != st.Latest || st.Pending != 0 {
		t.Fatalf("升级后状态异常: current=%d latest=%d pending=%d", st.Current, st.Latest, st.Pending)
	}
	if !db.Migrator().HasTable(&model.UserOAuthBinding{}) {
		t.Fatal("v2 应创建 oauth 绑定表")
	}

	// 幂等：再次 Up 无新增
	applied, err = r.Up(ctx)
	if err != nil || len(applied) != 0 {
		t.Fatalf("重复升级应幂等: applied=%d err=%v", len(applied), err)
	}

	// 回滚一步（v4）
	down, err := r.Down(ctx)
	if err != nil || len(down) != 1 {
		t.Fatalf("回滚一步失败: %d %v", len(down), err)
	}
	st, _ = r.Status(ctx)
	if st.Pending != 1 {
		t.Fatalf("回滚后应有 1 个待应用, got %d", st.Pending)
	}

	// UpTo 指定版本
	if _, err := r.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	st, _ = r.Status(ctx)
	if st.Current != 4 || st.Pending != 0 {
		t.Fatalf("UpTo(4) 后异常: current=%d", st.Current)
	}
}

func TestRunnerDownToRebuild(t *testing.T) {
	r, db := newTestRunner(t)
	ctx := context.Background()
	if _, err := r.Up(ctx); err != nil {
		t.Fatal(err)
	}
	// 回滚到 v1：oauth 绑定表应被删除
	if _, err := r.DownTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasTable(&model.UserOAuthBinding{}) {
		t.Fatal("DownTo(1) 后 oauth 绑定表应被删除")
	}
	// 再升级回来
	if _, err := r.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&model.UserOAuthBinding{}) {
		t.Fatal("重新升级后 oauth 绑定表应恢复")
	}
}
