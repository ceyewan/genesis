# testkit

`testkit` 是 Genesis 的测试辅助包，目标不是提供一套庞大的测试框架，而是把项目里反复出现的测试基础设施和通用 helper 沉淀成统一入口。

接口文档见 `go doc -all ./testkit`。这个包没有单独的设计 blog，因为它是测试辅助包，不是面向生产接入的组件。

它当前主要解决三类问题：
- 为测试提供统一的 `logger`、`meter`、`context` 和随机 ID helper。
- 通过 `testcontainers` 自动拉起 Redis、MySQL、PostgreSQL、Etcd、NATS、Kafka。
- 为 SQLite 这类无需容器的依赖提供快速 helper。

## 定位

`testkit` 只服务于测试代码，不应进入生产路径。它的设计目标是：
- 集成测试尽量贴近真实依赖。
- 运行测试前不需要手动执行 `make up`。
- 所有资源都挂在 `*testing.T` 生命周期上，由 `t.Cleanup` 自动回收。
- 需要文件持久化的 helper 会使用 `t.TempDir()`，不会把临时文件留在仓库里。
- 它不会替你自动建表、造业务数据或模拟完整调用链。

## 通用 helper

```go
func TestSomething(t *testing.T) {
    kit := testkit.NewKit(t)

    ctx, cancel := testkit.NewContext(t, 5*time.Second)
    defer cancel()

    kit.Logger.InfoContext(ctx, "Starting test")
    _ = testkit.NewID()
}
```

可直接使用的通用 helper 有：
- `NewKit(t)`：返回带默认 `Ctx`、`Logger`、`Meter` 的测试工具包。
- `NewLogger()`：返回适合本地调试的开发态 logger。
- `NewMeter()`：返回 discard meter。
- `NewContext(t, timeout)`：返回带超时的 context，并自动注册 cleanup。
- `NewID()`：返回 8 位随机 ID，适合拼接 Redis key、topic、表名后缀。

## 容器化依赖

所有容器化 helper 都遵循同一模式：
- `NewXxxContainerConfig(t)`：启动容器并返回 connector config。
- `NewXxxContainerConnector(t)`：启动容器并返回已连接的 Genesis connector。
- `NewXxxContainerClient/Conn/DB(t)`：返回原生 client 或 DB。

| 依赖 | Config | Connector | Client / DB |
| :--- | :--- | :--- | :--- |
| Redis | `NewRedisContainerConfig` | `NewRedisContainerConnector` | `NewRedisContainerClient` |
| MySQL | `NewMySQLContainerConfig` | `NewMySQLConnector` | `NewMySQLDB` |
| PostgreSQL | `NewPostgreSQLContainerConfig` | `NewPostgreSQLConnector` | `NewPostgreSQLDB` |
| Etcd | `NewEtcdContainerConfig` | `NewEtcdContainerConnector` | `NewEtcdContainerClient` |
| NATS | `NewNATSContainerConfig` | `NewNATSContainerConnector` | `NewNATSContainerConn` |
| Kafka | `NewKafkaContainerConfig` | `NewKafkaContainerConnector` | `NewKafkaContainerClient` |

Redis 示例：

```go
func TestRedis(t *testing.T) {
    rdb := testkit.NewRedisContainerClient(t)

    err := rdb.Set(context.Background(), "key", "value", 0).Err()
    require.NoError(t, err)
}
```

MySQL 示例：

```go
func TestMySQL(t *testing.T) {
    db := testkit.NewMySQLDB(t)

    type User struct {
        ID   uint
        Name string
    }

    require.NoError(t, db.AutoMigrate(&User{}))
}
```

Etcd 示例：

```go
func TestEtcd(t *testing.T) {
    client := testkit.NewEtcdContainerClient(t)

    _, err := client.Put(context.Background(), "test:key", "value")
    require.NoError(t, err)
}
```

## SQLite helper

SQLite 不依赖容器，适合快速测试：

```go
func TestSQLite(t *testing.T) {
    db := testkit.NewSQLiteDB(t)
    require.NotNil(t, db)
}
```

如果需要文件持久化：

```go
func TestPersistentSQLite(t *testing.T) {
    conn := testkit.NewPersistentSQLiteConnector(t)
    require.NotNil(t, conn.GetClient())
}
```

## 使用约束

- 集成测试优先复用 `testkit`，不要在各组件里重复实现容器拉起逻辑。
- 运行测试前不要手动执行 `make up`；`testkit` 会通过 `testcontainers` 自动处理依赖。
- 测试断言统一使用 `require`，不要新增 `assert`。
- 需要数据隔离时，使用 `testkit.NewID()` 生成唯一 key、topic、consumer group 或表后缀。

## 当前边界

`testkit` 目前提供的是基础设施级 helper，而不是完整测试 DSL。它不会替你自动建表、造业务数据或模拟完整调用链，这些仍应由各组件测试自行控制。

相关入口：
- `go doc -all ./testkit`
- [项目测试约束](../README.md#测试约束)
