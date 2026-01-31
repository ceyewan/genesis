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
	//   - NATS Core: 无操作（无持久化语义）
	//   - NATS JetStream: 发送 Ack 到服务端
	//   - Redis Stream: Consumer Group 模式下执行 XACK
	//   - Kafka (未来): 提交 offset
	Ack() error

	// Nak 拒绝消息，请求重投
	//
	// 不同后端行为：
	//   - NATS Core: 无操作
	//   - NATS JetStream: 发送 Nak，触发重投
	//   - Redis Stream: 无原生支持，消息留在 Pending 列表
	//   - Kafka (未来): 不提交 offset，触发 rebalance 后重投
	//
	// 注意：部分后端不支持真正的 Nak，错误处理需结合业务设计。
	Nak() error

	// ID 获取消息唯一标识（可选）
	//
	// 不同后端返回值：
	//   - NATS Core: 空字符串
	//   - NATS JetStream: Stream sequence
	//   - Redis Stream: 消息 ID (如 "1234567890-0")
	ID() string
}

// Handler 消息处理函数
//
// 设计说明：只接收 Message 参数，通过 msg.Context() 获取上下文，
// 避免 ctx 和 msg.Context() 同时存在造成的困惑。
//
// 返回值：
//   - nil: 处理成功，AutoAck 模式下自动确认
//   - error: 处理失败，AutoAck 模式下自动 Nak（如后端支持）
//
// 重要提示（JetStream 用户必读）：
//   - JetStream 的 Nak 会触发消息重新投递
//   - 如果返回 error 是非暂时性的（如数据格式错误），消息会无限重投
//   - 解决方案：
//     1. 使用 WithManualAck 手动控制 Ack/Nak
//     2. 对于不可恢复的错误，也调用 Ack() 并记录日志
//     3. 结合 WithRetry 中间件在应用层重试，失败后仍 Ack
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
