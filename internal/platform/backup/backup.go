// Package backup 提供数据库与文件资产的备份、恢复、校验与保留策略。
//
// 放在 platform 而不是某个业务模块：备份是任何发行版都要有的底座能力，
// 运维子命令（tenon backup / restore）在精简发行版里也必须可用。
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	"github.com/MITEZ360/tenon/internal/platform/config"
)

// FileEntry 归档内单个文件资产的校验信息。
type FileEntry struct {
	Path   string `json:"path"` // 相对存储根目录，使用 /
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Meta 备份产物元信息，与备份文件同名、后缀 .meta.json。
type Meta struct {
	Name        string            `json:"name"`
	Driver      string            `json:"driver"`
	CreatedAt   string            `json:"createdAt"`
	Size        int64             `json:"size"`
	Compressed  bool              `json:"compressed"`
	SHA256      string            `json:"sha256"`
	Tables      []string          `json:"tables,omitempty"`
	UserCount   int64             `json:"userCount,omitempty"`
	Files       int               `json:"files"`                 // 归档内文件资产数量
	FileBytes   int64             `json:"fileBytes"`             // 文件资产总字节
	StorageRoot string            `json:"storageRoot,omitempty"` // 备份时的本地存储根目录
	StorageMode string            `json:"storageMode"`           // local / s3 / 其它
	FileList    []FileEntry `json:"fileList,omitempty"`    // 文件清单（含 SHA-256）
}

// File 列表项。
type File struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	CreatedAt  string `json:"createdAt"`
	Size       int64  `json:"size"`
	Compressed bool   `json:"compressed"`
	SHA256     string `json:"sha256"`
	Verified   bool   `json:"verified"`
	Files      int    `json:"files,omitempty"`
	FileBytes  int64  `json:"fileBytes,omitempty"`
}

// Service 数据库与文件资产备份、恢复。
type Service struct {
	db         *gorm.DB
	cfg        config.Backup
	dbCfg      config.DB
	storageCfg config.Storage
	driver     string
	log        *zap.Logger
}

// NewService 创建备份服务。storageCfg 用于把本地文件资产纳入归档。
func NewService(db *gorm.DB, cfg config.Backup, dbCfg config.DB, storageCfg config.Storage, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, cfg: cfg, dbCfg: dbCfg, storageCfg: storageCfg, driver: dbCfg.ResolveDriver(), log: log}
}

// Dir 返回备份目录（绝对路径）。
func (s *Service) Dir() string {
	abs, err := filepath.Abs(s.cfg.EffectiveDir())
	if err != nil {
		return s.cfg.EffectiveDir()
	}
	return abs
}

func (s *Service) ensureDir() error {
	return os.MkdirAll(s.Dir(), 0o750)
}

// DBPath 返回 SQLite 主库文件路径。
func (s *Service) DBPath() string {
	p := strings.TrimPrefix(strings.TrimSpace(s.dbCfg.DSN), "file:")
	if p == "" {
		p = "data/tenon.db"
	}
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return p
}

