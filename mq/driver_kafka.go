package mq

import (
	"context"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// KafkaDriver Kafka 驱动实现
type KafkaDriver struct {
	conn      connector.KafkaConnector
	connCfg   *connector.KafkaConfig
	client    *kgo.Client
	logger    clog.Logger
}

// NewKafkaDriver 创建 Kafka 驱动
func NewKafkaDriver(conn connector.KafkaConnector, connCfg *connector.KafkaConfig, logger clog.Logger) *KafkaDriver {
	return &KafkaDriver{
		conn:    conn,
		connCfg: connCfg,
		client:  conn.GetClient(),
		logger:  logger,
	}
}

func (d *KafkaDriver) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	var o publishOptions
	for _, opt := range opts {
		opt(&o)
	}

	// 使用 Connector 的共享 Client 发送消息
	record := &kgo.Record{
		Topic: subject,
		Value: data,
		Key:   []byte(o.Key),
	}

	// 同步发送
	if err := d.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return err
	}
	return nil
}

func (d *KafkaDriver) Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	// 为了支持不同的 Consumer Group 和 Topic 订阅，我们需要为每个 Subscription 创建新的 kgo.Client
	// 使用传入的配置
	connCfg := d.connCfg

	// 基础选项
	kgoOpts := []kgo.Opt{
		kgo.SeedBrokers(connCfg.Seed...),
		kgo.ConsumeTopics(subject),
		kgo.WithLogger(&kgoLogger{logger: d.logger}),
		kgo.AllowAutoTopicCreation(),
	}

	// SASL/PLAIN 认证
	if connCfg.User != "" && connCfg.Password != "" {
		d.logger.Info("enabling SASL/PLAIN authentication for mq consumer", clog.String("user", connCfg.User))
		auth := plain.Auth{
			User: connCfg.User,
			Pass: connCfg.Password,
		}
		kgoOpts = append(kgoOpts, kgo.SASL(auth.AsMechanism()))
	}

	// 消费组配置
	if o.QueueGroup != "" {
		kgoOpts = append(kgoOpts, kgo.ConsumerGroup(o.QueueGroup))
		// 默认禁用自动提交，由 Handler 手动 Ack 触发提交
		kgoOpts = append(kgoOpts, kgo.DisableAutoCommit())
	} else {
		// 广播模式：随机 Group ID 或无 Group 模式
		// 无 Group 模式下，franz-go 默认 assign 所有 partitions
		// 如果想要广播到多个实例，每个实例需要唯一的 Group ID
		// 这里简单处理：生成随机 Group ID
		// 或者不使用 Group，直接 ConsumeTopics (Direct Consume)
		// franz-go 如果不指定 Group，就是 Direct Consume，所有 partitions 都会被分配给该 client
		// 这样每个实例都会收到消息，实现了广播
	}

	// 创建专用的 Client
	client, err := kgo.NewClient(kgoOpts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create kafka consumer client")
	}

	subCtx, cancel := context.WithCancel(context.Background())
	sub := &kafkaSubscription{
		client: client,
		cancel: cancel,
	}

	// 启动消费循环
	go func() {
		defer client.Close()
		defer cancel()

		for {
			// PollFetches
			fetches := client.PollFetches(subCtx)
			if fetches.IsClientClosed() {
				return
			}
			if errs := fetches.Errors(); len(errs) > 0 {
				// Log errors
				for _, err := range errs {
					d.logger.Error("kafka poll error", clog.String("topic", err.Topic), clog.Error(err.Err))
				}
				// 简单的退避
				time.Sleep(time.Second)
				continue
			}

			// 处理记录
			iter := fetches.RecordIter()
			for !iter.Done() {
				record := iter.Next()

				msg := &kafkaMessage{
					record: record,
					client: client,
				}

				err := handler(subCtx, msg)

				if o.AutoAck {
					if err == nil {
						if o.AsyncAck {
							go func() { _ = msg.Ack() }()
						} else {
							_ = msg.Ack()
						}
					}
				}
			}
		}
	}()

	return sub, nil
}

func (d *KafkaDriver) Close() error {
	return nil
}

// -----------------------------------------------------------
// 消息与订阅封装
// -----------------------------------------------------------

type kafkaMessage struct {
	record *kgo.Record
	client *kgo.Client
}

func (m *kafkaMessage) Subject() string {
	return m.record.Topic
}

func (m *kafkaMessage) Data() []byte {
	return m.record.Value
}

func (m *kafkaMessage) Ack() error {
	// 提交 offset
	// franz-go 推荐 CommitRecords 或 CommitOffsets
	return m.client.CommitRecords(context.Background(), m.record)
}

func (m *kafkaMessage) Nak() error {
	return nil
}

type kafkaSubscription struct {
	client *kgo.Client
	cancel context.CancelFunc
}

func (s *kafkaSubscription) Unsubscribe() error {
	s.cancel()
	s.client.Close()
	return nil
}

func (s *kafkaSubscription) IsValid() bool {
	return true
}

type kgoLogger struct {
	logger clog.Logger
}

func (l *kgoLogger) Level() kgo.LogLevel {
	return kgo.LogLevelInfo
}

func (l *kgoLogger) Log(level kgo.LogLevel, msg string, keyvals ...interface{}) {
	var fields []clog.Field
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			key, ok := keyvals[i].(string)
			if ok {
				fields = append(fields, clog.Any(key, keyvals[i+1]))
			}
		}
	}

	switch level {
	case kgo.LogLevelError:
		l.logger.Error(msg, fields...)
	case kgo.LogLevelWarn:
		l.logger.Warn(msg, fields...)
	case kgo.LogLevelInfo:
		l.logger.Info(msg, fields...)
	case kgo.LogLevelDebug:
		l.logger.Debug(msg, fields...)
	}
}
