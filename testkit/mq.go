package testkit

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/mq"
)

// GetNATSMQClient 获取 NATS MQ 客户端
func GetNATSMQClient(t *testing.T) mq.Client {
	natsConn := GetNATSConnector(t)
	client, err := mq.New(&mq.Config{
		Driver: mq.DriverNatsCore,
	}, mq.WithNATSConnector(natsConn), mq.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create MQ client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

// GetRedisMQClient 获取 Redis MQ 客户端
func GetRedisMQClient(t *testing.T) mq.Client {
	redisConn := GetRedisConnector(t)
	client, err := mq.New(&mq.Config{
		Driver: mq.DriverRedis,
	}, mq.WithRedisConnector(redisConn), mq.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create MQ client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
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
