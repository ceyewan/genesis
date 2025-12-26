package mq

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// RedisDriver Redis Stream 驱动实现
type RedisDriver struct {
	client *redis.Client
	logger clog.Logger
}

// NewRedisDriver 创建 Redis 驱动
func NewRedisDriver(conn connector.RedisConnector, logger clog.Logger) *RedisDriver {
	return &RedisDriver{
		client: conn.GetClient(),
		logger: logger,
	}
}

func (d *RedisDriver) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	// Redis Stream Publish: XADD
	// subject 作为 key
	// data 作为 value (字段名 "payload")
	err := d.client.XAdd(ctx, &redis.XAddArgs{
		Stream: subject,
		Values: map[string]interface{}{
			"payload": data,
		},
	}).Err()

	return err
}

func (d *RedisDriver) Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	// 构造订阅上下文 (用于取消订阅)
	subCtx, cancel := context.WithCancel(context.Background())
	sub := &redisSubscription{
		cancel: cancel,
	}

	go func() {
		defer cancel()

		var startID string
		// 如果是 QueueGroup 模式 (Consumer Group)
		if o.QueueGroup != "" {
			group := o.QueueGroup
			consumer := fmt.Sprintf("%s-%d", group, time.Now().UnixNano()) // 随机消费者名
			if o.DurableName != "" {
				consumer = o.DurableName
			}

			// 尝试创建组 (忽略已存在错误)
			d.client.XGroupCreateMkStream(subCtx, subject, group, "$").Err()

			for {
				select {
				case <-subCtx.Done():
					return
				default:
					// XREADGROUP
					streams, err := d.client.XReadGroup(subCtx, &redis.XReadGroupArgs{
						Group:    group,
						Consumer: consumer,
						Streams:  []string{subject, ">"},
						Count:    int64(o.BatchSize),
						Block:    2 * time.Second,
					}).Result()

					if err != nil {
						if err != redis.Nil && err != context.Canceled {
							d.logger.Error("Redis XReadGroup failed", clog.Error(err))
							time.Sleep(time.Second) // 避免忙轮询
						}
						continue
					}

					for _, stream := range streams {
						for _, msg := range stream.Messages {
							d.processMsg(subCtx, subject, group, msg, handler, o)
						}
					}
				}
			}

		} else {
			// 广播模式 (Broadcast)
			// 使用 XREAD (每个消费者独立读取)
			// StartID = "$" (只读新消息)
			startID = "$"

			for {
				select {
				case <-subCtx.Done():
					return
				default:
					streams, err := d.client.XRead(subCtx, &redis.XReadArgs{
						Streams: []string{subject, startID},
						Count:   int64(o.BatchSize),
						Block:   2 * time.Second,
					}).Result()

					if err != nil {
						if err != redis.Nil && err != context.Canceled {
							d.logger.Error("Redis XRead failed", clog.Error(err))
							time.Sleep(time.Second)
						}
						continue
					}

					for _, stream := range streams {
						for _, msg := range stream.Messages {
							d.processMsg(subCtx, subject, "", msg, handler, o)
							startID = msg.ID // 更新 lastID
						}
					}
				}
			}
		}
	}()

	return sub, nil
}

func (d *RedisDriver) processMsg(ctx context.Context, subject, group string, rMsg redis.XMessage, handler Handler, o subscribeOptions) {
	// 提取 payload
	dataStr, ok := rMsg.Values["payload"].(string)
	if !ok {
		// 尝试 bytes
		if bytesData, ok := rMsg.Values["payload"].([]byte); ok {
			dataStr = string(bytesData) // 暂时转 string 再转 byte，优化空间
		}
	}

	msg := &redisMessage{
		id:      rMsg.ID,
		subject: subject,
		data:    []byte(dataStr),
		client:  d.client,
		group:   group,
	}

	err := handler(ctx, msg)

	if o.AutoAck {
		if err == nil {
			if o.AsyncAck {
				msg.AckAsync()
			} else {
				_ = msg.Ack()
			}
		}
		// Redis 不支持 Nak 重投 (除非配合 Pending 检查机制)，所以 Nak 仅是应用层语义
	}
}

func (d *RedisDriver) Close() error {
	return nil
}

// -----------------------------------------------------------
// 消息与订阅封装
// -----------------------------------------------------------

type redisMessage struct {
	id      string
	subject string
	data    []byte
	client  *redis.Client
	group   string
}

func (m *redisMessage) Subject() string {
	return m.subject
}

func (m *redisMessage) Data() []byte {
	return m.data
}

func (m *redisMessage) Ack() error {
	// 只有 Consumer Group 模式才需要 Ack
	if m.group != "" {
		return m.client.XAck(context.Background(), m.subject, m.group, m.id).Err()
	}
	return nil
}

func (m *redisMessage) AckAsync() {
	if m.group != "" {
		go func() {
			_ = m.client.XAck(context.Background(), m.subject, m.group, m.id).Err()
		}()
	}
}

func (m *redisMessage) Nak() error {
	// Redis Stream 没有原生的 Nak。
	// 通常做法是不 Ack，然后由消费者重新声明 (XCLAIM) 超时的 pending 消息。
	return nil
}

type redisSubscription struct {
	cancel context.CancelFunc
}

func (s *redisSubscription) Unsubscribe() error {
	s.cancel()
	return nil
}

func (s *redisSubscription) IsValid() bool {
	return true
}
