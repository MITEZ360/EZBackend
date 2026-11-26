package configcenter

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/MITEZ360/tenon/internal/govserver"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/pkg/errs"
	"github.com/MITEZ360/tenon/pkg/gov"
)

// ConfigStore 是业务配置中心：命名空间、配置项、发布与回滚。
//
// 它与 Service（运行参数在线编辑）分开：那边改的是进程配置，这边是给业务
// 读取的配置。两者共用 governance 的 revision 与快照通道，但存储互不影响。
type ConfigStore struct {
	db  *gorm.DB
	log *zap.Logger
	gov *govserver.Server
}

// NewConfigStore 创建配置中心。
func NewConfigStore(db *gorm.DB, log *zap.Logger, gs *govserver.Server) *ConfigStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ConfigStore{db: db, log: log, gov: gs}
}

// NamespaceInput 是命名空间入参。
type NamespaceInput struct {
	Namespace string `json:"namespace"`
	Env       string `json:"env"`
	Desc      string `json:"desc"`
}

// Namespaces 返回全部命名空间。
func (s *ConfigStore) Namespaces(ctx context.Context) ([]model.GovConfigNamespace, error) {
	var rows []model.GovConfigNamespace
	err := s.db.WithContext(ctx).Order("namespace ASC").Find(&rows).Error
	return rows, err
}

// CreateNamespace 新建命名空间。
func (s *ConfigStore) CreateNamespace(ctx context.Context, in NamespaceInput) (*model.GovConfigNamespace, error) {
	ns := strings.TrimSpace(in.Namespace)
	if err := validateNamespace(ns); err != nil {
		return nil, err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.GovConfigNamespace{}).Where("namespace = ?", ns).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, errs.Conflict("命名空间已存在")
	}
	row := model.GovConfigNamespace{Namespace: ns, Env: in.Env, Desc: in.Desc}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		// Count 与 Create 之间有窗口：并发同名创建时唯一索引会拒绝后到的那个。
		// 回查确认是重名就报冲突，否则调用方会看到一个莫名的 500。
		var n int64
		if e := s.db.WithContext(ctx).Model(&model.GovConfigNamespace{}).
			Where("namespace = ?", ns).Count(&n).Error; e == nil && n > 0 {
			return nil, errs.Conflict("命名空间已存在")
		}
		return nil, err
	}
	return &row, nil
}

// namespaceRe 命名空间会拼进快照分区键（config:<ns>）与发布域（config:<ns>），
// 允许空格或斜杠会让分区名产生歧义，也让"按命名空间查找"变得不可靠。
var namespaceRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const maxNamespaceLen = 64

func validateNamespace(ns string) error {
	if ns == "" {
		return errs.Validation("命名空间不能为空")
	}
	if len(ns) > maxNamespaceLen {
		return errs.Validation(fmt.Sprintf("命名空间过长（上限 %d 字符）", maxNamespaceLen))
	}
	if !namespaceRe.MatchString(ns) {
		return errs.Validation("命名空间只能包含字母、数字、中划线、下划线与点，且需以字母或数字开头")
	}
	return nil
}

// maxItemValue 单个配置值的大小上限。配置会随快照整体下发，没有上限时
// 一个误传的大文件就会把所有数据面的内存和带宽打满。
const maxItemValue = 64 << 10

// validateItem 校验一条配置项的键与元数据。
func validateItem(it ItemInput) (string, string, error) {
	key := strings.TrimSpace(it.Key)
	if key == "" {
		return "", "", errs.Validation("配置键不能为空")
	}
	if len(key) > 128 {
		return "", "", errs.Validation("配置键过长（上限 128 字符）")
	}
	for _, r := range key {
		if r <= 0x20 || r == 0x7f {
			return "", "", errs.Validation("配置键不能包含空白或控制字符")
		}
	}
	if len(it.Value) > maxItemValue {
		return "", "", errs.Validation(fmt.Sprintf("配置值过大（上限 %d 字节）", maxItemValue))
	}
	if len(it.Type) > 32 {
		return "", "", errs.Validation("配置类型过长（上限 32 字符）")
	}
	scope := it.Scope
	if scope == "" {
		scope = "global"
	}
	switch scope {
	case "global", "service":
	default:
		return "", "", errs.Validation("作用域只能是 global / service")
	}
	if scope == "service" && strings.TrimSpace(it.Service) == "" {
		return "", "", errs.Validation("作用域为 service 时必须指定服务")
	}
	return key, scope, nil
}

