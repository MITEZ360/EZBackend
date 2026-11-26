package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadRejectsInvalidConfig 坏配置必须在启动前被挡住。
//
// 这些错误如果留到运行时才暴露，表现分别是：端口占用、gin panic、
// 连不上库、治理面订阅永远失败却只打 debug 日志。启动即失败远好排查。
func TestLoadRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"端口为 0", "server:\n  port: 0\n"},
		{"端口为负", "server:\n  port: -1\n"},
		{"端口超界", "server:\n  port: 70000\n"},
		{"server.mode 非法", "server:\n  mode: production\n"},
		{"数据库驱动非法", "db:\n  driver: oracle\n"},
		{"存储类型非法", "storage:\n  type: ftp\n"},
		{"micro.mode 非法", "micro:\n  mode: gatway\n"},
		{"日志级别非法", "log:\n  level: verbose\n"},
		{"时区非法", "app:\n  timezone: Mars/Olympus\n"},
		{"治理地址缺协议", "gov:\n  enabled: true\n  control_url: 127.0.0.1:9000\n"},
		{"治理地址协议非法", "gov:\n  control_url: ftp://127.0.0.1:9000\n"},
		{"治理地址无主机", "gov:\n  control_url: http://\n"},
		{"轮询间隔为负", "gov:\n  poll_interval: -1s\n"},
		{"轮询间隔过小", "gov:\n  poll_interval: 10ms\n"},
		{"轮询间隔写成裸数字", "gov:\n  poll_interval: 5\n"},
		{"治理服务名非法", "gov:\n  service_name: \"bad name\"\n"},
		{"控制面自身服务名非法", "gov:\n  self_service: \"a/b\"\n"},
		{"端口类型错误", "server:\n  port: \"abc\"\n"},
		{"时长类型错误", "gov:\n  poll_interval: \"five seconds\"\n"},
	}
	for _, tc := range cases {
		path := writeConfig(t, tc.body)
		if _, err := Load(path); err == nil {
			t.Fatalf("%s 应被拒绝", tc.name)
		}
	}
}

// TestLoadAcceptsValidEdgeConfig 边界内但看起来"极端"的配置应当接受。
func TestLoadAcceptsValidEdgeConfig(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		check func(*Config) error
	}{
		{
			name: "最小轮询间隔",
			body: "gov:\n  enabled: true\n  control_url: http://127.0.0.1:9000\n  poll_interval: 500ms\n",
			check: func(c *Config) error {
				if c.Gov.PollInterval != 500*time.Millisecond {
					t.Fatalf("轮询间隔应为 500ms，实际 %v", c.Gov.PollInterval)
				}
				return nil
			},
		},
		{
			name: "轮询间隔留空",
			body: "gov:\n  enabled: true\n  control_url: https://gov.internal:9000\n",
			check: func(c *Config) error {
				if c.Gov.ControlURL != "https://gov.internal:9000" {
					t.Fatalf("治理地址未生效: %q", c.Gov.ControlURL)
				}
				return nil
			},
		},
		{
			name:  "关闭治理订阅时允许空地址",
			body:  "gov:\n  enabled: false\n",
			check: nil,
		},
		{
			name: "纯治理网关暂无路由",
			body: "micro:\n  mode: gateway\n",
			check: func(c *Config) error {
				if len(c.Micro.Routes) != 0 {
					t.Fatal("不应有静态路由")
				}
				return nil
			},
		},
		{
			name: "离线阈值小于心跳时自动放大",
			body: "cluster:\n  heartbeat: 30s\n  offline_after: 5s\n",
			check: func(c *Config) error {
				if c.Cluster.OfflineAfter <= c.Cluster.Heartbeat {
					t.Fatalf("离线阈值应被放大到心跳的 3 倍，实际 %v <= %v",
						c.Cluster.OfflineAfter, c.Cluster.Heartbeat)
				}
				return nil
			},
		},
		{
			name: "备份保留数为 0 时补默认值",
			body: "backup:\n  retain: 0\n",
			check: func(c *Config) error {
				if c.Backup.Retain != 7 {
					t.Fatalf("保留数应补为 7，实际 %d", c.Backup.Retain)
				}
				return nil
			},
		},
		{
			name:  "未知配置项忽略",
			body:  "future_feature:\n  enabled: true\n",
			check: nil,
		},
	}
	for _, tc := range cases {
		path := writeConfig(t, tc.body)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("%s 应被接受: %v", tc.name, err)
		}
		if tc.check != nil {
			if err := tc.check(cfg); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestLoadEmptyFileUsesDefaults 空配置文件应落到完整默认值，而不是半零值。
func TestLoadEmptyFileUsesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatalf("空配置应可加载: %v", err)
	}
	if cfg.Server.Port != 9000 || cfg.Server.Mode != "release" {
		t.Fatalf("服务端默认值不对: %+v", cfg.Server)
	}
	if cfg.Micro.Mode != "mono" {
		t.Fatalf("默认形态应为 mono，实际 %s", cfg.Micro.Mode)
	}
	if !cfg.Gov.Enabled {
		t.Fatal("默认应开启治理订阅开关（地址为空时不订阅）")
	}
	if cfg.Gov.PollInterval != 5*time.Second {
		t.Fatalf("默认轮询间隔应为 5s，实际 %v", cfg.Gov.PollInterval)
	}
	if !strings.Contains(cfg.DB.DSN, "tenon.db") {
		t.Fatalf("默认 DSN 不对: %q", cfg.DB.DSN)
	}
}

// TestLoadMissingFileFails 显式指定的配置文件不存在时必须报错。
//
// 这里不能"落默认值继续跑"：运维把 -config 写错路径时，程序会以一个
// 完全不同的配置（默认 sqlite、默认端口、默认密钥）静默启动，
// 看起来正常，实际连的是另一个库。
func TestLoadMissingFileFails(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "not-exists.yaml")); err == nil {
		t.Fatal("指定的配置文件不存在时应报错")
	}
	// 空路径表示"只用默认值"，这是允许的。
	if _, err := Load(""); err != nil {
		t.Fatalf("空路径应落默认值: %v", err)
	}
}

// TestShippedConfigsLoad 仓库自带的配置必须能通过启动校验。
//
// 配置模板是运维的起点：模板本身就加载不了，等于把第一个坑埋在门口。
// 新增校验项时这条用例会立刻暴露"模板没跟上规则"。
func TestShippedConfigsLoad(t *testing.T) {
	paths, err := filepath.Glob("../../../configs/*.yaml")
	if err != nil {
		t.Fatalf("查找配置模板失败: %v", err)
	}
	if len(paths) == 0 {
		t.Skip("没有找到配置模板")
	}
	for _, path := range paths {
		name := filepath.Base(path)
		// oauth 示例文件是片段，只有 provider 段，不保证是完整配置；
		// 但它们同样必须能加载，所以不做例外。
		if _, err := Load(path); err != nil {
			t.Fatalf("%s 加载失败: %v", name, err)
		}
	}
}
