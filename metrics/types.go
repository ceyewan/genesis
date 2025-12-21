// Package metrics 为 Genesis 框架提供统一的指标收集能力。
// 基于 OpenTelemetry 标准构建，提供简洁的 Counter、Gauge、Histogram 指标接口。
//
// 架构说明：
//   - 属于 Genesis 四层架构中的 L0（Base）层
//   - 完全扁平化设计，无 types/ 子包
//   - 基于 OpenTelemetry 标准，确保与云原生生态兼容
//   - 内置 Prometheus HTTP 服务器，支持指标自动暴露
//
// 快速开始：
//
//	cfg := &metrics.Config{
//	    Enabled:     true,
//	    ServiceName: "my-service",
//	    Version:     "v1.0.0",
//	    Port:        9090,
//	    Path:        "/metrics",
//	}
//
//	meter, err := metrics.New(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer meter.Shutdown(ctx)
//
//	// 创建指标
//	counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
//	histogram, _ := meter.Histogram("request_duration_seconds", "请求耗时（秒）")
//
// 使用示例：
//
//	// 带标签增加计数器
//	counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
//
//	// 记录直方图值
//	histogram.Record(ctx, 0.123, metrics.L("endpoint", "/api/users"))
package metrics

import "context"

// Counter 计数器接口
// 用于记录只能增加的累计值，例如 HTTP 请求数、错误次数、订单创建数等
//
// 典型使用场景：
//   - HTTP 请求总数：Counter("http_requests_total", "HTTP 请求总数")
//   - 错误发生次数：Counter("errors_total", "错误发生总数")
//   - 用户注册数量：Counter("user_registrations_total", "用户注册总数")
//
// 使用示例：
//
//	counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
//	// 增加 1
//	counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
//	// 增加指定值
//	counter.Add(ctx, 5, metrics.L("endpoint", "/api/batch"))
type Counter interface {
	// Inc 将计数器增加 1
	//
	// 参数：
	//   ctx    - 上下文，用于传递截止时间、取消信号等
	//   labels - 可选的标签，用于指标分组和筛选
	Inc(ctx context.Context, labels ...Label)

	// Add 将计数器增加给定的值
	// 注意：如果传入负数，大部分监控系统会忽略或报错
	//
	// 参数：
	//   ctx    - 上下文，用于传递截止时间、取消信号等
	//   val    - 要增加的值，通常为正数
	//   labels - 可选的标签，用于指标分组和筛选
	Add(ctx context.Context, val float64, labels ...Label)
}

// Gauge 仪表盘接口
// 用于记录可以任意增减的瞬时值，例如内存使用率、连接数、队列长度等
//
// 典型使用场景：
//   - 内存使用率：Gauge("memory_usage_bytes", "内存使用字节数")
//   - 活跃连接数：Gauge("active_connections", "当前活跃连接数")
//   - 队列长度：Gauge("queue_size", "待处理队列长度")
//
// 使用示例：
//
//	gauge, _ := meter.Gauge("memory_usage_bytes", "内存使用字节数")
//	// 设置具体值
//	gauge.Set(ctx, 1024*1024*100, metrics.L("type", "heap"))
//	// 增加 1
//	gauge.Inc(ctx, metrics.L("node", "worker1"))
//	// 减少 1
//	gauge.Dec(ctx, metrics.L("node", "worker1"))
type Gauge interface {
	// Set 将 gauge 设置为给定的值
	// 会覆盖之前的值
	//
	// 参数：
	//   ctx    - 上下文，用于传递截止时间、取消信号等
	//   val    - 要设置的值，可以是任意浮点数
	//   labels - 可选的标签，用于指标分组和筛选
	Set(ctx context.Context, val float64, labels ...Label)

	// Inc 将 gauge 增加 1
	// 等价于 Set(currentValue + 1)
	//
	// 参数：
	//   ctx    - 上下文，用于传递截止时间、取消信号等
	//   labels - 可选的标签，用于指标分组和筛选
	Inc(ctx context.Context, labels ...Label)

	// Dec 将 gauge 减少 1
	// 等价于 Set(currentValue - 1)
	//
	// 参数：
	//   ctx    - 上下文，用于传递截止时间、取消信号等
	//   labels - 可选的标签，用于指标分组和筛选
	Dec(ctx context.Context, labels ...Label)
}

