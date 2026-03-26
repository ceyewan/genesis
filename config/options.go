package config

import "github.com/ceyewan/genesis/clog"

// Option 定义 Loader 的可选配置。
type Option func(*loader)

// WithLogger 为 Loader 注入日志器。
//
// 当配置热更新失败时，config 会通过该日志器输出告警，帮助调用方定位读取失败、
// 合并失败或校验失败等问题。未注入时默认使用 clog.Discard()。
func WithLogger(logger clog.Logger) Option {
	return func(l *loader) {
		if logger != nil {
			l.logger = logger
		}
	}
}
