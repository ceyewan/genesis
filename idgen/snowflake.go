package idgen

import (
	"context"
	"sync"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/idgen/internal/allocator"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	// maxClockBackwards 最大容忍的时钟回拨时间 (1秒)
	maxClockBackwards = 1000 * time.Millisecond
	// smallClockBackwards 微小回拨阈值 (5ms)，在此范围内尝试复用 lastTime
	smallClockBackwards = 5 * time.Millisecond
)

// snowflakeGen Snowflake 生成器实现（非导出）
type snowflakeGen struct {
	mu       sync.Mutex
	alloc    allocator.Allocator
	workerID int64
	dcID     int64
	sequence int64
	lastTime int64
	failCh   <-chan error
	logger   clog.Logger
	meter    metrics.Meter
}

// newSnowflakeGen 创建 Snowflake 生成器（内部函数）
func newSnowflakeGen(
	cfg *SnowflakeConfig,
	alloc allocator.Allocator,
	logger clog.Logger,
	meter metrics.Meter,
) (Int64Generator, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrConfigNil, "snowflake_config_nil")
	}
	if alloc == nil {
		return nil, xerrors.WithCode(ErrConnectorNil, "allocator_nil")
	}
	return &snowflakeGen{
		alloc:  alloc,
		dcID:   cfg.DatacenterID,
		logger: logger,
		meter:  meter,
	}, nil
}

// init 初始化生成器 (分配 WorkerID 并启动保活)
func (s *snowflakeGen) init(ctx context.Context) error {
	// 1. 分配 WorkerID
	workerID, err := s.alloc.Allocate(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("failed to allocate worker id", clog.Error(err))
		}
		return xerrors.Wrap(err, "allocate worker id")
	}
	s.workerID = workerID

	if s.logger != nil {
		s.logger.Info("worker id allocated",
			clog.Int64("worker_id", workerID),
			clog.Int64("datacenter_id", s.dcID),
		)
	}

	// 2. 启动保活
	failCh, err := s.alloc.Start(ctx, workerID)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("failed to start keep alive", clog.Error(err))
		}
		return xerrors.Wrap(err, "start keep alive")
	}
	s.failCh = failCh

	return nil
}

// NextInt64 生成 int64 ID
func (s *snowflakeGen) NextInt64() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检查熔断
	select {
	case err := <-s.failCh:
		return 0, xerrors.Wrap(err, "worker id lease lost")
	default:
	}

	now := time.Now().UnixMilli()

	// 处理时钟回拨
	if now < s.lastTime {
		drift := time.Duration(s.lastTime-now) * time.Millisecond

		if drift <= smallClockBackwards {
			// 1. 微小回拨 (<= 5ms): 尝试复用 lastTime
			if s.sequence < 0xFFF {
				now = s.lastTime
			} else {
				// 序列号已满，必须等待
				time.Sleep(drift + time.Millisecond)
				now = time.Now().UnixMilli()
			}
		} else if drift <= maxClockBackwards {
			// 2. 小回拨 (5ms < drift <= 1s): 等待时钟追上
			time.Sleep(drift + time.Millisecond)
			now = time.Now().UnixMilli()
		} else {
			// 3. 大回拨 (> 1s): 拒绝服务
			return 0, xerrors.Wrapf(ErrClockBackwards, "drift: %v (max: %v)", drift, maxClockBackwards)
		}
	}

	if now == s.lastTime {
		s.sequence = (s.sequence + 1) & 0xFFF // 12 位序列号
		if s.sequence == 0 {
			// 序列号溢出，等待下一毫秒
			for now <= s.lastTime {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}

	s.lastTime = now

	// 标准 Snowflake 位结构 (41+10+12)
	id := (now << 22) | (s.dcID << 17) | (s.workerID << 12) | s.sequence

	return id, nil
}

// Next 返回字符串形式的 ID
func (s *snowflakeGen) Next() string {
	id, err := s.NextInt64()
	if err != nil {
		return ""
	}
	return format64(id)
}

// Close 实现 io.Closer 接口，但由于 snowflakeGen 不拥有 Connector 资源，
// 所以这是 no-op，符合资源所有权规范
func (s *snowflakeGen) Close() error {
	// No-op: SnowflakeGen 不拥有 Redis/Etcd 连接，由 Connector 管理
	return nil
}
