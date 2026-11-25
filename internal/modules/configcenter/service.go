package configcenter

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/config"
	"github.com/MITEZ360/tenon/internal/platform/crypto"
	"github.com/MITEZ360/tenon/pkg/errs"
)

// Service 运行时配置中心。
//
// 配置分三层：内置默认值 -> 配置文件/环境变量 -> 管理端覆盖（本表）。
// 热刷新只重建 platform/config 并触发组件回调，不重启进程；标记为
// restart 的项保存后仅落库，由管理员在危险操作页统一下发重启。
type Service struct {
	db  *gorm.DB
	cfg *config.Config
	log *zap.Logger
	mu  sync.Mutex
	// activeModules / moduleSource 是本次启动实际启用的模块与来源。
	// 它不等于配置值：依赖补齐与常驻模块会让实际集合更大，
	// 页面必须展示"当前运行"而不是"配置写了什么"。
	activeModules []string
	moduleSource  string
}

// NewService 创建配置中心服务。
func NewService(db *gorm.DB, cfg *config.Config, log *zap.Logger) *Service {
	return &Service{db: db, cfg: cfg, log: log}
}

// SetActiveModules 注入本次启动解析出的实际模块集合，供页面展示。
func (s *Service) SetActiveModules(names []string, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeModules = append([]string(nil), names...)
	s.moduleSource = source
}

// ItemView 是配置项的当前状态。
type ItemView struct {
	Item
	Value      string `json:"value"`
	Source     string `json:"source"` // db / file / default
	Overridden bool   `json:"overridden"`
	Editable   bool   `json:"editable"`
	// Running 是运行期的实际生效值，仅 modules.enabled 使用：
	// 配置值可能因依赖补齐或常驻模块与实际启用集合不同。
	Running       []string `json:"running,omitempty"`
	RunningSource string   `json:"runningSource,omitempty"`
}

// GroupView 分组视图。
type GroupView struct {
	Name  string     `json:"name"`
	Label string     `json:"label"`
	Desc  string     `json:"description"`
	Items []ItemView `json:"items"`
}

// List 返回全部配置项及其当前值和来源。
func (s *Service) List(ctx context.Context) ([]GroupView, *DeployView, error) {
	stored, err := s.stored(ctx)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	active := append([]string(nil), s.activeModules...)
	activeSource := s.moduleSource
	s.mu.Unlock()
	groups := make([]GroupView, 0, len(groupMeta))
	for _, g := range groupMeta {
		visible := itemsOf(g.Name)
		// 整组配置项都不属于当前发行版时，连分组标题一起隐藏，
		// 否则页面上会留下一个空分组。
		if len(visible) == 0 {
			continue
		}
		gv := GroupView{Name: g.Name, Label: g.Label, Desc: g.Desc, Items: make([]ItemView, 0, len(visible))}
		for _, it := range visible {
			iv := ItemView{Item: it, Editable: true, Source: "default"}
			if it.Key == KeyModules {
				iv.Running = active
				iv.RunningSource = activeSource
				iv.Choices = moduleChoices()
			}
			if row, ok := stored[it.Key]; ok {
				iv.Overridden = true
				iv.Source = "db"
				if it.Secret {
					iv.Value = "******"
				} else {
					iv.Value = row.Value
				}
			} else if v, ok := lookup(s.cfg, it.Key); ok && v != "" {
				iv.Source = "file"
				if it.Secret {
					// 文件/环境变量里的密钥同样不能回显，只告知"已配置"。
					iv.Value = "******"
				} else {
					iv.Value = v
				}
			}
			gv.Items = append(gv.Items, iv)
		}
		groups = append(groups, gv)
	}
	return groups, s.deployView(), nil
}

// DeployView 汇总只读的部署参数，避免管理员在页面上找不到"数据库在哪配"。
type DeployView struct {
	Items []DeployItem `json:"items"`
	Note  string       `json:"note"`
}

