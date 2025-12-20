# Genesis Cache 组件示例

本示例演示了如何使用优化后的 Genesis Cache 组件，该组件已按照 `docs/refactoring-plan.md` 进行了扁平化重构。

## 架构特点

- ✅ **Go Native DI 模式**：显式依赖注入，无隐藏魔法
- ✅ **扁平化设计**：移除 `types/` 子包，简化导入路径
- ✅ **资源所有权清晰**：Cache 借用 Connector 连接，Close() 为 no-op
- ✅ **完整 Redis 支持**：Key-Value、Hash、Sorted Set、List 操作

## 运行示例

### 前置条件

1. 确保 Redis 服务运行
2. 配置环境变量（可选，默认值已提供）

### 环境变量配置

示例支持通过环境变量配置 Redis 连接：

```bash
# Redis 地址（默认：127.0.0.1:6379）
REDIS_ADDR=127.0.0.1:6379

# Redis 密码（默认：空）
REDIS_PASSWORD=your_redis_password

# Redis 数据库编号（默认：0）
REDIS_DB=0

# 连接池大小（默认：10）
REDIS_POOL_SIZE=10
```

### 使用 .env 文件

项目根目录的 `.env` 文件包含默认配置：

```bash
# Redis Configuration
REDIS_PASSWORD=genesis_redis_password
```

示例会自动加载 `.env` 文件，你也可以创建本地配置：

```bash
# 创建本地环境配置
cp ../.env .env.local
# 修改 .env.local 中的 Redis 配置
```

### 运行

```bash
cd examples/cache
go run main.go
```

## 使用模式

### 1. 标准 Go Native DI 模式

```go
// 1. 创建 Redis 连接器
redisConn, err := connector.NewRedis(&connector.RedisConfig{
    Addr:         "127.0.0.1:6379",
    Password:     "", // 如需密码请设置
    DB:           0,
    DialTimeout:  2 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    PoolSize:     10,
}, connector.WithLogger(logger))
if err != nil {
    return err
}
defer redisConn.Close() // 必须关闭连接器

// 2. 配置缓存
cacheCfg := &cache.Config{
    Prefix:     "myapp:",
    Serializer: "json",
}

// 3. 创建缓存实例
cacheClient, err := cache.New(redisConn, cacheCfg, cache.WithLogger(logger))
if err != nil {
    return err
}
// cacheClient.Close() 为 no-op，无需调用
```

### 2. 基础操作

```go
// 设置缓存
err := cacheClient.Set(ctx, "user:1001", user, 24*time.Hour)

// 获取缓存
var user User
err = cacheClient.Get(ctx, "user:1001", &user)

// 删除缓存
err = cacheClient.Delete(ctx, "user:1001")
```

### 3. 高级数据结构

```go
// Hash 操作
err = cacheClient.HSet(ctx, "profile:1001", "name", "Alice")
var name string
err = cacheClient.HGet(ctx, "profile:1001", "name", &name)

// Sorted Set 操作
err = cacheClient.ZAdd(ctx, "leaderboard", 95.5, "player:1001")
var topPlayers []string
err = cacheClient.ZRevRange(ctx, "leaderboard", 0, 9, &topPlayers)

// List 操作
err = cacheClient.LPushCapped(ctx, "logs", 100, logEntry)
var recentLogs []LogEntry
err = cacheClient.LRange(ctx, "logs", 0, 19, &recentLogs)
```

## 组件结构

```
pkg/cache/
├── cache.go          # 接口定义 + 工厂函数
├── config.go         # Config 结构体
├── options.go        # Option 函数
├── redis.go          # Redis 实现（非导出）
└── serializer/       # 序列化器
    └── serializer.go
```

## 与旧版本对比

| 特性 | 旧版本 | 新版本 |
|------|--------|--------|
| 导入路径 | `github.com/ceyewan/genesis/cache/types` | `github.com/ceyewan/genesis/cache` |
| 依赖注入 | Container 模式 | Go Native DI 模式 |
| 资源管理 | 不明确 | Connector 拥有连接，Cache 为 no-op |
| 目录结构 | `cache/types/` 子包 | 扁平化单层结构 |
| 配置方式 | 通过 Container | 显式构造函数 |

## 注意事项

1. **连接器必须关闭**：`defer redisConn.Close()`
2. **Cache Close() 为 no-op**：无需调用，调用也无害
3. **Redis 认证**：如需密码请在配置中设置
4. **序列化器**：目前支持 JSON，msgpack 待实现
