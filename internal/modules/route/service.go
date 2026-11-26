package route

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// Service 是动态路由的领域实现。
type Service struct {
	db  *gorm.DB
	log *zap.Logger
	gov *govserver.Server
}

// NewService 创建路由服务。
func NewService(db *gorm.DB, log *zap.Logger, gs *govserver.Server) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{db: db, log: log, gov: gs}
}

// RouteInput 是创建/更新规则的入参。
type RouteInput struct {
	Name      string            `json:"name"`
	Priority  int               `json:"priority"`
	Prefix    string            `json:"prefix"`
	Methods   []string          `json:"methods"`
	Headers   map[string]string `json:"headers"`
	Query     map[string]string `json:"query"`
	Service   string            `json:"service"`
	Version   string            `json:"version"`
	Weight    int               `json:"weight"`
	Strip     bool              `json:"strip"`
	TimeoutMs int               `json:"timeoutMs"`
	Retries   int               `json:"retries"`
	Status    string            `json:"status"`
}

// List 返回全部规则，按优先级与创建时间排序。
func (s *Service) List(ctx context.Context) ([]model.GovRoute, error) {
	var rows []model.GovRoute
	err := s.db.WithContext(ctx).Order("priority DESC, created_at ASC").Find(&rows).Error
	return rows, err
}

// Create 新建一条规则（默认草稿，发布后生效）。
func (s *Service) Create(ctx context.Context, in RouteInput) (*model.GovRoute, error) {
	// published 是发布动作的产物，不允许调用方直接写入：否则页面显示"已发布"，
	// 发布记录里却没有这条规则，网关也永远不会转发它。
	if strings.TrimSpace(in.Status) == "published" {
		return nil, errs.Validation("状态 published 只能由发布操作产生")
	}
	row, err := s.build(in)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	s.bump()
	return row, nil
}

// Update 更新规则。
func (s *Service) Update(ctx context.Context, id string, in RouteInput) (*model.GovRoute, error) {
	if strings.TrimSpace(in.Status) == "published" {
		return nil, errs.Validation("状态 published 只能由发布操作产生")
	}
	var existing model.GovRoute
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.NotFound("规则不存在")
		}
		return nil, err
	}
	next, err := s.build(in)
	if err != nil {
		return nil, err
	}
	next.ID = existing.ID
	// 内容被改动后回到草稿：published 的含义是"已包含在上一次发布里"，
	// 改完不重新发布却仍标记为已发布，页面就无法提示"有未生效的改动"。
	if existing.Status == "published" && next.Status == "published" {
		next.Status = "draft"
	}
	err = s.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
		"name": next.Name, "priority": next.Priority, "prefix": next.Prefix,
		"methods": next.Methods, "headers": next.Headers, "query": next.Query,
		"service": next.Service, "version": next.Version, "weight": next.Weight,
		"strip": next.Strip, "timeout_ms": next.TimeoutMs, "retries": next.Retries,
		"status": next.Status,
	}).Error
	if err != nil {
		return nil, err
	}
	s.db.WithContext(ctx).Where("id = ?", id).First(&existing)
	s.bump()
	return &existing, nil
}

// Delete 删除规则。已经发布的规则删除后需要重新发布才生效。
//
// 物理删除：规则没有唯一键，留软删墓碑只会让"同名重建"这类运维动作
// 在别处踩坑，历史发布记录里存的是快照，不依赖这张表。
func (s *Service) Delete(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Unscoped().Delete(&model.GovRoute{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("规则不存在")
	}
	s.bump()
	return nil
}

// Compile 只做校验与编译预览，不落发布记录。
func (s *Service) Compile(ctx context.Context, in []RouteInput) ([]gov.Route, error) {
	rows := make([]model.GovRoute, 0, len(in))
	for i, it := range in {
		row, err := s.build(it)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条规则: %w", i+1, err)
		}
		rows = append(rows, *row)
	}
	return s.compileRows(rows)
}

// build 把入参转成模型并做字段级校验。
func (s *Service) build(in RouteInput) (*model.GovRoute, error) {
	prefix, err := normalizePrefix(in.Prefix)
	if err != nil {
		return nil, err
	}
	service := strings.TrimSpace(in.Service)
	if service == "" {
		return nil, errs.Validation("目标服务不能为空")
	}
	var count int64
	if err := s.db.Model(&model.GovService{}).Where("name = ?", service).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errs.Validation("目标服务不存在: " + service)
	}

	methods, err := normalizeMethods(in.Methods)
	if err != nil {
		return nil, err
	}
	headers, err := normalizeConditions("Header", in.Headers)
	if err != nil {
		return nil, err
	}
	query, err := normalizeConditions("Query", in.Query)
	if err != nil {
		return nil, err
	}
	if in.TimeoutMs < 0 || in.TimeoutMs > 60000 {
		return nil, errs.Validation("超时必须在 0~60000ms 之间")
	}
	if in.Retries < 0 || in.Retries > 3 {
		return nil, errs.Validation("重试次数必须在 0~3 之间")
	}
	if in.Weight < 0 || in.Weight > 10000 {
		return nil, errs.Validation("权重必须在 0~10000 之间")
	}
	priority := in.Priority
	if priority == 0 {
		priority = 100
	}
	if priority < -10000 || priority > 10000 {
		return nil, errs.Validation("优先级超出范围")
	}
	status := in.Status
	if status == "" {
		status = "draft"
	}
	switch status {
	case "draft", "published", "archived":
	default:
		return nil, errs.Validation("状态只能是 draft / published / archived")
	}
	return &model.GovRoute{
		Name: orDefault(in.Name, prefix), Priority: priority, Prefix: prefix,
		Methods: methods, Headers: headers, Query: query,
		Service: service, Version: in.Version, Weight: orDefaultInt(in.Weight, 100),
		Strip: in.Strip, TimeoutMs: in.TimeoutMs, Retries: in.Retries, Status: status,
	}, nil
}

