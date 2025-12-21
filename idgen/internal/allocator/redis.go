package allocator

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// RedisAllocator Redis 分配器
type RedisAllocator struct {
	client    connector.RedisConnector
	keyPrefix string
	ttl       time.Duration
	logger    clog.Logger
}

// NewRedis 创建 Redis 分配器
func NewRedis(conn connector.RedisConnector, keyPrefix string, ttl int, logger clog.Logger) *RedisAllocator {
	if keyPrefix == "" {
		keyPrefix = "genesis:idgen:worker"
	}
	if ttl <= 0 {
		ttl = 30
	}
	return &RedisAllocator{
		client:    conn,
		keyPrefix: keyPrefix,
		ttl:       time.Duration(ttl) * time.Second,
		logger:    logger,
	}
}

// Allocate 使用 Lua 脚本原子分配 WorkerID
func (a *RedisAllocator) Allocate(ctx context.Context) (int64, error) {
	// Lua 脚本：遍历 0-1023，尝试 SET NX EX
	script := `
		local prefix = KEYS[1]
		local value = ARGV[1]
		local ttl = tonumber(ARGV[2])
		for i = 0, 1023 do
			local key = prefix .. ":" .. i
			if redis.call("SET", key, value, "NX", "EX", ttl) then
				return i
			end
		end
		return -1
	`
	value := fmt.Sprintf("host:%d", time.Now().UnixNano())
	result, err := a.client.GetClient().Eval(ctx, script, []string{a.keyPrefix}, value, int(a.ttl.Seconds())).Result()
	if err != nil {
		return 0, fmt.Errorf("redis eval failed: %w", err)
	}
	id, ok := result.(int64)
	if !ok || id < 0 {
		return 0, fmt.Errorf("no available worker id")
	}
	return id, nil
}

// Start 启动保活任务
func (a *RedisAllocator) Start(ctx context.Context, workerID int64) (<-chan error, error) {
	failCh := make(chan error, 1)
	key := fmt.Sprintf("%s:%d", a.keyPrefix, workerID)

	if a.logger != nil {
		a.logger.Info("starting worker id keep alive",
			clog.Int64("worker_id", workerID),
			clog.String("key", key),
			clog.Duration("ttl", a.ttl),
		)
	}

	// 启动后台 goroutine 续期
	go func() {
		ticker := time.NewTicker(a.ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if a.logger != nil {
					a.logger.Info("keep alive stopped", clog.Int64("worker_id", workerID))
				}
				return
			case <-ticker.C:
				// 续期失败则熔断
				if err := a.client.GetClient().Expire(ctx, key, a.ttl).Err(); err != nil {
					if a.logger != nil {
						a.logger.Error("keep alive failed, circuit breaking",
							clog.Int64("worker_id", workerID),
							clog.Error(err),
						)
					}
					select {
					case failCh <- fmt.Errorf("keep alive failed: %w", err):
					default:
					}
					return
				}
			}
		}
	}()

	return failCh, nil
}
