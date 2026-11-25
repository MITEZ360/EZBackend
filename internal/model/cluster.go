package model

import "time"

// RuntimeConfig 保存管理端对运行时配置的覆盖值。
//
// 与 sys_config（业务参数，如站点名称、上传上限文案）刻意分表：
// 这里的每一行都会在启动与热刷新时被合并进 platform/config，
// 键名是配置文件的 mapstructure 路径，误改会直接影响进程行为。
type RuntimeConfig struct {
	Base
	Key       string `gorm:"column:config_key;size:128;uniqueIndex;not null" json:"key"`
	Value     string `gorm:"type:text" json:"value"`
	Encrypted bool   `gorm:"default:false" json:"encrypted"`
	// Version 单调递增，节点靠它判断是否需要重新加载配置。
	Version   int64  `gorm:"index;not null;default:0" json:"version"`
	UpdatedBy string `gorm:"size:64" json:"updatedBy,omitempty"`
}

// TableName 指定表名。
func (RuntimeConfig) TableName() string { return "sys_runtime_config" }

// ClusterNode 是集群中的一个服务节点。
//
// ID 在重启后必须保持不变，默认取 runtime.instance_id，留空时用
// hostname:port 推导；因此同一台机器上改端口会被视为新节点。
type ClusterNode struct {
	ID            string    `gorm:"primaryKey;size:96" json:"id"`
	Name          string    `gorm:"size:96" json:"name"`
	Host          string    `gorm:"size:160" json:"host"`
	Port          int       `json:"port"`
	Version       string    `gorm:"size:32" json:"version"`
	ConfigVersion int64     `gorm:"not null;default:0" json:"configVersion"`
	Status        string    `gorm:"size:16;index" json:"status"` // online / offline
	Leader        bool      `gorm:"default:false" json:"leader"`
	StartedAt     time.Time `json:"startedAt"`
	LastSeen      time.Time `gorm:"index" json:"lastSeen"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// TableName 指定表名。
func (ClusterNode) TableName() string { return "sys_cluster_node" }

// ClusterCommand 表示一次运维指令（重启、配置下发等）。
type ClusterCommand struct {
	Base
	Action    string `gorm:"size:32;index;not null" json:"action"`
	Scope     string `gorm:"size:16" json:"scope"`  // all / selected
	Status    string `gorm:"size:16;index" json:"status"` // pending / running / success / partial / failed
	Payload   string `gorm:"type:text" json:"payload,omitempty"`
	Total     int    `gorm:"not null;default:0" json:"total"`
	Succeeded int    `gorm:"not null;default:0" json:"succeeded"`
	Failed    int    `gorm:"not null;default:0" json:"failed"`
	Operator  string `gorm:"size:64" json:"operator,omitempty"`
}

// TableName 指定表名。
func (ClusterCommand) TableName() string { return "sys_cluster_command" }

// ClusterCommandTarget 是指令在单个节点上的执行明细。
type ClusterCommandTarget struct {
	Base
	CommandID  string     `gorm:"size:32;index;not null" json:"commandId"`
	NodeID     string     `gorm:"size:96;index;not null" json:"nodeId"`
	NodeName   string     `gorm:"size:96" json:"nodeName"`
	Status     string     `gorm:"size:16;index" json:"status"` // pending / running / success / failed
	Message    string     `gorm:"size:255" json:"message,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// TableName 指定表名。
func (ClusterCommandTarget) TableName() string { return "sys_cluster_command_target" }
