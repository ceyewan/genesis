# 分布式锁使用指南

## 架构设计

本项目实现了一个简单、可扩展的分布式锁框架：

- **`pkg/lock`**: 定义了通用的分布式锁接口 `Locker`，与底层实现无关
- **`internal/lock`**: 提供具体实现，目前支持基于 etcd 的分布式锁
- **设计原则**: 接口与实现分离，方便未来扩展到 Redis、Zookeeper 等其他存储后端

## 接口设计

```go
type Locker interface {
    // 阻塞式加锁
    Lock(ctx context.Context, key string) error
    
    // 非阻塞式加锁
    TryLock(ctx context.Context, key string) (bool, error)
    
    // 释放锁
    Unlock(ctx context.Context, key string) error
    
    // 带TTL的加锁（自动续期）
    LockWithTTL(ctx context.Context, key string, ttl time.Duration) error
    
    // 关闭客户端
    Close() error
}
```

## 快速开始

### 1. 配置并创建锁实例

```go
import (
    lockpkg "github.com/ceyewan/genesis/pkg/lock"
    etcdlock "github.com/ceyewan/genesis/pkg/lock/etcd"
    "time"
)

cfg := &etcdlock.Config{
    Endpoints:   []string{"localhost:2379"},
    DialTimeout: 5 * time.Second,
}

opts := lockpkg.DefaultLockOptions()
opts.TTL = 10 * time.Second

locker, err := etcdlock.New(cfg, opts)
defer locker.Close()
```

### 3. 使用锁

```go
ctx := context.Background()
lockKey := "/locks/my-resource"

// 阻塞式加锁
err := locker.Lock(ctx, lockKey)
// ... 执行业务逻辑
err = locker.Unlock(ctx, lockKey)

// 非阻塞式加锁
success, err := locker.TryLock(ctx, lockKey)
if success {
    // ... 执行业务逻辑
    locker.Unlock(ctx, lockKey)
}

// 带TTL的加锁
err = locker.LockWithTTL(ctx, lockKey, 5*time.Second)
```

## 运行示例

```bash
# 启动 etcd（使用 docker）
docker-compose up -d etcd

# 运行示例
go run examples/locking-etcd/main.go
```

## 扩展到 Redis

未来扩展到 Redis 时，只需要：

1. 在 `internal/connector` 中添加 Redis 连接管理
2. 在 `internal/lock` 中实现 `RedisLocker`，实现 `lockpkg.Locker` 接口
3. 在 `pkg/lock/redis` 中暴露工厂方法
4. 使用方式完全相同，只需替换实例创建：

```go
// etcd 实现
locker, err := etcdlock.New(etcdCfg, opts)

// redis 实现
locker, err := redislock.New(redisCfg, opts)
```

接口保持不变，业务代码无需修改！

## 配置选项

```go
type LockOptions struct {
    TTL           time.Duration  // 锁的默认超时时间
    RetryInterval time.Duration  // 重试间隔
    AutoRenew     bool           // 是否自动续期
}
```

默认配置：
- TTL: 10秒
- RetryInterval: 100毫秒
- AutoRenew: true

## 特性

✅ 简单易用的接口设计  
✅ 支持阻塞式和非阻塞式加锁  
✅ 支持自定义 TTL  
✅ 自动续期机制  
✅ 线程安全  
✅ 易于扩展到其他存储后端
