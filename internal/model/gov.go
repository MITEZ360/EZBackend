package model

import "time"

// GovRevision 是治理面的全局版本分配器，整表只有一行。
//
// 全局号只回答"要不要拉"，分区号（各资源的 rev 字段）回答"拉哪一块"。
// 所有治理写操作都必须在事务内自增它，单调性是数据面判断新旧的唯一依据。
type GovRevision struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	GlobalRev int64     `gorm:"not null;default:0" json:"globalRev"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName 指定表名。
func (GovRevision) TableName() string { return "gov_revision" }

// GovService 是服务目录中的一项。
//
// 健康检查配置内嵌在这里而不是单开一张表：一个服务一套检查策略，
// 拆表只会多一次 join，没有表达力收益。
type GovService struct {
	Base
	Name   string `gorm:"size:96;uniqueIndex;not null" json:"name"`
	Owner  string `gorm:"size:64" json:"owner"`
	Group  string `gorm:"size:64;index" json:"group"`
	Desc   string `gorm:"size:255" json:"desc"`
	Meta   string `gorm:"type:text" json:"meta,omitempty"`
	Status string `gorm:"size:16;index;default:enabled" json:"status"` // enabled / disabled

	// 主动健康检查：none 表示不探测，靠实例心跳与被动失败摘除。
	CheckType          string `gorm:"size:16;default:none" json:"checkType"` // none / http / tcp
	CheckPath          string `gorm:"size:255" json:"checkPath"`
	CheckInterval      int    `gorm:"default:10" json:"checkInterval"`     // 秒
	CheckTimeout       int    `gorm:"default:3" json:"checkTimeout"`       // 秒
	CheckFailThreshold int    `gorm:"default:3" json:"checkFailThreshold"` // 连续失败次数
}

// TableName 指定表名。
func (GovService) TableName() string { return "gov_service" }

// GovInstance 是服务的一个实例。
//
// InstanceID 由实例自己上报并保持稳定（默认 host:port），Base.ID 只是库内主键。
// 数据面用 InstanceID 做失败摘除与灰度定位，重启后不能变。
type GovInstance struct {
	Base
	ServiceID     string    `gorm:"size:96;index;uniqueIndex:uk_gov_instance;not null" json:"serviceId"`
	InstanceID    string    `gorm:"size:96;index;uniqueIndex:uk_gov_instance;not null" json:"instanceId"`
	Name          string    `gorm:"size:96" json:"name"`
	Host          string    `gorm:"size:160;not null" json:"host"`
	Port          int       `gorm:"not null" json:"port"`
	Version       string    `gorm:"size:32" json:"version"`
	Zone          string    `gorm:"size:64" json:"zone"`
	Weight        int       `gorm:"default:100" json:"weight"`
	Labels        string    `gorm:"type:text" json:"labels,omitempty"`
	Status        string    `gorm:"size:16;index;default:online" json:"status"` // online / draining / offline
	ConfigVersion int64     `gorm:"default:0" json:"configVersion"`
	LastSeen      time.Time `gorm:"index" json:"lastSeen"`
	StartedAt     time.Time `json:"startedAt"`
}

// TableName 指定表名。
func (GovInstance) TableName() string { return "gov_instance" }

// GovRoute 是一条动态转发规则。
//
// Match 的三个条件存字符串而不是 JSON 列：prefix 要建索引做最长匹配，
// methods 用逗号分隔便于校验，headers/query 用 JSON 存小 map。
type GovRoute struct {
	Base
	Name     string `gorm:"size:96;not null" json:"name"`
	Priority int    `gorm:"default:100;index" json:"priority"`
	Prefix   string `gorm:"size:255;index;not null" json:"prefix"`
	Methods  string `gorm:"size:128" json:"methods,omitempty"`  // GET,POST
	Headers  string `gorm:"type:text" json:"headers,omitempty"` // {"X-Canary":"1"}
	Query    string `gorm:"type:text" json:"query,omitempty"`   // {"beta":"true"}

	Service   string `gorm:"size:96;index;not null" json:"service"`
	Version   string `gorm:"size:32" json:"version,omitempty"`
	Weight    int    `gorm:"default:100" json:"weight"`
	Strip     bool   `gorm:"default:false" json:"strip"`
	TimeoutMs int    `gorm:"default:0" json:"timeoutMs"`
	Retries   int    `gorm:"default:0" json:"retries"`

	Status string `gorm:"size:16;index;default:draft" json:"status"` // draft / published / archived
}

// TableName 指定表名。
func (GovRoute) TableName() string { return "gov_route" }

// GovRelease 是一次发布记录，也是数据面拉取快照的来源。
//
// 同一域同一时间只允许一条 canary/full 状态的记录处于 active，
// 否则数据面无法判断"当前应该用哪份快照"。
type GovRelease struct {
	Base
	Domain   string `gorm:"size:32;index;not null" json:"domain"` // route / registry / config
	Revision int64  `gorm:"index;not null" json:"revision"`
	Payload  []byte `gorm:"type:blob" json:"-"`
	Checksum string `gorm:"size:64" json:"checksum"`
	// Targets 是灰度目标：JSON 数组，元素为实例 ID 或标签选择器。
	// 空表示全量。
	Targets  string `gorm:"type:text" json:"targets,omitempty"`
	Stage    string `gorm:"size:16;default:full" json:"stage"`          // canary / full
	Status   string `gorm:"size:16;index;default:active" json:"status"` // active / done / rolled_back
	Operator string `gorm:"size:64" json:"operator,omitempty"`
	Note     string `gorm:"size:255" json:"note,omitempty"`
}

// TableName 指定表名。
func (GovRelease) TableName() string { return "gov_release" }

// GovConfigNamespace 是配置中心的命名空间（按环境或业务域隔离）。
type GovConfigNamespace struct {
	Base
	Namespace string `gorm:"size:96;uniqueIndex;not null" json:"namespace"`
	Env       string `gorm:"size:32;index" json:"env"`
	Desc      string `gorm:"size:255" json:"desc"`
}

// TableName 指定表名。
func (GovConfigNamespace) TableName() string { return "gov_config_namespace" }

// GovConfigItem 是一条配置项。
//
// Scope=service 时必须填 Service；instance 仅用于灰度覆盖，不鼓励常态使用。
type GovConfigItem struct {
	Base
	Namespace string `gorm:"size:96;index;uniqueIndex:uk_gov_config;not null" json:"namespace"`
	Key       string `gorm:"size:128;index;uniqueIndex:uk_gov_config;not null" json:"key"`
	Value     string `gorm:"type:text" json:"value"`
	Type      string `gorm:"size:16;default:string" json:"type"` // string / number / bool / json
	Encrypted bool   `gorm:"default:false" json:"encrypted"`
	Scope     string `gorm:"size:16;default:global" json:"scope"` // global / service / instance
	Service   string `gorm:"size:96" json:"service,omitempty"`
	UpdatedBy string `gorm:"size:64" json:"updatedBy,omitempty"`
}

// TableName 指定表名。
func (GovConfigItem) TableName() string { return "gov_config_item" }

// GovCommand 是一次治理指令（重启、配置重载、缓存清理）。
type GovCommand struct {
	Base
	Action    string `gorm:"size:32;index;not null" json:"action"`
	Scope     string `gorm:"size:16" json:"scope"`        // all / selected
	Status    string `gorm:"size:16;index" json:"status"` // pending / running / success / partial / failed
	Payload   string `gorm:"type:text" json:"payload,omitempty"`
	Total     int    `gorm:"not null;default:0" json:"total"`
	Succeeded int    `gorm:"not null;default:0" json:"succeeded"`
	Failed    int    `gorm:"not null;default:0" json:"failed"`
	Operator  string `gorm:"size:64" json:"operator,omitempty"`
}

// TableName 指定表名。
func (GovCommand) TableName() string { return "gov_command" }

// GovCommandTarget 是指令在单个实例上的执行明细。
type GovCommandTarget struct {
	Base
	CommandID  string     `gorm:"size:32;index;not null" json:"commandId"`
	InstanceID string     `gorm:"size:96;index;not null" json:"instanceId"`
	Name       string     `gorm:"size:96" json:"name"`
	Status     string     `gorm:"size:16;index" json:"status"`
	Message    string     `gorm:"size:255" json:"message,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// TableName 指定表名。
func (GovCommandTarget) TableName() string { return "gov_command_target" }
