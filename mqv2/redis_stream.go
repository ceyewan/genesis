package mqv2

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	redisFieldPayload = "payload"
	redisFieldHeaders = "headers"
)

// redisStreamTransport Redis Stream 传输层实现
type redisStreamTransport struct {
	client *redis.Client
	logger clog.Logger
}

// newRedisStreamTransport 创建 Redis Stream Transport
func newRedisStreamTransport(conn connector.RedisConnector, logger clog.Logger) *redisStreamTransport {
	return &redisStreamTransport{
		client: conn.GetClient(),
		logger: logger,
	}
}

// Publish 发布消息
func (t *redisStreamTransport) Publish(ctx context.Context, topic string, data []byte, opts publishOptions) error {
	values := map[string]interface{}{
		redisFieldPayload: data,
	}

	if len(opts.Headers) > 0 {
		headersJSON, err := json.Marshal(opts.Headers)
		if err != nil {
			return xerrors.Wrap(err, "marshal headers failed")
		}
		values[redisFieldHeaders] = headersJSON
	}

	return t.client.XAdd(ctx, &redis.XAddArgs{
		Stream: topic,
		Values: values,
	}).Err()
}

// Subscribe 订阅消息
func (t *redisStreamTransport) Subscribe(ctx context.Context, topic string, handler Handler, opts subscribeOptions) (Subscription, error) {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &redisStreamSubscription{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		defer func() {
			sub.once.Do(func() { close(sub.done) })
		}()

		if opts.QueueGroup != "" {
			t.consumeWithGroup(subCtx, topic, opts, handler)
		} else {
			t.consumeBroadcast(subCtx, topic, opts, handler)
		}
	}()

	return sub, nil
}

// consumeWithGroup Consumer Group 模式消费
//
// 实现策略：
// 1. 首先尝试 claim 超时的 Pending 消息（避免消费者崩溃后消息卡死）
// 2. 然后读取新消息
func (t *redisStreamTransport) consumeWithGroup(ctx context.Context, topic string, opts subscribeOptions, handler Handler) {
	group := opts.QueueGroup
	consumer := opts.DurableName
	if consumer == "" {
		consumer = fmt.Sprintf("%s-%d", group, time.Now().UnixNano())
	}

	// 尝试创建 Consumer Group（忽略已存在错误）
	_ = t.client.XGroupCreateMkStream(ctx, topic, group, "$").Err()

	// Pending 消息 claim 的配置
	const (
		pendingIdleTime   = 30 * time.Second // 消息空闲超过此时间可被 claim
		pendingClaimCount = 10               // 每次最多 claim 多少条
		pendingCheckRatio = 5                // 每 N 次循环检查一次 pending
	)
	loopCount := 0
	claimCursor := "0-0" // XAutoClaim 游标，避免每次从头扫描

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		loopCount++

		// 定期检查并 claim 超时的 Pending 消息
		if loopCount%pendingCheckRatio == 0 {
			claimCursor = t.claimPendingMessages(ctx, topic, group, consumer, pendingIdleTime, pendingClaimCount, claimCursor, handler)
		}

		// 读取新消息
		streams, err := t.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumer,
			Streams:  []string{topic, ">"},
			Count:    int64(opts.BatchSize),
			Block:    2 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil || err == context.Canceled {
				continue
			}
			t.logger.Error("XReadGroup failed", clog.String("topic", topic), clog.Error(err))
			time.Sleep(time.Second) // 避免忙轮询
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				t.processMessage(ctx, topic, group, msg, handler)
			}
		}
	}
}

// claimPendingMessages 认领超时的 Pending 消息
//
// 使用 XAUTOCLAIM 自动认领空闲时间超过 minIdleTime 的消息
// 这确保消费者崩溃后，未确认的消息会被其他消费者接管处理
//
// 参数 cursor：上次扫描的位置，避免每次从头扫描导致 O(n) 性能问题
// 返回值：下次扫描应使用的 cursor（如果扫描完成返回 "0-0" 重新开始）
func (t *redisStreamTransport) claimPendingMessages(
	ctx context.Context,
	topic, group, consumer string,
	minIdleTime time.Duration,
	count int,
	cursor string,
	handler Handler,
) string {
	// 使用 XAUTOCLAIM 自动认领超时消息
	messages, nextCursor, err := t.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   topic,
		Group:    group,
		Consumer: consumer,
		MinIdle:  minIdleTime,
		Start:    cursor,
		Count:    int64(count),
	}).Result()

	if err != nil {
		if err != redis.Nil {
			t.logger.Warn("XAutoClaim failed", clog.String("topic", topic), clog.Error(err))
		}
		return "0-0" // 出错时重置游标
	}

	if len(messages) > 0 {
		t.logger.Info("claimed pending messages",
			clog.String("topic", topic),
			clog.String("group", group),
			clog.Int("count", len(messages)),
		)
	}

	// 处理认领到的消息
	for _, msg := range messages {
		t.processMessage(ctx, topic, group, msg, handler)
	}

	// 返回下次扫描的起点
	// 如果 nextCursor 是 "0-0"，说明已扫描完一轮，下次从头开始
	return nextCursor
}