func (s *Service) deployView() *DeployView {
	mask := func(v string) string {
		if v == "" {
			return "（未配置）"
		}
		return v
	}
	dsn := s.cfg.DB.DSN
	if i := strings.Index(dsn, "://"); i >= 0 {
		if j := strings.Index(dsn[i+3:], "@"); j >= 0 {
			dsn = dsn[:i+3] + "******" + dsn[i+3+j:]
		}
	}
	return &DeployView{
		Note: "部署参数由配置文件或环境变量提供，修改后需要重启进程；这些项不放入覆盖表，避免把服务改到无法启动。",
		Items: []DeployItem{
			{Key: "server.host", Name: "监听地址", Value: s.cfg.Server.Host, Desc: "容器内通常为 0.0.0.0"},
			{Key: "server.port", Name: "监听端口", Value: strconv.Itoa(s.cfg.Server.Port), Desc: "对外服务端口"},
			{Key: "db.driver", Name: "数据库驱动", Value: s.cfg.DB.ResolveDriver(), Desc: "sqlite / mysql / postgres"},
			{Key: "db.dsn", Name: "数据库连接", Value: mask(dsn), Desc: "建议用 TENON_DB_DSN 环境变量注入"},
			{Key: "redis.addr", Name: "Redis 地址", Value: mask(s.cfg.Redis.Addr), Desc: "留空时使用进程内缓存，多节点部署必须配置"},
			{Key: "migration.mode", Name: "迁移模式", Value: s.cfg.Migration.ResolveMode(), Desc: "versioned / auto / off"},
			{Key: "runtime.instances", Name: "计划实例数", Value: strconv.Itoa(s.cfg.Runtime.Instances), Desc: "大于 1 时启用无状态自检"},
		},
	}
}

// Update 是一次保存请求。
type Update struct {
	Key   string `json:"key" binding:"required,max=128" label:"配置键"`
	Value string `json:"value" label:"配置值"`
}

// Result 描述一次应用结果。
type Result struct {
	Applied       []string `json:"applied"`
	NeedRestart   []string `json:"needRestart"`
	Changed       int      `json:"changed"`
	ConfigVersion int64    `json:"configVersion"`
	Message       string   `json:"message"`
}

// Save 保存并立即应用配置；需要重启的项只落库，由管理员后续统一下发。
func (s *Service) Save(ctx context.Context, updates []Update, operator string) (*Result, error) {
	if len(updates) == 0 {
		return nil, errs.BadRequest("没有需要保存的配置项")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}
	// 在副本上计算，apply 失败时数据库保持原样。
	next := make(map[string]storedValue, len(stored))
	for k, v := range stored {
		next[k] = v
	}
	// remove 表示恢复配置文件取值（删除覆盖行）；这类变更不需要重启提示。
	type op struct {
		item   Item
		remove bool
	}
	ops := make([]op, 0, len(updates))
	removes := 0
	for _, u := range updates {
		it, ok := itemIndex[u.Key]
		if !ok {
			return nil, errs.BadRequest(fmt.Sprintf("配置项 %s 不允许在管理端修改", u.Key))
		}
		raw := strings.TrimSpace(u.Value)
		if it.Secret && (raw == "" || raw == "******") {
			// 脱敏回显原样提交表示"不修改"，避免把密文覆盖成星号。
			continue
		}
		// 空值表示删除覆盖、恢复配置文件取值，不再做类型校验，
		// 否则布尔/数值项永远无法恢复默认。
		if raw != "" {
			if err := validate(it, raw); err != nil {
				return nil, err
			}
		}
		if raw == "" {
			delete(next, it.Key)
		} else {
			next[it.Key] = storedValue{Value: raw, Secret: it.Secret}
		}
		if raw == "" {
			removes++
		}
		ops = append(ops, op{item: it, remove: raw == ""})
	}
	if len(ops) == 0 {
		return &Result{Message: "没有实际变更"}, nil
	}

	overrides, err := s.overridesOf(next)
	if err != nil {
		return nil, err
	}
	if _, err := config.Apply(overrides); err != nil {
		return nil, errs.BadRequest("配置校验未通过：" + err.Error())
	}

	version := s.nextVersion(ctx, stored)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, o := range ops {
			it := o.item
			row, ok := next[it.Key]
			if o.remove || !ok {
				// 物理删除：软删除会保留唯一索引占位，之后重新设置同一键
				// 既插不进去也查不出来。
				if err := tx.Unscoped().Where("config_key = ?", it.Key).Delete(&model.RuntimeConfig{}).Error; err != nil {
					return err
				}
				continue
			}
			val := row.Value
			if it.Secret {
				enc, err := crypto.Encrypt(val, s.cfg.Security.DataEncryptKey)
				if err != nil {
					return err
				}
				val = enc
			}
			rec := model.RuntimeConfig{Key: it.Key, Value: val, Encrypted: it.Secret, Version: version, UpdatedBy: operator}
			// deleted_at 置空用于复活历史上被软删除的行（唯一索引仍被占用）。
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "config_key"}},
				DoUpdates: clause.Assignments(map[string]any{
					"value": val, "encrypted": it.Secret, "version": version,
					"updated_by": operator, "updated_at": time.Now(), "deleted_at": nil,
				}),
			}).Create(&rec).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res := &Result{Changed: len(ops), ConfigVersion: version}
	// 白名单里没有配置中心时，重启后本页会消失，只能改配置文件或直连数据库。
	// 不阻止这种裁剪（发行版可能刻意不带管理端），但必须留下痕迹。
	for _, o := range ops {
		if o.item.Key != "modules.enabled" || o.remove {
			continue
		}
		if row, ok := next[o.item.Key]; ok && !slices.Contains(splitList(row.Value), "configcenter") {
			s.log.Warn("模块白名单未包含配置中心，重启后管理端配置页将不可用",
				zap.String("operator", operator), zap.Strings("modules", splitList(row.Value)))
		}
	}
	for _, o := range ops {
		if o.remove {
			// 删除覆盖后回落到配置文件值；热项立即生效，冷项本来就在按文件值运行。
			if o.item.Hot {
				res.Applied = append(res.Applied, o.item.Key)
			}
			continue
		}
		if o.item.Hot {
			res.Applied = append(res.Applied, o.item.Key)
		} else {
			res.NeedRestart = append(res.NeedRestart, o.item.Key)
		}
	}
	if removes == len(ops) {
		res.Message = "已恢复为配置文件取值"
	} else {
		res.Message = s.message(res)
	}
	s.log.Info("运行时配置已更新", zap.String("operator", operator),
		zap.Int64("version", version), zap.Strings("hot", res.Applied), zap.Strings("restart", res.NeedRestart))
	return res, nil
}