// DeleteNamespace 删除命名空间及其配置项。
//
// 走物理删除：namespace 上有唯一索引，软删的墓碑会挡住同名重建；
// 删除动作本身已进审计日志，不需要靠墓碑留痕。
func (s *ConfigStore) DeleteNamespace(ctx context.Context, id string) error {
	var row model.GovConfigNamespace
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return errs.NotFound("命名空间不存在")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("namespace = ?", row.Namespace).
			Delete(&model.GovConfigItem{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&row).Error
	})
	if err == nil {
		s.bump()
	}
	return err
}

// ConfigItemView 是配置项的展示形态；加密项不回显明文。
type ConfigItemView struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Type      string `json:"type"`
	Encrypted bool   `json:"encrypted"`
	Scope     string `json:"scope"`
	Service   string `json:"service"`
	UpdatedBy string `json:"updatedBy"`
}

// Items 返回命名空间下的配置项。
func (s *ConfigStore) Items(ctx context.Context, namespace string) ([]ConfigItemView, error) {
	if namespace == "" {
		return nil, errs.Validation("缺少命名空间")
	}
	var rows []model.GovConfigItem
	if err := s.db.WithContext(ctx).Where("namespace = ?", namespace).Order("key ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ConfigItemView, 0, len(rows))
	for _, r := range rows {
		val := r.Value
		if r.Encrypted {
			val = gov.Masked(r.Value)
		}
		out = append(out, ConfigItemView{
			ID: r.ID, Namespace: r.Namespace, Key: r.Key, Value: val,
			Type: r.Type, Encrypted: r.Encrypted, Scope: r.Scope, Service: r.Service, UpdatedBy: r.UpdatedBy,
		})
	}
	return out, nil
}

// ItemInput 是单条配置的入参。
type ItemInput struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Type      string `json:"type"`
	Encrypted bool   `json:"encrypted"`
	Scope     string `json:"scope"`
	Service   string `json:"service"`
}

