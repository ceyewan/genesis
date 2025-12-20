package clog

import "fmt"

// New 创建一个新的 Logger 实例
//
// config 为日志基本配置，opts 为函数式选项。
//
// 基本使用：
//
//	logger, _ := clog.New(&clog.Config{
//	    Level:  "info",
//	    Format: "console",
//	    Output: "stdout",
//	})
//
// 使用选项：
//
//	logger, _ := clog.New(&clog.Config{Level: "info"},
//	    clog.WithNamespace("my-service"),
//	    clog.WithStandardContext(),
//	)
//
// 参数：
//
//	config - 日志配置，如果为 nil 会使用默认配置
//	opts   - 函数式选项列表，用于命名空间、Context 字段等配置
//
// 返回：
//
//	Logger - 日志实例
//	error  - 配置验证错误
func New(config *Config, opts ...Option) (Logger, error) {
	if config == nil {
		config = &Config{}
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 应用选项
	options := applyOptions(opts...)

	// 调用内部实现
	return newLogger(config, options)
}
