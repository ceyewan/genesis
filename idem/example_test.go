package idem_test

import (
	"context"

	"github.com/ceyewan/genesis/idem"
)

func Example() {
	component, err := idem.New(&idem.Config{Driver: idem.DriverMemory})
	if err != nil {
		return
	}
	defer component.Close()
	_, _ = component.Execute(context.Background(), "create-order:42", func(context.Context) (any, error) {
		return "order-42", nil
	})
}
