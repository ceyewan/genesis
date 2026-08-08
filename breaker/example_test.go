package breaker_test

import "github.com/ceyewan/genesis/breaker"

func Example() {
	_, _ = breaker.New(&breaker.Config{})
}
