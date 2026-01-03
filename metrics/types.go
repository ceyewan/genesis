package metrics

import "context"

// Counter 计数器接口，用于记录只能增加的累计值
type Counter interface {
	Inc(ctx context.Context, labels ...Label)
	Add(ctx context.Context, val float64, labels ...Label)
}

// Gauge 仪表盘接口，用于记录可以任意增减的瞬时值
type Gauge interface {
	Set(ctx context.Context, val float64, labels ...Label)
	Inc(ctx context.Context, labels ...Label)
	Dec(ctx context.Context, labels ...Label)
}

// Histogram 直方图接口，用于记录值的分布情况
type Histogram interface {
	Record(ctx context.Context, val float64, labels ...Label)
}

// Meter 指标创建工厂接口
type Meter interface {
	Counter(name string, desc string, opts ...MetricOption) (Counter, error)
	Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)
	Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)
	Shutdown(ctx context.Context) error
}

// MetricOption 指标配置选项
type MetricOption func(*metricOptions)

// metricOptions 指标选项（内部使用）
type metricOptions struct {
	Unit    string
	Buckets []float64
}

// WithUnit 设置指标的单位
func WithUnit(unit string) MetricOption {
	return func(o *metricOptions) {
		o.Unit = unit
	}
}

// WithBuckets 设置直方图的桶分布
func WithBuckets(buckets []float64) MetricOption {
	return func(o *metricOptions) {
		o.Buckets = buckets
	}
}
