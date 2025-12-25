package testkit

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/mq"
)

// GetNATSMQDriver 获取 NATS Core 驱动用于测试
func GetNATSMQDriver(t *testing.T) mq.Driver {
	natsConn := GetNATSConnector(t)
	return mq.NewNatsCoreDriver(natsConn, NewLogger())
}

// GetNATSMQClient 获取 NATS MQ 客户端
func GetNATSMQClient(t *testing.T) mq.Client {
	driver := GetNATSMQDriver(t)
	client, err := mq.New(driver, mq.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create MQ client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

// GetRedisMQDriver 获取 Redis Stream 驱动用于测试
func GetRedisMQDriver(t *testing.T) mq.Driver {
	redisConn := GetRedisConnector(t)
	return mq.NewRedisDriver(redisConn, NewLogger())
}

// GetRedisMQClient 获取 Redis MQ 客户端
func GetRedisMQClient(t *testing.T) mq.Client {
	driver := GetRedisMQDriver(t)
	client, err := mq.New(driver, mq.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create MQ client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

// GetKafkaMQDriver 获取 Kafka 驱动用于测试
func GetKafkaMQDriver(t *testing.T) mq.Driver {
	kafkaConn := GetKafkaConnector(t)
	return mq.NewKafkaDriver(kafkaConn, NewLogger())
}

// GetKafkaMQClient 获取 Kafka MQ 客户端
func GetKafkaMQClient(t *testing.T) mq.Client {
	driver := GetKafkaMQDriver(t)
	client, err := mq.New(driver, mq.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create MQ client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

// GetNATSMQDriverWithConnector 获取 NATS 驱动和连接器（用于需要管理连接器生命周期的测试）
func GetNATSMQDriverWithConnector(t *testing.T) (mq.Driver, connector.NATSConnector) {
	natsConn := GetNATSConnector(t)
	driver := mq.NewNatsCoreDriver(natsConn, NewLogger())
	return driver, natsConn
}

// GetRedisMQDriverWithConnector 获取 Redis 驱动和连接器
func GetRedisMQDriverWithConnector(t *testing.T) (mq.Driver, connector.RedisConnector) {
	redisConn := GetRedisConnector(t)
	driver := mq.NewRedisDriver(redisConn, NewLogger())
	return driver, redisConn
}

// GetKafkaMQDriverWithConnector 获取 Kafka 驱动和连接器
func GetKafkaMQDriverWithConnector(t *testing.T) (mq.Driver, connector.KafkaConnector) {
	kafkaConn := GetKafkaConnector(t)
	driver := mq.NewKafkaDriver(kafkaConn, NewLogger())
	return driver, kafkaConn
}

// CleanupStream 清理测试后的 Stream 数据
// 用于不同驱动的数据清理
func CleanupStream(t *testing.T, ctx context.Context, driverType string, streamName string) {
	switch driverType {
	case "redis":
		redisConn := GetRedisConnector(t)
		_ = redisConn.GetClient().Del(ctx, streamName).Err()
	case "nats":
		// NATS 不支持删除已发布的消息，但订阅会自动清理
	// Kafka 消息会根据 retention policy 自动清理
	}
}

// NewTestSubject 生成唯一的测试主题名称
func NewTestSubject(prefix string) string {
	return "test." + NewID() + "." + prefix
}

// NewTestConsumerGroup 生成唯一的消费者组名
func NewTestConsumerGroup(prefix string) string {
	return "test-group-" + NewID() + "-" + prefix
}
