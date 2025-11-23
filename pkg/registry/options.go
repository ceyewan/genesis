package registry

import (
	"github.com/ceyewan/genesis/pkg/clog"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 选项结构
type options struct {
	logger clog.Logger
	meter  telemetrytypes.Meter
	tracer telemetrytypes.Tracer
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 "registry" namespace
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("registry")
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m telemetrytypes.Meter) Option {
	return func(o *options) {
		o.meter = m
	}
}

// WithTracer 注入 Tracer
func WithTracer(t telemetrytypes.Tracer) Option {
	return func(o *options) {
		o.tracer = t
	}
}

// defaultOptions 返回默认选项
func defaultOptions() *options {
	return &options{
		logger: clog.Default(),
	}
}
