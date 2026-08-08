package metrics_test

import "github.com/ceyewan/genesis/metrics"

func Example() {
	meter := metrics.Discard()
	_, _ = meter.Counter("requests", "handled requests")
}
