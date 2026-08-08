package breaker

const (
	// MetricExecutions 是 breaker 执行结果计数器名称。
	MetricExecutions = "genesis_breaker_executions_total"
	// MetricRejections 是 breaker 拒绝计数器名称。
	MetricRejections = "genesis_breaker_rejections_total"
	// MetricState 是 breaker 当前状态 gauge 名称。
	MetricState = "genesis_breaker_state"
)