// consumeBroadcast 广播模式消费
func (t *redisStreamTransport) consumeBroadcast(ctx context.Context, topic string, opts subscribeOptions, handler Handler) {
	lastID := "$" // 只读新消息

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streams, err := t.client.XRead(ctx, &redis.XReadArgs{
			Streams: []string{topic, lastID},
			Count:   int64(opts.BatchSize),
			Block:   2 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil || err == context.Canceled {
				continue
			}
			t.logger.Error("XRead failed", clog.String("topic", topic), clog.Error(err))
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				t.processMessage(ctx, topic, "", msg, handler)
				lastID = msg.ID
			}
		}
	}
}

// processMessage 处理单条消息
func (t *redisStreamTransport) processMessage(ctx context.Context, topic, group string, rMsg redis.XMessage, handler Handler) {
	// 提取 payload
	var data []byte
	if v, ok := rMsg.Values[redisFieldPayload]; ok {
		switch val := v.(type) {
		case string:
			data = []byte(val)
		case []byte:
			data = val
		}
	}

	// 提取 headers
	headers := t.decodeHeaders(rMsg.Values[redisFieldHeaders])

	msg := &redisStreamMessage{
		id:      rMsg.ID,
		topic:   topic,
		data:    data,
		headers: headers,
		client:  t.client,
		group:   group,
		ctx:     ctx,
	}

	// 错误已在上层 wrapHandler 中处理
	_ = handler(msg)
}

// decodeHeaders 解码 Headers
func (t *redisStreamTransport) decodeHeaders(v interface{}) Headers {
	if v == nil {
		return nil
	}

	var raw []byte
	switch val := v.(type) {
	case string:
		raw = []byte(val)
	case []byte:
		raw = val
	default:
		return nil
	}

	if len(raw) == 0 {
		return nil
	}

	var h Headers
	if err := json.Unmarshal(raw, &h); err != nil {
		t.logger.Warn("decode headers failed", clog.Error(err))
		return nil
	}
	return h
}

// Close 关闭 Transport
func (t *redisStreamTransport) Close() error {
	return nil
}

// Capabilities 返回能力描述
func (t *redisStreamTransport) Capabilities() Capabilities {
	return CapabilitiesRedisStream
}

// ==================== Message 实现 ====================

// redisStreamMessage Redis Stream 消息实现
type redisStreamMessage struct {
	id      string
	topic   string
	data    []byte
	headers Headers
	client  *redis.Client
	group   string
	ctx     context.Context
}

func (m *redisStreamMessage) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *redisStreamMessage) Topic() string {
	return m.topic
}

func (m *redisStreamMessage) Data() []byte {
	return m.data
}

func (m *redisStreamMessage) Headers() Headers {
	return m.headers.Clone()
}

func (m *redisStreamMessage) Ack() error {
	if m.group == "" {
		// 非 Consumer Group 模式，无需 Ack
		return nil
	}
	return m.client.XAck(context.Background(), m.topic, m.group, m.id).Err()
}

func (m *redisStreamMessage) Nak() error {
	// Redis Stream 没有原生 Nak
	// 消息会留在 Pending 列表，可通过 XCLAIM 重新获取
	return nil
}

func (m *redisStreamMessage) ID() string {
	return m.id
}

// ==================== Subscription 实现 ====================

// redisStreamSubscription Redis Stream 订阅实现
type redisStreamSubscription struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *redisStreamSubscription) Unsubscribe() error {
	s.cancel()
	return nil
}

func (s *redisStreamSubscription) Done() <-chan struct{} {
	return s.done
}
