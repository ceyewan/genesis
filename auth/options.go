package auth

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

type Option func(*options)

func defaultOptions() *options {
	return &options{
		logger: nilLogger{},
		meter:  nil,
	}
}

// nilLogger 无操作的日志实现
type nilLogger struct{}

func (n nilLogger) Debug(msg string, fields ...clog.Field)                             {}
func (n nilLogger) Info(msg string, fields ...clog.Field)                              {}
func (n nilLogger) Warn(msg string, fields ...clog.Field)                              {}
func (n nilLogger) Error(msg string, fields ...clog.Field)                             {}
func (n nilLogger) Fatal(msg string, fields ...clog.Field)                             {}
func (n nilLogger) DebugContext(ctx context.Context, msg string, fields ...clog.Field) {}
func (n nilLogger) InfoContext(ctx context.Context, msg string, fields ...clog.Field)  {}
func (n nilLogger) WarnContext(ctx context.Context, msg string, fields ...clog.Field)  {}
func (n nilLogger) ErrorContext(ctx context.Context, msg string, fields ...clog.Field) {}
func (n nilLogger) FatalContext(ctx context.Context, msg string, fields ...clog.Field) {}
func (n nilLogger) With(fields ...clog.Field) clog.Logger                              { return n }
func (n nilLogger) WithNamespace(parts ...string) clog.Logger                          { return n }
func (n nilLogger) SetLevel(level clog.Level) error                                    { return nil }
func (n nilLogger) Flush()                                                             {}

// WithLogger 注入 Logger
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("auth")
		}
	}
}

// WithMeter 注入 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.meter = m
	}
}

// GetCounter 获取 Counter 指标
func (o *options) GetCounter(name, desc string) metrics.Counter {
	if o.meter == nil {
		return nil
	}
	counter, _ := o.meter.Counter(name, desc)
	return counter
}

// GetHistogram 获取 Histogram 指标
func (o *options) GetHistogram(name, desc string) metrics.Histogram {
	if o.meter == nil {
		return nil
	}
	histogram, _ := o.meter.Histogram(name, desc)
	return histogram
}
