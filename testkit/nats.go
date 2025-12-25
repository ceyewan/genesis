package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/nats-io/nats.go"
)

// GetNATSConfig 返回 NATS 测试配置
// 默认连接 nats://localhost:4222
func GetNATSConfig() *connector.NATSConfig {
	return &connector.NATSConfig{
		Name:          "test-nats",
		URL:           "nats://localhost:4222",
		MaxReconnects: 10,
		ReconnectWait: 100 * time.Millisecond,
	}
}

// GetNATSConnector 获取 NATS 连接器
func GetNATSConnector(t *testing.T) connector.NATSConnector {
	cfg := GetNATSConfig()
	conn, err := connector.NewNATS(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create nats connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to nats: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// GetNATSConn 获取原生 NATS 连接
func GetNATSConn(t *testing.T) *nats.Conn {
	return GetNATSConnector(t).GetClient()
}
