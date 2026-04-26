// D1 store 工厂注册
package store

import (
	"context"
	"fmt"

	"go.zoe.im/x"
)

func d1Creator(cfg x.TypedLazyConfig, opts ...Option) (Store, error) {
	var c D1Config
	if err := cfg.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("d1 config unmarshal: %w", err)
	}
	return NewD1(context.Background(), c)
}

func init() {
	_ = Register("d1", d1Creator)
}
