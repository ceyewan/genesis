# idgen - Genesis ID 生成组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/idgen.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/idgen)

`idgen` 是 Genesis 业务层的核心组件，提供高性能的分布式 ID 生成能力，支持多种 ID 生成策略：Snowflake（雪花算法）、UUID 和基于 Redis 的序列号生成器。

## 特性

- **所属层级**：L2 (Business) — 业务能力，提供 ID 生成抽象
- **核心职责**：提供分布式唯一 ID 生成服务，支持多种生成策略
- **设计原则**：
  - **多模式支持**：开箱即用的 Snowflake、UUID 和序列号生成器
  - **灵活的 WorkerID 分配**：Static、IP、Redis、Etcd 四种策略，适应不同部署场景
  - **序列号生成**：基于 Redis INCR 命令的分布式递增 ID，支持批量生成
  - **安全熔断**：连接丢失时自动停止发号，防止 ID 冲突
  - **时钟回拨保护**：内置时钟漂移检测与处理机制
  - **标准化接口**：统一的 `Generator` 接口，便于业务层使用和 Mock
  - **可观测性**：集成 `clog`、`metrics`、`xerrors`

## 目录结构

```text
idgen/                      # 公开 API + 实现（完全扁平化）
├── README.md               # 本文档
├── idgen.go                # 接口定义 + 工厂函数 + 配置结构
├── interfaces.go           # 接口定义（独立文件，便于文档排序）
├── options.go              # 函数式选项：Option、WithLogger/WithMeter
├── snowflake.go            # Snowflake 算法实现
├── uuid.go                 # UUID 生成实现
├── sequence.go             # 序列号生成器实现
├── errors.go               # 错误定义
├── metrics.go              # 指标常量
├── util.go                 # 工具函数
├── internal/               # 内部实现包
│   └── allocator/          # WorkerID 分配器
│       ├── interface.go    # 分配器接口
│       ├── static.go       # 静态分配
│       ├── ip.go          # IP 分配
│       ├── redis.go       # Redis 分配
│       └── etcd.go        # Etcd 分配
└── *_test.go              # 测试文件
```

## 快速开始

```go
import "github.com/ceyewan/genesis/idgen"
```

### Snowflake 分布式有序 ID

```go
// Redis 模式（推荐）
redisConn, _ := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
}, connector.WithLogger(logger))
defer redisConn.Close()

snowflakeGen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
    Method:       "redis",        // WorkerID 分配策略
    DatacenterID: 1,              // 数据中心 ID
    KeyPrefix:    "myapp:idgen:", // Redis 键前缀
    TTL:          30,            // 租约 TTL（秒）
}, redisConn, nil, idgen.WithLogger(logger))

id, _ := snowflakeGen.NextInt64()
fmt.Printf("Generated ID: %d\n", id)
```

### UUID 标准化 ID

```go
// UUID v7（时间排序）
uuidGen, _ := idgen.NewUUID(&idgen.UUIDConfig{
    Version: "v7",
}, idgen.WithLogger(logger))

id := uuidGen.Next()
fmt.Printf("Generated UUID: %s\n", id)
```

### 序列号生成器

```go
// IM 消息序列号场景
sequencer, _ := idgen.NewSequencer(&idgen.SequenceConfig{
    KeyPrefix: "im:msg_seq",  // 键前缀
    Step:      1,             // 步长
    TTL:       int64(time.Hour), // 过期时间
}, redisConn, idgen.WithLogger(logger))

// 为不同用户生成消息序号
ctx := context.Background()
aliceSeq1, _ := sequencer.Next(ctx, "alice")  // Alice: 1
aliceSeq2, _ := sequencer.Next(ctx, "alice")  // Alice: 2
bobSeq1, _ := sequencer.Next(ctx, "bob")      // Bob: 1

// 批量生成
batchSeqs, _ := sequencer.NextBatch(ctx, "alice", 5)  // [3, 4, 5, 6, 7]
```

## 核心接口

### Generator - 通用 ID 生成器接口

```go
type Generator interface {
    // Next 返回字符串形式的 ID
    Next() string
}
```

### Int64Generator - 数字 ID 生成器接口

```go
type Int64Generator interface {
    Generator
    // NextInt64 返回 int64 形式的 ID
    NextInt64() (int64, error)
}
```

### Sequencer - 序列号生成器接口

```go
type Sequencer interface {
    // Next 为指定键生成下一个序列号
    Next(ctx context.Context, key string) (int64, error)

    // NextBatch 为指定键批量生成序列号
    NextBatch(ctx context.Context, key string, count int) ([]int64, error)
}
```

## WorkerID 分配策略

### Static（静态）

适合：开发环境、K8s StatefulSet

```go
cfg := &idgen.SnowflakeConfig{
    Method:   "static",
    WorkerID: 1,  // 手动指定
}
```

### IP（IP 后 8 位）

适合：扁平网络的小型集群

```go
cfg := &idgen.SnowflakeConfig{
    Method: "ip_24",  // WorkerID = IP & 0xFF
}
```

### Redis（推荐）

适合：通用微服务环境

