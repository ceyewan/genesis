package types

// Config 是遥测系统的配置。
type Config struct {
	ServiceName          string  // ServiceName 是用于遥测数据识别的服务名称。
	ExporterType         string  // ExporterType 指定追踪导出器类型（"otlp"、"zipkin"、"stdout"）。
	ExporterEndpoint     string  // ExporterEndpoint 是追踪导出器的端点 URL。
	PrometheusListenAddr string  // PrometheusListenAddr 是用于 Prometheus 指标抓取的监听地址。如果为空，则禁用 Prometheus 导出器。
	SamplerType          string  // SamplerType 定义追踪采样策略（"always_on"、"always_off"、"trace_id_ratio"）。
	SamplerRatio         float64 // SamplerRatio 是当 SamplerType 为 "trace_id_ratio" 时的采样概率。
}
