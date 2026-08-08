package trace_test

import "github.com/ceyewan/genesis/trace"

func Example() {
	shutdown, err := trace.InstallLocalProvider("worker")
	if err != nil {
		return
	}
	_ = shutdown
}
