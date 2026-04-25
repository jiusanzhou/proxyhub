// Package source 提供代理源工厂注册
//
// 各代理源实现在 init() 时注册到工厂，
// 启动时通过配置的 Type 字段动态创建实例。
//
// 配置示例（YAML）:
//
//	sources:
//	  - type: proxifly
//	  - type: text
//	    config:
//	      url: https://example.com/proxies.txt
//	      protocol: http
package source

import (
	"fmt"

	"go.zoe.im/x"
	"go.zoe.im/x/factory"

	"go.zoe.im/proxyhub/internal/pool"
)

// Option 源创建可选项（暂时无内容，预留扩展）
type Option func(any)

var (
	// Factory 代理源工厂
	Factory = factory.NewFactory[pool.Source, Option]()
)

// Create 从配置创建代理源
func Create(cfg x.TypedLazyConfig, opts ...Option) (pool.Source, error) {
	return Factory.Create(cfg, opts...)
}

// Register 注册代理源（各源 init 时调用）
func Register(name string, creator factory.Creator[pool.Source, Option], alias ...string) error {
	return Factory.Register(name, creator, alias...)
}

// proxiflyCreator Proxifly 源创建
func proxiflyCreator(cfg x.TypedLazyConfig, opts ...Option) (pool.Source, error) {
	// Proxifly 无需额外配置
	return NewProxifly(), nil
}

// textCreator 文本源创建
func textCreator(cfg x.TypedLazyConfig, opts ...Option) (pool.Source, error) {
	var config struct {
		URL      string `json:"url"`
		Protocol string `json:"protocol"`
	}
	if err := cfg.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("text source unmarshal: %w", err)
	}
	if config.URL == "" {
		return nil, fmt.Errorf("text source missing url")
	}
	if config.Protocol == "" {
		config.Protocol = "http"
	}
	name := cfg.Name
	if name == "" {
		name = "custom-text"
	}
	return NewText(name, config.URL, pool.Protocol(config.Protocol)), nil
}

func init() {
	_ = Register("proxifly", proxiflyCreator)
	_ = Register("text", textCreator)
}
