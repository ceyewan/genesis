package registry_test

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/registry"
)

func Example() {
	ctx := context.Background()
	conn, err := connector.NewEtcd(&connector.EtcdConfig{Endpoints: []string{"localhost:2379"}})
	if err != nil {
		return
	}
	defer conn.Close()
	if err := conn.Connect(ctx); err != nil {
		return
	}
	reg, err := registry.New(conn, nil)
	if err != nil {
		return
	}
	defer reg.Close()
	_ = reg.Register(ctx, &registry.ServiceInstance{
		ID: "worker-1", Name: "worker", Endpoints: []string{"grpc://127.0.0.1:9000"},
	}, 30*time.Second)
}
