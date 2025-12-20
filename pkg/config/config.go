package config

import (
	"context"
)

// New 创建一个新的配置加载器实例
// 推荐使用此函数而非 NewLoader，以保持简洁的 API
func New(opts ...Option) (Loader, error) {
	return NewLoader(opts...)
}

// Load 创建 Loader 实例并立即加载配置
func Load(opts ...Option) (Loader, error) {
	loader, err := New(opts...)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	if err := loader.Load(ctx); err != nil {
		return nil, err
	}

	return loader, nil
}

// MustLoad 类似 Load，但出错时 panic（仅用于初始化）
func MustLoad(opts ...Option) Loader {
	loader, err := Load(opts...)
	if err != nil {
		panic(err)
	}
	return loader
}
