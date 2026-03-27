package mq

import (
	"context"
	"maps"
)

// Headers 消息元数据（键值对）
//
// 用于传递 trace、业务标签等元信息。
// 注意：MQ 不做自动注入/提取，业务自行处理。
type Headers map[string]string

// Clone 返回 Headers 的深拷贝
func (h Headers) Clone() Headers {
	if h == nil {
		return nil
	}
	clone := make(Headers, len(h))
	maps.Copy(clone, h)
	return clone
}

// Get 获取指定 key 的值，不存在返回空字符串
func (h Headers) Get(key string) string {
	if h == nil {
		return ""
	}
	return h[key]
}

// Set 设置键值对
func (h Headers) Set(key, value string) {
	if h != nil {
		h[key] = value
	}
}

// Message 消息接口
//
// 封装底层消息细节，提供统一的数据访问和确认机制。
// 不同后端的 Ack/Nak 行为有差异，详见各方法注释。
type Message interface {
	// Context 获取消息处理上下文
	//
	// 该上下文继承自 Subscribe 调用时的 ctx，可用于：
	//   - 超时控制
	//   - 取消传播
	//   - 传递 trace 信息（需业务自行注入）
	Context() context.Context

	// Topic 获取消息主题
	Topic() string

	// Data 获取消息体（原始字节）
	Data() []byte

	// Headers 获取消息头（返回副本）
	Headers() Headers

	// Ack 确认消息处理成功
	//
	// 不同后端行为：
	//   - NATS JetStream: 发送 Ack 到服务端，消息从 pending 移除
	//   - Redis Stream: Consumer Group 模式下执行 XACK；广播模式下无操作
	Ack() error

	// Nak 拒绝消息，请求重投
	//
	// 不同后端行为：
	//   - NATS JetStream: 触发消息立即重投
	//   - Redis Stream: 返回 ErrNotSupported；消息留在 Pending 列表，
	//     由 XAUTOCLAIM 在 PendingIdle 超时后重新认领
	//
	// 调用方应通过 errors.Is(err, ErrNotSupported) 区分"不支持"与真实错误。
	// AutoAck 模式下 ErrNotSupported 会被自动忽略。
	Nak() error

	// ID 获取消息唯一标识（可选）
	//
	// 不同后端返回值：
	//   - NATS JetStream: "<stream>:<sequence>"（如 "S-orders:42"）
	//   - Redis Stream: 消息 ID（如 "1700000000000-0"）
	ID() string
}

// Handler 消息处理函数
//
// 设计说明：只接收 Message 参数，通过 msg.Context() 获取上下文，
// 避免 ctx 与 msg.Context() 并存造成语义歧义。
//
// 返回值：
//   - nil: 处理成功，AutoAck 模式下自动调用 Ack
//   - error: 处理失败，AutoAck 模式下自动调用 Nak（Redis 下 ErrNotSupported 会被忽略）
//
// 注意（JetStream）：Nak 触发消息立即重投。对非暂时性错误（如解码失败），
// 建议使用 WithManualAck 手动 Ack，或配合 WithRetry 在应用层重试后 Ack，
// 避免消息无限重投。
type Handler func(msg Message) error

// Subscription 订阅句柄
//
// 用于管理订阅的生命周期。
type Subscription interface {
	// Unsubscribe 取消订阅
	//
	// 调用后停止接收新消息。
	// 注意：不保证等待当前 Handler 完成，具体行为依赖后端实现。
	Unsubscribe() error

	// Done 返回一个 channel，订阅结束时关闭
	//
	// 可用于等待订阅完全停止：
	//   <-sub.Done()
	//   sub.Unsubscribe()
	Done() <-chan struct{}
}
