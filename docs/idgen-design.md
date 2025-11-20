# ID Generator 组件设计文档

## 1. 概述 (Overview)

`idgen` 是 Genesis 框架的分布式 ID 生成组件。它旨在提供高性能、全局唯一的 ID 生成服务，支持 **Snowflake (雪花算法)** 和 **UUID** 两种主流模式。

针对 Snowflake 算法在分布式环境下的 **WorkerID 分配** 难题，本组件提供了多种策略（Static, IP, Redis, Etcd），以适应从单机开发到大规模云原生集群的各种部署场景。

## 2. 核心特性 (Features)

* **多模式支持**: 开箱即用的 Snowflake 和 UUID 生成器。
* **灵活的 WorkerID 分配**:
  * **Static**: 静态配置，适合 StatefulSet 或固定节点。
  * **IP**: 基于 IP 地址后 8 位，零依赖，适合扁平网络。
  * **Redis**: 基于 Lua 脚本的原子分配，支持自动续期 (KeepAlive)。
  * **Etcd**: 基于 Lease 和 Txn 的强一致性分配。
* **安全熔断**: 当 Redis/Etcd 连接丢失导致 WorkerID 租约失效时，自动停止发号，防止 ID 冲突。
* **时钟回拨保护**: 内置时钟漂移检测与处理机制，容忍小回拨，拒绝大回拨。
* **标准化接口**: 统一的 `Generator` 接口，便于业务层使用和 Mock。

## 3. 架构设计 (Architecture)

### 3.1. 模块结构

```text
pkg/idgen/
├── idgen.go            # 工厂方法 (NewSnowflake, NewUUID)
└── types/
    ├── config.go       # 配置定义
    └── interface.go    # 公开接口 (Generator)

internal/idgen/
├── snowflake/          # Snowflake 核心算法
├── uuid/               # UUID 包装
└── allocator/          # WorkerID 分配器 (核心复杂逻辑)
    ├── interface.go    # Allocator 内部接口
    ├── static.go
    ├── ip.go
    ├── redis.go
    └── etcd.go
```

### 3.2. 接口定义

#### 公开接口 (`pkg/idgen/types`)

```go
// Generator 通用 ID 生成器
type Generator interface {
    String() string
}

// Int64Generator 数字 ID 生成器 (Snowflake 特有)
type Int64Generator interface {
    Generator
    Int64() (int64, error)
}
```

#### 内部接口 (`internal/idgen/allocator`)

```go
// Allocator 定义 WorkerID 的分配策略
type Allocator interface {
    // Allocate 分配一个可用的 WorkerID
    // ctx: 用于控制超时
    Allocate(ctx context.Context) (int64, error)

    // Start 启动后台保活任务
    // workerID: 已分配的 ID
    // 返回: error channel。如果保活失败（租约失效），会发送 error，上层必须停止发号。
    Start(ctx context.Context, workerID int64) (<-chan error, error)
}
```

## 4. WorkerID 分配策略详解

### 4.1. Static (静态)

* **原理**: 直接读取配置文件中的 `worker_id` 字段。
* **场景**: 开发环境、K8s StatefulSet (配合启动脚本注入)。
* **优点**: 简单，无外部依赖。
* **缺点**: 运维成本高，容易配置冲突。

### 4.2. IP (IP 后 8 位)

* **原理**: `WorkerID = IP & 0xFF`。截取本机 IPv4 的最后一段。
* **场景**: 容器网络 IP 不重叠的小型集群。
* **优点**: 极速，无中心依赖。
* **缺点**: 依赖网络规划，最大支持 256 个节点，有冲突风险。

### 4.3. Redis (推荐)

* **原理**: 利用 Redis 的原子性进行 "Slot Grabbing" (槽位抢占)。
* **流程**:
    1. **Allocate**: 执行 Lua 脚本，遍历 `0-1023`，尝试 `SET key NX EX`。**O(1) 网络开销**。
    2. **KeepAlive**: 启动 Goroutine，每隔 `TTL/3` 时间发送 `EXPIRE` 命令续期。
    3. **熔断**: 如果连续 N 次续期失败，关闭 error channel，通知 Snowflake 停止服务。
* **场景**: 通用微服务环境。

### 4.4. Etcd (高一致性)

* **原理**: 利用 Etcd 的 Lease 和 Transaction。
* **流程**:
    1. **Allocate**: `GetPrefix` 获取当前所有占用 -> 本地计算空闲 ID -> `Txn(If NotExist Then Put+Lease)`。
    2. **KeepAlive**: 使用 `client.KeepAlive` 自动续租。
* **场景**: 对一致性要求极高，且已有 Etcd 设施的环境。

## 5. 配置设计 (`config.yaml`)

```yaml
idgen:
  # 模式: "snowflake" | "uuid"
  mode: "snowflake"
  
  snowflake:
    # 分配策略: "static" | "ip_24" | "redis" | "etcd"
    method: "redis"
    
    # 静态模式下的 ID (method="static" 时必填)
    worker_id: 0
    
    # 数据中心 ID (可选，默认 0)
    datacenter_id: 0
    
    # Redis/Etcd 键前缀
    key_prefix: "genesis:idgen:worker"
```

## 6. 使用示例

```go
// 初始化
cfg := config.Load()
// 自动根据配置选择 Allocator
gen, err := idgen.NewSnowflake(cfg.IDGen, redisConnector) 

// 使用
id := gen.Int64()
