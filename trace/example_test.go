package trace_test

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/trace"
)

func Example() {
	shutdown, err := trace.InstallLocalProvider("worker")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdown(ctx)
}