// maxPrefixLen 前缀长度上限。超过这个长度的路径在实际请求里几乎不会出现，
// 放进去只会让路由表膨胀，并在编译预览里刷屏。
const maxPrefixLen = 200

// normalizePrefix 规范化并校验路径前缀。
//
// 路由前缀是精确的路径字符串，不是模式：允许空白、查询串或重复斜杠，
// 写进去之后请求永远匹配不上，而页面上看不出任何异常。
func normalizePrefix(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", errs.Validation("前缀不能为空")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// 折叠连续斜杠：/api//v1 与 /api/v1 在路由表里是两个不同的键。
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		p = "/"
	}
	if len(p) > maxPrefixLen {
		return "", errs.Validation(fmt.Sprintf("前缀过长（上限 %d 字符）", maxPrefixLen))
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", errs.Validation("前缀不能包含控制字符")
		}
	}
	if strings.ContainsAny(p, " ?#\\") {
		return "", errs.Validation("前缀不能包含空白、?、# 或反斜杠")
	}
	return p, nil
}

// normalizeConditions 校验并返回去掉键首尾空白的条件副本。
//
// 空键的 Header/Query 条件永远匹配不上（HTTP 头名不可能为空），
// 与其静默忽略，不如在保存时直接拒绝。
func normalizeConditions(kind string, in map[string]string) (string, error) {
	if len(in) == 0 {
		return "", nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k == "" {
			return "", errs.Validation(kind + " 条件的键不能为空")
		}
		if len(k) > 128 {
			return "", errs.Validation(kind + " 条件的键过长（上限 128 字符）")
		}
		if len(v) > 512 {
			return "", errs.Validation(kind + " 条件的值过长（上限 512 字符）")
		}
		for _, r := range k + v {
			if r < 0x20 || r == 0x7f {
				return "", errs.Validation(kind + " 条件不能包含控制字符")
			}
		}
		out[k] = v
	}
	return marshalMap(out)
}

// compileRows 校验并编译一组规则，冲突直接拒绝。
func (s *Service) compileRows(rows []model.GovRoute) ([]gov.Route, error) {
	out := make([]gov.Route, 0, len(rows))
	seen := map[string]string{}
	for _, r := range rows {
		if r.Status == "archived" {
			continue
		}
		key := conflictKey(r)
		if prev, ok := seen[key]; ok {
			return nil, errs.Conflict(fmt.Sprintf("规则冲突: %s 与 %s 的匹配条件完全重复", prev, r.Name))
		}
		seen[key] = r.Name
		out = append(out, gov.Route{
			ID: r.ID, Name: r.Name, Priority: r.Priority, Prefix: r.Prefix,
			Methods: splitMethods(r.Methods), Headers: parseMap(r.Headers), Query: parseMap(r.Query),
			Service: r.Service, Version: r.Version, Weight: r.Weight, Strip: r.Strip,
			TimeoutMs: r.TimeoutMs, Retries: r.Retries,
		})
	}
	// 与数据面同一套编译逻辑，提前暴露排序与匹配问题；
	// 返回排好序的副本，让页面上的编译预览就是真实的命中顺序。
	gov.Compile(out)
	return gov.SortRoutes(out), nil
}

func conflictKey(r model.GovRoute) string {
	parts := []string{r.Prefix, r.Methods, sortedJSON(r.Headers), sortedJSON(r.Query)}
	return strings.Join(parts, "|")
}

func sortedJSON(raw string) string {
	m := parseMap(raw)
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(m[k])
		b.WriteString(";")
	}
	return b.String()
}

// PublishInput 是一次发布请求。
type PublishInput struct {
	// Targets 为空表示全量；非空只对这些实例可见（灰度）。
	Targets  []string `json:"targets"`
	Note     string   `json:"note"`
	Operator string   `json:"-"`
}

