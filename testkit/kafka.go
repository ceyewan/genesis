package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/twmb/franz-go/pkg/kgo"
)

// GetKafkaConfig 返回 Kafka 测试配置
// 默认连接 localhost:9092
func GetKafkaConfig() *connector.KafkaConfig {
	return &connector.KafkaConfig{
		Name:           "test-kafka",
		Seed:           []string{"localhost:9092"},
		ConnectTimeout: 10 * time.Second,
		RequestTimeout: 5 * time.Second,
	}
}

// GetKafkaConnector 获取 Kafka 连接器
func GetKafkaConnector(t *testing.T) connector.KafkaConnector {
	cfg := GetKafkaConfig()
	conn, err := connector.NewKafka(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create kafka connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to kafka: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// GetKafkaClient 获取原生 Kafka 客户端
func GetKafkaClient(t *testing.T) *kgo.Client {
	return GetKafkaConnector(t).GetClient()
}
