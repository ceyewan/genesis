package distributed

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	metrics "github.com/ceyewan/genesis/pkg/metrics"
	"github.com/ceyewan/genesis/pkg/ratelimit/types"
)

// luaScript 令牌桶算法的 Lua 脚本
const luaScript = `
-- 令牌桶算法的纯时间戳实现 (Token Bucket with Timestamp)
-- KEYS[1]: 限流器的唯一键
-- ARGV[1]: 速率 (rate, 每秒允许的请求数)
-- ARGV[2]: 桶容量 (capacity, 峰值/并发容量)
-- ARGV[3]: 当前时间戳 (now, 浮点数，秒.毫秒)
-- ARGV[4]: 本次请求需要消耗的令牌数 (tokens_to_consume)

local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- 每个令牌代表的时间间隔（秒）
local interval_per_token = 1 / rate
-- 桶装满所需要的时间
local fill_time = capacity * interval_per_token

-- 获取上一次的状态（即下一次允许放行的时间戳）
local last_refreshed = tonumber(redis.call("GET", KEYS[1]))
if last_refreshed == nil then
  last_refreshed = now
end

-- 计算理论上的下一次放行时间
local next_available_time = math.max(last_refreshed, now)

-- 判断是否允许请求
local new_refreshed = next_available_time + requested * interval_per_token
local allow_at_most = now + fill_time

if new_refreshed <= allow_at_most then
  -- 令牌足够，请求被允许
  redis.call("SET", KEYS[1], new_refreshed, "EX", math.ceil(fill_time * 2))
  
  -- 计算剩余可用令牌数
  local remaining_tokens = math.floor((allow_at_most - new_refreshed) / interval_per_token)
  
  return {1, remaining_tokens}
else
  -- 令牌不足，拒绝请求
  local remaining_tokens = math.floor((allow_at_most - next_available_time) / interval_per_token)
  
  return {0, remaining_tokens}
end
`

// Limiter 分布式限流器实现
type Limiter struct {
	client *redis.Client
	prefix string
	logger clog.Logger
	meter  metrics.Meter
	script *redis.Script
}

// New 创建分布式限流器
func New(
	cfg *types.Config,
	redisConn connector.RedisConnector,
	logger clog.Logger,
	meter metrics.Meter,
) (*Limiter, error) {
	if redisConn == nil {
		return nil, fmt.Errorf("redis connector is nil")
	}

	prefix := cfg.Distributed.Prefix
	if prefix == "" {
		prefix = "ratelimit:"
	}

	// 派生 Logger
	if logger != nil {
		logger = logger.WithNamespace("ratelimit.distributed")
	}

	l := &Limiter{
		client: redisConn.GetClient(),
		prefix: prefix,
		logger: logger,
		meter:  meter,
		script: redis.NewScript(luaScript),
	}

	if logger != nil {
		logger.Info("distributed rate limiter created", clog.String("prefix", prefix))
	}

	return l, nil
}

// Allow 尝试获取 1 个令牌
func (l *Limiter) Allow(ctx context.Context, key string, limit types.Limit) (bool, error) {
	return l.AllowN(ctx, key, limit, 1)
}

// AllowN 尝试获取 N 个令牌
func (l *Limiter) AllowN(ctx context.Context, key string, limit types.Limit, n int) (bool, error) {
	if key == "" {
		return false, types.ErrKeyEmpty
	}

	if limit.Rate <= 0 || limit.Burst <= 0 {
		return false, types.ErrInvalidLimit
	}

	if n <= 0 {
		return false, fmt.Errorf("n must be positive")
	}

	// 构建 Redis key
	fullKey := l.prefix + key

	// 当前时间戳（秒.毫秒）
	now := float64(time.Now().UnixNano()) / 1e9

	// 执行 Lua 脚本
	result, err := l.script.Run(ctx, l.client, []string{fullKey}, limit.Rate, limit.Burst, now, n).Result()
	if err != nil {
		if l.logger != nil {
			l.logger.Error("failed to execute lua script",
				clog.String("key", key),
				clog.Error(err))
		}
		return false, fmt.Errorf("execute lua script: %w", err)
	}

	// 解析结果
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 2 {
		return false, fmt.Errorf("invalid lua script result")
	}

	allowed, ok := resultSlice[0].(int64)
	if !ok {
		return false, fmt.Errorf("invalid allowed value")
	}

	remaining, ok := resultSlice[1].(int64)
	if !ok {
		remaining = 0
	}

	if l.logger != nil {
		l.logger.Debug("rate limit check",
			clog.String("key", key),
			clog.Bool("allowed", allowed == 1),
			clog.Int64("remaining", remaining),
			clog.Float64("rate", limit.Rate),
			clog.Int("burst", limit.Burst),
			clog.Int("requested", n))
	}

	return allowed == 1, nil
}

// Wait 阻塞等待直到获取 1 个令牌
// 注意：分布式模式不支持 Wait 操作
func (l *Limiter) Wait(ctx context.Context, key string, limit types.Limit) error {
	// 分布式环境下 Wait 难以精确实现且代价高昂
	return types.ErrNotSupported
}
