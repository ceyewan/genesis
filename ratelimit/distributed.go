package ratelimit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync/atomic"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	maxRedisExactInteger int64 = 1<<53 - 1
	maxRedisTTLSeconds         = maxRedisExactInteger / 1000
)

// luaScript is a standard token bucket: refill by elapsed time, cap at Burst,
// then consume n tokens. State contains only remaining tokens and last update.
// Integer microseconds avoid adding tiny intervals to absolute Unix seconds.
const luaScript = `
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
if requested > capacity then return {0, 0} end

local time_parts = redis.call("TIME")
local now = tonumber(time_parts[1]) * 1000000 + tonumber(time_parts[2])
local state = redis.call("HMGET", KEYS[1], "tokens", "last_us")
local tokens = tonumber(state[1]) or capacity
local last = tonumber(state[2]) or now
-- A backward clock step must not refill tokens or move the reference backward.
now = math.max(now, last)
local elapsed = (now - last) / 1000000
tokens = math.min(capacity, tokens + elapsed * rate)

local allowed = 0
if requested <= tokens then
  tokens = tokens - requested
  allowed = 1
end
-- Preserve fractional tokens and integer microseconds without Lua's implicit
-- 14-significant-digit conversion when passing numbers to Redis commands.
redis.call("HSET", KEYS[1], "tokens", string.format("%.17g", tokens),
  "last_us", string.format("%.0f", now))
redis.call("EXPIRE", KEYS[1], math.max(1, math.ceil(2 * capacity / rate)))
return {allowed, math.floor(tokens)}
`

func validRedisLimit(limit Limit) bool {
	if !limit.valid() || int64(limit.Burst) > maxRedisExactInteger {
		return false
	}
	// Keep expiration arithmetic representable by Redis and Lua. This also
	// rejects extremely small rates whose full refill horizon overflows.
	horizon := 2 * float64(limit.Burst) / limit.Rate
	return !math.IsInf(horizon, 1) && horizon <= float64(maxRedisTTLSeconds)
}

// distributedLimiter 分布式限流器实现（非导出）
type distributedLimiter struct {
	client *redis.Client
	prefix string
	logger clog.Logger
	script *redis.Script

	// 指标
	allowedCounter metrics.Counter
	deniedCounter  metrics.Counter
	errorCounter   metrics.Counter
	closed         atomic.Bool
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
	client := redisConn.GetClient()
	if client == nil {
		return nil, xerrors.Wrap(connector.ErrClientNil, "ratelimit: redis connector is not connected")
	}

	if cfg == nil {
		cfg = &DistributedConfig{}
	}
	cfg.setDefaults()
	prefix := cfg.Prefix

	l := &distributedLimiter{
		client: client,
		prefix: prefix,
		logger: logger,
		script: redis.NewScript(luaScript),
	}

	// 初始化指标
	if meter != nil {
		l.allowedCounter, _ = meter.Counter(MetricAllowed, "Number of allowed requests")
		l.deniedCounter, _ = meter.Counter(MetricDenied, "Number of denied requests")
		l.errorCounter, _ = meter.Counter(MetricErrors, "Number of limiter operation errors")
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
func (l *distributedLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (allowed bool, err error) {
	defer func() {
		if err != nil {
			recordLimiterError(ctx, l.errorCounter, "distributed")
		}
	}()
	if l.closed.Load() {
		return false, ErrLimiterClosed
	}
	if key == "" {
		return false, ErrKeyEmpty
	}

	if !validRedisLimit(limit) {
		return false, ErrInvalidLimit
	}

	if n <= 0 {
		return false, ErrInvalidLimit
	}
	if n > limit.Burst {
		if l.deniedCounter != nil {
			l.deniedCounter.Inc(ctx, metrics.L(LabelMode, "distributed"))
		}
		return false, nil
	}

	// 构建 Redis key
	fullKey := l.buildKey(key, limit)

	// 执行 Lua 脚本
	result, err := l.script.Run(ctx, l.client, []string{fullKey}, limit.Rate, limit.Burst, n).Result()
	if err != nil {
		if l.logger != nil {
			l.logger.Error("failed to execute lua script",
				clog.String("key", key),
				clog.Error(err))
		}
		return false, xerrors.Wrap(err, "execute lua script")
	}

	// 解析结果
	resultSlice, ok := result.([]any)
	if !ok || len(resultSlice) != 2 {
		return false, xerrors.New("invalid lua script result")
	}

	allowedValue, ok := resultSlice[0].(int64)
	if !ok {
		return false, xerrors.New("invalid allowed value")
	}

	remaining, ok := resultSlice[1].(int64)
	if !ok {
		remaining = 0
	}

	isAllowed := allowedValue == 1

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

func (l *distributedLimiter) buildKey(key string, limit Limit) string {
	return fmt.Sprintf(
		"%s%s:rate=%s:burst=%d:v2",
		l.prefix,
		key,
		strconv.FormatFloat(limit.Rate, 'g', -1, 64),
		limit.Burst,
	)
}

// Wait 阻塞等待直到获取 1 个令牌
// 注意：分布式模式不支持 Wait 操作
func (l *distributedLimiter) Wait(ctx context.Context, key string, limit Limit) (err error) {
	defer func() {
		if err != nil {
			recordLimiterError(ctx, l.errorCounter, "distributed")
		}
	}()
	if l.closed.Load() {
		return ErrLimiterClosed
	}
	// 分布式环境下 Wait 难以精确实现且代价高昂
	return ErrNotSupported
}

// Close 释放资源（分布式连接由 Connector 管理）
func (l *distributedLimiter) Close() error {
	l.closed.Store(true)
	return nil
}
