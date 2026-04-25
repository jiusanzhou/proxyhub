// Package config 定义 proxyhub 的全局配置
//
// 字段 tag 约定：
//   - opts  : cli flag/env 自动生成 (go.zoe.im/x/cli/opts)
//   - json  : 配置文件 key（yaml/toml/json 都转 json 后解码）
//
// 同一份 struct 同时满足：
//   - 命令行 flag 自动注册（cli.GlobalConfig + cli.WithAutoFlags）
//   - 配置文件加载（yaml/toml/json）
//   - 环境变量覆盖
package config

import (
	"time"

	"go.zoe.im/x"
)

// Config proxyhub 的完整配置
//
// 启动方式（优先级 flag > env > config file > defaults）：
//
//	proxyhub serve --config /etc/proxyhub.yaml   # 配置文件
//	PROXY_PORT=8000 proxyhub serve               # 环境变量
//	proxyhub serve --proxy-port 8000             # 命令行 flag
//
// 简单模式（默认 SQLite + Proxifly，不写配置文件即可）：
//
//	proxyhub serve --db /var/lib/proxyhub.db
//
// 高级模式（配置文件指定 store / sources）:
//
//	store:
//	  type: postgres
//	  config:
//	    dsn: postgres://user:pass@localhost:5432/proxyhub?sslmode=disable
//
//	sources:
//	  - type: proxifly
//	  - name: my-list
//	    type: text
//	    config:
//	      url: https://example.com/proxies.txt
//	      protocol: http
type Config struct {
	// 端口
	ProxyPort int    `opts:"env, help=HTTP 前向代理端口"                json:"proxy_port"`
	APIPort   int    `opts:"env, help=REST API + Prometheus 端口"     json:"api_port"`
	DB        string `opts:"env, help=SQLite 数据库路径 (兼容旧版本)"            json:"db,omitempty"`
	LogLevel  string `opts:"env, help=日志级别 debug/info/warn/error"   json:"log_level"`

	// 刷新 / 冷却
	RefreshInterval time.Duration `opts:"env, help=代理池刷新间隔"        json:"refresh_interval"`
	FailCooldown    time.Duration `opts:"env, help=失败代理冷却时间"      json:"fail_cooldown"`

	// 额外源（向后兼容字符串格式: "name=url:proto;name2=url2:proto2"）
	// 推荐使用 Sources 字段（结构化）
	ExtraSource string `opts:"env, help=额外文本订阅源 (name=url:proto; 多个用分号分隔)" json:"extra_source,omitempty"`

	// 健康探测
	CheckEnabled     bool          `opts:"name=check, env, help=启用后台健康探测"                    json:"check_enabled"`
	CheckInterval    time.Duration `opts:"env, help=健康探测整轮间隔"                                 json:"check_interval"`
	CheckDialTimeout time.Duration `opts:"env, help=L4 TCP dial 超时"                             json:"check_dial_timeout"`
	CheckHTTPTimeout time.Duration `opts:"env, help=L7 HTTP CONNECT 探测超时"                      json:"check_http_timeout"`
	CheckConcurrency int           `opts:"env, help=健康探测并发度"                                   json:"check_concurrency"`
	CheckL7          bool          `opts:"env, help=启用 L7 HTTP CONNECT 探测 (对目标 host 有压力)"   json:"check_l7"`
	CheckTarget      string        `opts:"env, help=L7 探测目标 host:port"                         json:"check_target"`
	CheckBanOnFail   int           `opts:"env, help=连续探测失败多少次标记 banned"                      json:"check_ban_on_fail"`

	// 高级配置：store + sources（仅配置文件支持，命令行通过 --db / --extra-source 兼容）
	Store   x.TypedLazyConfig    `opts:"-" json:"store,omitempty"`
	Sources x.TypedLazyConfigs   `opts:"-" json:"sources,omitempty"`
}

// Global 单例配置。启动时由 cli.GlobalConfig() 自动填充
var Global = NewConfig()

// NewConfig 默认值
func NewConfig() *Config {
	return &Config{
		ProxyPort:        7000,
		APIPort:          7001,
		DB:               "./proxyhub.db",
		LogLevel:         "info",
		RefreshInterval:  10 * time.Minute,
		FailCooldown:     5 * time.Minute,
		CheckEnabled:     true,
		CheckInterval:    60 * time.Second,
		CheckDialTimeout: 5 * time.Second,
		CheckHTTPTimeout: 8 * time.Second,
		CheckConcurrency: 50,
		CheckL7:          false,
		CheckTarget:      "httpbin.org:80",
		CheckBanOnFail:   3,
	}
}
