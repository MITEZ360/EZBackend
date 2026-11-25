package backup

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/keel-framework/keel/internal/platform/config"
)

func TestIsBackupNameAndDriver(t *testing.T) {
	cases := []struct {
		name   string
		valid  bool
		driver string
	}{
		{"keel-sqlite-20261122-222506.tar.gz", true, "sqlite"},
		{"keel-mysql-20261122-222506.sql.tar", true, "mysql"},
		{"keel-postgres-20261122-222506.tar.gz", true, "postgres"},
		{"keel-sqlite-20261122-222506.db.gz", true, "sqlite"}, // 旧格式兼容
		{"pre-restore-20261122.db", false, ""},
		{"random.txt", false, ""},
	}
	for _, c := range cases {
		if got := isBackupName(c.name); got != c.valid {
			t.Errorf("isBackupName(%q) = %v, want %v", c.name, got, c.valid)
		}
		if c.valid {
			if got := driverFromName(c.name); got != c.driver {
				t.Errorf("driverFromName(%q) = %q, want %q", c.name, got, c.driver)
			}
		}
	}
	if err := validName("../keel-sqlite-x.tar.gz"); err == nil {
		t.Error("validName should reject path separator")
	}
}

func TestBackupTime(t *testing.T) {
	for _, name := range []string{
		"keel-sqlite-20261122-222506.tar.gz",
		"keel-mysql-20260102-030405.sql.tar.gz",
	} {
		if _, err := backupTime(name); err != nil {
			t.Errorf("backupTime(%q) failed: %v", name, err)
		}
	}
	if _, err := backupTime("garbage"); err == nil {
		t.Error("backupTime(garbage) should fail")
	}
}

func TestPackOpenArchive(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "src.db")
	if err := os.WriteFile(db, []byte("SQLITE-DUMMY"), 0o640); err != nil {
		t.Fatal(err)
	}
	filesRoot := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(filepath.Join(filesRoot, "2026/11"), 0o750); err != nil {
		t.Fatal(err)
	}
	icon := filepath.Join(filesRoot, "2026/11/icon.png")
	if err := os.WriteFile(icon, []byte("png-bytes"), 0o640); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(icon)
	if err != nil {
		t.Fatal(err)
	}
	entries := []FileEntry{{Path: "2026/11/icon.png", Size: 9, SHA256: sum}}
	final := filepath.Join(dir, "backup.tar.gz")
	if _, _, err := packBackup(db, final, true, "database.db", entries, filesRoot); err != nil {
		t.Fatal(err)
	}
	a, err := openArchive(final)
	if err != nil {
		t.Fatal(err)
	}
	defer a.cleanup()
	if a.dbPath == "" {
		t.Fatal("archive missing database.db")
	}
	if a.filesDir == "" {
		t.Fatal("archive missing files/")
	}
	got, err := os.ReadFile(filepath.Join(a.filesDir, "2026/11/icon.png"))
	if err != nil || string(got) != "png-bytes" {
		t.Fatalf("file asset mismatch: %q %v", got, err)
	}
	checked, bad := verifyFiles(a.filesDir)
	if checked != 1 || bad != 0 {
		t.Fatalf("verifyFiles = (%d,%d), want (1,0)", checked, bad)
	}
}

func TestOpenArchiveRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "evil.tar")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg, Size: 3})
	_, _ = tw.Write([]byte("bad"))
	_ = tw.Close()
	_ = f.Close()
	if _, err := openArchive(p); err == nil {
		t.Fatal("openArchive should reject path traversal")
	}
}

func TestPrunePlanGFS(t *testing.T) {
	dir := t.TempDir()
	// 5 天 × 8 小时 = 40 份
	for d := 1; d <= 5; d++ {
		for h := 0; h < 8; h++ {
			name := filepath.Join(dir, "keel-sqlite-202611")
			name += twoDigits(d) + "-" + twoDigits(h) + "0000.tar.gz"
			if err := os.WriteFile(name, []byte("x"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	}
	svc := &Service{
		cfg: config.Backup{
			Dir: dir, Retain: 0,
			RetainHourly: 12, RetainDaily: 3, RetainWeekly: 0, RetainMonthly: 0,
		},
		log: zap.NewNop(),
	}
	plan, err := svc.PrunePlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slots["hourly"] != 12 {
		t.Fatalf("hourly slots = %d, want 12", plan.Slots["hourly"])
	}
	if plan.Slots["daily"] != 3 {
		t.Fatalf("daily slots = %d, want 3", plan.Slots["daily"])
	}
	if len(plan.Keep) == 0 || plan.Keep[0] != "keel-sqlite-20261105-070000.tar.gz" {
		t.Fatalf("latest not kept: %v", plan.Keep)
	}
	if len(plan.Keep)+len(plan.Remove) != 40 {
		t.Fatalf("total mismatch: keep=%d remove=%d", len(plan.Keep), len(plan.Remove))
	}
}

func TestPrunePlanKeepsLatestWhenAllZero(t *testing.T) {
	dir := t.TempDir()
	for d := 1; d <= 3; d++ {
		name := filepath.Join(dir, "keel-sqlite-202611"+twoDigits(d)+"-010000.tar.gz")
		if err := os.WriteFile(name, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	// Retain=1 模拟「至少保留最近 1 份」的兜底
	svc := &Service{cfg: config.Backup{Dir: dir, Retain: 1}, log: zap.NewNop()}
	plan, err := svc.PrunePlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Keep) != 1 || plan.Keep[0] != "keel-sqlite-20261103-010000.tar.gz" {
		t.Fatalf("want only latest kept, got %v", plan.Keep)
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
