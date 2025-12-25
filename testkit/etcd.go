package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// GetEtcdConfig 返回 Etcd 测试配置
// 默认连接 localhost:2379
func GetEtcdConfig() *connector.EtcdConfig {
	return &connector.EtcdConfig{
		Name:        "test-etcd",
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	}
}

// GetEtcdConnector 获取 Etcd 连接器
func GetEtcdConnector(t *testing.T) connector.EtcdConnector {
	cfg := GetEtcdConfig()
	conn, err := connector.NewEtcd(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create etcd connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to etcd: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// GetEtcdClient 获取原生 Etcd 客户端
func GetEtcdClient(t *testing.T) *clientv3.Client {
	return GetEtcdConnector(t).GetClient()
}
