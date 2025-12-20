# Idempotency 幂等组件设计文档

## 1. 目标与原则

`idempotency` 旨在为微服务框架提供一个通用的幂等性控制组件，防止因网络波动、客户端重试等原因导致的重复操作。

**设计原则：**

1. **封装实现 (Encapsulation):** 对外隐藏存储细节，仅暴露 `Idempotent` 接口。内部直接基于 Redis 实现，确保数据共享和持久化。
2. **原子性 (Atomicity):** 利用 Redis `SETNX` 或 Lua 脚本保证并发场景下的状态检查与锁定的原子性。
3. **结果缓存 (Result Caching):** 支持缓存首次执行的响应结果，对于重复请求直接返回缓存结果，实现“结果幂等”。
4. **多场景适配 (Multi-Scenario):** 提供 Gin Middleware、gRPC Interceptor 和通用函数封装，适配 HTTP、RPC 和 MQ 场景。
5. **直接依赖 (Direct Dependency):** 分布式模式直接依赖 `connector.Redis`，不经过 Cache 层，以确保原子操作支持和数据可靠性。

## 2. 项目结构

```text
genesis/
├── pkg/
│   └── idempotency/            # 公开 API 入口
│       ├── idempotency.go      # 工厂函数 (New)
│       ├── adapter/            # 适配器
│       │   ├── gin.go          # Gin Middleware
│       │   └── grpc.go         # gRPC Interceptor
│       └── types/              # 类型定义
│           ├── interface.go    # Idempotent/Store 接口
│           └── config.go       # 配置定义
├── internal/
│   └── idempotency/            # 内部实现
│       ├── store/              # 存储实现
│       │   └── redis.go        # Redis Store
│       └── core.go             # 核心逻辑
└── ...
```

## 3. 核心 API 设计

### 3.1. 接口定义

```go
// pkg/idempotency/types/interface.go

package types

import (
 "context"
 "time"
)

// Status 幂等记录的状态
type Status int

const (
 StatusProcessing Status = iota // 处理中 (锁定)
 StatusSuccess                  // 处理成功
 StatusFailed                   // 处理失败
)

// Idempotent 幂等组件对外接口
type Idempotent interface {
    // Do 执行幂等操作
    // key: 幂等键
    // fn: 实际业务逻辑，返回 result 和 error
    // opts: 选项 (如 TTL)
    Do(ctx context.Context, key string, fn func() (any, error), opts ...Option) (any, error)
}
```

### 3.2. 配置 (Config)

```go
// pkg/idempotency/types/config.go

package types

import "time"

type Config struct {
    // RedisConnector 引用 connector 的名称
    RedisConnector string `yaml:"redis_connector" json:"redis_connector"`
    
    // Prefix Key 前缀，默认 "idempotency:"
    Prefix string `yaml:"prefix" json:"prefix"`

    // DefaultTTL 默认记录保留时间，默认 24h
    DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`
}
```

## 4. 内部实现细节

### 4.1. Redis Store 实现

* **Lock**: 使用 `SET key processing NX EX ttl`。
* **Unlock**: 使用 Lua 脚本。
  * 如果状态是 Processing，更新为 Success/Failed，并设置 Result 和新的 TTL。
  * 如果 Key 不存在或状态不匹配，返回错误。
* **Value 结构**: 建议使用 JSON 或 MsgPack 序列化存储 `{ "status": 1, "result": "..." }`。

### 4.2. 核心流程 (Do 方法)

```go
func (i *Idempotent) Do(ctx context.Context, key string, fn func() (any, error), opts ...Option) (any, error) {
    // 1. 尝试加锁
    locked, status, err := i.store.Lock(ctx, key, ttl)
    if err != nil {
        return nil, err
    }

    // 2. 如果加锁失败 (Key 已存在)
    if !locked {
        if status == StatusSuccess {
            // 获取缓存结果并返回
            _, res, _ := i.store.Get(ctx, key)
            return res, nil
        }
        if status == StatusProcessing {
            return nil, ErrProcessing // 429/409
        }
        // StatusFailed: 通常 Lock 会自动处理过期或允许重试，视策略而定
    }

    // 3. 执行业务逻辑
    res, err := fn()

    // 4. 处理结果
    if err != nil {
        // 业务失败：删除 Key (允许重试) 或 记录失败状态
        i.store.Delete(ctx, key)
        return nil, err
    }

    // 业务成功：更新状态并缓存结果
    i.store.Unlock(ctx, key, StatusSuccess, encode(res), ttl)
    return res, nil
}
```

## 5. 适配器设计

### 5.1. Gin Middleware

* **Key 获取**: 从 Header `X-Idempotency-Key` 获取。
* **响应拦截**: 使用自定义 `ResponseWriter` 拦截 `Write` 和 `WriteHeader`，捕获 Response Body。
* **流程**:
  * 调用 `store.Lock`。
  * 如果成功，`c.Next()`。
  * `c.Next()` 返回后，将捕获的 Body 写入 `store.Unlock`。
  * 如果 Lock 失败且状态为 Success，直接 `c.Data(cachedBody)` 并 `c.Abort()`。

### 5.2. MQ 消费端

MQ 场景通常不需要返回结果，只需要去重。

```go
// 示例用法
err := idempotency.Do(ctx, msg.ID, func() (any, error) {
    return nil, process(msg)
})
```

## 6. 待办事项

* [ ] 实现 Redis Store (Lua 脚本编写)
* [ ] 实现 Memory Store (sync.Map)
* [ ] 实现 Gin Middleware (Response Writer Wrapper)
* [ ] 实现 gRPC Interceptor
