package config

import (
	"context"
)

// New 创建一个新的配置加载器实例
func New(opts ...Option) (Loader, error) {
	return newLoader(opts...)
}

// MustLoad 创建 Loader 实例并立即加载配置，出错时 panic（仅用于初始化）
func MustLoad(opts ...Option) Loader {
	loader, err := New(opts...)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	if err := loader.Load(ctx); err != nil {
		panic(err)
	}

	return loader
}
