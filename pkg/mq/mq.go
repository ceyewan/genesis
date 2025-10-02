package mq

import "context"

// Message represents a generic event transported by the queue.
type Message struct {
	Key       string
	Value     []byte
	Headers   map[string]string
	Timestamp int64
}

// Producer publishes messages to a topic.
type Producer interface {
	Publish(ctx context.Context, topic string, messages ...Message) error
	Close(ctx context.Context) error
}

// Consumer subscribes to topics and polls messages.
type Consumer interface {
	Consume(ctx context.Context, handler Handler) error
	Close(ctx context.Context) error
}

// Handler processes a single message.
type Handler interface {
	Handle(ctx context.Context, msg Message) error
}

// Factory can create producers or consumers based on configuration.
type Factory interface {
	NewProducer(ctx context.Context, opts ...Option) (Producer, error)
	NewConsumer(ctx context.Context, opts ...Option) (Consumer, error)
}

// Option customises client creation.
type Option interface{}
