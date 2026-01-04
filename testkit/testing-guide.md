# Genesis 测试指南

本文档规定了 Genesis 项目的测试策略和规范，旨在确保代码质量、稳定性和可维护性。

## 1. 核心原则

1.  **轻量快速**：单元测试应能在毫秒级完成，`go test ./...` 应能快速反馈。
2.  **高覆盖率**：核心业务逻辑覆盖率 > 80%，工具包 > 90%。
3.  **面向接口**：依赖接口而非具体实现，便于 Mock 和测试。
4.  **真实集成**：集成测试应优先使用真实的外部依赖（通过 `testkit` 连接本地 Docker 服务）。
5.  **并发安全**：必须启用 `-race` 进行竞态检测。

## 2. 分层测试策略

### L1: 单元测试 (Unit Tests)

-   **目标**：验证函数、方法、核心逻辑的正确性。
-   **范围**：纯函数、业务逻辑、工具类。
-   **工具**：标准库 `testing`，`testify/assert`，`gomock`。
-   **要求**：
    -   不依赖外部 I/O（数据库、网络）。
    -   使用表格驱动测试 (Table-driven tests)。
    -   Mock 所有外部依赖。

### L2: 集成测试 (Integration Tests)

-   **目标**：验证组件与外部服务（Redis, MySQL, Etcd 等）的交互。
-   **范围**：Connector, Repository, Cache 实现等。
-   **工具**：`testkit`，`testcontainers` (可选)，`make up` 环境。
-   **要求**：
    -   使用 `testkit` 获取真实连接。
    -   测试环境应与生产环境尽可能一致（版本、配置）。
    -   测试后清理数据 (Teardown)。

### L3: 端到端测试 (E2E Tests)

-   **目标**：验证完整业务链路。
-   **范围**：跨多个组件或服务的完整流程。
-   **方式**：启动完整环境，模拟用户请求。

## 3. 使用 `testkit` 辅助测试

`testkit` 是 Genesis 提供的测试辅助包，位于项目根目录。它提供了：
1.  **通用依赖**：快速创建 Logger, Meter, Context。
2.  **基础设施连接**：一键获取连接到本地开发环境（docker-compose）的 Redis, MySQL, Etcd 等客户端。

### 3.1 初始化测试套件

```go
import (
    "testing"
    "github.com/ceyewan/genesis/testkit"
)

func TestMyComponent(t *testing.T) {
    // 获取基础工具（Logger, Meter 等）
    kit := testkit.NewKit(t)
    
    // 使用 Logger
    kit.Logger.Info("starting test")
}
```

### 3.2 使用真实连接 (Integration Test)

确保本地环境已启动 (`make up`)。

```go
func TestRedisCache(t *testing.T) {
    // 获取真实的 Redis 客户端
    // 默认连接 localhost:6379 (DB 1)
    rdb := testkit.GetRedisClient(t)
    
    // 使用完毕自动关闭连接 (t.Cleanup)
    
    // 执行测试...
    err := rdb.Set(context.Background(), "key", "value", 0).Err()
    if err != nil {
        t.Fatalf("redis set failed: %v", err)
    }
}
```

支持的服务：
-   `testkit.GetRedisClient(t)` / `testkit.GetRedisConnector(t)`
-   `testkit.GetMySQLDB(t)` / `testkit.GetMySQLConnector(t)`
-   `testkit.GetSQLiteDB(t)` / `testkit.GetSQLiteConnector(t)` - SQLite 内存或文件数据库
-   `testkit.GetEtcdClient(t)` / `testkit.GetEtcdConnector(t)`
-   `testkit.GetNATSConn(t)` / `testkit.GetNATSConnector(t)`
-   `testkit.GetKafkaClient(t)` / `testkit.GetKafkaConnector(t)`

### 3.3 MQ 消息队列测试支持

`testkit` 提供了 MQ 组件的测试辅助函数，支持 NATS、Redis Stream 两种驱动：

```go
// NATS 驱动测试
natsClient := testkit.GetNATSMQClient(t)

// Redis Stream 驱动测试
redisClient := testkit.GetRedisMQClient(t)

```

