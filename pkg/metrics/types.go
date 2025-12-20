package metrics

import "context"

// Counter 累加器（如：请求数、错误数）
type Counter interface {
	// Inc 将计数器增加 1
	Inc(ctx context.Context, labels ...Label)
	// Add 将计数器增加给定的值
	Add(ctx context.Context, val float64, labels ...Label)
}

// Gauge 仪表盘（如：内存使用率、Goroutine 数量）
type Gauge interface {
	// Set 将 gauge 设置为给定的值
	Set(ctx context.Context, val float64, labels ...Label)
	// Inc 将 gauge 增加 1
	Inc(ctx context.Context, labels ...Label)
	// Dec 将 gauge 减少 1
	Dec(ctx context.Context, labels ...Label)
}

// Histogram 直方图（如：请求耗时分布）
type Histogram interface {
	// Record 在直方图中记录一个值
	Record(ctx context.Context, val float64, labels ...Label)
}

// Meter 是指标的创建工厂
type Meter interface {
	// Counter 创建累加器
	Counter(name string, desc string, opts ...MetricOption) (Counter, error)
	// Gauge 创建仪表盘
	Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)
	// Histogram 创建直方图
	Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)
	// Shutdown 关闭 Meter，刷新所有指标
	Shutdown(ctx context.Context) error
}

// MetricOption 定义指标的配置选项
type MetricOption func(*MetricOptions)

// MetricOptions 指标选项
type MetricOptions struct {
	Unit string
}

// WithUnit 设置指标的单位
func WithUnit(unit string) MetricOption {
	return func(o *MetricOptions) {
		o.Unit = unit
	}
}