// Reload 从数据库重新读取覆盖并应用，用于"刷新配置"按钮与节点配置下发。
func (s *Service) Reload(ctx context.Context) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reload(ctx, true)
}

// ApplyFromDB 启动时把数据库覆盖合并进配置，失败不阻断启动（记录告警）。
func (s *Service) ApplyFromDB(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.reload(ctx, false)
	return err
}

func (s *Service) reload(ctx context.Context, verbose bool) (*Result, error) {
	stored, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}
	overrides, err := s.overridesOf(stored)
	if err != nil {
		return nil, err
	}
	if _, err := config.Apply(overrides); err != nil {
		return nil, err
	}
	res := &Result{ConfigVersion: s.maxVersion(ctx), Applied: make([]string, 0, len(overrides))}
	for _, it := range items {
		if _, ok := overrides[it.Key]; ok && it.Hot {
			res.Applied = append(res.Applied, it.Key)
		}
	}
	if verbose {
		res.Message = fmt.Sprintf("已加载 %d 项覆盖配置，其中 %d 项热生效", len(overrides), len(res.Applied))
	}
	return res, nil
}

// Version 返回覆盖表的最大版本号，节点用它判断是否需要重新加载。
func (s *Service) Version(ctx context.Context) int64 {
	return s.maxVersion(ctx)
}

// maxVersion 读取最大版本号；表尚未建好时返回 0。
func (s *Service) maxVersion(ctx context.Context) int64 {
	var v int64
	row := s.db.WithContext(ctx).Model(&model.RuntimeConfig{}).Select("COALESCE(MAX(version),0)")
	if err := row.Scan(&v).Error; err != nil {
		return 0
	}
	return v
}

type storedValue struct {
	Value  string
	Secret bool
}

// stored 读取数据库中的覆盖值；密文在这里解密，供应用与展示使用。
func (s *Service) stored(ctx context.Context) (map[string]storedValue, error) {
	var rows []model.RuntimeConfig
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]storedValue, len(rows))
	for _, r := range rows {
		if _, ok := itemIndex[r.Key]; !ok {
			// 历史遗留或已下线的键直接忽略，避免把未知键塞进配置。
			continue
		}
		val := r.Value
		if r.Encrypted {
			dec, err := crypto.Decrypt(val, s.cfg.Security.DataEncryptKey)
			if err != nil {
				s.log.Warn("配置项解密失败，已跳过", zap.String("key", r.Key), zap.Error(err))
				continue
			}
			val = dec
		}
		out[r.Key] = storedValue{Value: val, Secret: r.Encrypted}
	}
	return out, nil
}

// overridesOf 把字符串值转成 viper 可接受的原生类型。
func (s *Service) overridesOf(stored map[string]storedValue) (map[string]any, error) {
	out := make(map[string]any, len(stored))
	for k, v := range stored {
		it, ok := itemIndex[k]
		if !ok {
			continue
		}
		val, err := parseValue(it, v.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", it.Name, err)
		}
		out[k] = val
	}
	return out, nil
}

func (s *Service) nextVersion(ctx context.Context, _ map[string]storedValue) int64 {
	v := s.maxVersion(ctx)
	if v <= 0 {
		return 1
	}
	return v + 1
}

