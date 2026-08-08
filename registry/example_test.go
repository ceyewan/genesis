package registry_test

import (
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/registry"
)

func Example() {
	var conn connector.EtcdConnector
	_, _ = registry.New(conn, nil)
}
