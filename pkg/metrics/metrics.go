package metrics

import "context"

// Counter records monotonically increasing values.
type Counter interface {
	Inc(delta float64)
}

// Gauge records instantaneous values.
type Gauge interface {
	Set(value float64)
	Add(delta float64)
}

// Histogram records observations into buckets.
type Histogram interface {
	Observe(value float64)
}

// Timer measures execution duration around a block.
type Timer interface {
	Observe(ctx context.Context, fn func(context.Context))
}

// Provider exposes metric creation helpers scoped by module/service labels.
type Provider interface {
	Counter(name string, opts ...Option) (Counter, error)
	Gauge(name string, opts ...Option) (Gauge, error)
	Histogram(name string, opts ...Option) (Histogram, error)
	Timer(name string, opts ...Option) (Timer, error)
}

// Option configures a metric instrument.
type Option interface{}
