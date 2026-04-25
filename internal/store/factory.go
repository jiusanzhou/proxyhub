// Package store 提供代理持久化的工厂注册
//
// 各 store 实现在 init() 时注册到工厂，
// 启动时通过配置的 Type 字段动态创建实例。
//
// 配置示例（YAML）:
//
//	store:
//	  type: sqlite
//	  config:
//	    path: /var/lib/proxyhub.db
//
//	# 或：
//	store:
//	  type: postgres
//	  config:
//	    dsn: postgres://user:pass@localhost:5432/proxyhub?sslmode=disable
package store

import (
	"go.zoe.im/x"
	"go.zoe.im/x/factory"

	"go.zoe.im/proxyhub/internal/pool"
)

// Store 代理持久化接口（pool.Store + Close）
type Store interface {
	pool.Store
	Close() error
}

// Option store 创建可选项（预留扩展）
type Option func(any)

var (
	// Factory store 工厂
	Factory = factory.NewFactory[Store, Option]()
)

// Create 从配置创建 store
func Create(cfg x.TypedLazyConfig, opts ...Option) (Store, error) {
	return Factory.Create(cfg, opts...)
}

// Register 注册 store 实现（各实现 init 时调用）
func Register(name string, creator factory.Creator[Store, Option], alias ...string) error {
	return Factory.Register(name, creator, alias...)
}
