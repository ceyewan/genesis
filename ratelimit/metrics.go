package ratelimit

const (
	// MetricAllowed counts requests accepted by a limiter.
	MetricAllowed = "ratelimit_allowed_total"
	// MetricDenied counts requests rejected by a limiter.
	MetricDenied = "ratelimit_denied_total"
	// LabelMode identifies standalone and distributed implementations.
	LabelMode = "mode"
)
