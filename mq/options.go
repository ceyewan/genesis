package mq

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
)

// Option MQ 组件选项函数
type Option func(*options)

// options 选项结构（内部使用）
type options struct {
	Logger       clog.Logger
	Meter        metrics.Meter
	KafkaConfig  *connector.KafkaConfig // Kafka 配置（使用 Kafka Driver 时需要）
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 Namespace: logger.WithNamespace("mq")
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.Logger = l.WithNamespace("mq")
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.Meter = m
	}
}

// WithKafkaConfig 注入 Kafka 配置（使用 Kafka Driver 时必须提供）
func WithKafkaConfig(cfg *connector.KafkaConfig) Option {
	return func(o *options) {
		o.KafkaConfig = cfg
	}
}
