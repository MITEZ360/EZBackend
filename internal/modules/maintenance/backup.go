// Package maintenance 提供数据库备份/恢复与版本迁移的运维能力。
package maintenance

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/keel-framework/keel/internal/platform/config"
)

// BackupMeta 备份产物元信息，与备份文件同名、后缀 .meta.json。
type BackupMeta struct {
	Name       string   `json:"name"`
	Driver     string   `json:"driver"`
	CreatedAt  string   `json:"createdAt"`
	Size       int64    `json:"size"`
	Compressed bool     `json:"compressed"`
	SHA256     string   `json:"sha256"`
	Tables     []string `json:"tables,omitempty"`
	UserCount  int64    `json:"userCount,omitempty"`
}

// BackupFile 列表项。
type BackupFile struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	CreatedAt  string `json:"createdAt"`
	Size       int64  `json:"size"`
	Compressed bool   `json:"compressed"`
	SHA256     string `json:"sha256"`
	Verified   bool   `json:"verified"`
}

// BackupService 数据库备份与恢复。
type BackupService struct {
	db     *gorm.DB
	cfg    config.Backup
	dbCfg  config.DB
	driver string
	log    *zap.Logger
}

// NewBackupService 创建备份服务。
func NewBackupService(db *gorm.DB, cfg config.Backup, dbCfg config.DB, log *zap.Logger) *BackupService {
	if log == nil {
		log = zap.NewNop()
	}
	return &BackupService{db: db, cfg: cfg, dbCfg: dbCfg, driver: dbCfg.ResolveDriver(), log: log}
}

// Dir 返回备份目录（绝对路径）。
func (s *BackupService) Dir() string {
	abs, err := filepath.Abs(s.cfg.EffectiveDir())
	if err != nil {
		return s.cfg.EffectiveDir()
	}
	return abs
}

func (s *BackupService) ensureDir() error {
	return os.MkdirAll(s.Dir(), 0o750)
}

// DBPath 返回 SQLite 主库文件路径。
func (s *BackupService) DBPath() string {
	p := strings.TrimPrefix(strings.TrimSpace(s.dbCfg.DSN), "file:")
	if p == "" {
		p = "data/keel.db"
	}
	// 去掉查询参数（_pragma=...）
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return p
}

// Backup 立即生成一份备份，返回元信息。
func (s *BackupService) Backup(ctx context.Context) (*BackupMeta, error) {
	if err := s.ensureDir(); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	ext := ".db"
	if s.driver != "sqlite" {
		ext = ".sql"
	}
	name := fmt.Sprintf("keel-%s-%s%s", s.driver, stamp, ext)
	if s.cfg.Compress {
		name += ".gz"
	}
	final := filepath.Join(s.Dir(), name)
	// 同一秒重复执行时避开同名覆盖
	for i := 2; ; i++ {
		if _, err := os.Stat(final); errors.Is(err, os.ErrNotExist) {
			break
		}
		base := strings.TrimSuffix(name, ".gz")
		name = fmt.Sprintf("%s-%d%s", base, i, map[bool]string{true: ".gz", false: ""}[s.cfg.Compress])
		final = filepath.Join(s.Dir(), name)
	}

	tmp := filepath.Join(s.Dir(), ".tmp-"+stamp+"-"+randomSuffix(6))
	defer os.Remove(tmp)

	switch s.driver {
	case "sqlite":
		if err := s.dumpSQLite(ctx, tmp); err != nil {
			return nil, err
		}
	case "mysql":
		if err := s.dumpMySQL(ctx, tmp); err != nil {
			return nil, err
		}
	case "postgres":
		if err := s.dumpPostgres(ctx, tmp); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("暂不支持备份数据库驱动: %s", s.driver)
	}

	sum, size, err := finalizeBackup(tmp, final, s.cfg.Compress)
	if err != nil {
		return nil, err
	}
	meta := &BackupMeta{
		Name: name, Driver: s.driver, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Size: size, Compressed: s.cfg.Compress, SHA256: sum,
	}
	if s.driver == "sqlite" {
		if tables, users, err := inspectSQLite(final, s.cfg.Compress); err == nil {
			meta.Tables = tables
			meta.UserCount = users
		}
	}
	if err := writeMeta(s.Dir(), meta); err != nil {
		return nil, err
	}
	if err := s.prune(); err != nil {
		s.log.Warn("清理过期备份失败", zap.Error(err))
	}
	s.log.Info("数据库备份完成", zap.String("file", name), zap.Int64("size", size))
	return meta, nil
}