// SaveItems 批量 upsert 配置项。
//
// 加密项：入参是新明文时加密落库；value 为空或脱敏占位时保留原值，
// 避免前端只改类型却把密钥清掉。
//
// 整批在一个事务里完成：逐条写入时中途校验失败会把前几条留在库里，
// 调用方收到 400 却以为什么都没改。
func (s *ConfigStore) SaveItems(ctx context.Context, namespace string, items []ItemInput, operator string) error {
	if namespace == "" {
		return errs.Validation("缺少命名空间")
	}
	var ns model.GovConfigNamespace
	if err := s.db.WithContext(ctx).Where("namespace = ?", namespace).First(&ns).Error; err != nil {
		return errs.NotFound("命名空间不存在")
	}
	if len(items) == 0 {
		return errs.Validation("没有需要保存的配置项")
	}

	rows := make([]model.GovConfigItem, 0, len(items))
	for _, it := range items {
		key, scope, err := validateItem(it)
		if err != nil {
			return err
		}
		value := it.Value
		if it.Encrypted {
			if value == "" || value == "******" {
				// 保留原值
				var old model.GovConfigItem
				if err := s.db.WithContext(ctx).Where("namespace = ? AND key = ?", namespace, key).First(&old).Error; err == nil {
					value = old.Value
				}
			} else if !strings.HasPrefix(value, gov.EncPrefix) {
				enc, err := gov.Encrypt(value, gov.MasterKey())
				if err != nil {
					return errs.Validation("加密失败: " + err.Error())
				}
				value = enc
			}
		}
		rows = append(rows, model.GovConfigItem{
			Namespace: namespace, Key: key, Value: value,
			Type: orDefault(it.Type, "string"), Encrypted: it.Encrypted,
			Scope: scope, Service: it.Service, UpdatedBy: operator,
		})
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range rows {
			// 同一批次里的重复键按顺序覆盖，最后一条为准。
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "namespace"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "type", "encrypted", "scope", "service", "updated_by", "updated_at"}),
			}).Create(&rows[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.bump()
	return nil
}

// DeleteItem 删除一条配置。
func (s *ConfigStore) DeleteItem(ctx context.Context, id string) error {
	// 物理删除，理由同 DeleteNamespace：uk_gov_config 是唯一索引。
	res := s.db.WithContext(ctx).Unscoped().Delete(&model.GovConfigItem{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.NotFound("配置项不存在")
	}
	s.bump()
	return nil
}

// Publish 发布一个命名空间。
//
// payload 里加密项保持密文：发布记录会长期保留，把明文再复制一份到
// gov_release 等于给密钥开了第二个出口。解密发生在下发时。
func (s *ConfigStore) Publish(ctx context.Context, namespace, operator, note string) (*model.GovRelease, error) {
	if namespace == "" {
		return nil, errs.Validation("缺少命名空间")
	}
	var items []model.GovConfigItem
	if err := s.db.WithContext(ctx).Where("namespace = ?", namespace).Find(&items).Error; err != nil {
		return nil, err
	}
	payloadItems := make(map[string]gov.ConfigItem, len(items))
	for _, it := range items {
		payloadItems[it.Key] = gov.ConfigItem{Value: it.Value, Type: it.Type, Encrypted: it.Encrypted}
	}

	domain := gov.PartConfigPrefix + namespace
	var rel model.GovRelease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rev, err := govserver.NextRev(tx)
		if err != nil {
			return err
		}
		b, err := json.Marshal(gov.ConfigPayload{Rev: rev, Namespace: namespace, Items: payloadItems})
		if err != nil {
			return err
		}
		if err := tx.Model(&model.GovRelease{}).
			Where("domain = ? AND status = ?", domain, "active").
			Updates(map[string]any{"status": "done"}).Error; err != nil {
			return err
		}
		rel = model.GovRelease{
			Domain: domain, Revision: rev, Payload: b, Checksum: gov.Checksum(b),
			Stage: "full", Status: "active", Operator: operator, Note: note,
		}
		return tx.Create(&rel).Error
	})
	if err != nil {
		return nil, err
	}
	s.bump()
	return &rel, nil
}

// Releases 返回命名空间的发布历史。
func (s *ConfigStore) Releases(ctx context.Context, namespace string) ([]model.GovRelease, error) {
	domain := gov.PartConfigPrefix + namespace
	var rows []model.GovRelease
	err := s.db.WithContext(ctx).Where("domain = ?", domain).
		Order("revision DESC").Limit(50).Find(&rows).Error
	return rows, err
}

// Rollback 用旧发布内容生成新版本。
func (s *ConfigStore) Rollback(ctx context.Context, id, operator string) (*model.GovRelease, error) {
	var old model.GovRelease
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&old).Error; err != nil {
		return nil, errs.NotFound("发布记录不存在")
	}
	if !strings.HasPrefix(old.Domain, gov.PartConfigPrefix) {
		return nil, errs.Validation("该记录不是配置发布")
	}
	var payload gov.ConfigPayload
	if err := json.Unmarshal(old.Payload, &payload); err != nil {
		return nil, errs.Internal("发布内容损坏").WithCause(err)
	}
	var rel model.GovRelease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rev, err := govserver.NextRev(tx)
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
		// 配置项本身也要回到旧值，否则下次发布会把回滚覆盖掉。
		for key, item := range payload.Items {
			if err := tx.Model(&model.GovConfigItem{}).
				Where("namespace = ? AND key = ?", payload.Namespace, key).
				Updates(map[string]any{"value": item.Value, "type": item.Type}).Error; err != nil {
				return err
			}
		}
		rel = model.GovRelease{
			Domain: old.Domain, Revision: rev, Payload: b, Checksum: gov.Checksum(b),
			Stage: "full", Status: "active", Operator: operator,
			Note: fmt.Sprintf("回滚到 r%d", old.Revision),
		}
		return tx.Create(&rel).Error
	})
	if err != nil {
		return nil, err
	}
	s.bump()
	return &rel, nil
}

func (s *ConfigStore) bump() {
	if s.gov != nil {
		s.gov.Bump()
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
