package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
)

// NewNATSContainerConfig 使用 testcontainers 创建 NATS 容器并返回配置
// 生命周期由 t.Cleanup 管理
func NewNATSContainerConfig(t *testing.T) *connector.NATSConfig {
	ctx := context.Background()

	container, err := natscontainer.Run(ctx, "nats:2.10-alpine")
	require.NoError(t, err, "failed to start NATS container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "4222")
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	return &connector.NATSConfig{
		Name:          "testcontainer-nats",
		URL:           "nats://" + host + ":" + mappedPort.Port(),
		MaxReconnects: 10,
		ReconnectWait: 100 * time.Millisecond,
	}
}

// NewNATSContainerConnector 使用 testcontainers 创建并连接 NATS 连接器
// 生命周期由 t.Cleanup 管理
func NewNATSContainerConnector(t *testing.T) connector.NATSConnector {
	cfg := NewNATSContainerConfig(t)

	conn, err := connector.NewNATS(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create nats connector")

	err = conn.Connect(context.Background())
	require.NoError(t, err, "failed to connect to nats")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewNATSContainerConn 使用 testcontainers 创建并返回原生 NATS 连接
// 生命周期由 t.Cleanup 管理
func NewNATSContainerConn(t *testing.T) *nats.Conn {
	return NewNATSContainerConnector(t).GetClient()
}
