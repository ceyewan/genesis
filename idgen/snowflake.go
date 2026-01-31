package idgen

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	// maxClockBackwards 最大容忍的时钟回拨时间 (1秒)
	maxClockBackwards = 1000 * time.Millisecond
	// smallClockBackwards 微小回拨阈值 (5ms)，在此范围内尝试复用 lastTime
	smallClockBackwards = 5 * time.Millisecond
)

// ========================================
// Snowflake 雪花算法生成器 (实现 Generator 接口)
// ========================================

// snowflake 雪花算法生成器
// 实现 Generator 接口，提供高性能的分布式有序 ID 生成能力
type snowflake struct {
	// state 包含 48bit lastTime 和 12bit sequence
	// 使用 atomic 操作保证并发安全
	state      atomic.Uint64
	workerID   int64
	dcID       int64
	logger     clog.Logger
	genCounter metrics.Counter
}

// NewGenerator 创建 ID 生成器 (Snowflake 实现)
//
// 使用示例:
//
//	gen, _ := idgen.NewGenerator(&idgen.GeneratorConfig{WorkerID: 1})
//	id := gen.Next()
func NewGenerator(cfg *GeneratorConfig, opts ...Option) (Generator, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrInvalidInput, "config_nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	logger := opt.Logger
	if logger == nil {
		logger = clog.Discard()
	}

	meter := opt.Meter
	if meter == nil {
		meter = metrics.Discard()
	}

	genCounter, _ := meter.Counter(MetricSnowflakeGenerated, "雪花算法 ID 生成总数")

	sf := &snowflake{
		workerID:   cfg.WorkerID,
		dcID:       cfg.DatacenterID,
		logger:     logger.With(clog.String("component", "generator")),
		genCounter: genCounter,
	}

	sf.logger.Info("generator created",
		clog.Int64("worker_id", cfg.WorkerID),
		clog.Int64("datacenter_id", cfg.DatacenterID),
	)

	return sf, nil
}

// nextInt64 生成 int64 ID（内部方法）
func (s *snowflake) nextInt64() (int64, error) {
	for {
		oldState := s.state.Load()
		lastTime := int64(oldState >> 12)
		sequence := int64(oldState & 0xFFF)
		now := time.Now().UnixMilli()

		// 处理时钟回拨
		if now < lastTime {
			drift := time.Duration(lastTime-now) * time.Millisecond

			if drift <= smallClockBackwards {
				// 1. 微小回拨 (<= 5ms): 尝试复用 lastTime
				if sequence < 0xFFF {
					now = lastTime
				} else {
					// 序列号已满，必须等待
					time.Sleep(drift + time.Millisecond)
					continue
				}
			} else if drift <= maxClockBackwards {
				// 2. 小回拨 (5ms < drift <= 1s): 等待时钟追上
				time.Sleep(drift + time.Millisecond)
				continue
			} else {
				// 3. 大回拨 (> 1s): 拒绝服务
				return 0, xerrors.Wrapf(ErrClockBackwards, "drift: %v (max: %v)", drift, maxClockBackwards)
			}
		}

		newSequence := int64(0)
		if now == lastTime {
			newSequence = (sequence + 1) & 0xFFF
			if newSequence == 0 {
				// 序列号溢出，等待下一毫秒
				time.Sleep(time.Millisecond)
				continue
			}
		}

		// 尝试更新状态
		newState := (uint64(now) << 12) | uint64(newSequence)
		if s.state.CompareAndSwap(oldState, newState) {
			// 标准 Snowflake 位结构 (41+10+12)
			// 默认: 41bit 时间戳 + 5bit datacenterID + 5bit workerID + 12bit 序列号
			id := (now << 22) | (s.dcID << 17) | (s.workerID << 12) | newSequence
			return id, nil
		}
		// CAS 失败，重试
	}
}

// Next 生成下一个 ID
func (s *snowflake) Next() int64 {
	id, err := s.nextInt64()
	if err != nil {
		return -1
	}
	s.genCounter.Inc(context.Background())
	return id
}

// NextString 生成下一个 ID (字符串形式)
func (s *snowflake) NextString() string {
	id, err := s.nextInt64()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

// ParseGeneratorID 解析 Snowflake ID，返回其组成部分
func ParseGeneratorID(id int64) (timestamp, datacenterID, workerID, sequence int64) {
	timestamp = id >> 22
	datacenterID = (id >> 17) & 0x1F
	workerID = (id >> 12) & 0x1F
	sequence = id & 0xFFF
	return
}
