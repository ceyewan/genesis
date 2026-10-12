package idgen

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

func initCounter(meter metrics.Meter, logger clog.Logger, name, description string) metrics.Counter {
	counter, err := meter.Counter(name, description)
	if err == nil && counter != nil {
		return counter
	}
	if logger != nil {
		logger.Warn("Initialize ID counter failed, using discard", clog.String("metric", name), clog.Error(err))
	}
	counter, _ = metrics.Discard().Counter(name, description)
	return counter
}

// Metrics 指标常量定义
const (
	// MetricSnowflakeGenerated 雪花算法 ID 生成总数 (Counter)
	MetricSnowflakeGenerated = "idgen_snowflake_generated_total"

	// MetricSequenceGenerated 序列号生成总数 (Counter)
	MetricSequenceGenerated = "idgen_sequence_generated_total"
)
