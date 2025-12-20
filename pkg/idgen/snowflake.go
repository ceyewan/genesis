package idgen

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/internal/idgen/allocator"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/metrics"
)

const (
	// maxClockBackwards 最大容忍的时钟回拨时间 (1秒)
	maxClockBackwards = 1000 * time.Millisecond
	// smallClockBackwards 微小回拨阈值 (5ms)，在此范围内尝试复用 lastTime
	smallClockBackwards = 5 * time.Millisecond
)

// snowflakeGenerator Snowflake 生成器实现（非导出）
type snowflakeGenerator struct {
	mu        sync.Mutex
	allocator allocator.Allocator
	workerID  int64
	dcID      int64
	sequence  int64
	lastTime  int64
	// 熔断通道
	failCh <-chan error
	// 可观测性组件
	logger clog.Logger
	meter  metrics.Meter
	tracer interface{} // TODO: 实现 Tracer 接口，暂时使用 interface{}
}

// newSnowflake 创建 Snowflake 生成器（内部函数）
func newSnowflake(
	cfg *SnowflakeConfig,
	alloc allocator.Allocator,
	logger clog.Logger,
	meter metrics.Meter,
	tracer interface{},
) (Int64Generator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("snowflake config is nil")
	}
	if alloc == nil {
		return nil, fmt.Errorf("allocator is nil")
	}
	return &snowflakeGenerator{
		allocator: alloc,
		dcID:      cfg.DatacenterID,
		logger:    logger,
		meter:     meter,
		tracer:    tracer,
	}, nil
}

// Init 初始化生成器 (分配 WorkerID 并启动保活)
func (g *snowflakeGenerator) Init(ctx context.Context) error {
	// 1. 分配 WorkerID
	workerID, err := g.allocator.Allocate(ctx)
	if err != nil {
		if g.logger != nil {
			g.logger.Error("failed to allocate worker id", clog.Error(err))
		}
		return fmt.Errorf("allocate worker id failed: %w", err)
	}
	g.workerID = workerID

	if g.logger != nil {
		g.logger.Info("worker id allocated",
			clog.Int64("worker_id", workerID),
			clog.Int64("datacenter_id", g.dcID),
		)
	}

	// 2. 启动保活
	failCh, err := g.allocator.Start(ctx, workerID)
	if err != nil {
		if g.logger != nil {
			g.logger.Error("failed to start keep alive", clog.Error(err))
		}
		return fmt.Errorf("start keep alive failed: %w", err)
	}
	g.failCh = failCh

	return nil
}

// Int64 生成 int64 ID
func (g *snowflakeGenerator) Int64() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// 检查熔断
	select {
	case err := <-g.failCh:
		return 0, fmt.Errorf("worker id lease lost: %w", err)
	default:
	}

	now := time.Now().UnixMilli()

	// 处理时钟回拨
	if now < g.lastTime {
		drift := time.Duration(g.lastTime-now) * time.Millisecond

		if drift <= smallClockBackwards {
			// 1. 微小回拨 (<= 5ms): 尝试复用 lastTime
			// 只有当序列号未溢出时才能复用
			if g.sequence < 0xFFF {
				now = g.lastTime
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
			return 0, fmt.Errorf("clock moved backwards too much: %v (max allowed: %v)", drift, maxClockBackwards)
		}
	}

	if now == g.lastTime {
		g.sequence = (g.sequence + 1) & 0xFFF // 12 位序列号
		if g.sequence == 0 {
			// 序列号溢出，等待下一毫秒
			for now <= g.lastTime {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		g.sequence = 0
	}

	g.lastTime = now

	// 标准 Snowflake 位结构 (41+10+12)
	id := (now << 22) | (g.dcID << 17) | (g.workerID << 12) | g.sequence
	return id, nil
}

// String 返回字符串形式的 ID
func (g *snowflakeGenerator) String() string {
	id, err := g.Int64()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

// Close 实现 io.Closer 接口，但由于 snowflakeGenerator 不拥有 Connector 资源，
// 所以这是 no-op，符合资源所有权规范
func (g *snowflakeGenerator) Close() error {
	// No-op: SnowflakeGenerator 不拥有 Redis/Etcd 连接，由 Connector 管理
	// 调用方应关闭 Connector 而非 SnowflakeGenerator
	return nil
}