func (s *BackupService) dumpSQLite(ctx context.Context, tmp string) error {
	// VACUUM INTO 生成一致性快照，包含 WAL 中已提交的数据，且不需要停服。
	// 目标文件必须不存在，路径由本函数生成并转义单引号。
	_ = os.Remove(tmp)
	quoted := strings.ReplaceAll(tmp, "'", "''")
	if err := s.db.WithContext(ctx).Exec(fmt.Sprintf("VACUUM INTO '%s'", quoted)).Error; err != nil {
		return fmt.Errorf("SQLite 在线备份失败: %w", err)
	}
	return nil
}

func (s *BackupService) dumpMySQL(ctx context.Context, tmp string) error {
	cfg, err := mysql.ParseDSN(s.dbCfg.DSN)
	if err != nil {
		return fmt.Errorf("解析 MySQL DSN 失败: %w", err)
	}
	args := []string{"--single-transaction", "--routines", "--triggers",
		"--default-character-set=utf8mb4", "--skip-column-statistics"}
	host, port := "127.0.0.1", "3306"
	if cfg.Net == "unix" {
		args = append(args, "--socket", cfg.Addr)
	} else {
		if h, p, e := splitHostPort(cfg.Addr); e == nil {
			host, port = h, p
		}
		args = append(args, "--host", host, "--port", port)
	}
	args = append(args, "--user", cfg.User, cfg.DBName)
	cmd := exec.CommandContext(ctx, s.cfg.MySQLDump, args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+cfg.Passwd)
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd.Stdout = f
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysqldump 执行失败: %w: %s", err, stderr.String())
	}
	return nil
}

func (s *BackupService) dumpPostgres(ctx context.Context, tmp string) error {
	host, port, user, password, dbname, err := parsePGDSN(s.dbCfg.DSN)
	if err != nil {
		return err
	}
	args := []string{"--no-owner", "--no-privileges", "--clean", "--if-exists",
		"--host", host, "--port", port, "--username", user, "--dbname", dbname}
	cmd := exec.CommandContext(ctx, s.cfg.PGDump, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd.Stdout = f
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump 执行失败: %w: %s", err, stderr.String())
	}
	return nil
}

// finalizeBackup 计算校验和并按需 gzip，返回 (sha256, 最终大小, error)。
func finalizeBackup(tmp, final string, compress bool) (string, int64, error) {
	in, err := os.Open(tmp)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(final, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", 0, err
	}
	defer out.Close()

	var w io.Writer = out
	var gz *gzip.Writer
	if compress {
		gz = gzip.NewWriter(out)
		w = gz
	}
	if _, err := io.Copy(w, in); err != nil {
		return "", 0, err
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			return "", 0, err
		}
	}
	if err := out.Sync(); err != nil {
		return "", 0, err
	}
	if err := out.Close(); err != nil {
		return "", 0, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return "", 0, err
	}
	// 校验和针对最终落盘文件（含 gzip 头），Verify 直接对同一文件取哈希即可比对
	sum, err := fileSHA256(final)
	if err != nil {
		return "", 0, err
	}
	return sum, st.Size(), nil
}

