package ratelimit

// Metrics 指标常量定义
const (
	// MetricAllowTotal 限流检查总次数 (Counter)
	MetricAllowTotal = "ratelimit_allow_total"

	// MetricAllowed 允许通过的请求数 (Counter)
	MetricAllowed = "ratelimit_allowed_total"

	// MetricDenied 被拒绝的请求数 (Counter)
	MetricDenied = "ratelimit_denied_total"

	// MetricErrors 限流器错误数 (Counter)
	MetricErrors = "ratelimit_errors_total"

	// LabelMode 模式标签 (standalone/distributed)
	LabelMode = "mode"

	// LabelKey 限流键标签
	LabelKey = "key"

	// LabelErrorType 错误类型标签
	LabelErrorType = "error_type"
)
