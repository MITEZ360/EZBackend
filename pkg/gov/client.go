package gov

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ClientConfig 是数据面客户端的配置。
type ClientConfig struct {
	// BaseURL 控制面地址，如 http://127.0.0.1:9000。
	BaseURL string
	// Token 内部接口令牌；为空表示控制面未开启鉴权（仅限本机调试）。
	Token string
	// InstanceID 本实例标识，随请求上报，便于控制面统计收敛情况。
	InstanceID string
	// Service 本实例所属服务名，用于按服务分片订阅（M3）。
	Service string
	// PollInterval 版本轮询间隔；SSE 可用时仍保留作为兜底。
	PollInterval time.Duration
	// SnapshotFile 本地快照路径。控制面不可用时靠它启动。
	SnapshotFile string
	// OnApply 在每次成功应用快照后调用，网关用它重建路由表。
	OnApply func(*Snapshot)
	// Announce 非空时把本进程登记为服务实例并周期续约，
	// 让网关也出现在服务目录里，并让"配置版本"列反映真实的收敛进度。
	Announce *AnnounceConfig
	Logger   *zap.Logger
}

// AnnounceConfig 是数据面自注册所需的信息。
type AnnounceConfig struct {
	Service    string
	InstanceID string
	Host       string
	Port       int
	Version    string
	Zone       string
	Labels     map[string]string
	// Interval 心跳间隔；默认 30s。
	Interval time.Duration
}

// Client 是数据面的配置客户端。
//
// 它只做三件事：拉快照、校验、原子替换；再加两条订阅（SSE + 轮询）保证收敛。
// 所有入口最终都走 Apply，幂等且乱序安全。
type Client struct {
	cfg   ClientConfig
	store Store
	log   *zap.Logger
	http  *http.Client

	mu      sync.Mutex // 串行化拉取，避免 SSE 与轮询同时打控制面
	applied int64      // 已应用的 revision，仅用于日志与心跳回报，不再作为去重依据
	fp      string     // 已应用快照的内容指纹，是"变了没有"的唯一判据

	annMu    sync.Mutex // 串行化版本回报，避免逐条快照都发一次心跳
	reported int64      // 已回报给控制面的 revision
}

// NewClient 创建客户端。
func NewClient(cfg ClientConfig) *Client {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}
	return &Client{
		cfg:  cfg,
		log:  log,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Snapshot 返回当前快照，供网关在请求路径上读取。
func (c *Client) Snapshot() *Snapshot { return c.store.Load() }

// Start 按"本地快照 → 控制面 → 轮询/SSE"的顺序启动。
//
// 第一步失败不阻断：控制面不可用时，本地快照仍能让网关提供服务。
func (c *Client) Start(ctx context.Context) {
	if c.cfg.SnapshotFile != "" {
		if err := c.loadLocal(); err != nil {
			c.log.Debug("本地治理快照不可用", zap.Error(err))
		}
	}
	if err := c.Sync(); err != nil {
		c.log.Warn("治理快照首次拉取失败，沿用本地快照", zap.Error(err))
	}
	go c.pollLoop(ctx)
	go c.watchLoop(ctx)
	if c.cfg.Announce != nil {
		go c.announceLoop(ctx)
	}
}

// Sync 拉一次快照，内容变了才应用。
//
// 判据是内容指纹而不是版本号：带上已应用的指纹，控制面算出来一样就回 304。
// 版本号只在发布时递增，表达不了"实例超时下线"这类随时间发生的变化，
// 也表达不了"版本号变了但内容一模一样"，所以它只能用于观测，不能用于去重。
func (c *Client) Sync() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, err := c.fetch(c.fp)
	if err != nil {
		return err
	}
	if snap == nil {
		return nil // 304，内容没变
	}
	// 服务端没算指纹（或以 -1 状态绕过了 304）时再比一次，避免无谓重建路由表。
	if fp := snap.Fingerprint(); fp == c.fp {
		return nil
	}
	c.applyLocked(snap)
	return nil
}

// Reload 强行拉一份全量并应用，供手动下发使用：不带指纹，控制面不会回 304。
func (c *Client) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	snap, err := c.fetch("")
	if err != nil {
		return err
	}
	if snap == nil {
		return nil
	}
	c.applyLocked(snap)
	return nil
}

