package testkit_test

import "github.com/ceyewan/genesis/internal/testkit"

func Example() {
	_ = testkit.NewID()
	_ = testkit.NewMeter()
}