// Histogram 直方图接口
// 用于记录值的分布情况，例如请求耗时、响应大小、延迟分布等
// 直方图会自动计算分位数（如 P95、P99）和总计数值
//
// 典型使用场景：
//   - HTTP 请求耗时：Histogram("http_request_duration_seconds", "HTTP 请求耗时")
//   - 数据库查询时间：Histogram("db_query_duration_ms", "数据库查询耗时")
//   - 响应大小：Histogram("response_size_bytes", "响应数据大小")
//
// 使用示例：
//
//	histogram, _ := meter.Histogram(
//	    "http_request_duration_seconds",
//	    "HTTP 请求耗时",
//	    metrics.WithUnit("s"),
//	)
//	histogram.Record(ctx, 0.123, metrics.L("method", "GET"), metrics.L("endpoint", "/api/users"))
type Histogram interface {
	// Record 在直方图中记录一个值
	// 该值会被自动归类到相应的桶中，用于计算分位数
	//
	// 参数：
	//   ctx    - 上下文，用于传递截止时间、取消信号等
	//   val    - 要记录的值，必须为正数
	//   labels - 可选的标签，用于指标分组和筛选
	Record(ctx context.Context, val float64, labels ...Label)
}

// Meter 指标创建工厂接口
// 是所有指标类型的创建入口，负责管理指标的生命周期
//
// 一个 Meter 实例通常对应一个服务，通过 Meter 创建的指标会自动关联到该服务
// Meter 创建的指标是线程安全的，可以在多个 goroutine 中并发使用
type Meter interface {
	// Counter 创建计数器实例
	//
	// 参数：
	//   name - 指标名称，应该符合 Prometheus 命名规范（如：http_requests_total）
	//   desc - 指标描述，用于说明指标的用途和含义
	//   opts - 指标选项，目前支持 WithUnit 设置单位
	//
	// 返回：
	//   Counter - 计数器实例
	//   error   - 创建过程中的错误
	Counter(name string, desc string, opts ...MetricOption) (Counter, error)

	// Gauge 创建仪表盘实例
	//
	// 参数：
	//   name - 指标名称，应该符合 Prometheus 命名规范（如：memory_usage_bytes）
	//   desc - 指标描述，用于说明指标的用途和含义
	//   opts - 指标选项，目前支持 WithUnit 设置单位
	//
	// 返回：
	//   Gauge - 仪表盘实例
	//   error - 创建过程中的错误
	Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)

	// Histogram 创建直方图实例
	//
	// 参数：
	//   name - 指标名称，应该符合 Prometheus 命名规范（如：http_request_duration_seconds）
	//   desc - 指标描述，用于说明指标的用途和含义
	//   opts - 指标选项，目前支持 WithUnit 设置单位
	//
	// 返回：
	//   Histogram - 直方图实例
	//   error     - 创建过程中的错误
	Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)

	// Shutdown 关闭 Meter，刷新所有指标
	// 调用此方法后，Meter 将不再接受新的指标记录请求
	// 通常在应用程序退出时调用
	//
	// 参数：
	//   ctx - 上下文，用于控制关闭操作的超时
	//
	// 返回：
	//   error - 关闭过程中的错误
	Shutdown(ctx context.Context) error
}

// MetricOption 指标配置选项函数类型
// 用于在创建指标时进行额外配置，例如设置单位等
type MetricOption func(*MetricOptions)

// MetricOptions 指标选项结构体
// 存储指标的配置信息，这个结构体会在指标创建时被使用
type MetricOptions struct {
	// Unit 指标的单位，例如 "bytes"、"seconds"、"requests"
	// 建议使用 UCUM 单位代码：https://unitsofmeasure.org/ucum.html
	// 常用单位：bytes（字节）、seconds（秒）、requests（请求数）、errors（错误数）
	Unit string
}

// WithUnit 设置指标的单位
// 用于明确指标的计量单位，帮助用户理解指标含义
//
// 参数：
//
//	unit - 单位字符串，应该使用标准单位代码
//
// 使用示例：
//
//	histogram, _ := meter.Histogram(
//	    "response_time_seconds",
//	    "响应时间",
//	    metrics.WithUnit("seconds"), // 设置单位为秒
//	)
//
// 返回：
//
//	MetricOption - 可用于指标创建的选项函数
func WithUnit(unit string) MetricOption {
	return func(o *MetricOptions) {
		o.Unit = unit
	}
}