```go
cfg := &idgen.SnowflakeConfig{
    Method:       "redis",
    DatacenterID: 1,
    KeyPrefix:    "myapp:idgen:",
    TTL:          30,  // 30秒租约
}
```

### Etcd（高一致性）

适合：对一致性要求极高的环境

```go
etcdConn, _ := connector.NewEtcd(&connector.EtcdConfig{
    Endpoints: []string{"127.0.0.1:2379"},
})

cfg := &idgen.SnowflakeConfig{
    Method:       "etcd",
    DatacenterID: 1,
    KeyPrefix:    "myapp:idgen:",
    TTL:          30,
}
```

## 配置结构

### SnowflakeConfig

```go
type SnowflakeConfig struct {
    Method     string `yaml:"method" json:"method"`                     // "static" | "ip_24" | "redis" | "etcd"
    WorkerID   int64  `yaml:"worker_id" json:"worker_id"`               // static 模式下的 ID
    DatacenterID int64 `yaml:"datacenter_id" json:"datacenter_id"`     // 数据中心 ID
    KeyPrefix  string `yaml:"key_prefix" json:"key_prefix"`           // Redis/Etcd 键前缀
    TTL        int    `yaml:"ttl" json:"ttl"`                         // 租约 TTL（秒）
    MaxDriftMs int64  `yaml:"max_drift_ms" json:"max_drift_ms"`       // 最大时钟回拨（毫秒）
    MaxWaitMs  int64  `yaml:"max_wait_ms" json:"max_wait_ms"`         // 时钟回拨最大等待（毫秒）
}
```

### UUIDConfig

```go
type UUIDConfig struct {
    Version string `yaml:"version" json:"version"`  // "v4" | "v7"（默认 "v4"）
}
```

### SequenceConfig

```go
type SequenceConfig struct {
    KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`    // 键前缀
    Step      int64  `yaml:"step" json:"step"`                // 步长（默认 1）
    MaxValue  int64  `yaml:"max_value" json:"max_value"`      // 最大值限制（0 表示不限制）
    TTL       int64  `yaml:"ttl" json:"ttl"`                  // Redis 键过期时间（0 表示永不过期）
}
```

## 应用场景

### 1. 微服务订单 ID

```go
// 高性能分布式订单号
orderGen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
    Method:       "redis",
    DatacenterID: 1,
    KeyPrefix:    "ecommerce:order:",
}, redisConn, nil, idgen.WithLogger(logger))

orderID, _ := orderGen.NextInt64()
fmt.Printf("Order ID: %d\n", orderID)
```

### 2. IM 消息序列号

```go
// 为每个会话维护独立的消息序号
msgGen, _ := idgen.NewSequencer(&idgen.SequenceConfig{
    KeyPrefix: "chat:msg_seq",
    Step:      1,
    TTL:       int64(time.Hour),
}, redisConn, idgen.WithLogger(logger))

// 发送消息
msgSeq, _ := msgGen.Next(ctx, "room:123")
fmt.Printf("Message Seq: %d\n", msgSeq)
```

### 3. 业务流水号

```go
// 每日订单流水号
orderSeqGen, _ := idgen.NewSequencer(&idgen.SequenceConfig{
    KeyPrefix: "business:order",
    Step:      1,
    MaxValue:  999999,  // 6位流水号，循环使用
    TTL:       24 * int64(time.Hour),
}, redisConn, idgen.WithLogger(logger))

today := time.Now().Format("20060102")
orderNo := fmt.Sprintf("ORD%s%06d", today, seq)
fmt.Printf("Order No: %s\n", orderNo)
```

## 可观测性

### 日志命名空间

组件会自动为 Logger 添加 `component=idgen` 字段：

```go
// 日志输出示例
// level=info msg="worker id allocated" namespace=myapp component=idgen worker_id=123 datacenter_id=1
// level=info msg="generated sequence number" namespace=myapp component=idgen key=alice seq=42
```

### 指标

内置以下指标：

```go
const (
    MetricIDGenerated                 = "idgen_generated_total"
    MetricIDGenerationErrors          = "idgen_generation_errors_total"
    MetricWorkerIDAllocationErrors    = "idgen_worker_id_allocation_errors_total"
    MetricClockBackwardsCount         = "idgen_clock_backwards_total"
    MetricSequenceGenerated           = "idgen_sequence_generated_total"
    MetricSequenceGenerationErrors    = "idgen_sequence_generation_errors_total"
)
```

## 工厂函数

```go
// 创建 Snowflake 生成器
func NewSnowflake(cfg *SnowflakeConfig, redis connector.RedisConnector, etcd connector.EtcdConnector, opts ...Option) (Int64Generator, error)

// 创建 UUID 生成器
func NewUUID(cfg *UUIDConfig, opts ...Option) (Generator, error)

// 创建序列号生成器
func NewSequencer(cfg *SequenceConfig, redis connector.RedisConnector, opts ...Option) (Sequencer, error)
```

### 选项函数

```go
func WithLogger(logger clog.Logger) Option
func WithMeter(meter metrics.Meter) Option
```

## 更多示例

查看 `examples/idgen/main.go` 获取完整的使用示例。