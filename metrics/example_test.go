package metrics_test

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/metrics"
)

func Example() {
	meter, err := metrics.New(&metrics.Config{ServiceName: "worker"})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer meter.Shutdown(ctx)
	counter, err := meter.Counter("requests", "handled requests")
	if err != nil {
		return
	}
	counter.Inc(context.Background())
}
