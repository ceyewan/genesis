# 治理组件 (Ratelimit/Breaker) 重构设计文档

## 概述

本文档记录了 Genesis 治理层组件（Ratelimit 和 Breaker）的重构设计，旨在解决当前存在的侵入性、熔断粒度粗糙等问题。

**关联 Issue**: #19

**参考设计**: `clog` 组件的设计模式（Discard、Option、Config）

---

## 一、问题分析

### 1.1 Ratelimit: 强依赖导致的初始化侵入性 (Major)

**现状**:
- 组件强制要求显式创建 `Limiter` 实例并注入到业务流中
- 缺乏空对象模式（类似 `clog.Discard()`）
- `GinMiddleware` 必须传入非 `nil` 的 `Limiter` 实例

**影响**:
- 无法实现"按需开启、透明接入"
- 增加系统初始化复杂度
- 产生不必要的内存开销

---

### 1.2 Ratelimit: 缺少 gRPC 拦截器支持 (Major)

**现状**:
- 仅支持 HTTP/Gin 中间件
- 无法在 gRPC 客户端/服务端使用

**影响**:
- gRPC 服务需要手动调用 `Allow`，侵入业务代码
- 无法统一 HTTP 和 gRPC 的限流体验

---

### 1.3 Ratelimit: 限流维度单一 (Minor)

**现状**:
- `Allow(key, limit)` 的 key 由调用方自行构造
- 没有统一的维度抽象（服务级、方法级、用户级等）
- 与 breaker 的维度管理不一致

---

### 1.4 Breaker: 熔断粒度过粗 (Major - 致命)

**现状**:
- `UnaryClientInterceptor` 使用 `cc.Target()` (如 `etcd:///logic-service`) 作为熔断 Key
- 在客户端负载均衡场景下，单个后端实例故障会触发整个服务的熔断

