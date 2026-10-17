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

// luaScript 实现普通令牌桶，以 "tokens,last_us" 字符串保存剩余令牌和更新时间。
// 沿用原 Key 及字符串类型；旧的单时间戳值在首次读取时折算为剩余令牌。
// 所有读、补充、扣减及写入在同一次 Lua 调用中完成。
const luaScript = `
-- KEYS[1]：限流状态键。
-- ARGV[1]：Rate，每秒补充的令牌数。
-- ARGV[2]：Burst，桶容量；ARGV[3]：本次申请的令牌数。
-- 返回：{是否允许（1/0）, 剩余完整令牌数}。
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
if requested > capacity then return {0, 0} end

-- 1. 使用 Redis 统一时钟，先以整数微秒做差，再换算为秒。
local time_parts = redis.call("TIME")
local now = tonumber(time_parts[1]) * 1000000 + tonumber(time_parts[2])
local tokens, last = capacity, now
local state = redis.call("GET", KEYS[1])
if state then
  local saved_tokens, saved_last = string.match(state, "^([^,]+),([^,]+)$")
  if saved_tokens then
    tokens, last = tonumber(saved_tokens), tonumber(saved_last)
    if not tokens or not last then return redis.error_reply("invalid token bucket state") end
  else
    -- 旧值是理论放行时间。未偿还时间 * Rate 即已消耗额度，不能直接重置满桶。
    local previous = tonumber(state)
    if not previous then return redis.error_reply("invalid legacy token bucket state") end
    tokens = math.max(0, capacity - math.max(0, previous * 1000000 - now) / 1000000 * rate)
  end
end
-- 2. available = min(Burst, tokens + elapsed_seconds * Rate)。
-- 时钟回退时不补充令牌，也不把参考时间往回移动。
now = math.max(now, last)
local elapsed = (now - last) / 1000000
tokens = math.min(capacity, tokens + elapsed * rate)

-- 3. 令牌足够才扣减；拒绝请求也保存已补充的小数令牌。
local allowed = 0
if requested <= tokens then
  tokens = tokens - requested
  allowed = 1
end
-- 4. 显式格式化，避免 Lua 隐式转字符串时丢失令牌小数或微秒时间精度。
-- TTL 取两倍回满时间（最短 1 秒），使过期时的桶已恢复满额。
-- SET 同时替换状态及 TTL；不先删旧键，避免转换中途丢弃状态。
local value = string.format("%.17g,%.0f", tokens, now)
redis.call("SET", KEYS[1], value, "EX", math.max(1, math.ceil(2 * capacity / rate)))
return {allowed, math.floor(tokens)}
`

func validRedisLimit(limit Limit) bool {
	if !limit.valid() || int64(limit.Burst) > maxRedisExactInteger {
		return false
	}
	// 限制过期时间在 Redis/Lua 可安全表示的范围内，拒绝导致溢出的极小 Rate。
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
		"%s%s:rate=%s:burst=%d",
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
