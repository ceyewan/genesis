package breaker_test

import (
	"context"

	"github.com/ceyewan/genesis/breaker"
)

func Example() {
	b, err := breaker.New(&breaker.Config{})
	if err != nil {
		return
	}
	_, _ = b.Execute(context.Background(), "payments", func() (any, error) {
		return "ok", nil
	})
}
