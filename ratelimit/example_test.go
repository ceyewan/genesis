package ratelimit_test

import (
	"context"

	"github.com/ceyewan/genesis/ratelimit"
)

func Example() {
	limiter, err := ratelimit.New(&ratelimit.Config{Driver: ratelimit.DriverStandalone})
	if err != nil {
		return
	}
	defer limiter.Close()
	_, _ = limiter.Allow(context.Background(), "user:42", ratelimit.Limit{Rate: 10, Burst: 20})
}
