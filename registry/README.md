# registry

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/registry.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/registry)

`registry` 是 Genesis 的 L3 治理组件，提供基于 Etcd 的服务注册发现能力，并内置 gRPC resolver 集成。它解决的是服务实例动态上下线后的注册、发现和客户端负载均衡问题，而不是通用配置中心或多协议服务目录。

## 适用场景

- 你的服务实例需要用 Etcd 做注册发现。
- 你的客户端通过 gRPC 调用下游服务，希望直接拿到带 resolver 的 `grpc.ClientConn`。
- 你的进程只有一个 active 服务角色，接受“一个进程一个 active registry”的约束。

不适合的场景：

- 同一个进程要同时维护多个独立 registry 实例。
- 需要把 HTTP、HTTPS、gRPC 等多种协议地址混放在一个 endpoint 列表里。
- 需要跨多种注册中心驱动统一切换。

## 核心能力

- `Register` / `Deregister`：注册和注销服务实例，并用 Etcd lease 管理生命周期。
- `GetService` / `Watch`：获取实例列表，或订阅实例变化。
- `GetConnection`：返回已经接入 etcd resolver 的 gRPC 连接。
- `Close`：停止后台 keepalive / watch，并尽力撤销 registry 创建的 lease。

## 关键边界

- 进程内只允许一个 active registry。这是有意设计，不是实现限制。
- `ServiceInstance.Endpoints` 只接受 gRPC 地址：
  - `grpc://host:port`
  - `host:port`
- `http://`、`https://` 和其他协议地址不会通过注册校验。
- `GetConnection` 只有在 `ctx` 带 deadline 时才会主动等待连接进入 `Ready`；否则只返回已经绑定 resolver 的 `grpc.ClientConn`。
- `Watch` 在遇到 Etcd compaction 时会回到最新快照，并基于快照与本地已知状态做 diff，补发必要的 `PUT` / `DELETE` 事件。
- `Close` 会返回 lease 撤销失败，而不是只打日志。

## 快速开始

```go
package main

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/registry"
)

func main() {
	ctx := context.Background()

	logger, _ := clog.New(clog.NewDevDefaultConfig("registry-example"))
	defer logger.Close()

	etcdConn, _ := connector.NewEtcd(nil, connector.WithLogger(logger))
	defer etcdConn.Close()
	_ = etcdConn.Connect(ctx)

	reg, _ := registry.New(etcdConn, &registry.Config{
		Namespace:  "/genesis/services",
		DefaultTTL: 30 * time.Second,
	}, registry.WithLogger(logger))
	defer reg.Close()

	service := &registry.ServiceInstance{
		ID:        "user-service-001",
		Name:      "user-service",
		Version:   "v1.0.0",
		Endpoints: []string{"grpc://127.0.0.1:9001"},
	}

	_ = reg.Register(ctx, service, 30*time.Second)
}
```

## 服务注册

```go
service := &registry.ServiceInstance{
	ID:        "order-service-001",
	Name:      "order-service",
	Version:   "v1.2.0",
	Endpoints: []string{"127.0.0.1:9002"},
	Metadata: map[string]string{
		"zone":   "ap-southeast-1a",
		"weight": "100",
	},
}

if err := reg.Register(ctx, service, 30*time.Second); err != nil {
	logger.Error("Register failed", clog.Error(err))
	return
}
```

说明：

- `ttl == 0` 时使用 `Config.DefaultTTL`。
- `ttl > 0` 时必须至少为 `1s`。
- 注册成功后，registry 会在后台保持 lease keepalive。

## 服务发现

```go
instances, err := reg.GetService(ctx, "order-service")
if err != nil {
	logger.Error("GetService failed", clog.Error(err))
	return
}

for _, inst := range instances {
	logger.Info("Service instance",
		clog.String("id", inst.ID),
		clog.Any("endpoints", inst.Endpoints))
}
```

## 监听服务变化

```go
eventCh, err := reg.Watch(ctx, "order-service")
if err != nil {
	logger.Error("Watch failed", clog.Error(err))
	return
}

go func() {
	for event := range eventCh {
		switch event.Type {
		case registry.EventTypePut:
			logger.Info("Service updated", clog.String("id", event.Service.ID))
		case registry.EventTypeDelete:
			logger.Info("Service removed", clog.String("id", event.Service.ID))
		}
	}
}()
```

如果 watch 期间遇到 Etcd compaction，registry 不会直接把 revision 跳到最新值后继续监听，而是会读取当前快照并和本地已知实例做 diff，尽量把变化恢复成连续的 `PUT` / `DELETE` 事件。

## gRPC 集成

推荐直接使用 `GetConnection`：

```go
import "google.golang.org/grpc/credentials/insecure"

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

conn, err := reg.GetConnection(ctx, "order-service",
	grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
	logger.Error("GetConnection failed", clog.Error(err))
	return
}
defer conn.Close()
```

说明：

- service name 会被解析成 `etcd:///order-service`。
- 默认使用 `round_robin` 负载均衡策略。
- 如果 `ctx` 没有 deadline，`GetConnection` 不会主动等待连接进入 `Ready`。

## 配置

| 字段 | 说明 |
| --- | --- |
| `Namespace` | Etcd key 前缀，默认 `/genesis/services` |
| `DefaultTTL` | 默认租约时长，默认 `30s`，必须为 `0` 或 `>= 1s` |
| `RetryInterval` | watch / resolver 重试间隔，默认 `1s` |

## 资源管理

`registry` 借用外部 `EtcdConnector` 的连接，不负责 connector 的生命周期。推荐遵循：

```go
etcdConn, _ := connector.NewEtcd(...)
defer etcdConn.Close()

reg, _ := registry.New(etcdConn, cfg)
defer reg.Close()
```

`Close()` 会：

- 停止 keepalive 和 watch 后台任务。
- 尽力撤销当前 registry 创建的 lease。
- 在 lease 撤销失败时返回错误。

## 常见误区

- 把 `Endpoints` 当成通用 URL 列表：当前只支持 gRPC 地址。
- 以为 `GetConnection` 一定返回已经 ready 的连接：只有带 deadline 的 `ctx` 才会主动等待。
- 忘记调用 `Close()`：这样会让 keepalive 和 watch 在后台继续运行。

## 进一步阅读

- `go doc -all ./registry`
- [docs/genesis-registry-blog.md](../docs/genesis-registry-blog.md)
