# Genesis 使用示例

本目录包含了 Genesis 所有组件的使用示例。每个示例都是一个完整的、可运行的程序，展示如何正确使用 Genesis 组件。

## 📖 示例列表

### Level 0 - 基础设施示例

* **[clog](./clog/)** - 日志组件使用示例
* **[config](./config/)** - 配置管理示例
* **[metrics](./metrics/)** - 指标收集示例（包含 Grafana 仪表板）
* **[xerrors](./xerrors/)** - 错误处理示例

### Level 1 - 连接管理示例

* **[connector](./connector/)** - 连接器使用示例
* **[db](./db/)** - 数据库组件示例

### Level 2 - 业务组件示例

* **[cache](./cache/)** - 缓存组件使用示例
* **[dlock-redis](./dlock-redis/)** - 基于 Redis 的分布式锁示例
* **[dlock-etcd](./dlock-etcd/)** - 基于 Etcd 的分布式锁示例
* **[idgen](./idgen/)** - ID 生成器示例
* **[mq](./mq/)** - 消息队列使用示例

### Level 3 - 流量治理示例

* **[auth](./auth/)** - 认证授权示例
* **[ratelimit](./ratelimit/)** - 限流组件示例
* **[breaker](./breaker/)** - 熔断器示例（包含 gRPC 测试）
* **[registry](./registry/)** - 服务注册发现示例
* **[grpc-registry](./grpc-registry/)** - gRPC 服务注册发现示例

## 🚀 运行示例

### 使用 Make 命令

```bash
# 查看所有可用示例
make examples

# 运行特定组件示例
make example-cache      # 运行缓存示例
make example-dlock      # 运行分布式锁示例
make example-metrics    # 运行指标示例

# 运行所有示例
make example-all
```

### 直接运行示例

```bash
# 进入特定示例目录并运行
cd examples/cache
go run main.go

# 或者直接运行
go run examples/cache/main.go

# 带配置文件运行
cd examples/config
go run main.go -config config.yaml
```

## 📝 示例说明

每个示例都包含：

1. **完整的初始化流程** - 展示如何正确初始化组件
2. **核心功能演示** - 展示组件的主要功能
3. **最佳实践** - 遵循 Genesis 的设计原则
4. **错误处理** - 展示如何处理各种错误情况
5. **配置文件** - 部分示例包含配置文件（如 config 示例）

### 特殊说明

* **metrics** 示例包含 Grafana 仪表板配置文件
* **breaker** 和 **grpc-registry** 示例包含 Protocol Buffer 定义文件
* **dlock** 提供了 Redis 和 Etcd 两种实现的示例
* **config** 示例展示了多环境配置的使用

## 🔧 开发环境

运行示例前，请确保开发环境已经启动：

```bash
# 启动所有依赖服务
make up

# 查看服务状态
make status
```

## 📚 更多文档

* [项目主页](../README.md) - 返回项目主页
* [组件开发规范](../docs/component-spec.md) - 了解如何开发组件
* [架构设计](../docs/genesis-design.md) - 了解项目架构