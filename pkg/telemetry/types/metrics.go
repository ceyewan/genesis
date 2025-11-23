package types

import "context"

// Meter 是指标的创建工厂
type Meter interface {
	Counter(name string, desc string, opts ...MetricOption) (Counter, error)
	Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)
	Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)
}

// Counter 累加器 (如：请求数、错误数)
type Counter interface {
	Inc(ctx context.Context, labels ...Label)
	Add(ctx context.Context, val float64, labels ...Label)
}

// Gauge 仪表盘 (如：内存使用率、Goroutine 数量)
type Gauge interface {
	Set(ctx context.Context, val float64, labels ...Label)
	Record(ctx context.Context, val float64, labels ...Label) // 同 Set
}

// Histogram 直方图 (如：请求耗时分布)
type Histogram interface {
	Record(ctx context.Context, val float64, labels ...Label)
}

// Label 定义指标的维度
type Label struct {
	Key   string
	Value string
}

// MetricOption 定义指标的配置选项
type MetricOption func(*MetricOptions)

type MetricOptions struct {
	Unit string
}

func WithUnit(unit string) MetricOption {
	return func(o *MetricOptions) {
		o.Unit = unit
	}
}
