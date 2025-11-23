package idempotency

import (
	"github.com/ceyewan/genesis/pkg/clog"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Option 组件初始化选项函数
type Option func(*Options)

// Options 组件初始化选项配置
type Options struct {
	Logger clog.Logger
	Meter  telemetrytypes.Meter
	Tracer telemetrytypes.Tracer
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

// WithMeter 设置 Meter
func WithMeter(meter telemetrytypes.Meter) Option {
	return func(o *Options) {
		o.Meter = meter
	}
}

// WithTracer 设置 Tracer
func WithTracer(tracer telemetrytypes.Tracer) Option {
	return func(o *Options) {
		o.Tracer = tracer
	}
}

