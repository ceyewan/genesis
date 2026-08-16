package clog

import "github.com/ceyewan/genesis/xerrors"

// New 创建一个新的 Logger 实例
//
// config - 日志配置，如果为 nil 则等同于 &Config{}，
// 使用零值默认配置：Level=info、Format=console、Output=stdout
// opts   - 函数式选项列表，用于命名空间、Context 字段等配置
//
// 返回的 Logger 可能持有底层文件句柄；当 Output 为文件路径时，
// 调用方应在使用完成后执行根 logger 的 Close() 释放资源。
func New(config *Config, opts ...Option) (Logger, error) {
	if config == nil {
		config = &Config{}
	} else {
		configCopy := *config
		config = &configCopy
	}

	if err := config.validate(); err != nil {
		return nil, xerrors.Wrap(err, "invalid config")
	}

	// 应用选项
	options := applyOptions(opts...)
	if err := validateOptions(options); err != nil {
		return nil, err
	}

	// 调用内部实现
	return newLogger(config, options)
}
