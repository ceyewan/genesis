package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
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

// distributedLimiter 分布式限流器实现（非导出）
type distributedLimiter struct {
	client *redis.Client
	prefix string
	logger clog.Logger
	script *redis.Script

	// 指标
	allowedCounter metrics.Counter
	deniedCounter  metrics.Counter
}

// newDistributed 创建分布式限流器（内部函数）
func newDistributed(
	cfg *DistributedConfig,
	redisConn connector.RedisConnector,
	logger clog.Logger,
	meter metrics.Meter,
) (Limiter, error) {
	if redisConn == nil {
		return nil, ErrConnectorNil
	}

	if cfg == nil {
		cfg = &DistributedConfig{}
	}
	cfg.setDefaults()
	prefix := cfg.Prefix

	l := &distributedLimiter{
		client: redisConn.GetClient(),
		prefix: prefix,
		logger: logger,
		script: redis.NewScript(luaScript),
	}

	// 初始化指标
	if meter != nil {
		l.allowedCounter, _ = meter.Counter(MetricAllowed, "Number of allowed requests")
		l.deniedCounter, _ = meter.Counter(MetricDenied, "Number of denied requests")
	}

	if logger != nil {
		logger.Info("distributed rate limiter created", clog.String("prefix", prefix))
	}

	return l, nil
}

// Allow 尝试获取 1 个令牌
func (l *distributedLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
	return l.AllowN(ctx, key, limit, 1)
}

// AllowN 尝试获取 N 个令牌
func (l *distributedLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error) {
	if key == "" {
		return false, ErrKeyEmpty
	}

	if limit.Rate <= 0 || limit.Burst <= 0 {
		return false, ErrInvalidLimit
	}

	if n <= 0 {
		return false, ErrInvalidLimit
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
		return false, xerrors.Wrap(err, "execute lua script")
	}

	// 解析结果
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 2 {
		return false, xerrors.New("invalid lua script result")
	}

	allowed, ok := resultSlice[0].(int64)
	if !ok {
		return false, xerrors.New("invalid allowed value")
	}

	remaining, ok := resultSlice[1].(int64)
	if !ok {
		remaining = 0
	}

	isAllowed := allowed == 1

	// 记录指标
	if isAllowed {
		if l.allowedCounter != nil {
			l.allowedCounter.Inc(ctx, metrics.L(LabelMode, "distributed"))
		}
	} else {
		if l.deniedCounter != nil {
			l.deniedCounter.Inc(ctx, metrics.L(LabelMode, "distributed"))
		}
	}

	if l.logger != nil {
		l.logger.Debug("rate limit check",
			clog.String("key", key),
			clog.Bool("allowed", isAllowed),
			clog.Int64("remaining", remaining),
			clog.Float64("rate", limit.Rate),
			clog.Int("burst", limit.Burst),
			clog.Int("requested", n))
	}

	return isAllowed, nil
}

// Wait 阻塞等待直到获取 1 个令牌
// 注意：分布式模式不支持 Wait 操作
func (l *distributedLimiter) Wait(ctx context.Context, key string, limit Limit) error {
	// 分布式环境下 Wait 难以精确实现且代价高昂
	return ErrNotSupported
}

// Close 释放资源（分布式连接由 Connector 管理）
func (l *distributedLimiter) Close() error {
	return nil
}
