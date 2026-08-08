package idem_test

import "github.com/ceyewan/genesis/idem"

func Example() {
	component, err := idem.New(&idem.Config{Driver: idem.DriverMemory})
	if err != nil {
		return
	}
	defer component.Close()
}