func (c *Client) applyLocked(snap *Snapshot) {
	c.store.Store(snap)
	c.applied = snap.GlobalRev
	c.fp = snap.Fingerprint()
	if err := c.saveLocal(snap); err != nil {
		c.log.Debug("写入本地治理快照失败", zap.Error(err))
	}
	if c.cfg.OnApply != nil {
		c.cfg.OnApply(snap)
	}
	c.log.Info("治理快照已更新",
		zap.Int64("globalRev", snap.GlobalRev), zap.Int("parts", len(snap.Parts)), zap.String("fp", shortFP(c.fp)))
	// 快照换完立刻回报一次，控制面据此显示"哪些实例还停在旧版本"。
	// 放在锁内只做取值，网络请求仍在独立 goroutine 里，不拖慢热更新路径。
	c.reportApplied(snap.GlobalRev)
}

// announcePrefix 是实例注册/心跳所在的 API 前缀。
//
// 这两个接口挂在业务 API 下而不是 /internal/gov：注册是给任意业务进程调的，
// 与只给数据面用的快照接口分开，避免把 /internal 变成半公开的东西。
const announcePrefix = "/api/v1/gov/instances"

// announceLoop 先登记再按间隔续约。控制面不可用时按轮询同样节奏重试，
// 不额外引入退避状态机：心跳本身就是廉价的幂等写。
func (c *Client) announceLoop(ctx context.Context) {
	ann := c.cfg.Announce
	interval := ann.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if err := c.register(); err != nil {
		c.log.Warn("实例自注册失败，稍后重试", zap.Error(err))
	} else {
		c.log.Info("已登记为服务实例",
			zap.String("service", ann.Service), zap.String("instance", c.announceInstanceID()))
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			rev := c.applied
			c.mu.Unlock()
			if err := c.post(announcePrefix+"/heartbeat", c.announceBody(rev)); err != nil {
				c.log.Debug("实例心跳失败", zap.Error(err))
			}
		}
	}
}

func (c *Client) register() error {
	c.mu.Lock()
	rev := c.applied
	c.mu.Unlock()
	return c.post(announcePrefix+"/register", c.announceBody(rev))
}

func (c *Client) announceInstanceID() string {
	if id := strings.TrimSpace(c.cfg.Announce.InstanceID); id != "" {
		return id
	}
	return c.cfg.InstanceID
}

func (c *Client) announceBody(rev int64) map[string]any {
	ann := c.cfg.Announce
	// 必须复制一份再补 role：ann.Labels 是配置对象，注册、心跳、版本回报三个
	// goroutine 会并发走到这里，直接往里写既是数据竞争，也把调用方的配置改脏了。
	labels := make(map[string]string, len(ann.Labels)+1)
	for k, v := range ann.Labels {
		labels[k] = v
	}
	labels["role"] = "data-plane"
	return map[string]any{
		"service":       ann.Service,
		"instanceId":    c.announceInstanceID(),
		"host":          ann.Host,
		"port":          ann.Port,
		"version":       ann.Version,
		"zone":          ann.Zone,
		"weight":        100,
		"labels":        labels,
		"configVersion": rev,
	}
}

// reportApplied 异步把已应用版本推给控制面；不阻塞、不重试、失败只记日志。
//
// 版本收敛最终由下次心跳兜住，这里只是把观测延迟从"一个心跳周期"压到"秒级"。
func (c *Client) reportApplied(rev int64) {
	if c.cfg.Announce == nil || rev <= 0 {
		return
	}
	c.annMu.Lock()
	if rev <= c.reported {
		c.annMu.Unlock()
		return
	}
	c.reported = rev
	c.annMu.Unlock()

	go func() {
		if err := c.post(announcePrefix+"/heartbeat", c.announceBody(rev)); err != nil {
			c.log.Debug("回报已应用版本失败", zap.Error(err))
		}
	}()
}

