package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	etcdcontainer "github.com/testcontainers/testcontainers-go/modules/etcd"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/ceyewan/genesis/connector"
)

// NewEtcdContainerConfig 使用 testcontainers 创建 Etcd 容器并返回配置。
// 生命周期由 t.Cleanup 管理。
func NewEtcdContainerConfig(t *testing.T) *connector.EtcdConfig {
	t.Helper()
	_, cfg := NewEtcdContainer(t)
	return cfg
}

// NewEtcdContainer 返回容器和连接配置，供重连与故障恢复测试控制容器。
// 容器最终清理由 t.Cleanup 负责。
func NewEtcdContainer(t *testing.T) (*etcdcontainer.EtcdContainer, *connector.EtcdConfig) {
	t.Helper()
	RequireDocker(t)

	ctx := context.Background()

	container, err := etcdcontainer.Run(ctx, "quay.io/coreos/etcd:v3.5.12")
	require.NoError(t, err, "failed to start Etcd container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "2379")
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	return container, &connector.EtcdConfig{
		Name:        "testcontainer-etcd",
		Endpoints:   []string{host + ":" + mappedPort.Port()},
		DialTimeout: 5 * time.Second,
	}
}

// NewEtcdContainerConnector 使用 testcontainers 创建并连接 Etcd 连接器。
// 生命周期由 t.Cleanup 管理。
func NewEtcdContainerConnector(t *testing.T) connector.EtcdConnector {
	t.Helper()

	cfg := NewEtcdContainerConfig(t)

	conn, err := connector.NewEtcd(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create etcd connector")

	err = conn.Connect(context.Background())
	require.NoError(t, err, "failed to connect to etcd")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewEtcdContainerClient 使用 testcontainers 创建并返回原生 Etcd 客户端。
// 生命周期由 t.Cleanup 管理。
func NewEtcdContainerClient(t *testing.T) *clientv3.Client {
	t.Helper()
	return NewEtcdContainerConnector(t).GetClient()
}