// Publish 把当前全部非归档规则编译后发布。
func (s *Service) Publish(ctx context.Context, in PublishInput) (*model.GovRelease, error) {
	var rows []model.GovRoute
	if err := s.db.WithContext(ctx).Where("status <> ?", "archived").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errs.Validation("没有可发布的规则")
	}
	compiled, err := s.compileRows(rows)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUniquePrefix(ctx, compiled); err != nil {
		return nil, err
	}

	stage := "full"
	if len(in.Targets) > 0 {
		stage = "canary"
	}
	targets, _ := json.Marshal(in.Targets)

	var rel model.GovRelease
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rev, err := nextRev(tx)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(gov.RoutePayload{Rev: rev, Routes: compiled})
		if err != nil {
			return err
		}
		// 同一域只能有一个 active 的 canary 与 full，旧的收口为 done。
		tx.Model(&model.GovRelease{}).
			Where("domain = ? AND status = ?", "route", "active").
			Updates(map[string]any{"status": "done"})
		rel = model.GovRelease{
			Domain: "route", Revision: rev, Payload: payload, Checksum: gov.Checksum(payload),
			Targets: string(targets), Stage: stage, Status: "active",
			Operator: in.Operator, Note: in.Note,
		}
		if err := tx.Create(&rel).Error; err != nil {
			return err
		}
		return tx.Model(&model.GovRoute{}).Where("status = ?", "draft").Update("status", "published").Error
	})
	if err != nil {
		return nil, err
	}
	s.bump()
	return &rel, nil
}

// ensureUniquePrefix 拦截完全相同前缀 + 完全相同条件的规则。
func (s *Service) ensureUniquePrefix(ctx context.Context, routes []gov.Route) error {
	seen := map[string]bool{}
	for _, r := range routes {
		key := r.Prefix + "|" + strings.Join(r.Methods, ",")
		if seen[key] {
			return errs.Conflict("存在重复前缀与方法的规则: " + r.Prefix)
		}
		seen[key] = true
	}
	return nil
}

// Releases 返回发布历史。
func (s *Service) Releases(ctx context.Context, domain string) ([]model.GovRelease, error) {
	if domain == "" {
		domain = "route"
	}
	var rows []model.GovRelease
	err := s.db.WithContext(ctx).Where("domain = ?", domain).
		Order("revision DESC").Limit(50).Find(&rows).Error
	return rows, err
}

// Promote 把灰度批次转为全量。
func (s *Service) Promote(ctx context.Context, id string) (*model.GovRelease, error) {
	var rel model.GovRelease
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&rel).Error; err != nil {
		return nil, errs.NotFound("发布记录不存在")
	}
	if rel.Stage == "full" {
		return nil, errs.Validation("该发布已经是全量")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.GovRelease{}).
			Where("domain = ? AND status = ?", rel.Domain, "active").
			Update("status", "done").Error; err != nil {
			return err
		}
		return tx.Model(&model.GovRelease{}).Where("id = ?", rel.ID).
			Updates(map[string]any{"stage": "full", "targets": "[]", "status": "active"}).Error
	})
	if err != nil {
		return nil, err
	}
	s.db.WithContext(ctx).Where("id = ?", id).First(&rel)
	s.bump()
	return &rel, nil
}

// Rollback 按旧发布内容生成一个新 revision。
//
// 不复用旧 revision 号：数据面只接受更大的版本，回退版本号会让它拒绝应用。
func (s *Service) Rollback(ctx context.Context, id, operator string) (*model.GovRelease, error) {
	var old model.GovRelease
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&old).Error; err != nil {
		return nil, errs.NotFound("发布记录不存在")
	}
	var payload gov.RoutePayload
	if err := json.Unmarshal(old.Payload, &payload); err != nil {
		return nil, errs.Internal("发布内容损坏").WithCause(err)
	}
	var rel model.GovRelease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rev, err := nextRev(tx)
		if err != nil {
			return err
		}
		payload.Rev = rev
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.GovRelease{}).
			Where("domain = ? AND status = ?", old.Domain, "active").
			Updates(map[string]any{"status": "done"}).Error; err != nil {
			return err
		}
		rel = model.GovRelease{
			Domain: old.Domain, Revision: rev, Payload: b, Checksum: gov.Checksum(b),
			Stage: "full", Status: "active", Operator: operator,
			Note: "回滚到 r" + fmt.Sprint(old.Revision),
		}
		return tx.Create(&rel).Error
	})
	if err != nil {
		return nil, err
	}
	s.bump()
	return &rel, nil
}

// nextRev 委托给控制面，保证路由与配置中心共用同一个全局版本序列。
func nextRev(tx *gorm.DB) (int64, error) { return govserver.NextRev(tx) }

func (s *Service) bump() {
	if s.gov != nil {
		s.gov.Bump()
	}
}

func normalizeMethods(in []string) (string, error) {
	if len(in) == 0 {
		return "", nil
	}
	allowed := map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true, "HEAD": true, "OPTIONS": true}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, m := range in {
		m = strings.ToUpper(strings.TrimSpace(m))
		if !allowed[m] {
			return "", errs.Validation("不支持的 HTTP 方法: " + m)
		}
		// 去重：GET / get 是同一个方法，重复存进条件里只会让冲突判断失真。
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return strings.Join(out, ","), nil
}

func splitMethods(s string) []string {
	if strings.TrimSpace(s) == "" {
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

func marshalMap(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
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

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func orDefaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
