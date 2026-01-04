package auth

// Auth 组件导出的指标名称常量。
//
// 详细说明和 Prometheus 查询示例请参考 README.md。

const (
	// MetricTokensValidated Token 验证计数，标签: status, error_type
	MetricTokensValidated = "auth_tokens_validated_total"

	// MetricTokensRefreshed Token 刷新计数，标签: status
	MetricTokensRefreshed = "auth_tokens_refreshed_total"
)
