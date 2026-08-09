package config_test

import "github.com/ceyewan/genesis/config"

func Example() {
	loader, err := config.New(nil)
	if err != nil {
		return
	}
	defer loader.Close()
}
