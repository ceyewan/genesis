package idgen

import (
	"sync"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	// maxClockBackwards 最大容忍的时钟回拨时间 (1秒)
	maxClockBackwards = 1000 * time.Millisecond
	// smallClockBackwards 微小回拨阈值 (5ms)，在此范围内尝试复用 lastTime
	smallClockBackwards = 5 * time.Millisecond
)

// Snowflake 雪花算法生成器
// 提供高性能的分布式有序 ID 生成能力，无需外部依赖
type Snowflake struct {
	mu       sync.Mutex
	workerID int64
	dcID     int64
	sequence int64
	lastTime int64
	logger   clog.Logger
}

// SnowflakeOption Snowflake 初始化选项
type SnowflakeOption func(*Snowflake)

// WithSnowflakeLogger 设置 Logger
func WithSnowflakeLogger(logger clog.Logger) SnowflakeOption {
	return func(s *Snowflake) {
		s.logger = logger
	}
}

// NewSnowflake 创建 Snowflake 生成器
//
// 参数:
//   - workerID: 工作节点 ID [0, 1023] (当 DatacenterID=0 时)
//   - opts: 可选参数 (DatacenterID, Logger)
//
// 注意: workerID 和 datacenterID 的总比特数不能超过 10 bit
// 默认分配: 5 bit datacenterID + 5 bit workerID
//
// 使用示例:
//
//	// 简单使用
//	sf, _ := idgen.NewSnowflake(1)
//	id := sf.NextInt64()
//
//	// 带配置
//	sf, _ := idgen.NewSnowflake(100,
//	    idgen.WithDatacenterID(1),
//	    idgen.WithSnowflakeLogger(logger),
//	)
func NewSnowflake(workerID int64, opts ...SnowflakeOption) (*Snowflake, error) {
	sf := &Snowflake{
		workerID: workerID,
		dcID:     0,
	}

	for _, opt := range opts {
		opt(sf)
	}

	// 校验位宽冲突
	// 1. 如果使用了 DatacenterID (>0)，则 WorkerID 只能用 5 bit (Max 31)
	if sf.dcID > 0 && workerID > 31 {
		return nil, xerrors.WithCode(ErrInvalidInput, "worker_id_overflow_with_dc")
	}

	// 2. 如果没有使用 DatacenterID (=0)，则 WorkerID 可以用 10 bit (Max 1023)
	if workerID < 0 || workerID > 1023 {
		return nil, xerrors.WithCode(ErrInvalidInput, "worker_id_out_of_range")
	}

	// 确保 logger 不为空，避免后续调用 panic
	if sf.logger == nil {
		sf.logger = clog.Discard()
	}

	sf.logger.Info("snowflake generator created",
		clog.Int64("worker_id", workerID),
		clog.Int64("datacenter_id", sf.dcID),
	)

	return sf, nil
}

// WithDatacenterID 设置数据中心 ID
func WithDatacenterID(dcID int64) SnowflakeOption {
	return func(s *Snowflake) {
		s.dcID = dcID
	}
}

// NextInt64 生成 int64 ID
func (s *Snowflake) nextInt64() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	// 默认: 41bit 时间戳 + 5bit datacenterID + 5bit workerID + 12bit 序列号
	id := (now << 22) | (s.dcID << 17) | (s.workerID << 12) | s.sequence

	return id, nil
}

// Next 返回字符串形式的 ID
func (s *Snowflake) Next() int64 {
	id, err := s.nextInt64()
	if err != nil {
		return -1
	}
	return id
}
