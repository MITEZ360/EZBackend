// Package govserver 是治理面的控制面服务端：把库里的服务、实例、路由、配置
// 编译成数据面能直接执行的快照，并通过 HTTP + SSE 下发。
//
// 它不承担任何业务逻辑，也不做鉴权之外的校验；写操作与校验在 registry /
// route / configcenter 模块里完成。这样控制面可以单独部署，也可以随主程序
// 一起装配。
package govserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/gov"
	"github.com/MITEZ360/tenon/pkg/response"
	"go.uber.org/zap"
)

// Server 是控制面快照服务。
type Server struct {
	db           *gorm.DB
	token        string
	log          *zap.Logger
	offlineAfter time.Duration

	// cache 是构建结果的一秒缓存。gen 是"缓存代际"：每次 Bump 递增，
	// 慢构建只在代际未变时才能回填，否则会把发布前的旧快照写回缓存。
	cacheMu sync.Mutex
	cache   map[string]cachedSnapshot
	gen     uint64

	subMu sync.Mutex
	subs  map[chan int64]struct{}
}

type cachedSnapshot struct {
	at   time.Time
	gen  uint64
	snap *gov.Snapshot
}

// generation 返回当前缓存代际。Build 在开始拼装前取一次，写回时比对。
func (s *Server) generation() uint64 {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.gen
}

// cachePut 在代际未变化时写入构建结果。
//
// 必须比对代际：一次发布（Bump）清空缓存后，可能仍有一个更早开始的 Build
// 在拼装。它读到的是旧数据，若直接写缓存，接下来整个 TTL 内数据面拿到的
// 都是发布前的视图——表现就是"发布了但网关没反应"，而且只持续一秒，
// 几乎无法复现。
func (s *Server) cachePut(instanceID string, gen uint64, snap *gov.Snapshot) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.gen != gen {
		return
	}
	s.cache[instanceID] = cachedSnapshot{at: time.Now(), gen: gen, snap: snap}
}

// New 创建控制面服务。
func New(db *gorm.DB, token string, log *zap.Logger) *Server {
	if log == nil {
		log = zap.NewNop()
	}
	return &Server{
		db:           db,
		token:        token,
		log:          log,
		offlineAfter: 90 * time.Second,
		cache:        map[string]cachedSnapshot{},
		subs:         map[chan int64]struct{}{},
	}
}

// Register 把内部接口挂到 /internal/gov 下。调用方负责决定挂在哪个引擎。
func (s *Server) Register(g *gin.RouterGroup) {
	g.Use(s.auth)
	g.GET("/snapshot", s.Snapshot)
	g.GET("/watch", s.Watch)
	g.POST("/ack", s.Ack)
}

func (s *Server) auth(c *gin.Context) {
	if s.token == "" {
		return
	}
	got := c.GetHeader("X-Internal-Token")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1 {
		return
	}
	c.Abort()
	response.Fail(c, errs.Unauthorized("内部令牌无效"))
}

// Snapshot 返回快照；内容与请求方手上的一致时回 304。
//
// 判据是内容指纹（?fp=）而不是全局版本号。全局版本号只在发布时递增，
// 而 registry 分区的内容还会随时间变化（实例超时下线不需要任何写操作），
// 拿版本号做判据会让数据面永远停在旧的实例表上。globalRev 仍然返回，
// 只用于观测与“哪些实例落后”的统计，不再参与 304 决策。
//
// 客户端会带上 X-Instance-Id，用于灰度发布时判断这台实例该拿哪一版。
func (s *Server) Snapshot(c *gin.Context) {
	inst := c.GetHeader("X-Instance-Id")
	snap, err := s.Build(c.Request.Context(), inst)
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("构建治理快照失败").WithCause(err))
		return
	}
	if fp := c.Query("fp"); fp != "" && fp == snap.Fingerprint() {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, snap)
}

// Watch 是 SSE 订阅：只在版本变化时推一个信号，不推载荷。
func (s *Server) Watch(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		response.Fail(c, errs.Internal("当前响应不支持流式输出"))
		return
	}
	ch := make(chan int64, 8)
	s.subMu.Lock()
	s.subs[ch] = struct{}{}
	s.subMu.Unlock()
	defer func() {
		s.subMu.Lock()
		delete(s.subs, ch)
		s.subMu.Unlock()
	}()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)
	fmt.Fprintf(c.Writer, "event: ready\ndata: {\"globalRev\":%d}\n\n", s.rev(c.Request.Context()))
	flusher.Flush()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case rev := <-ch:
			fmt.Fprintf(c.Writer, "event: revised\ndata: {\"globalRev\":%d}\n\n", rev)
			flusher.Flush()
		}
	}
}

