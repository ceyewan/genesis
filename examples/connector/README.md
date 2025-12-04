# Genesis Connector 测试示例

这个示例展示了如何使用 Genesis Connector 组件连接各种基础设施服务。

## 功能特性

- ✅ 支持 Redis、MySQL、Etcd、NATS 连接器
- ✅ 结构化日志输出
- ✅ Prometheus 指标收集
- ✅ 连接健康检查
- ✅ 连接池管理
- ✅ 连接断开和重连测试
- ✅ Grafana 仪表盘监控

## 快速开始

### 1. 启动依赖服务

确保以下服务正在运行：

```bash
# 启动 Redis (如果没有运行)
docker run -d --name redis-test -p 6379:6379 redis:alpine

# 启动 Prometheus (使用根目录的配置)
cd /Users/ceyewan/CodeField/genesis
docker-compose -f docker-compose.dev.yml up prometheus -d

# 启动 Grafana (使用根目录的配置)
docker-compose -f docker-compose.dev.yml up grafana -d
```

### 2. 运行连接器测试

```bash
cd examples/connector
go run main.go
```

程序将会：
- 创建多个 Redis 连接器并测试连接
- 测试 MySQL 连接器（如果没有数据库会跳过）
- 测试 Etcd 连接器（如果没有 etcd 会跳过）
- 测试 NATS 连接器（如果没有 NATS 会跳过）
- 模拟连接断开和重连
- 收集指标和日志

### 3. 查看监控数据

**Prometheus Metrics**: http://localhost:9090/metrics

**Grafana Dashboard**: http://localhost:3000

#### Grafana 仪表盘导入

1. 登录 Grafana (admin/admin)
2. 导入 `grafana-dashboard.json` 文件
3. 选择 Prometheus 数据源：`http://prometheus:9090`

## 测试场景

### Redis 测试

- 创建 3 个 Redis 连接器，使用不同的 DB
- 执行 10 轮健康检查和数据操作
- 在第 5 轮模拟连接断开和重连
- 记录连接延迟、健康检查状态、连接池大小

### MySQL 测试

- 创建 MySQL 连接器
- 执行 5 次健康检查
- 测试数据库连接池配置

### Etcd 测试

- 创建 Etcd 连接器
- 执行键值对操作和健康检查
- 测试连接状态监控

### NATS 测试

- 创建 NATS 连接器
- 发布测试消息
- 监控连接状态和消息发布

## 指标说明

### 连接指标

- `connector_redis_connections_total` - Redis 连接尝试总数
- `connector_mysql_connections_total` - MySQL 连接尝试总数
- `connector_etcd_connections_total` - Etcd 连接尝试总数
- `connector_nats_connections_total` - NATS 连接尝试总数

### 延迟指标

- `connector_redis_connection_duration_seconds` - Redis 连接延迟直方图
- `connector_mysql_connection_duration_seconds` - MySQL 连接延迟直方图

### 健康检查指标

- `connector_redis_health_checks_total` - Redis 健康检查总数
- `connector_mysql_health_checks_total` - MySQL 健康检查总数
- `connector_etcd_health_checks_total` - Etcd 健康检查总数

### 连接池指标

- `connector_redis_pool_size` - Redis 连接池大小
- `connector_mysql_pool_size` - MySQL 连接池大小

## Grafana 仪表盘

仪表盘包含以下面板：

1. **连接器连接速率** - 各连接器每秒连接数
2. **连接器总连接数** - 各连接器累计连接数
3. **连接器连接延迟分布** - P50/P95/P99 延迟分布
4. **Redis 健康检查状态分布** - 成功/失败比例饼图
5. **健康检查错误分布** - 错误类型分布饼图
6. **连接池大小监控** - 实时连接池大小
7. **NATS 消息速率** - 每秒消息发布数

## 日志输出

程序使用 Genesis clog 组件输出结构化日志：

```
INFO    === Genesis Connector 测试程序启动 ===
INFO    === 测试 Redis 连接器 ===
INFO    redis connected    {"connector": "redis", "name": "redis-test-0", "addr": "localhost:6379"}
INFO    redis health check success    {"connector": "redis", "index": 0}
INFO    redis set success    {"key": "test_key_0_0"}
INFO    redis get success    {"key": "test_key_0_0", "value": "test_value_0"}
```

## 故障排除

### Redis 连接失败

```bash
# 检查 Redis 是否运行
redis-cli ping

# 检查端口是否开放
telnet localhost 6379
```

### MySQL 连接失败

```bash
# 检查 MySQL 配置
mysql -h localhost -u root -p

# 检查数据库是否存在
mysql -h localhost -u root -p -e "SHOW DATABASES;"
```

### Grafana 无法连接 Prometheus

```bash
# 检查 Prometheus 是否运行
curl http://localhost:9090/metrics

# 检查网络连接
docker network ls
docker network inspect genesis-net
```

## 扩展使用

### 创建自定义连接器

```go
// 创建 Redis 连接器
redisConn, err := connector.NewRedis(&connector.RedisConfig{
    BaseConfig: connector.BaseConfig{
        Name: "my-redis",
    },
    Addr:     "localhost:6379",
    PoolSize: 20,
}, connector.WithLogger(logger), connector.WithMeter(meter))
```

### 健康检查监控

```go
// 定期健康检查
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    if err := conn.HealthCheck(ctx); err != nil {
        logger.Warn("health check failed", clog.Error(err))
    } else {
        logger.Info("health check success")
    }
}
```

### 指标集成

```go
// 创建自定义指标
counter, err := meter.Counter(
    "custom_operations_total",
    "Total number of custom operations",
)

// 记录指标
counter.Inc(ctx, metrics.L("operation", "test"))
```

## 性能基准

在本地测试环境中，预期的性能指标：

- **Redis 连接延迟**: < 5ms (P95)
- **连接池利用率**: < 80%
- **健康检查成功率**: > 99%
- **重连成功率**: > 95%

## 相关文档

- [Genesis Connector 设计文档](../../docs/infrastructure/connector-design.md)
- [Genesis 总体设计文档](../../docs/genesis-design.md)
- [clog 组件文档](../../docs/foundation/clog-design.md)
- [metrics 组件文档](../../docs/foundation/metrics-design.md)