**代码位置**: [interceptor.go:24-25](../breaker/interceptor.go#L24-L25)

**影响场景**:
```
Client
  ├─ Backend A (10.0.0.1:9001) - 健康
  ├─ Backend B (10.0.0.2:9001) - 故障 ❌
  └─ Backend C (10.0.0.3:9001) - 健康

问题：Backend B 故障触发熔断 → 整个 service 被熔断 → A 和 C 也无法访问
```

---

### 1.5 Breaker: 流式熔断保护有限 (Minor)

**现状**:
- `StreamClientInterceptor` 仅在流建立阶段进行熔断保护
- 长连接建立后的 `Send/Recv` 错误无法触发熔断

---

## 二、重构设计

### 2.1 Ratelimit 重构

#### 2.1.1 新增 Discard 模式（参考 clog.Discard）

**设计**: 提供零开销的 No-op 实现

```go
// Discard 返回一个静默的限流器实例
// 返回的 Limiter 实现了接口，但所有方法始终返回 true（允许通过）
//
// 使用示例:
//
//	limiter := ratelimit.Discard()
//	allowed, _ := limiter.Allow(ctx, "any-key", ratelimit.Limit{Rate: 0, Burst: 0})
//	// allowed 永远为 true
func Discard() Limiter

// noopLimiter 空限流器实现（非导出）
type noopLimiter struct{}

func (n *noopLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
    return true, nil
}

func (n *noopLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error) {
    return true, nil
}
```

**使用场景**: 配置驱动的条件启用

```go
var limiter ratelimit.Limiter
if cfg.RateLimitEnabled {
    limiter, _ = ratelimit.NewStandalone(&cfg.Standalone, ratelimit.WithLogger(logger))
} else {
    limiter = ratelimit.Discard()  // 零开销
}

// 中间件使用同一接口，无需修改
r.Use(ratelimit.GinMiddleware(limiter, nil, limitFunc))
```

#### 2.1.2 统一 Config 结构（参考 clog.Config）

```go
// Config 限流组件统一配置
type Config struct {
    // Enabled 是否启用限流（默认 false）
    // 为 false 时 New() 返回 Discard() 实例
    Enabled bool `json:"enabled" yaml:"enabled"`

    // Mode 限流模式: "standalone" | "distributed"
    Mode string `json:"mode" yaml:"mode"`

    // Standalone 单机限流配置
    Standalone *StandaloneConfig `json:"standalone" yaml:"standalone"`

    // Distributed 分布式限流配置
    Distributed *DistributedConfig `json:"distributed" yaml:"distributed"`
}

// New 根据配置创建 Limiter
// 当 cfg.Enabled == false 时，返回 Discard() 实例
//
// 使用示例:
//
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Enabled: true,
//	    Mode:    "standalone",
//	    Standalone: &ratelimit.StandaloneConfig{
//	        CleanupInterval: 1 * time.Minute,
//	    },
//	}, ratelimit.WithLogger(logger))
func New(cfg *Config, opts ...Option) (Limiter, error)

// 保留原有工厂函数作为便捷方法
func NewStandalone(cfg *StandaloneConfig, opts ...Option) (Limiter, error)
func NewDistributed(redisConn connector.RedisConnector, cfg *DistributedConfig, opts ...Option) (Limiter, error)
```

#### 2.1.3 新增 gRPC 拦截器（仅一元调用）

**服务端拦截器**:

```go
// UnaryServerInterceptor 返回 gRPC 一元调用服务端拦截器
//
// 使用示例:
//
//	server := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        ratelimit.UnaryServerInterceptor(limiter, keyFunc, limitFunc),
//	    ),
//	)
func UnaryServerInterceptor(
    limiter Limiter,
    keyFunc func(ctx context.Context, fullMethod string) string,
    limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.UnaryServerInterceptor
```

**客户端拦截器**:

```go
// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
func UnaryClientInterceptor(
    limiter Limiter,
    keyFunc func(ctx context.Context, fullMethod string) string,
    limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.UnaryClientInterceptor
```

**KeyFunc 工具函数**:

```go
// KeyFunc 限流键生成函数类型
type KeyFunc func(ctx context.Context, fullMethod string) string

// ServiceLevelKey 服务级别限流键
// 返回: "logic-service"
func ServiceLevelKey() KeyFunc

// MethodLevelKey 方法级别限流键
// 返回: "/pkg.Service/Method"
func MethodLevelKey() KeyFunc

// UserLevelKey 用户级别限流键
// 从 Context 提取 user_id
// 返回: "user:123"
func UserLevelKey() KeyFunc

// IPLevelKey IP 级别限流键
// 从 Peer 信息提取 IP
// 返回: "ip:10.0.0.1"
func IPLevelKey() KeyFunc

// CompositeKey 组合多维度
// 返回: "logic-service:/pkg.Service/Method:user:123"
func CompositeKey(keyFuncs ...KeyFunc) KeyFunc
```

#### 2.1.4 限流维度抽象

```go
// Dimension 限流维度
type Dimension string

const (
    DimensionService   Dimension = "service"   // 服务级别
    DimensionMethod    Dimension = "method"    // 方法级别
    DimensionUser      Dimension = "user"     // 用户级别
    DimensionIP        Dimension = "ip"       // IP 级别
    DimensionAPIKey    Dimension = "api_key"  // API Key 级别
    DimensionTenant    Dimension = "tenant"   // 租户级别
)

// LimitRule 限流规则定义
type LimitRule struct {
    Dimension Dimension               // 限流维度
    Limit     Limit                   // 限流配置
    KeyFunc   KeyFunc                 // 自定义 Key 生成
    Matchers  []func(fullMethod string) bool // 方法匹配器
}

// RuleBasedLimiter 基于规则的限流器（可选高级功能）
type RuleBasedLimiter interface {
    Limiter

    // AddRule 添加限流规则
    AddRule(rule LimitRule) error

    // RemoveRule 移除限流规则
    RemoveRule(dimension Dimension, fullMethod string) error

    // Check 检查请求是否允许（自动匹配规则）
    Check(ctx context.Context, fullMethod string) (bool, error)
}
```

---

### 2.2 Breaker 重构

#### 2.2.1 Key 生成策略抽象

```go
// KeyFunc 从 gRPC 调用上下文中提取熔断 Key
type KeyFunc func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string

// ========================================
// 内置 KeyFunc 实现
// ========================================

// ServiceLevelKey 服务级别 Key（原有行为）
// 使用服务名作为熔断维度
// 返回示例: "etcd:///logic-service"
func ServiceLevelKey() KeyFunc

// BackendLevelKey 后端级别 Key
// 尝试从 Peer 信息中提取真实后端地址
// 返回示例: "10.0.0.1:9001"
// 注意: 需要等连接建立后才能获取 Peer 信息，第一次调用可能回退到服务名
func BackendLevelKey() KeyFunc

// MethodLevelKey 方法级别 Key
// 按方法进行熔断
// 返回示例: "/pkg.Service/Method"
func MethodLevelKey() KeyFunc

// CompositeKey 组合 Key
// 组合服务名和后端地址
// 返回示例: "etcd:///logic-service@10.0.0.1:9001"
func CompositeKey(primary KeyFunc, secondary ...KeyFunc) KeyFunc
```

#### 2.2.2 InterceptorOption 模式（参考 clog.Option）

```go
// InterceptorOption 拦截器选项函数类型
type InterceptorOption func(*interceptorConfig)

// WithKeyFunc 设置 Key 生成函数
func WithKeyFunc(fn KeyFunc) InterceptorOption

// WithServiceLevelKey 使用服务级别 Key（默认）
func WithServiceLevelKey() InterceptorOption

// WithBackendLevelKey 使用后端级别 Key
func WithBackendLevelKey() InterceptorOption

// WithMethodLevelKey 使用方法级别 Key
func WithMethodLevelKey() InterceptorOption

// 内部配置
type interceptorConfig struct {
    keyFunc KeyFunc
}
```

#### 2.2.3 Breaker 接口调整

```go
// Breaker 熔断器核心接口
type Breaker interface {
    // Execute 执行受熔断保护的函数
    Execute(ctx context.Context, key string, fn func() (interface{}, error)) (interface{}, error)

    // UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
    // 支持 InterceptorOption 配置 Key 生成策略
    UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor

    // State 获取指定 Key 的熔断器状态
    State(key string) (State, error)
}

// 注: StreamClientInterceptor 保留现有实现，暂不扩展新功能
// 流式调用的熔断增强（双向流场景）作为未来优化项
```

#### 2.2.4 拦截器实现改造

```go
func (cb *circuitBreaker) UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor {
    // 默认使用服务级别 Key
    cfg := &interceptorConfig{keyFunc: ServiceLevelKey()}
    for _, opt := range opts {
        opt(cfg)
    }

    return func(ctx context.Context, fullMethod string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
        // 使用配置的 KeyFunc 生成熔断 Key
        key := cfg.keyFunc(ctx, fullMethod, cc)

        if cb.logger != nil {
            cb.logger.Debug("unary call with circuit breaker",
                clog.String("key", key),
                clog.String("method", fullMethod))
        }

        // 使用熔断器执行调用
        _, err := cb.Execute(ctx, key, func() (interface{}, error) {
            err := invoker(ctx, fullMethod, req, reply, cc, opts...)
            return nil, err
        })

        // ... 指标记录逻辑
        return err
    }
}
```

#### 2.2.5 使用示例

```go
// 场景 1: 默认行为（服务级别熔断）
conn, _ := grpc.Dial(
    "etcd:///logic-service",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
)

// 场景 2: 后端级别熔断（推荐用于负载均衡场景）
conn, _ := grpc.Dial(
    "etcd:///logic-service",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
        breaker.WithBackendLevelKey(),
    )),
)

// 场景 3: 组合维度（服务 + 后端）
conn, _ := grpc.Dial(
    "etcd:///logic-service",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
        breaker.WithKeyFunc(breaker.CompositeKey(
            breaker.ServiceLevelKey(),
            breaker.BackendLevelKey(),
        )),
    )),
)

// 场景 4: 自定义 Key 生成（按 Method 熔断）
conn, _ := grpc.Dial(
    "localhost:9001",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
        breaker.WithKeyFunc(func(ctx context.Context, method string, cc *grpc.ClientConn) string {
            return method  // "/pkg.Service/Method"
        }),
    )),
)
```

---

## 三、统一维度抽象

为保持治理组件的一致性，定义统一的维度抽象：

### 3.1 共享维度类型

```go
// governance/internal/dimension/dimension.go
package dimension

// Type 治理维度类型
type Type string

const (
    Service   Type = "service"   // 服务级别
    Method    Type = "method"    // 方法级别
    Backend   Type = "backend"   // 后端实例级别
    User      Type = "user"     // 用户级别
    IP        Type = "ip"       // IP 级别
    Tenant    Type = "tenant"   // 租户级别
    APIKey    Type = "api_key"  // API Key 级别
)

// Key 生成函数类型（通用）
type KeyFunc func(ctx context.Context, fullMethod string, opts ...Option) string

// Option Key 生成选项
type Option func(*keyConfig)

type keyConfig struct {
    cc      *grpc.ClientConn
    fallback string
}
```

### 3.2 组件间共享

```go
// ratelimit 使用
import "github.com/ceyewan/genesis/governance/internal/dimension"

keyFunc := dimension.ServiceLevelKey()
limiter.Allow(ctx, keyFunc(ctx, fullMethod), limit)

// breaker 使用
keyFunc := dimension.BackendLevelKey()
breaker.Execute(ctx, keyFunc(ctx, fullMethod, dimension.WithClientConn(cc)), fn)
```

---

## 四、实施计划

### 4.1 阶段划分

| 阶段 | 内容 | 优先级 | 预计工作量 |
|:-----|:-----|:-------|:-----------|
| P0   | Ratelimit: Discard 模式 | Major | 0.5 天 |
| P0   | Ratelimit: 统一 Config 结构 | Major | 0.5 天 |
| P0   | Ratelimit: gRPC 一元拦截器（客户端+服务端） | Major | 1 天 |
| P0   | Breaker: KeyFunc 抽象 | Major | 1 天 |
| P0   | Breaker: InterceptorOption | Major | 0.5 天 |
| P1   | 统一维度抽象 | Major | 0.5 天 |
| P1   | 文档更新与示例 | - | 0.5 天 |
| P2   | 单元测试与集成测试 | - | 1 天 |

**注**: 流式拦截器保留现有实现，暂不扩展新功能（避免过早优化）

### 4.2 兼容性考虑

#### 非破坏性变更

- `NewStandalone` / `NewDistributed` 保持原有签名
- `UnaryClientInterceptor()` 无参数调用保持原有行为（服务级别 Key）
- `GinMiddleware` 支持 `limiter == nil`，内部自动使用 `Discard()`

#### 推荐迁移路径

```go
// === Ratelimit ===

// 旧代码（仍然有效）
limiter, _ := ratelimit.NewStandalone(&cfg.Standalone, ratelimit.WithLogger(logger))

// 新代码（推荐，配置驱动）
limiter, _ := ratelimit.New(&ratelimit.Config{
    Enabled: true,
    Mode:    "standalone",
    Standalone: &cfg.Standalone,
}, ratelimit.WithLogger(logger))

// === Breaker ===

// 旧代码（仍然有效）
conn, _ := grpc.Dial(target, grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()))

// 新代码（推荐，后端级别熔断）
conn, _ := grpc.Dial(target, grpc.WithUnaryInterceptor(
    brk.UnaryClientInterceptor(breaker.WithBackendLevelKey()),
))
```

---

## 五、测试策略

### 5.1 Ratelimit 测试

```go
func TestDiscard(t *testing.T) {
    limiter := ratelimit.Discard()

    // 即使配置无效也应允许
    allowed, err := limiter.Allow(ctx, "any-key", ratelimit.Limit{Rate: -1, Burst: -1})
    assert.True(t, allowed)
    assert.NoError(t, err)

    allowed, err = limiter.AllowN(ctx, "any-key", ratelimit.Limit{Rate: 0, Burst: 0}, 1000)
    assert.True(t, allowed)
    assert.NoError(t, err)
}

func TestNewConfigDisabled(t *testing.T) {
    cfg := &ratelimit.Config{Enabled: false}
    limiter, _ := ratelimit.New(cfg)

    // 应返回 Discard 实例
    allowed, _ := limiter.Allow(ctx, "key", ratelimit.Limit{Rate: 0, Burst: 0})
    assert.True(t, allowed)
}

func TestGRPCInterceptor(t *testing.T) {
    limiter, _ := ratelimit.NewStandalone(&ratelimit.StandaloneConfig{})

    // 服务端拦截器测试
    handler := func(ctx context.Context, req interface{}) (interface{}, error) {
        return &emptypb.Empty{}, nil
    }

    // 配置限流: 1 QPS
    limitFunc := func(ctx context.Context, method string) ratelimit.Limit {
        return ratelimit.Limit{Rate: 1, Burst: 1}
    }

    interceptor := ratelimit.UnaryServerInterceptor(
        limiter,
        ratelimit.MethodLevelKey(),
        limitFunc,
    )

    // 第一次调用成功
    _, err := interceptor(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/test/Method"}, handler)
    assert.NoError(t, err)

    // 第二次调用被限流（需要等待或增加 burst）
    // ...
}
```

### 5.2 Breaker 测试

```go
func TestKeyFuncVariations(t *testing.T) {
    tests := []struct {
        name         string
        keyFunc      breaker.KeyFunc
        target       string
        peerAddr     string
        method       string
        expectedKey  string
    }{
        {
            name:        "service level",
            keyFunc:     breaker.ServiceLevelKey(),
            target:      "etcd:///logic-service",
            method:      "/pkg.Service/Method",
            expectedKey: "etcd:///logic-service",
        },
        {
            name:        "method level",
            keyFunc:     breaker.MethodLevelKey(),
            method:      "/pkg.Service/Method",
            expectedKey: "/pkg.Service/Method",
        },
        {
            name:        "backend level with peer",
            keyFunc:     breaker.BackendLevelKey(),
            peerAddr:    "10.0.0.1:9001",
            expectedKey: "10.0.0.1:9001",
        },
        {
            name:        "composite",
            keyFunc:     breaker.CompositeKey(breaker.ServiceLevelKey(), breaker.BackendLevelKey()),
            target:      "service",
            peerAddr:    "10.0.0.1:9001",
            expectedKey: "service@10.0.0.1:9001",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := context.Background()
            if tt.peerAddr != "" {
                ctx = peer.NewContext(ctx, &peer.Peer{
                    Addr: &addr.Addr{StringFunc: func() string { return tt.peerAddr }},
                })
            }

            key := tt.keyFunc(ctx, tt.method, &grpc.ClientConn{})
            assert.Equal(t, tt.expectedKey, key)
        })
    }
}

func TestMultiBackendIsolation(t *testing.T) {
    backends := []string{"10.0.0.1:9001", "10.0.0.2:9001", "10.0.0.3:9001"}

    brk, _ := breaker.New(cfg)

    // 使用后端级别 Key
    interceptor := brk.UnaryClientInterceptor(breaker.WithBackendLevelKey())

    // 模拟 Backend 2 故障
    for i := 0; i < 100; i++ {
        ctx := peerContext(backends[1])
        _, _ := brk.Execute(ctx, backends[1], failingFunc)
    }

    // 验证隔离性
    state, _ := brk.State(backends[1])
    assert.Equal(t, breaker.StateOpen, state)

    state, _ = brk.State(backends[0])
    assert.Equal(t, breaker.StateClosed, state)
}
```

---

## 六、完成标准

- [ ] Ratelimit 支持 `Discard()` 模式
- [ ] Ratelimit 支持统一 `Config` 结构
- [ ] Ratelimit 提供 gRPC 一元拦截器（客户端 + 服务端）
- [ ] Ratelimit 支持多维度 KeyFunc（服务级、方法级、用户级、IP 级）
- [ ] Breaker 支持自定义 `KeyFunc`
- [ ] Breaker 提供内置 `BackendLevelKey` 实现
- [ ] Breaker Interceptor 支持 `InterceptorOption`
- [ ] 定义统一的维度抽象
- [ ] 所有变更保持向后兼容
- [ ] 单元测试覆盖率 > 80%
- [ ] 更新相关文档和示例

**不包含**（避免过早优化）:
- 流式拦截器的增强功能
- 双向流 Send/Recv 监控

---

## 附录

### A. 相关文件清单

| 文件 | 变更类型 |
|:-----|:---------|
| ratelimit/ratelimit.go | 新增 Discard、Config、New |
| ratelimit/grpc.go | 新增 gRPC 一元拦截器 |
| ratelimit/keyfunc.go | 新增维度 KeyFunc |
| breaker/breaker.go | 新增 KeyFunc、InterceptorOption |
| breaker/interceptor.go | 重构支持 KeyFunc |
| governance/internal/dimension | 新增统一维度包 |

### B. API 设计参考

**clog 设计模式**:
- `Discard()` → 零开销 No-op
- `New(cfg *Config, opts ...Option)` → 统一工厂函数
- `Option func(*options)` → 函数式选项

**应用到 Ratelimit**:
- `ratelimit.Discard()` → 零开销限流器
- `ratelimit.New(&ratelimit.Config{}, ...)` → 统一入口
- `ratelimit.WithLogger/Meter/...` → 函数式选项

**应用到 Breaker**:
- `breaker.UnaryClientInterceptor(opts ...InterceptorOption)` → 拦截器选项
- `breaker.WithKeyFunc/WithBackendLevelKey/...` → 函数式选项

### C. 参考资料

- [sony/gobreaker](https://github.com/sony/gobreaker) - 底层熔断器实现
- [gRPC Peer Context](https://pkg.go.dev/google.golang.org/grpc/peer) - Peer 信息提取
- [gRPC Interceptors](https://pkg.go.dev/google.golang.org/grpc#UnaryInterceptor) - 拦截器文档
- Issue #19 - 原始问题报告
- clog 组件设计 - 参考模式
