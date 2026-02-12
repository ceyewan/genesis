package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	kafkacontainer "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/ceyewan/genesis/connector"
)

// NewKafkaContainerConfig 使用 testcontainers 创建 Kafka 容器并返回配置
// 生命周期由 t.Cleanup 管理
func NewKafkaContainerConfig(t *testing.T) *connector.KafkaConfig {
	ctx := context.Background()

	kafkaContainer, err := kafkacontainer.Run(ctx, "confluentinc/confluent-local:7.5.0",
		kafkacontainer.WithClusterID("test-kafka-cluster"),
	)
	require.NoError(t, err, "failed to start Kafka container")

	brokers, err := kafkaContainer.Brokers(ctx)
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = kafkaContainer.Terminate(ctx)
	})

	return &connector.KafkaConfig{
		Name:           "testcontainer-kafka",
		Seed:           brokers,
		ConnectTimeout: 10 * time.Second,
		RequestTimeout: 5 * time.Second,
	}
}

// NewKafkaContainerConnector 使用 testcontainers 创建并连接 Kafka 连接器
// 生命周期由 t.Cleanup 管理
func NewKafkaContainerConnector(t *testing.T) connector.KafkaConnector {
	cfg := NewKafkaContainerConfig(t)

	conn, err := connector.NewKafka(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create kafka connector")

	err = conn.Connect(context.Background())
	require.NoError(t, err, "failed to connect to kafka")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewKafkaContainerClient 使用 testcontainers 创建并返回原生 Kafka 客户端
// 生命周期由 t.Cleanup 管理
func NewKafkaContainerClient(t *testing.T) *kgo.Client {
	return NewKafkaContainerConnector(t).GetClient()
}
