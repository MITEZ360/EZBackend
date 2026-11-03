package model

import "time"

// DictType 字典类型。
type DictType struct {
	Base
	Code   string     `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name   string     `gorm:"size:64;not null" json:"name"`
	Remark string     `gorm:"size:255" json:"remark,omitempty"`
	Status int8       `gorm:"index" json:"status"`
	Items  []DictItem `gorm:"-" json:"items,omitempty"`
}

// TableName 指定表名。
func (DictType) TableName() string { return "sys_dict_type" }

// DictItem 字典项。
type DictItem struct {
	Base
	TypeCode string `gorm:"size:64;index;not null" json:"typeCode"`
	Label    string `gorm:"size:64;not null" json:"label"`
	Value    string `gorm:"size:64;not null" json:"value"`
	TagColor string `gorm:"size:32" json:"tagColor,omitempty"`
	Sort     int    `gorm:"default:0" json:"sort"`
	Status   int8   `json:"status"`
	Remark   string `gorm:"size:255" json:"remark,omitempty"`
}

// TableName 指定表名。
func (DictItem) TableName() string { return "sys_dict_item" }

// SysConfig 系统参数（键值对，支持加密存储）。
type SysConfig struct {
	Base
	Key       string `gorm:"column:config_key;size:64;uniqueIndex;not null" json:"key"`
	Value     string `gorm:"type:text" json:"value"`
	Name      string `gorm:"size:64" json:"name"`
	Group     string `gorm:"column:group_name;size:32;index;default:default" json:"group"`
	Encrypted bool   `gorm:"default:false" json:"encrypted"`
	Public    bool   `gorm:"default:false" json:"public"`
	Remark    string `gorm:"size:255" json:"remark,omitempty"`
}

// TableName 指定表名。
func (SysConfig) TableName() string { return "sys_config" }

// Notice 通知公告。
type Notice struct {
	Base
	Title       string     `gorm:"size:128;not null" json:"title"`
	Content     string     `gorm:"type:text" json:"content"`
	Type        string     `gorm:"size:16;default:notice" json:"type"`
	Status      int8       `gorm:"index" json:"status"`
	PublisherID string     `gorm:"size:32" json:"publisherId,omitempty"`
	PublishAt   *time.Time `json:"publishAt,omitempty"`
}

// TableName 指定表名。
func (Notice) TableName() string { return "sys_notice" }

// LoginLog 登录日志。
type LoginLog struct {
	ID        string    `gorm:"primaryKey;size:32" json:"id"`
	UserID    string    `gorm:"size:32;index" json:"userId,omitempty"`
	Username  string    `gorm:"size:64;index" json:"username"`
	IP        string    `gorm:"size:64;index" json:"ip"`
	Location  string    `gorm:"size:64" json:"location,omitempty"`
	Browser   string    `gorm:"size:32" json:"browser,omitempty"`
	OS        string    `gorm:"size:32" json:"os,omitempty"`
	Status    int8      `gorm:"index" json:"status"` // 1 成功 0 失败
	Message   string    `gorm:"size:255" json:"message,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
}

// TableName 指定表名。
func (LoginLog) TableName() string { return "sys_login_log" }

// OperationLog 操作审计日志。
type OperationLog struct {
	ID        string    `gorm:"primaryKey;size:32" json:"id"`
	UserID    string    `gorm:"size:32;index" json:"userId,omitempty"`
	Username  string    `gorm:"size:64;index" json:"username,omitempty"`
	Module    string    `gorm:"size:64;index" json:"module"`
	Action    string    `gorm:"size:64" json:"action"`
	Method    string    `gorm:"size:8" json:"method"`
	Path      string    `gorm:"size:255" json:"path"`
	IP        string    `gorm:"size:64" json:"ip,omitempty"`
	Request   string    `gorm:"type:text" json:"request,omitempty"`
	Response  string    `gorm:"type:text" json:"response,omitempty"`
	Status    int       `gorm:"index" json:"status"`
	CostMS    int64     `json:"costMs"`
	TraceID   string    `gorm:"size:64;index" json:"traceId,omitempty"`
	Error     string    `gorm:"size:512" json:"error,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
}

// TableName 指定表名。
func (OperationLog) TableName() string { return "sys_operation_log" }

// Job 定时任务。
type Job struct {
	Base
	Name       string     `gorm:"size:64;not null" json:"name"`
	Cron       string     `gorm:"size:64;not null" json:"cron"`
	Handler    string     `gorm:"size:64;not null" json:"handler"`
	Params     string     `gorm:"size:512" json:"params,omitempty"`
	Status     int8       `gorm:"index" json:"status"`
	Remark     string     `gorm:"size:255" json:"remark,omitempty"`
	LastRunAt  *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt  *time.Time `json:"nextRunAt,omitempty"`
	RunCount   int64      `gorm:"default:0" json:"runCount"`
	FailCount  int64      `gorm:"default:0" json:"failCount"`
	LastResult string     `gorm:"size:32" json:"lastResult,omitempty"`
}

// TableName 指定表名。
func (Job) TableName() string { return "sys_job" }

// JobLog 任务执行日志。
type JobLog struct {
	ID        string    `gorm:"primaryKey;size:32" json:"id"`
	JobID     string    `gorm:"size:32;index" json:"jobId"`
	JobName   string    `gorm:"size:64" json:"jobName"`
	Status    int8      `gorm:"index" json:"status"` // 1 成功 0 失败
	Output    string    `gorm:"type:text" json:"output,omitempty"`
	CostMS    int64     `json:"costMs"`
	StartedAt time.Time `gorm:"index" json:"startedAt"`
}

// TableName 指定表名。
func (JobLog) TableName() string { return "sys_job_log" }

// File 文件元数据。
type File struct {
	Base
	Name       string `gorm:"size:255" json:"name"`
	Path       string `gorm:"size:512" json:"path"`
	URL        string `gorm:"size:512" json:"url"`
	Mime       string `gorm:"size:128" json:"mime"`
	Ext        string `gorm:"size:32" json:"ext"`
	Size       int64  `json:"size"`
	Hash       string `gorm:"size:64;index" json:"hash,omitempty"`
	UploaderID string `gorm:"size:32;index" json:"uploaderId,omitempty"`
	Storage    string `gorm:"size:16;default:local" json:"storage"`
}

// TableName 指定表名。
func (File) TableName() string { return "sys_file" }
