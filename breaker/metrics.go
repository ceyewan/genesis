package breaker

// Metrics 指标常量定义
const (
	// MetricRequestsTotal 请求总数 (Counter)
	MetricRequestsTotal = "breaker_requests_total"

	// MetricSuccessTotal 成功请求数 (Counter)
	MetricSuccessTotal = "breaker_success_total"

	// MetricFailuresTotal 失败请求数 (Counter)
	MetricFailuresTotal = "breaker_failures_total"

	// MetricRejectsTotal 被熔断拒绝的请求数 (Counter)
	MetricRejectsTotal = "breaker_rejects_total"

	// MetricStateChanges 状态变更次数 (Counter)
	MetricStateChanges = "breaker_state_changes_total"

	// MetricRequestDuration 请求耗时 (Histogram)
	MetricRequestDuration = "breaker_request_duration_seconds"

	// LabelService 服务名标签
	LabelService = "service"

	// LabelState 状态标签
	LabelState = "state"

	// LabelFromState 源状态标签
	LabelFromState = "from_state"

	// LabelToState 目标状态标签
	LabelToState = "to_state"

	// LabelMethod gRPC 方法标签
	LabelMethod = "method"

	// LabelResult 结果标签 (success/failure)
	LabelResult = "result"
)