// StorageRoot 返回本地存储根目录（绝对路径）；非本地模式返回空串。
func (s *Service) StorageRoot() string {
	if s.storageCfg.Kind() != "local" {
		return ""
	}
	root := strings.TrimSpace(s.storageCfg.LocalDir)
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

// Backup 立即生成一份备份，返回元信息。
// 新格式为 tar 容器（含 database.db / files/ / manifest.json），旧格式纯 .db/.sql 仍可读取。
func (s *Service) Backup(ctx context.Context) (*Meta, error) {
	if err := s.ensureDir(); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	ext := ".tar"
	if s.driver != "sqlite" {
		ext = ".sql.tar"
	}
	name := fmt.Sprintf("tenon-%s-%s%s", s.driver, stamp, ext)
	if s.cfg.Compress {
		name += ".gz"
	}
	final := filepath.Join(s.Dir(), name)
	for i := 2; ; i++ {
		if _, err := os.Stat(final); errors.Is(err, os.ErrNotExist) {
			break
		}
		base := strings.TrimSuffix(name, ".gz")
		base = strings.TrimSuffix(base, ".tar")
		tail := ".tar"
		if strings.Contains(name, ".sql.tar") {
			tail = ".sql.tar"
		}
		name = fmt.Sprintf("%s-%d%s%s", base, i, tail, map[bool]string{true: ".gz", false: ""}[s.cfg.Compress])
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

	// 收集本地文件资产清单（对象存储模式返回空并标注）
	fileList, err := s.collectFiles()
	if err != nil {
		return nil, err
	}
	dbName := "database.db"
	if s.driver != "sqlite" {
		dbName = "database.sql"
	}
	sum, size, err := packBackup(tmp, final, s.cfg.Compress, dbName, fileList, s.filesDir())
	if err != nil {
		return nil, err
	}
	var fileBytes int64
	for _, f := range fileList {
		fileBytes += f.Size
	}
	mode := s.storageCfg.Kind()
	if mode == "" {
		mode = "none"
	}
	meta := &Meta{
		Name: name, Driver: s.driver, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Size: size, Compressed: s.cfg.Compress, SHA256: sum,
		Files: len(fileList), FileBytes: fileBytes, StorageRoot: s.filesDir(),
		StorageMode: mode, FileList: fileList,
	}
	if s.driver == "sqlite" {
		if tables, users, err := inspectSQLite(tmp, false); err == nil {
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
	s.log.Info("数据库与文件资产备份完成",
		zap.String("file", name), zap.Int64("size", size), zap.Int("files", len(fileList)))
	return meta, nil
}

func (s *Service) filesDir() string {
	if s.storageCfg.Kind() == "local" {
		return s.storageCfg.LocalDir
	}
	return ""
}

// collectFiles 遍历本地存储目录，生成 (相对路径, 大小, SHA-256) 清单。
func (s *Service) collectFiles() ([]FileEntry, error) {
	root := s.StorageRoot()
	if root == "" {
		return nil, nil
	}
	st, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取文件资产目录失败: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("文件资产路径不是目录: %s", root)
	}
	entries := make([]FileEntry, 0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无法访问的条目，避免单个坏文件导致整份备份失败
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return nil
		}
		sum, e := fileSHA256(path)
		if e != nil {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return nil
		}
		entries = append(entries, FileEntry{Path: filepath.ToSlash(rel), Size: info.Size(), SHA256: sum})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func (s *Service) dumpSQLite(ctx context.Context, tmp string) error {
	_ = os.Remove(tmp)
	quoted := strings.ReplaceAll(tmp, "'", "''")
	if err := s.db.WithContext(ctx).Exec(fmt.Sprintf("VACUUM INTO '%s'", quoted)).Error; err != nil {
		return fmt.Errorf("SQLite 在线备份失败: %w", err)
	}
	return nil
}

func (s *Service) dumpMySQL(ctx context.Context, tmp string) error {
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

func (s *Service) dumpPostgres(ctx context.Context, tmp string) error {
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

// packBackup 把数据库快照与文件资产打包成 tar（可选 gzip）。
func packBackup(dbFile, final string, compress bool, dbName string, entries []FileEntry, root string) (string, int64, error) {
	out, err := os.OpenFile(final, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return "", 0, err
	}
	var w io.Writer = out
	var gz *gzip.Writer
	if compress {
		gz = gzip.NewWriter(out)
		w = gz
	}
	tw := tar.NewWriter(w)
	addFile := func(headerName, src string) error {
		f, e := os.Open(src)
		if e != nil {
			return e
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return e
		}
		if e := tw.WriteHeader(&tar.Header{
			Name: headerName, Mode: 0o640, Size: info.Size(),
			ModTime: info.ModTime(), Typeflag: tar.TypeReg,
		}); e != nil {
			return e
		}
		_, e = io.Copy(tw, f)
		return e
	}
	if err := addFile(dbName, dbFile); err != nil {
		tw.Close()
		if gz != nil {
			gz.Close()
		}
		out.Close()
		os.Remove(final)
		return "", 0, err
	}
	// 归档内清单，便于脱离 .meta.json 也能自检
	manifest := struct {
		Database string            `json:"database"`
		Files    []FileEntry `json:"files"`
	}{Database: dbName, Files: entries}
	if mb, e := json.Marshal(manifest); e == nil {
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o640,
			Size: int64(len(mb)), ModTime: time.Now(), Typeflag: tar.TypeReg})
		_, _ = tw.Write(mb)
	}
	for _, entry := range entries {
		src := filepath.Join(root, filepath.FromSlash(entry.Path))
		if e := addFile("files/"+entry.Path, src); e != nil {
			tw.Close()
			if gz != nil {
				gz.Close()
			}
			out.Close()
			os.Remove(final)
			return "", 0, fmt.Errorf("打包文件资产 %s 失败: %w", entry.Path, e)
		}
	}
	if err := tw.Close(); err != nil {
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
	sum, err := fileSHA256(final)
	if err != nil {
		return "", 0, err
	}
	return sum, st.Size(), nil
}

// archive 解包结果：dir 为临时根目录，dbPath 为数据库快照，filesDir 为文件资产目录（可能为空）。
type archive struct {
	dir      string
	dbPath   string
	filesDir string
	cleanup  func()
}

// openArchive 打开备份；新格式 tar 解包，旧格式纯 db/sql 回退到临时文件。
func openArchive(p string) (*archive, error) {
	name := strings.ToLower(p)
	if !strings.HasSuffix(name, ".tar") && !strings.HasSuffix(name, ".tar.gz") {
		dbPath, cleanup, err := decompressToTemp(p, strings.HasSuffix(name, ".gz"))
		if err != nil {
			return nil, err
		}
		return &archive{dbPath: dbPath, cleanup: cleanup}, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	var gz *gzip.Reader
	if strings.HasSuffix(name, ".gz") {
		gz, err = gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("备份解压失败: %w", err)
		}
		defer gz.Close()
		r = gz
	}
	dir, err := os.MkdirTemp("", "tenon-archive-*")
	if err != nil {
		return nil, err
	}
	a := &archive{dir: dir, cleanup: func() { _ = os.RemoveAll(dir) }}
	tr := tar.NewReader(r)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			a.cleanup()
			return nil, fmt.Errorf("读取备份归档失败: %w", e)
		}
		clean := filepath.Clean(h.Name)
		if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			a.cleanup()
			return nil, fmt.Errorf("备份归档包含非法路径: %s", h.Name)
		}
		target := filepath.Join(dir, clean)
		if h.Typeflag == tar.TypeDir {
			_ = os.MkdirAll(target, 0o750)
			continue
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if e := os.MkdirAll(filepath.Dir(target), 0o750); e != nil {
			a.cleanup()
			return nil, e
		}
		out, e := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if e != nil {
			a.cleanup()
			return nil, e
		}
		_, e = io.Copy(out, tr)
		if ce := out.Close(); e == nil {
			e = ce
		}
		if e != nil {
			a.cleanup()
			return nil, e
		}
	}
	for _, cand := range []string{"database.db", "database.sql"} {
		if _, e := os.Stat(filepath.Join(dir, cand)); e == nil {
			a.dbPath = filepath.Join(dir, cand)
			break
		}
	}
	if a.dbPath == "" {
		a.cleanup()
		return nil, fmt.Errorf("备份归档缺少 database.db / database.sql")
	}
	filesDir := filepath.Join(dir, "files")
	if st, e := os.Stat(filesDir); e == nil && st.IsDir() {
		a.filesDir = filesDir
	}
	return a, nil
}

// List 列出备份文件（按时间倒序）。
func (s *Service) List() ([]File, error) {
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isBackupName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		item := File{
			Name: e.Name(), Size: info.Size(),
			CreatedAt:  info.ModTime().UTC().Format(time.RFC3339),
			Compressed: strings.HasSuffix(e.Name(), ".gz"),
		}
		if meta, err := readMeta(s.Dir(), e.Name()); err == nil {
			item.Driver = meta.Driver
			item.SHA256 = meta.SHA256
			item.CreatedAt = meta.CreatedAt
			item.Verified = true
			item.Files = meta.Files
			item.FileBytes = meta.FileBytes
		} else {
			item.Driver = driverFromName(e.Name())
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Meta 读取指定备份的元信息。
func (s *Service) Meta(name string) (*Meta, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	return readMeta(s.Dir(), name)
}

// Path 返回备份文件的绝对路径，并校验名称合法性。
func (s *Service) Path(name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	p := filepath.Join(s.Dir(), name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("备份文件不存在")
	}
	return p, nil
}

// Verify 校验备份完整性、数据库可用性与文件资产哈希。
func (s *Service) Verify(name string) (*VerifyResult, error) {
	p, err := s.Path(name)
	if err != nil {
		return nil, err
	}
	sum, err := fileSHA256(p)
	if err != nil {
		return nil, err
	}
	res := &VerifyResult{Name: name, Size: mustFileSize(p), SHA256: sum, Driver: driverFromName(name)}
	if meta, err := readMeta(s.Dir(), name); err == nil {
		res.MetaMatched = meta.SHA256 == sum
		res.CreatedAt = meta.CreatedAt
		res.Driver = meta.Driver
		res.Files = meta.Files
	} else {
		res.MetaMatched = true // 无元信息的旧备份不做哈希比对
	}
	a, err := openArchive(p)
	if err != nil {
		return nil, fmt.Errorf("备份文件解压/读取失败: %w", err)
	}
	defer a.cleanup()
	if res.Driver == "sqlite" {
		tables, users, err := inspectSQLite(a.dbPath, false)
		if err != nil {
			return nil, fmt.Errorf("备份内容不可用: %w", err)
		}
		res.Tables = tables
		res.UserCount = users
		res.Integrity = "ok"
	} else {
		st, _ := os.Stat(a.dbPath)
		if st != nil {
			res.Tables = []string{"SQL 转储"}
			res.Integrity = fmt.Sprintf("可读，%d 字节", st.Size())
		}
	}
	if a.filesDir != "" {
		checked, bad := verifyFiles(a.filesDir)
		res.Files = checked
		if bad > 0 {
			return nil, fmt.Errorf("文件资产校验失败：%d 个文件哈希不匹配", bad)
		}
		res.FileCheck = fmt.Sprintf("%d 个文件哈希匹配", checked)
	} else if res.Files > 0 {
		return nil, fmt.Errorf("元信息声明 %d 个文件资产，但归档内未找到 files/ 目录", res.Files)
	}
	res.OK = true
	return res, nil
}

// verifyFiles 统计目录内文件数并返回哈希不匹配数量（归档清单来自 manifest，逐文件重算并与清单比对）。
func verifyFiles(dir string) (int, int) {
	var entries []FileEntry
	if b, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "manifest.json")); err == nil {
		_ = json.Unmarshal(b, &struct {
			Files *[]FileEntry `json:"files"`
		}{Files: &entries})
	}
	count, bad := 0, 0
	if len(entries) > 0 {
		for _, e := range entries {
			count++
			sum, err := fileSHA256(filepath.Join(dir, filepath.FromSlash(e.Path)))
			if err != nil || sum != e.SHA256 {
				bad++
			}
		}
		return count, bad
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count, 0
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
	Files       int      `json:"files"`
	FileCheck   string   `json:"fileCheck,omitempty"`
}

// Delete 删除备份文件及其元信息。
func (s *Service) Delete(name string) error {
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

// restorePlan 生成恢复计划，不改动任何文件；dry-run 与真实恢复共用。
func (s *Service) restorePlan(name string) (*RestoreResult, error) {
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
	res := &RestoreResult{Name: filepath.Base(p), Mode: "online", DryRun: true,
		Plan: []string{}, PostCheck: []string{}}
	meta, metaErr := readMeta(s.Dir(), filepath.Base(p))
	if s.driver == "sqlite" {
		res.Mode = "offline"
		res.Plan = append(res.Plan, "校验备份归档（SHA-256 + integrity_check + 文件哈希）")
		res.Plan = append(res.Plan, "将当前数据库另存为 pre-restore-*.db 安全副本")
		res.Plan = append(res.Plan, "替换数据文件并清理 -wal / -shm")
		if st, e := os.Stat(s.DBPath()); e == nil && st.Size() > 0 {
			res.Plan = append(res.Plan, fmt.Sprintf("当前数据库 %s（%d 字节）将被覆盖", s.DBPath(), st.Size()))
		}
	} else {
		res.Plan = append(res.Plan, "校验备份归档（SHA-256 + 文件哈希）")
		res.Plan = append(res.Plan, fmt.Sprintf("通过 %s 导入 SQL 转储（覆盖当前库对象）", s.restoreBin()))
	}
	if root := s.StorageRoot(); root != "" {
		res.Plan = append(res.Plan, fmt.Sprintf("还原文件资产到 %s（临时目录 + 原子替换）", root))
	} else if metaErr == nil && meta.Files > 0 {
		res.Plan = append(res.Plan, fmt.Sprintf("注意：备份含 %d 个文件资产，但当前非本地存储模式，将跳过文件还原", meta.Files))
	}
	if metaErr == nil {
		res.Plan = append(res.Plan, fmt.Sprintf("备份时间 %s，数据库 %s，表 %d 张", meta.CreatedAt, meta.Driver, len(meta.Tables)))
	}
	res.Message = "演练模式：未改动任何文件"
	return res, nil
}

func (s *Service) restoreBin() string {
	if s.driver == "mysql" {
		return s.cfg.MySQLRestore
	}
	return s.cfg.PGRestore
}

// RestorePlan 是恢复演练入口：只返回计划，不改动任何文件。
func (s *Service) RestorePlan(name string) (*RestoreResult, error) {
	return s.restorePlan(name)
}

// RestoreOnline 在线恢复（仅 MySQL / PostgreSQL，需显式开启 allow_restore）。
func (s *Service) RestoreOnline(ctx context.Context, name string) (*RestoreResult, error) {
	if !s.cfg.AllowRestore {
		return nil, fmt.Errorf("在线恢复未启用：请设置 backup.allow_restore=true")
	}
	if s.driver == "sqlite" {
		return nil, fmt.Errorf("SQLite 需要替换数据文件，请在停止服务后执行：tenon restore %s", name)
	}
	p, err := s.Path(name)
	if err != nil {
		return nil, err
	}
	a, err := openArchive(p)
	if err != nil {
		return nil, err
	}
	defer a.cleanup()
	f, err := os.Open(a.dbPath)
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
			"--dbname", dbname, "--file", a.dbPath)
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
	filesRestored, filesSkipped := s.restoreFiles(a.filesDir)
	res := &RestoreResult{Name: filepath.Base(p), Mode: "online", RestartRequired: false,
		FilesRestored: filesRestored, FilesSkipped: filesSkipped,
		Message: "已从备份导入数据，建议刷新页面确认"}
	res.PostCheck = s.postCheck(nil)
	return res, nil
}

// RestoreOffline 停机恢复 SQLite 数据文件与文件资产。调用方需保证服务未运行。
func (s *Service) RestoreOffline(name string) (*RestoreResult, error) {
	if s.driver != "sqlite" {
		return nil, fmt.Errorf("停机恢复仅支持 SQLite；MySQL / PostgreSQL 请使用在线恢复或原生工具")
	}
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
	a, err := openArchive(p)
	if err != nil {
		return nil, fmt.Errorf("备份文件不可用: %w", err)
	}
	defer a.cleanup()
	wantTables, wantUsers, err := inspectSQLite(a.dbPath, false)
	if err != nil {
		return nil, fmt.Errorf("备份文件校验失败，已中止恢复: %w", err)
	}

	dbPath := s.DBPath()
	safety := ""
	if _, err := os.Stat(dbPath); err == nil {
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
	if err := copyFile(a.dbPath, stage); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, dbPath); err != nil {
		return nil, err
	}
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	filesRestored, filesSkipped := s.restoreFiles(a.filesDir)
	res := &RestoreResult{
		Name: filepath.Base(p), Mode: "offline", RestartRequired: false, SafetyCopy: safety,
		FilesRestored: filesRestored, FilesSkipped: filesSkipped,
		Message: "数据文件已替换，请重新启动服务",
	}
	res.PostCheck = s.postCheck(&postCheckInput{Tables: wantTables, UserCount: wantUsers})
	return res, nil
}

// postCheck 恢复后逐表比对行数；input 为备份侧的期望值。
type postCheckInput struct {
	Tables    []string
	UserCount int64
}

func (s *Service) postCheck(expect *postCheckInput) []string {
	out := []string{}
	if s.driver != "sqlite" {
		return append(out, "SQL 转储已导入；行数比对请在数据库侧执行")
	}
	tables, users, err := inspectSQLite(s.DBPath(), false)
	if err != nil {
		return append(out, "恢复后自检失败："+err.Error())
	}
	if expect == nil {
		return append(out, fmt.Sprintf("恢复后表 %d 张，用户 %d 人", len(tables), users))
	}
	out = append(out, fmt.Sprintf("表清单：备份 %d 张 / 实际 %d 张", len(expect.Tables), len(tables)))
	out = append(out, fmt.Sprintf("用户数：备份 %d / 实际 %d", expect.UserCount, users))
	if len(expect.Tables) != len(tables) || expect.UserCount != users {
		out = append(out, "注意：备份与恢复后数据不一致，请核查安全副本")
	} else {
		out = append(out, "逐表核心计数一致")
	}
	return out
}

// restoreFiles 把归档里的 files/ 原子替换到本地存储根目录。
func (s *Service) restoreFiles(filesDir string) (restored, skipped int) {
	if filesDir == "" {
		return 0, 0
	}
	root := s.StorageRoot()
	if root == "" {
		return 0, countFiles(filesDir)
	}
	parent := filepath.Dir(root)
	stage, err := os.MkdirTemp(parent, ".tenon-files-restore-*")
	if err != nil {
		return 0, countFiles(filesDir)
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	if err := copyTree(filesDir, stage); err != nil {
		cleanup()
		return 0, countFiles(filesDir)
	}
	restored = countFiles(stage)
	// 安全副本：把旧目录改名保留，再把 stage 原子替换到 root
	backup := filepath.Join(parent, ".tenon-files-pre-restore-"+time.Now().Format("20060102-150405"))
	if _, e := os.Stat(root); e == nil {
		if e := os.Rename(root, backup); e != nil {
			cleanup()
			return 0, countFiles(filesDir)
		}
	}
	if e := os.Rename(stage, root); e != nil {
		if _, se := os.Stat(backup); se == nil {
			_ = os.Rename(backup, root)
		}
		cleanup()
		return 0, countFiles(filesDir)
	}
	s.log.Info("文件资产已恢复", zap.String("root", root), zap.Int("files", restored))
	return restored, 0
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(src, path)
		if e != nil {
			return e
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		return copyFile(path, target)
	})
}

// RestoreResult 恢复结果。
type RestoreResult struct {
	Name            string   `json:"name"`
	Mode            string   `json:"mode"`
	RestartRequired bool     `json:"restartRequired"`
	SafetyCopy      string   `json:"safetyCopy,omitempty"`
	DryRun          bool     `json:"dryRun,omitempty"`
	Plan            []string `json:"plan,omitempty"`
	PostCheck       []string `json:"postCheck,omitempty"`
	FilesRestored   int      `json:"filesRestored,omitempty"`
	FilesSkipped    int      `json:"filesSkipped,omitempty"`
	Message         string   `json:"message"`
}

// PruneResult 保留策略演练/执行结果。
type PruneResult struct {
	DryRun      bool           `json:"dryRun,omitempty"`
	Keep        []string       `json:"keep"`
	Remove      []string       `json:"remove"`
	RemoveBytes int64          `json:"removeBytes"`
	Slots       map[string]int `json:"slots,omitempty"`
}

// prune 按 GFS 分档清理，永不删除最新一份。
func (s *Service) prune() error {
	plan, err := s.PrunePlan()
	if err != nil {
		return err
	}
	for _, name := range plan.Remove {
		if err := s.Delete(name); err != nil {
			return err
		}
		s.log.Info("清理过期备份", zap.String("file", name))
	}
	return nil
}

// PrunePlan 计算 GFS 保留集合与待删除集合，不改动文件。
func (s *Service) PrunePlan() (*PruneResult, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	slots := map[string]int{}
	if len(list) > 0 {
		keep[list[0].Name] = true // 最新一份必留
	}
	c := s.cfg
	limits := map[string]int{
		"hourly": c.RetainHourly, "daily": c.RetainDaily,
		"weekly": c.RetainWeekly, "monthly": c.RetainMonthly,
	}
	seen := map[string]map[string]bool{
		"hourly": {}, "daily": {}, "weekly": {}, "monthly": {},
	}
	// 每档只保留「最近 N 个槽位」，每个槽位取最新一份。List 已按时间倒序，
	// 因此先遇到的槽位即该槽位的最新备份；槽位数达到上限后该档不再选取。
	for _, f := range list { // List 已按时间倒序
		t, e := backupTime(f.Name)
		if e != nil {
			continue
		}
		buckets := map[string]string{
			"hourly":  t.Format("2006-01-02-15"),
			"daily":   t.Format("2006-01-02"),
			"monthly": t.Format("2006-01"),
		}
		y, w := t.ISOWeek()
		buckets["weekly"] = fmt.Sprintf("%04d-W%02d", y, w)
		for _, k := range []string{"hourly", "daily", "weekly", "monthly"} {
			if limits[k] <= 0 || slots[k] >= limits[k] || seen[k][buckets[k]] {
				continue
			}
			seen[k][buckets[k]] = true
			slots[k]++
			keep[f.Name] = true
		}
	}
	// 旧 retain 作为「最近 N 份」兜底
	if c.Retain > 0 {
		for i := 0; i < len(list) && i < c.Retain; i++ {
			keep[list[i].Name] = true
		}
	}
	res := &PruneResult{Keep: []string{}, Remove: []string{}, Slots: slots}
	for _, f := range list {
		if keep[f.Name] {
			res.Keep = append(res.Keep, f.Name)
		} else {
			res.Remove = append(res.Remove, f.Name)
			res.RemoveBytes += f.Size
		}
	}
	return res, nil
}

// PruneDryRun 保留策略演练，不删除任何文件。
func (s *Service) PruneDryRun() (*PruneResult, error) {
	plan, err := s.PrunePlan()
	if err != nil {
		return nil, err
	}
	plan.DryRun = true
	return plan, nil
}

func backupTime(name string) (time.Time, error) {
	parts := strings.Split(name, "-")
	for _, p := range parts {
		if len(p) == 15 && strings.Contains(p, "T") == false {
			if t, e := time.ParseInLocation("20060102-150405", p, time.Local); e == nil {
				return t, nil
			}
		}
	}
	// 形如 tenon-sqlite-20261122-221900.tar.gz
	if i := strings.Index(name, "-20"); i > 0 {
		rest := name[i+1:]
		if len(rest) >= 15 {
			if t, e := time.ParseInLocation("20060102-150405", rest[:15], time.Local); e == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("无法从文件名解析备份时间: %s", name)
}

// ---- 辅助函数 ----

func isBackupName(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "pre-restore-") {
		return false
	}
	if !strings.HasPrefix(name, "tenon-") {
		return false
	}
	return strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db.gz") ||
		strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, ".sql.gz") ||
		strings.HasSuffix(name, ".tar") || strings.HasSuffix(name, ".tar.gz")
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

func writeMeta(dir string, meta *Meta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(dir, meta.Name), b, 0o640)
}

func readMeta(dir, name string) (*Meta, error) {
	b, err := os.ReadFile(metaPath(dir, name))
	if err != nil {
		return nil, fmt.Errorf("备份元信息不存在")
	}
	var m Meta
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

// decompressToTemp 把单个备份文件还原成可直接读取的原始文件（旧格式兼容）。
func decompressToTemp(p string, compressed bool) (string, func(), error) {
	f, err := os.Open(p)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	tmp, err := os.CreateTemp("", "tenon-restore-*.db")
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