// List 列出备份文件（按时间倒序）。
func (s *BackupService) List() ([]BackupFile, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		return nil, err
	}
	out := make([]BackupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isBackupName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		item := BackupFile{
			Name: e.Name(), Size: info.Size(),
			CreatedAt:  info.ModTime().UTC().Format(time.RFC3339),
			Compressed: strings.HasSuffix(e.Name(), ".gz"),
		}
		if meta, err := readMeta(s.Dir(), e.Name()); err == nil {
			item.Driver = meta.Driver
			item.SHA256 = meta.SHA256
			item.CreatedAt = meta.CreatedAt
			item.Verified = true
		} else {
			item.Driver = driverFromName(e.Name())
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Meta 读取指定备份的元信息。
func (s *BackupService) Meta(name string) (*BackupMeta, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	meta, err := readMeta(s.Dir(), name)
	if err != nil {
		return nil, err
	}
	return meta, nil
}

// Path 返回备份文件的绝对路径，并校验名称合法性。
func (s *BackupService) Path(name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	p := filepath.Join(s.Dir(), name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("备份文件不存在")
	}
	return p, nil
}

// Verify 校验备份完整性与可用性，返回可读的结论。
func (s *BackupService) Verify(name string) (*VerifyResult, error) {
	p, err := s.Path(name)
	if err != nil {
		return nil, err
	}
	compressed := strings.HasSuffix(name, ".gz")
	sum, err := fileSHA256(p)
	if err != nil {
		return nil, err
	}
	res := &VerifyResult{Name: name, Size: mustFileSize(p), SHA256: sum, Driver: driverFromName(name)}
	if meta, err := readMeta(s.Dir(), name); err == nil {
		res.MetaMatched = meta.SHA256 == sum
		res.CreatedAt = meta.CreatedAt
		res.Driver = meta.Driver
	}
	tmp, cleanup, err := decompressToTemp(p, compressed)
	if err != nil {
		return nil, fmt.Errorf("备份文件解压/读取失败: %w", err)
	}
	defer cleanup()
	if res.Driver == "sqlite" {
		tables, users, err := inspectSQLite(tmp, false)
		if err != nil {
			return nil, fmt.Errorf("备份内容不可用: %w", err)
		}
		res.Tables = tables
		res.UserCount = users
		res.Integrity = "ok"
	} else {
		st, _ := os.Stat(tmp)
		if st != nil {
			res.Tables = []string{"SQL 转储"}
			res.Integrity = fmt.Sprintf("可读，%d 字节", st.Size())
		}
	}
	res.OK = true
	return res, nil
}

// VerifyResult 校验结论。
type VerifyResult struct {
	Name        string   `json:"name"`
	OK          bool     `json:"ok"`
	Driver      string   `json:"driver"`
	Size        int64    `json:"size"`
	SHA256      string   `json:"sha256"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	MetaMatched bool     `json:"metaMatched"`
	Integrity   string   `json:"integrity,omitempty"`
	Tables      []string `json:"tables,omitempty"`
	UserCount   int64    `json:"userCount,omitempty"`
}

// Delete 删除备份文件及其元信息。
func (s *BackupService) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	_ = os.Remove(metaPath(s.Dir(), name))
	return nil
}

// RestoreOnline 在线恢复（仅 MySQL / PostgreSQL，需显式开启 allow_restore）。
// SQLite 需要替换数据文件，必须停机后使用 CLI 的 restore 子命令。
func (s *BackupService) RestoreOnline(ctx context.Context, name string) (*RestoreResult, error) {
	if !s.cfg.AllowRestore {
		return nil, fmt.Errorf("在线恢复未启用：请设置 backup.allow_restore=true")
	}
	if s.driver == "sqlite" {
		return nil, fmt.Errorf("SQLite 需要替换数据文件，请在停止服务后执行：keel restore %s", name)
	}
	p, err := s.Path(name)
	if err != nil {
		return nil, err
	}
	compressed := strings.HasSuffix(name, ".gz")
	tmp, cleanup, err := decompressToTemp(p, compressed)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	f, err := os.Open(tmp)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cmd *exec.Cmd
	switch s.driver {
	case "mysql":
		cfg, e := mysql.ParseDSN(s.dbCfg.DSN)
		if e != nil {
			return nil, fmt.Errorf("解析 MySQL DSN 失败: %w", e)
		}
		args := []string{"--user", cfg.User}
		if cfg.Net == "unix" {
			args = append(args, "--socket", cfg.Addr)
		} else {
			h, port, _ := splitHostPort(cfg.Addr)
			args = append(args, "--host", h, "--port", port)
		}
		args = append(args, cfg.DBName)
		cmd = exec.CommandContext(ctx, s.cfg.MySQLRestore, args...)
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+cfg.Passwd)
	case "postgres":
		host, port, user, password, dbname, e := parsePGDSN(s.dbCfg.DSN)
		if e != nil {
			return nil, e
		}
		cmd = exec.CommandContext(ctx, s.cfg.PGRestore,
			"--host", host, "--port", port, "--username", user,
			"--dbname", dbname, "--file", tmp)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	default:
		return nil, fmt.Errorf("暂不支持在线恢复: %s", s.driver)
	}
	cmd.Stdin = f
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("恢复执行失败: %w: %s", err, stderr.String())
	}
	return &RestoreResult{Name: name, Mode: "online", RestartRequired: false,
		Message: "已从备份导入数据，建议刷新页面确认"}, nil
}

// RestoreOffline 停机恢复 SQLite 数据文件。会先把当前库另存为安全副本。
// 由 CLI 子命令调用，调用方需保证服务未运行。
func (s *BackupService) RestoreOffline(name string) (*RestoreResult, error) {
	if s.driver != "sqlite" {
		return nil, fmt.Errorf("停机恢复仅支持 SQLite；MySQL / PostgreSQL 请使用在线恢复或原生工具")
	}
	// 既接受备份目录里的文件名，也接受外部传入的完整路径
	var p string
	if st, e := os.Stat(name); e == nil && !st.IsDir() {
		p = name
	} else {
		var err error
		p, err = s.Path(name)
		if err != nil {
			return nil, err
		}
	}
	compressed := strings.HasSuffix(p, ".gz")
	tmp, cleanup, err := decompressToTemp(p, compressed)
	if err != nil {
		return nil, fmt.Errorf("备份文件不可用: %w", err)
	}
	defer cleanup()
	if _, _, err := inspectSQLite(tmp, false); err != nil {
		return nil, fmt.Errorf("备份文件校验失败，已中止恢复: %w", err)
	}

	dbPath := s.DBPath()
	safety := ""
	if _, err := os.Stat(dbPath); err == nil {
		// 先把当前库复制出来，恢复失败时可回滚
		safety = filepath.Join(s.Dir(), "pre-restore-"+time.Now().Format("20060102-150405")+".db")
		src, e := os.Open(dbPath)
		if e != nil {
			return nil, e
		}
		dst, e := os.OpenFile(safety, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if e != nil {
			src.Close()
			return nil, e
		}
		_, e = io.Copy(dst, src)
		src.Close()
		_ = dst.Close()
		if e != nil {
			return nil, e
		}
	}

	stage := dbPath + ".restoring"
	if err := copyFile(tmp, stage); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, dbPath); err != nil {
		return nil, err
	}
	// WAL / SHM 属于旧库，必须清掉，否则 SQLite 会用旧日志覆盖恢复后的数据
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	return &RestoreResult{
		Name: filepath.Base(p), Mode: "offline", RestartRequired: false, SafetyCopy: safety,
		Message: "数据文件已替换，请重新启动服务",
	}, nil
}

// RestoreResult 恢复结果。
type RestoreResult struct {
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	RestartRequired bool   `json:"restartRequired"`
	SafetyCopy      string `json:"safetyCopy,omitempty"`
	Message         string `json:"message"`
}

// prune 按保留份数清理最旧的备份。
func (s *BackupService) prune() error {
	list, err := s.List()
	if err != nil {
		return err
	}
	if len(list) <= s.cfg.Retain {
		return nil
	}
	for _, f := range list[s.cfg.Retain:] {
		if err := s.Delete(f.Name); err != nil {
			return err
		}
		s.log.Info("清理过期备份", zap.String("file", f.Name))
	}
	return nil
}

// ---- 辅助函数 ----

func isBackupName(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "pre-restore-") {
		return false
	}
	return strings.HasPrefix(name, "keel-") &&
		(strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db.gz") ||
			strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".sql.gz"))
}

func driverFromName(name string) string {
	switch {
	case strings.Contains(name, "sqlite"):
		return "sqlite"
	case strings.Contains(name, "mysql"):
		return "mysql"
	case strings.Contains(name, "postgres"):
		return "postgres"
	default:
		return "unknown"
	}
}

func validName(name string) error {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return fmt.Errorf("备份文件名非法")
	}
	if !isBackupName(name) {
		return fmt.Errorf("不是有效的备份文件")
	}
	return nil
}

func metaPath(dir, name string) string { return filepath.Join(dir, name+".meta.json") }

func writeMeta(dir string, meta *BackupMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(dir, meta.Name), b, 0o640)
}

func readMeta(dir, name string) (*BackupMeta, error) {
	b, err := os.ReadFile(metaPath(dir, name))
	if err != nil {
		return nil, fmt.Errorf("备份元信息不存在")
	}
	var m BackupMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("备份元信息损坏: %w", err)
	}
	return &m, nil
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func mustFileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

// decompressToTemp 把备份还原成可直接读取的原始文件，返回临时路径与清理函数。
func decompressToTemp(p string, compressed bool) (string, func(), error) {
	f, err := os.Open(p)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	tmp, err := os.CreateTemp("", "keel-restore-*.db")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(tmp.Name())
		_ = os.Remove(tmp.Name() + "-wal")
		_ = os.Remove(tmp.Name() + "-shm")
	}
	var r io.Reader = f
	if compressed {
		gz, err := gzip.NewReader(f)
		if err != nil {
			tmp.Close()
			cleanup()
			return "", nil, err
		}
		defer gz.Close()
		r = gz
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp.Name(), cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// inspectSQLite 校验 SQLite 备份并列出表与用户数。
func inspectSQLite(path string, compressed bool) ([]string, int64, error) {
	realPath, cleanup, err := decompressToTemp(path, compressed)
	if err != nil {
		return nil, 0, err
	}
	defer cleanup()
	db, err := gorm.Open(sqlite.Open(realPath), &gorm.Config{})
	if err != nil {
		return nil, 0, err
	}
	if sqlDB, e := db.DB(); e == nil {
		defer sqlDB.Close()
	}
	var status string
	if err := db.Raw("PRAGMA integrity_check").Scan(&status).Error; err != nil {
		return nil, 0, err
	}
	if status != "ok" {
		return nil, 0, fmt.Errorf("integrity_check=%s", status)
	}
	var tables []string
	if err := db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").
		Scan(&tables).Error; err != nil {
		return nil, 0, err
	}
	var users int64
	if contains(tables, "sys_user") {
		_ = db.Raw("SELECT COUNT(*) FROM sys_user").Scan(&users).Error
	}
	return tables, users, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func randomSuffix(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	v := time.Now().UnixNano()
	for i := range b {
		b[i] = chars[int(v%int64(len(chars)))]
		v = v/int64(len(chars)) + int64(i)*31
	}
	return string(b)
}

func splitHostPort(addr string) (string, string, error) {
	if addr == "" {
		return "127.0.0.1", "3306", nil
	}
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[:i], addr[i+1:], nil
	}
	return addr, "3306", nil
}

func parsePGDSN(dsn string) (host, port, user, password, dbname string, err error) {
	host, port = "127.0.0.1", "5432"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			return "", "", "", "", "", fmt.Errorf("解析 PostgreSQL DSN 失败: %w", e)
		}
		host, port = u.Hostname(), u.Port()
		if port == "" {
			port = "5432"
		}
		if host == "" {
			host = "127.0.0.1"
		}
		dbname = strings.TrimPrefix(u.Path, "/")
		if u.User != nil {
			user = u.User.Username()
			password, _ = u.User.Password()
		}
	} else {
		for _, kv := range strings.Fields(dsn) {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			switch k {
			case "host":
				host = v
			case "port":
				port = v
			case "user":
				user = v
			case "password":
				password = v
			case "dbname":
				dbname = v
			}
		}
	}
	if dbname == "" || user == "" {
		return "", "", "", "", "", fmt.Errorf("PostgreSQL DSN 缺少 dbname 或 user")
	}
	return host, port, user, password, dbname, nil
}
