package metrics

// Config 指标系统的配置结构体
// 用于控制指标系统的启用状态、服务标识和 Prometheus 暴露配置
//
// 这个结构体支持 mapstructure 标签，可以从配置文件中加载：
//
//	cfg := &metrics.Config{}
//	viper.UnmarshalKey("metrics", cfg)
//
// 典型配置示例（YAML）：
//
//	metrics:
//	  enabled: true
//	  service_name: "user-service"
//	  version: "v1.2.3"
//	  port: 9090
//	  path: "/metrics"
type Config struct {
	// Enabled 是否启用指标收集
	// 为 false 时，metrics.New() 会返回 noop Meter，所有操作都是空操作
	// 为 true 时，会启动完整的 OpenTelemetry + Prometheus 指标收集
	Enabled bool `mapstructure:"enabled"`

	// ServiceName 服务名称，用于标识指标的来源
	// 这个值会作为 OpenTelemetry Resource 的 service.name 属性
	// 建议使用有意义的服务名，如 "user-service"、"order-api" 等
	ServiceName string `mapstructure:"service_name"`

	// Version 服务版本，用于标识服务的版本信息
	// 这个值会作为 OpenTelemetry Resource 的 service.version 属性
	// 在监控面板中可以帮助区分不同版本的指标表现
	Version string `mapstructure:"version"`

	// Port Prometheus HTTP 服务器监听的端口
	// 如果设置大于 0，会启动 HTTP 服务器用于暴露 Prometheus 格式的指标
	// 常用端口：9090（默认值）、8080、8081 等
	// 注意：确保端口没有被其他服务占用
	Port int `mapstructure:"port"`

	// Path Prometheus 指标的 HTTP 路径
	// Prometheus 服务器会通过这个路径采集指标数据
	// 常用路径："/metrics"（默认值）、"/api/metrics" 等
	// 注意：路径必须以 "/" 开头
	Path string `mapstructure:"path"`
}
