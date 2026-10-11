package connector

import (
	"crypto/tls"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

type options struct {
	logger   clog.Logger
	meter    metrics.Meter
	redisTLS *tls.Config
}

// Option 配置连接器的选项
type Option func(*options)

// applyDefaults 确保未设置的选项使用默认值
func (o *options) applyDefaults() {
	if o.logger == nil {
		o.logger = clog.Discard()
	}
	if o.meter == nil {
		o.meter = metrics.Discard()
	}
}

// WithRedisTLSConfig 为 Redis 注入 TLS 配置，nil 表示使用默认明文连接。
// 创建 Connector 时复制配置和 CA 池；证书内容、私钥与回调仍遵守标准库不可变约束。
func WithRedisTLSConfig(cfg *tls.Config) Option {
	return func(o *options) {
		if cfg == nil {
			o.redisTLS = nil
			return
		}
		o.redisTLS = cfg.Clone()
		if cfg.RootCAs != nil {
			o.redisTLS.RootCAs = cfg.RootCAs.Clone()
		}
		if cfg.ClientCAs != nil {
			o.redisTLS.ClientCAs = cfg.ClientCAs.Clone()
		}
		if cfg.Certificates != nil {
			o.redisTLS.Certificates = append([]tls.Certificate(nil), cfg.Certificates...)
		}
	}
}

// WithMeter 注入 connector 内部健康检查和重连指标。
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		if meter == nil {
			meter = metrics.Discard()
		}
		o.meter = meter
	}
}

func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger == nil {
			logger = clog.Discard()
		}
		o.logger = logger.WithNamespace("connector")
	}
}
