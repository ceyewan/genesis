# Genesis 测试指南

本文档规定了 Genesis 项目的测试策略和规范，旨在确保代码质量、稳定性和可维护性。

## 1. 核心原则

1.  **轻量快速**：单元测试应能在毫秒级完成，`go test ./...` 应能快速反馈。
2.  **高覆盖率**：核心业务逻辑覆盖率 > 80%，工具包 > 90%。
3.  **面向接口**：依赖接口而非具体实现，便于 Mock 和测试。
4.  **真实集成**：集成测试使用 `testcontainers` 自动管理外部依赖，无需手动启动服务。
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
-   **工具**：`testkit`（基于 `testcontainers`）。
-   **要求**：
    -   使用 `testkit` 自动启动和管理测试容器。
    -   测试环境应与生产环境尽可能一致（版本、配置）。
    -   测试后自动清理资源 (通过 `t.Cleanup`)。

### L3: 端到端测试 (E2E Tests)

-   **目标**：验证完整业务链路。
-   **范围**：跨多个组件或服务的完整流程。
-   **方式**：启动完整环境，模拟用户请求。

## 3. 使用 `testkit` 辅助测试

`testkit` 是 Genesis 提供的测试辅助包，位于 `testkit/` 目录。它提供了：
1.  **通用依赖**：快速创建 Logger, Meter, Context。
2.  **基础设施连接**：使用 `testcontainers` 自动启动和管理 Redis, MySQL, Etcd 等容器。

**无需手动启动 Docker 服务**，`testkit` 会自动处理容器的生命周期。

### 3.1 基础工具

```go
import (
    "testing"
    "github.com/ceyewan/genesis/testkit"
)

func TestMyComponent(t *testing.T) {
    // 获取基础工具（Logger, Meter, Context）
    kit := testkit.NewKit(t)

    // 使用 Logger
    kit.Logger.Info("starting test")

    // 生成唯一 ID
    id := testkit.NewID()  // 例如: "a3f7c9e2"
}
```

### 3.2 容器化服务 (Integration Test)

所有容器化服务都遵循统一的 API 命名规范：

| 组件 | Config | Connector | Client/DB |
|------|--------|-----------|-----------|
| Redis | `NewRedisContainerConfig` | `NewRedisContainerConnector` | `NewRedisContainerClient` |
| MySQL | `NewMySQLContainerConfig` | `NewMySQLConnector` | `NewMySQLDB` |
| PostgreSQL | `NewPostgreSQLContainerConfig` | `NewPostgreSQLConnector` | `NewPostgreSQLDB` |
| Etcd | `NewEtcdContainerConfig` | `NewEtcdContainerConnector` | `NewEtcdContainerClient` |
| NATS | `NewNATSContainerConfig` | `NewNATSContainerConnector` | `NewNATSContainerConn` |
| Kafka | `NewKafkaContainerConfig` | `NewKafkaContainerConnector` | `NewKafkaContainerClient` |

**Redis 示例**：
```go
func TestRedisCache(t *testing.T) {
    // 自动启动 Redis 容器并获取客户端
    rdb := testkit.NewRedisContainerClient(t)
    // 容器会在测试结束后自动清理

    err := rdb.Set(context.Background(), "key", "value", 0).Err()
    require.NoError(t, err)
}
```

**MySQL 示例**：
```go
func TestMySQLRepository(t *testing.T) {
    // 自动启动 MySQL 容器并获取 GORM DB
    db := testkit.NewMySQLDB(t)

    // 执行数据库操作...
    result := db.Create(&User{Name: "test"})
    require.NoError(t, result.Error)
}
```

**PostgreSQL 示例**：
```go
func TestPostgreSQLRepository(t *testing.T) {
    db := testkit.NewPostgreSQLDB(t)
    // 同 MySQL...
}
```

**Etcd 示例**：
```go
func TestEtcdDLock(t *testing.T) {
    client := testkit.NewEtcdContainerClient(t)
    // 使用 etcd client...
}
```

**NATS 示例**：
```go
func TestNATSPublish(t *testing.T) {
    nc := testkit.NewNATSContainerConn(t)
    // 使用 NATS 连接...
}
```

**Kafka 示例**：
```go
func TestKafkaProduce(t *testing.T) {
    client := testkit.NewKafkaContainerClient(t)
    // 使用 Kafka client...
}
```

### 3.3 SQLite 测试支持

SQLite 是嵌入式数据库，无需容器，适合快速测试：

```go
// 使用内存数据库
sqliteDB := testkit.NewSQLiteDB(t)

// 使用持久化文件（存储在 t.TempDir()）
cfg := testkit.NewPersistentSQLiteConfig(t)
conn := testkit.NewPersistentSQLiteConnector(t)
```

### 3.4 Mock 依赖与代码复用

-   **复用原则**：凡是可以复用的测试相关代码（如通用接口的 Mock 实现、辅助断言函数等），**必须** 编写在 `testkit` 包中，严禁在不同组件中重复编写。
-   **Mock 位置**：将通用的 Mock 结构体放在 `testkit` 下（例如 `testkit/mock_logger.go`）。
-   **单元测试**：对于特定组件的私有接口，使用 `gomock` 或手写 Mock。

### 3.5 数据隔离策略

为了避免测试间的数据污染和冲突（"脏数据"），请遵循以下隔离策略：

1.  **MySQL/PostgreSQL**:
    -   **事务回滚**（推荐）：在测试开始时开启事务，测试结束时 `defer tx.Rollback()`。
    -   **唯一表名**：如果无法使用事务，使用 `testkit.NewID()` 生成唯一的后缀创建临时表。

2.  **Redis/Etcd/KV**:
    -   **随机 Key 前缀**：使用 `testkit.NewID()` 生成唯一的 Key 前缀。
    ```go
    prefix := "test:" + testkit.NewID() + ":"
    key := prefix + "user:1"
    ```
    -   **自动过期**：为测试 Key 设置较短的 TTL。

3.  **消息队列 (NATS/Kafka)**:
    -   **随机 Topic/Subject**：使用 `testkit.NewID()` 生成唯一的名称。
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

测试用例之间应相互独立，避免修改全局变量。使用 `testkit` 获取的连接是独立的实例（每个测试都有自己的容器），天然隔离。

## 5. CI/CD 集成

CI 流程会自动运行所有测试。testcontainers 确保 CI 环境无需预装任何服务，只要有 Docker 即可运行完整的集成测试。
