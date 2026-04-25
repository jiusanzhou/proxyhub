// SQLite store 工厂注册
package store

import (
	"fmt"

	"go.zoe.im/x"
)

func sqliteCreator(cfg x.TypedLazyConfig, opts ...Option) (Store, error) {
	var config struct {
		Path string `json:"path"`
	}
	if err := cfg.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("sqlite config unmarshal: %w", err)
	}
	if config.Path == "" {
		config.Path = "./proxyhub.db"
	}
	return NewSQLite(config.Path)
}

func init() {
	_ = Register("sqlite", sqliteCreator, "sqlite3")
}
