package connector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
	"github.com/twmb/franz-go/pkg/kgo"
)

type kafkaConnector struct {
	cfg     *KafkaConfig
	client  *kgo.Client
	logger  clog.Logger
	meter   metrics.Meter
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewKafka 创建 Kafka 连接器
func NewKafka(cfg *KafkaConfig, opts ...Option) (KafkaConnector, error) {
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid kafka config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}

	if opt.logger == nil {
		opt.logger = clog.Discard()
	}

	return &kafkaConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "kafka"), clog.String("name", cfg.Name)),
		meter:  opt.meter,
	}, nil
}

func (c *kafkaConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	// SASL Config
	if c.cfg.User != "" && c.cfg.Password != "" {
		// TODO: 支持 SASL/PLAIN or SCRAM
		// 由于 franz-go 的 SASL 需要引入 sasl 包，这里暂且留空或支持基础 PLAIN
		// 为了简单起见，本示例暂不支持 SASL，或后续添加
		c.logger.Warn("SASL auth configured but not implemented in basic connector yet")
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		c.logger.Error("failed to create kafka client", clog.Error(err))
		return xerrors.Wrapf(err, "kafka connector[%s]: create client failed", c.cfg.Name)
	}

	// Ping to verify connection
	// franz-go 连接是异步的，我们可以通过 Ping 验证
	if err := client.Ping(ctx); err != nil {
		client.Close()
		c.logger.Error("failed to connect to kafka seeds", clog.Error(err))
		return xerrors.Wrapf(err, "kafka connector[%s]: ping failed", c.cfg.Name)
	}

	c.client = client
	c.healthy.Store(true)
	c.logger.Info("successfully connected to kafka")

	return nil
}

func (c *kafkaConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.healthy.Store(false)
	if c.client != nil {
		c.client.Close()
		c.client = nil
		c.logger.Info("kafka connection closed")
	}
	return nil
}

func (c *kafkaConnector) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return xerrors.New("kafka client not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx); err != nil {
		c.healthy.Store(false)
		return err
	}
	c.healthy.Store(true)
	return nil
}

func (c *kafkaConnector) IsHealthy() bool {
	return c.healthy.Load()
}

func (c *kafkaConnector) Name() string {
	return c.cfg.Name
}

func (c *kafkaConnector) GetClient() *kgo.Client {
	return c.client
}

func (c *kafkaConnector) Config() *KafkaConfig {
	return c.cfg
}

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
