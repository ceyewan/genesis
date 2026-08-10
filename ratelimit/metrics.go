package ratelimit

import (
	"context"

	"github.com/ceyewan/genesis/metrics"
)

const (
	// MetricAllowed counts requests accepted by a limiter.
	MetricAllowed = "ratelimit_allowed_total"
	// MetricDenied counts requests rejected by a limiter.
	MetricDenied = "ratelimit_denied_total"
	// MetricErrors counts limiter operation errors.
	MetricErrors = "ratelimit_errors_total"
	// LabelMode identifies standalone and distributed implementations.
	LabelMode = "mode"
)

func recordLimiterError(ctx context.Context, counter metrics.Counter, mode string) {
	if counter != nil {
		counter.Inc(ctx, metrics.L(LabelMode, mode))
	}
}
