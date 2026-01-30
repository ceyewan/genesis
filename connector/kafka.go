package connector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

type kafkaConnector struct {
	cfg     *KafkaConfig
	client  *kgo.Client
	logger  clog.Logger
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewKafka 创建 Kafka 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewKafka(cfg *KafkaConfig, opts ...Option) (KafkaConnector, error) {
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid kafka config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	return &kafkaConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "kafka"), clog.String("name", cfg.Name)),
	}, nil
}

// Connect 建立连接
func (c *kafkaConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.client != nil {
		return nil
	}

	c.logger.Info("attempting to connect to kafka", clog.Any("seeds", c.cfg.Seed))

	opts := []kgo.Opt{
		kgo.SeedBrokers(c.cfg.Seed...),
		kgo.ClientID(c.cfg.ClientID),
		kgo.WithLogger(&kgoLogger{logger: c.logger}),
		kgo.AllowAutoTopicCreation(),
	}

	// SASL/PLAIN 认证
	if c.cfg.User != "" && c.cfg.Password != "" {
		c.logger.Info("enabling SASL/PLAIN authentication", clog.String("user", c.cfg.User))
		auth := plain.Auth{
			User: c.cfg.User,
			Pass: c.cfg.Password,
		}
		opts = append(opts, kgo.SASL(auth.AsMechanism()))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		c.logger.Error("failed to create kafka client", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "kafka connector[%s]: %v", c.cfg.Name, err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx); err != nil {
		client.Close()
		c.logger.Error("failed to connect to kafka seeds", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "kafka connector[%s]: ping failed: %v", c.cfg.Name, err)
	}

	c.client = client
	c.healthy.Store(true)
	c.logger.Info("successfully connected to kafka")

	return nil
}

// Close 关闭连接
func (c *kafkaConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.healthy.Store(false)

	if c.client == nil {
		return nil
	}

	c.client.Close()
	c.client = nil
	c.logger.Info("kafka connection closed")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *kafkaConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrClientNil, "kafka connector[%s]", c.cfg.Name)
	}

	// 只在用户未设置超时时才设置 5s 默认超时
	var healthCtx context.Context
	var cancel context.CancelFunc
	if _, ok := ctx.Deadline(); !ok {
		healthCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
	} else {
		healthCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	if err := client.Ping(healthCtx); err != nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrHealthCheck, "kafka connector[%s]: %v", c.cfg.Name, err)
	}

	c.healthy.Store(true)
	return nil
}

// IsHealthy 返回缓存的健康状态
func (c *kafkaConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *kafkaConnector) Name() string {
	return c.cfg.Name
}

// GetClient 返回 Kafka 客户端
func (c *kafkaConnector) GetClient() *kgo.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// kgoLogger 适配 kgo.Logger 接口
type kgoLogger struct {
	logger clog.Logger
}

func (l *kgoLogger) Level() kgo.LogLevel {
	return kgo.LogLevelInfo
}

func (l *kgoLogger) Log(level kgo.LogLevel, msg string, keyvals ...interface{}) {
	// 简单的键值对转换
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
