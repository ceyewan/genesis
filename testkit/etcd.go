package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/stretchr/testify/require"
	etcdcontainer "github.com/testcontainers/testcontainers-go/modules/etcd"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// NewEtcdContainerConfig 使用 testcontainers 创建 Etcd 容器并返回配置
// 生命周期由 t.Cleanup 管理
func NewEtcdContainerConfig(t *testing.T) *connector.EtcdConfig {
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

	return &connector.EtcdConfig{
		Name:        "testcontainer-etcd",
		Endpoints:   []string{host + ":" + mappedPort.Port()},
		DialTimeout: 5 * time.Second,
	}
}

// NewEtcdContainerConnector 使用 testcontainers 创建并连接 Etcd 连接器
// 生命周期由 t.Cleanup 管理
func NewEtcdContainerConnector(t *testing.T) connector.EtcdConnector {
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

// NewEtcdContainerClient 使用 testcontainers 创建并返回原生 Etcd 客户端
// 生命周期由 t.Cleanup 管理
func NewEtcdContainerClient(t *testing.T) *clientv3.Client {
	return NewEtcdContainerConnector(t).GetClient()
}