完整的 MQ 测试示例：

```go
func TestMQPublishSubscribe(t *testing.T) {
    // 获取 MQ 客户端（自动清理）
    mqClient := testkit.GetNATSMQClient(t)

    ctx := context.Background()
    subject := testkit.NewTestSubject("orders")

    // 订阅消息
    var receivedMsg string
    var wg sync.WaitGroup
    wg.Add(1)

    sub, err := mqClient.Subscribe(ctx, subject, func(ctx context.Context, msg mq.Message) error {
        receivedMsg = string(msg.Data())
        wg.Done()
        return nil
    })
    require.NoError(t, err)
    defer sub.Unsubscribe()

    // 发布消息
    err = mqClient.Publish(ctx, subject, []byte("test message"))
    require.NoError(t, err)

    // 等待接收
    wg.Wait()
    assert.Equal(t, "test message", receivedMsg)
}
```

MQ 测试的数据隔离策略：

```go
// 生成唯一的主题/流名称
subject := testkit.NewTestSubject("orders")      // test.{timestamp}.orders
group := testkit.NewTestConsumerGroup("workers") // test-group-{timestamp}-workers
```

### 3.4 SQLite 测试支持

SQLite 是嵌入式数据库，无需外部服务，适合快速测试：

```go
// 使用内存数据库（测试结束自动清理）
sqliteDB := testkit.GetSQLiteDB(t)

// 或使用持久化文件（存储在临时目录）
cfg := testkit.GetPersistentSQLiteConfig(t)
conn := testkit.GetPersistentSQLiteConnector(t)
// 数据库文件存储在 t.TempDir()，测试结束后自动删除
```

### 3.5 Mock 依赖与代码复用

-   **复用原则**：凡是可以复用的测试相关代码（如通用接口的 Mock 实现、辅助断言函数等），**必须** 编写在 `testkit` 包中，严禁在不同组件中重复编写。
-   **Mock 位置**：将通用的 Mock 结构体放在 `testkit` 下（例如 `testkit/mock_logger.go`）。
-   **单元测试**：对于特定组件的私有接口，使用 `gomock` 或手写 Mock。

### 3.6 数据隔离策略

为了避免测试间的数据污染和冲突（"脏数据"），请遵循以下隔离策略：

1.  **MySQL/PostgreSQL**: 
    -   **事务回滚**（推荐）：在测试开始时开启事务，测试结束时 `defer tx.Rollback()`。
    -   **唯一表名/库名**：如果无法使用事务，使用 `testkit.NewID()` 生成唯一的后缀创建临时表。

2.  **Redis/Etcd/KV**:
    -   **随机 Key 前缀**：使用 `testkit.NewID()` 生成唯一的 Key 前缀。
    ```go
    prefix := "test:" + testkit.NewID() + ":"
    key := prefix + "user:1"
    ```
    -   **自动过期**：为测试 Key 设置较短的 TTL，防止长期占用。

3.  **消息队列 (NATS/Redis Stream)**:
    -   **随机 Topic/Subject**：使用 `testkit.NewID()` 生成唯一的 Topic 名称。
    ```go
    topic := "test-topic-" + testkit.NewID()
    ```

## 4. 最佳实践

### 4.1 表格驱动测试

```go
func TestAdd(t *testing.T) {
    tests := []struct {
        name string
        a, b int
        want int
    }{
        {"positive", 1, 2, 3},
        {"negative", -1, -2, -3},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := Add(tt.a, tt.b); got != tt.want {
                t.Errorf("Add() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### 4.2 并发测试

使用 `-race` 标志运行测试：

```bash
go test -race ./...
```

### 4.3 避免全局状态

测试用例之间应相互独立，避免修改全局变量。使用 `testkit` 获取的连接是独立的实例（但在基础设施服务端是共享的），注意数据隔离（如使用随机 Key 前缀）。

## 5. CI/CD 集成

CI 流程会自动运行所有测试。确保提交前本地 `make test` 通过。