func (s *Service) message(res *Result) string {
	if len(res.Applied) == 0 && len(res.NeedRestart) == 0 {
		// 删除覆盖不产生生效项：回落到配置文件值，通常无需重启。
		return "已恢复为配置文件取值"
	}
	switch {
	case len(res.NeedRestart) > 0 && len(res.Applied) > 0:
		return "部分配置已热生效，另有配置需要重启后生效"
	case len(res.NeedRestart) > 0:
		return "配置已保存，需要重启服务后生效"
	case len(res.Applied) > 0:
		return "配置已保存并立即生效"
	default:
		return "配置已保存"
	}
}

// validate 校验类型与取值范围；只有通过校验才会落库。
func validate(it Item, raw string) error {
	// 带候选项的 list 逐项校验：写错模块名会让下次启动 fail-fast 起不来，
	// 必须在保存时拦住，而不是留到重启才暴露。
	choices := it.Choices
	if it.Key == KeyModules {
		choices = moduleChoices()
	}
	if it.Type == "list" && len(choices) > 0 {
		allowed := make(map[string]bool, len(choices))
		for _, c := range choices {
			allowed[c.Value] = true
		}
		var unknown []string
		for _, v := range splitList(raw) {
			if !allowed[v] {
				unknown = append(unknown, v)
			}
		}
		if len(unknown) > 0 {
			return errs.BadRequest(fmt.Sprintf("%s 含未知取值：%s", it.Name, strings.Join(unknown, "、")))
		}
		return nil
	}
	if len(it.Options) > 0 {
		ok := false
		for _, opt := range it.Options {
			if opt == raw {
				ok = true
				break
			}
		}
		if !ok {
			return errs.BadRequest(fmt.Sprintf("%s 只能取 %s", it.Name, strings.Join(it.Options, " / ")))
		}
	}
	switch it.Type {
	case "bool":
		if _, err := strconv.ParseBool(raw); err != nil {
			return errs.BadRequest(it.Name + " 必须是 true 或 false")
		}
	case "int", "float":
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return errs.BadRequest(it.Name + " 必须是数字")
		}
		if it.Min != nil && n < *it.Min {
			return errs.BadRequest(fmt.Sprintf("%s 不能小于 %v", it.Name, *it.Min))
		}
		if it.Max != nil && n > *it.Max {
			return errs.BadRequest(fmt.Sprintf("%s 不能大于 %v", it.Name, *it.Max))
		}
	case "duration":
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return errs.BadRequest(it.Name + " 必须是正的时间间隔，如 15m、2h")
		}
	}
	return nil
}

func parseValue(it Item, raw string) (any, error) {
	switch it.Type {
	case "bool":
		return strconv.ParseBool(raw)
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		return int(n), err
	case "float":
		return strconv.ParseFloat(raw, 64)
	case "duration":
		d, err := time.ParseDuration(raw)
		return d, err
	case "list":
		return splitList(raw), nil
	default:
		return raw, nil
	}
}

// splitList 把逗号分隔的输入拆成去空白的切片。
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// lookup 按 mapstructure 路径从配置结构体取值并格式化为字符串。
// 配置项都有对应的结构体字段，用反射可以避免为每个键手写 getter。
func lookup(cfg *config.Config, key string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	v := reflect.ValueOf(cfg)
	for _, part := range strings.Split(key, ".") {
		v = indirect(v)
		if !v.IsValid() || v.Kind() != reflect.Struct {
			return "", false
		}
		t := v.Type()
		found := false
		for i := 0; i < t.NumField(); i++ {
			name := strings.Split(t.Field(i).Tag.Get("mapstructure"), ",")[0]
			if name == part {
				v, found = v.Field(i), true
				break
			}
		}
		if !found {
			return "", false
		}
	}
	v = indirect(v)
	if !v.IsValid() {
		return "", false
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), true
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.Type() == reflect.TypeOf(time.Duration(0)) {
			return humanDuration(time.Duration(v.Int())), true
		}
		return strconv.FormatInt(v.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), true
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			out := make([]string, v.Len())
			for i := 0; i < v.Len(); i++ {
				out[i] = v.Index(i).String()
			}
			return strings.Join(out, ","), true
		}
	}
	return "", false
}

// humanDuration 输出配置文件常见的写法：15m 而不是 15m0s。
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return d.String()
	}
	switch {
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d%time.Minute == 0:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d%time.Second == 0:
		return strconv.Itoa(int(d/time.Second)) + "s"
	default:
		return d.String()
	}
}

func indirect(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}