// Ack 记录实例已应用到哪个版本，用于收敛观测与灰度门禁。
func (s *Server) Ack(c *gin.Context) {
	var body struct {
		InstanceID string `json:"instanceId"`
		GlobalRev  int64  `json:"globalRev"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, errs.BindError(err))
		return
	}
	if body.InstanceID == "" {
		body.InstanceID = c.GetHeader("X-Instance-Id")
	}
	if body.InstanceID == "" {
		response.Fail(c, errs.Validation("缺少实例标识"))
		return
	}
	err := s.db.WithContext(c.Request.Context()).Model(&model.GovInstance{}).
		Where("instance_id = ?", body.InstanceID).
		Update("config_version", body.GlobalRev).Error
	if err != nil {
		c.Error(err)
		response.Fail(c, errs.Internal("记录版本失败").WithCause(err))
		return
	}
	response.OK(c, gin.H{"instanceId": body.InstanceID, "globalRev": body.GlobalRev})
}

// Bump 在任意治理数据变更后调用：清缓存并广播新版本。
func (s *Server) Bump() {
	s.cacheMu.Lock()
	s.gen++
	s.cache = map[string]cachedSnapshot{}
	s.cacheMu.Unlock()

	rev := s.rev(context.Background())
	s.subMu.Lock()
	for ch := range s.subs {
		select {
		case ch <- rev:
		default:
		}
	}
	s.subMu.Unlock()
}

// Build 组装快照。instanceID 为空表示不区分灰度目标。
func (s *Server) Build(ctx context.Context, instanceID string) (*gov.Snapshot, error) {
	s.cacheMu.Lock()
	gen := s.gen
	if c, ok := s.cache[instanceID]; ok && time.Since(c.at) < time.Second {
		s.cacheMu.Unlock()
		return c.snap, nil
	}
	s.cacheMu.Unlock()

	rev := s.rev(ctx)
	snap := &gov.Snapshot{GlobalRev: rev, Parts: map[string]gov.Part{}}

	// 路由分区只在真的发布过时下发。
	//
	// 从未发布过时不给 route 分区，数据面 ApplySnapshot 会拒绝并继续用自己的
	// 兜底（配置文件 / static endpoints）。否则新部署会出现"控制面还没配路由，
	// 网关已经拿到空表 → 全站 404"的顺序陷阱。反过来，一旦发布过（哪怕发布的
	// 是空规则集），路由就是权威，网关不再回落配置文件。
	if p, ok := s.releasePart(ctx, "route", instanceID); ok {
		snap.Parts[gov.PartRoute] = p
	}

	p, err := s.buildRegistry(ctx, rev)
	if err != nil {
		return nil, err
	}
	snap.Parts[gov.PartRegistry] = p

	namespaces, err := s.buildConfigs(ctx, rev, instanceID)
	if err != nil {
		return nil, err
	}
	for k, v := range namespaces {
		snap.Parts[k] = v
	}

	s.cachePut(instanceID, gen, snap)
	return snap, nil
}

// rev 返回当前全局版本号；从未发布过任何东西时是 0。
//
// 不要给它加"至少为 1"的下限：分配器从 0 起跳，下限会让"还没发布"和
// "第一版"都报 1，运维看到的是"发布完了版本号没动"，排查时极具误导性。
func (s *Server) rev(ctx context.Context) int64 {
	var row model.GovRevision
	if err := s.db.WithContext(ctx).Where("id = 1").First(&row).Error; err != nil {
		return 0
	}
	return row.GlobalRev
}

// CurrentRev 返回当前全局版本号。
//
// 控制面自己也在服务目录里，它永远处于最新版本；把自己也纳入版本口径，
// 页面上的"待同步实例"就不会把它误算成落后节点。
func (s *Server) CurrentRev(ctx context.Context) int64 { return s.rev(ctx) }

// releasePart 取某个域的发布载荷。
//
// 灰度语义：处于 active+canary 的发布只对名单内实例可见，名单外实例继续用
// 上一版；没有灰度时用最新一版 full。这样不需要第二套开关，回滚也只是把
// 旧 revision 重新发布。
func (s *Server) releasePart(ctx context.Context, domain, instanceID string) (gov.Part, bool) {
	db := s.db.WithContext(ctx)
	var canary model.GovRelease
	if err := db.Where("domain = ? AND status = ? AND stage = ?", domain, "active", "canary").
		Order("revision DESC").First(&canary).Error; err == nil && len(canary.Payload) > 0 {
		if matchTargets(canary.Targets, instanceID) {
			return partOf(canary), true
		}
		var prev model.GovRelease
		if err := db.Where("domain = ? AND revision < ? AND status IN ?", domain, canary.Revision,
			[]string{"active", "done"}).Order("revision DESC").First(&prev).Error; err == nil && len(prev.Payload) > 0 {
			return partOf(prev), true
		}
		return gov.Part{}, false
	}
	var full model.GovRelease
	if err := db.Where("domain = ? AND status IN ?", domain, []string{"active", "done"}).
		Order("revision DESC").First(&full).Error; err == nil && len(full.Payload) > 0 {
		return partOf(full), true
	}
	return gov.Part{}, false
}

func partOf(rel model.GovRelease) gov.Part {
	return gov.Part{Rev: rel.Revision, Payload: rel.Payload, Checksum: rel.Checksum}
}

// matchTargets 判断实例是否命中灰度名单；空名单表示全量。
func matchTargets(raw, instanceID string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return true
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return true // 解析失败按全量处理，宁可多推不可漏推
	}
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if v == "*" || v == "all" || v == instanceID {
			return true
		}
	}
	return false
}

func (s *Server) buildRegistry(ctx context.Context, rev int64) (gov.Part, error) {
	db := s.db.WithContext(ctx)
	var services []model.GovService
	if err := db.Where("status = ?", "enabled").Find(&services).Error; err != nil {
		return gov.Part{}, err
	}
	var instances []model.GovInstance
	cutoff := time.Now().Add(-s.offlineAfter)
	if err := db.Where("status = ? AND last_seen >= ?", "online", cutoff).Find(&instances).Error; err != nil {
		return gov.Part{}, err
	}
	byService := map[string][]gov.Instance{}
	for _, ins := range instances {
		byService[ins.ServiceID] = append(byService[ins.ServiceID], gov.Instance{
			ID: ins.InstanceID, Addr: net.JoinHostPort(ins.Host, strconv.Itoa(ins.Port)),
			Version: ins.Version, Zone: ins.Zone, Weight: ins.Weight, Labels: parseMap(ins.Labels),
		})
	}
	out := map[string]gov.Service{}
	for _, svc := range services {
		list := byService[svc.Name]
		if list == nil {
			list = []gov.Instance{} // 序列化成 []，不要让前端处理 null
		}
		out[svc.Name] = gov.Service{Name: svc.Name, Instances: list}
	}
	return marshalPart(gov.RegistryPayload{Rev: rev, Services: out})
}

func (s *Server) buildConfigs(ctx context.Context, rev int64, instanceID string) (map[string]gov.Part, error) {
	db := s.db.WithContext(ctx)
	var namespaces []model.GovConfigNamespace
	if err := db.Find(&namespaces).Error; err != nil {
		return nil, err
	}
	out := map[string]gov.Part{}
	for _, ns := range namespaces {
		key := gov.PartConfigPrefix + ns.Namespace
		// 已发布过就走发布快照；从未发布过则实时下发，避免"配了却不生效"。
		if p, ok := s.releasePart(ctx, key, instanceID); ok {
			if dp, err := s.decryptConfigPart(p); err == nil {
				out[key] = dp
				continue
			}
		}
		var items []model.GovConfigItem
		if err := db.Where("namespace = ?", ns.Namespace).Find(&items).Error; err != nil {
			return nil, err
		}
		m := make(map[string]gov.ConfigItem, len(items))
		for _, it := range items {
			val := it.Value
			if it.Encrypted {
				plain, err := gov.Decrypt(it.Value, gov.MasterKey())
				if err != nil {
					s.log.Warn("配置项解密失败，跳过下发",
						zap.String("namespace", ns.Namespace), zap.String("key", it.Key), zap.Error(err))
					continue
				}
				val = plain
			}
			m[it.Key] = gov.ConfigItem{Value: val, Type: it.Type, Encrypted: it.Encrypted}
		}
		p, err := marshalPart(gov.ConfigPayload{Rev: rev, Namespace: ns.Namespace, Items: m})
		if err != nil {
			return nil, err
		}
		out[key] = p
	}
	return out, nil
}

// decryptConfigPart 把发布快照里的密文项解密后重新封装。
//
// 必须重算校验和：客户端会校验 payload，解密后字节已经变了。
func (s *Server) decryptConfigPart(p gov.Part) (gov.Part, error) {
	var payload gov.ConfigPayload
	if err := json.Unmarshal(p.Payload, &payload); err != nil {
		return p, err
	}
	changed := false
	for k, it := range payload.Items {
		if !it.Encrypted || !strings.HasPrefix(it.Value, gov.EncPrefix) {
			continue
		}
		plain, err := gov.Decrypt(it.Value, gov.MasterKey())
		if err != nil {
			s.log.Warn("发布快照解密失败，跳过该项", zap.String("key", k), zap.Error(err))
			continue
		}
		it.Value = plain
		payload.Items[k] = it
		changed = true
	}
	if !changed {
		return p, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return p, err
	}
	return gov.Part{Rev: p.Rev, Payload: b, Checksum: gov.Checksum(b)}, nil
}

func marshalPart(v any) (gov.Part, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return gov.Part{}, err
	}
	var meta struct {
		Rev int64 `json:"rev"`
	}
	_ = json.Unmarshal(b, &meta)
	return gov.Part{Rev: meta.Rev, Payload: b, Checksum: gov.Checksum(b)}, nil
}

func splitMethods(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}

func parseMap(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}
