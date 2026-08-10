package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	natscontainer "github.com/testcontainers/testcontainers-go/modules/nats"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ceyewan/genesis/connector"
)

// NewNATSContainerConfig 使用 testcontainers 创建 NATS 容器并返回配置。
// 生命周期由 t.Cleanup 管理。
func NewNATSContainerConfig(t *testing.T) *connector.NATSConfig {
	t.Helper()
	_, cfg := NewNATSContainer(t)
	return cfg
}

// NewNATSContainer 返回容器和对应连接配置，供重连与恢复测试控制容器生命周期。
// 容器最终清理由 t.Cleanup 负责。
func NewNATSContainer(t *testing.T) (*natscontainer.NATSContainer, *connector.NATSConfig) {
	t.Helper()
	RequireDocker(t)

	ctx := context.Background()

	container, err := natscontainer.Run(ctx, "nats:2.10-alpine",
		testcontainers.WithWaitStrategy(
			wait.ForLog("Server is ready").WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err, "failed to start NATS container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "4222")
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	return container, &connector.NATSConfig{
		Name:          "testcontainer-nats",
		URL:           "nats://" + host + ":" + mappedPort.Port(),
		MaxReconnects: 10,
		ReconnectWait: 100 * time.Millisecond,
	}
}

// NewNATSContainerConnector 使用 testcontainers 创建并连接 NATS 连接器。
// 生命周期由 t.Cleanup 管理。
func NewNATSContainerConnector(t *testing.T) connector.NATSConnector {
	t.Helper()

	cfg := NewNATSContainerConfig(t)

	conn, err := connector.NewNATS(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create nats connector")

	connectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for {
		err = conn.Connect(connectCtx)
		if err == nil {
			break
		}
		select {
		case <-connectCtx.Done():
			require.NoError(t, err, "failed to connect to ready nats container before deadline")
		case <-time.After(100 * time.Millisecond):
		}
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewNATSContainerConn 使用 testcontainers 创建并返回原生 NATS 连接。
// 生命周期由 t.Cleanup 管理。
func NewNATSContainerConn(t *testing.T) *nats.Conn {
	t.Helper()
	return NewNATSContainerConnector(t).GetClient()
}