func (c *Client) post(path string, body any) error {
	base := strings.TrimSpace(c.cfg.BaseURL)
	if base == "" {
		return errors.New("未配置控制面地址")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.sign(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("%s 返回 %d: %s", path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	// 只看 HTTP 状态不够：前端兜底路由对未知路径返回 200 和一段 HTML，
	// 不校验响应体就会把"路径写错"当成"注册成功"。
	var env struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &env); err != nil || env.Code == nil {
		return fmt.Errorf("%s 返回了非接口响应: %s", path, strings.TrimSpace(string(payload[:min(len(payload), 120)])))
	}
	if *env.Code != 0 {
		return fmt.Errorf("%s 失败(%d): %s", path, *env.Code, env.Message)
	}
	return nil
}

// fetch 拉取快照。knownFP 非空时带上去重指纹，内容一致控制面回 304（返回 nil, nil）。
func (c *Client) fetch(knownFP string) (*Snapshot, error) {
	if strings.TrimSpace(c.cfg.BaseURL) == "" {
		return nil, errors.New("未配置控制面地址")
	}
	u, err := url.Parse(strings.TrimRight(c.cfg.BaseURL, "/") + "/internal/gov/snapshot")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	// 灰度按实例区分：同一版本在名单内外拿到的载荷不同，指纹也随之不同，
	// 所以 fingerprint 必须落在"这台实例看到的视图"上，X-Instance-Id 由 sign 之外的
	// 请求头带上，见下方 Header 设置。
	if knownFP != "" {
		q.Set("fp", knownFP)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	c.sign(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("控制面返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var snap Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("解析治理快照失败: %w", err)
	}
	if err := verify(&snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// verify 校验每个分区的 checksum；不匹配说明传输损坏或不完整，必须拒绝。
func verify(snap *Snapshot) error {
	for key, p := range snap.Parts {
		if p.Checksum == "" {
			continue
		}
		if got := Checksum(p.Payload); got != p.Checksum {
			return fmt.Errorf("治理快照分区 %s 校验失败", key)
		}
	}
	return nil
}

func (c *Client) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Sync(); err != nil {
				c.log.Debug("治理快照轮询失败", zap.Error(err))
			}
		}
	}
}

// watchLoop 维护 SSE 长连接：只收变更信号，收到后主动拉取。
// 断线按 1s→30s 指数退避，轮询始终在跑，所以退避期间不会失联。
func (c *Client) watchLoop(ctx context.Context) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err := c.watchOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.log.Debug("治理变更订阅断开", zap.Error(err), zap.Duration("retryIn", backoff))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (c *Client) watchOnce(ctx context.Context) error {
	u := strings.TrimRight(c.cfg.BaseURL, "/") + "/internal/gov/watch"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.sign(req)

	// SSE 连接不能设整体超时，用无超时的 client；断开由 ctx 与读失败驱动。
	cli := &http.Client{}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("订阅返回 %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev struct {
			GlobalRev int64 `json:"globalRev"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev); err != nil {
			continue
		}
		if err := c.Sync(); err != nil {
			c.log.Debug("收到变更信号但拉取失败", zap.Int64("rev", ev.GlobalRev), zap.Error(err))
		}
	}
	return sc.Err()
}

func (c *Client) sign(req *http.Request) {
	if c.cfg.Token != "" {
		req.Header.Set("X-Internal-Token", c.cfg.Token)
	}
	if c.cfg.InstanceID != "" {
		req.Header.Set("X-Instance-Id", c.cfg.InstanceID)
	}
	if c.cfg.Service != "" {
		req.Header.Set("X-Caller-Service", c.cfg.Service)
	}
}

func (c *Client) loadLocal() error {
	b, err := os.ReadFile(c.cfg.SnapshotFile)
	if err != nil {
		return err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return err
	}
	if err := verify(&snap); err != nil {
		return err
	}
	c.store.Store(&snap)
	c.applied = snap.GlobalRev
	c.fp = snap.Fingerprint()
	if c.cfg.OnApply != nil {
		c.cfg.OnApply(&snap)
	}
	return nil
}

// saveLocal 原子写入：先写临时文件再 rename，避免进程崩溃留下半个快照。
func (c *Client) saveLocal(snap *Snapshot) error {
	if c.cfg.SnapshotFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.cfg.SnapshotFile), 0o750); err != nil {
		return err
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	tmp := c.cfg.SnapshotFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.cfg.SnapshotFile)
}

// shortFP 截断指纹用于日志，完整值写在请求里。
func shortFP(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}
