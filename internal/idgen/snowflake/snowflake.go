package snowflake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/internal/idgen/allocator"
	"github.com/ceyewan/genesis/pkg/idgen/types"
)

const (
	// maxClockBackwards 最大容忍的时钟回拨时间 (1秒)
	maxClockBackwards = 1000 * time.Millisecond
	// smallClockBackwards 微小回拨阈值 (5ms)，在此范围内尝试复用 lastTime
	smallClockBackwards = 5 * time.Millisecond
)

// Generator Snowflake 生成器实现
type Generator struct {
	mu        sync.Mutex
	allocator allocator.Allocator
	workerID  int64
	dcID      int64
	sequence  int64
	lastTime  int64
	// 熔断通道
	failCh <-chan error
}

// New 创建 Snowflake 生成器
func New(cfg *types.SnowflakeConfig, alloc allocator.Allocator) (*Generator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("snowflake config is nil")
	}
	if alloc == nil {
		return nil, fmt.Errorf("allocator is nil")
	}
	return &Generator{
		allocator: alloc,
		dcID:      cfg.DatacenterID,
	}, nil
}

// Init 初始化生成器 (分配 WorkerID 并启动保活)
func (g *Generator) Init(ctx context.Context) error {
	// 1. 分配 WorkerID
	workerID, err := g.allocator.Allocate(ctx)
	if err != nil {
		return fmt.Errorf("allocate worker id failed: %w", err)
	}
	g.workerID = workerID

	// 2. 启动保活
	failCh, err := g.allocator.Start(ctx, workerID)
	if err != nil {
		return fmt.Errorf("start keep alive failed: %w", err)
	}
	g.failCh = failCh

	return nil
}

// Int64 生成 int64 ID
func (g *Generator) Int64() (int64, error) {
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
func (g *Generator) String() string {
	id, err := g.Int64()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d", id)
}
