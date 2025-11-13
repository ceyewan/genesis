# etcd分布式锁设计方案

## 设计目标
- 基于etcd的强一致性特性实现分布式锁
- 支持锁的自动续期，防止业务逻辑未执行完锁就过期
- 支持可重入锁（可选）
- 提供与现有Redis锁相同的接口
- 支持锁的公平性（按照请求顺序获取锁）

## 核心实现原理

### 基于etcd Lease机制
etcd的Lease机制提供了键值对的自动过期功能，非常适合实现分布式锁：

1. **创建租约**：为每个锁创建一个租约，设置TTL
2. **写入键值**：使用租约ID作为版本号，写入锁信息
3. **续期机制**：后台goroutine定期续期租约
4. **释放锁**：删除键值并释放租约

### 锁的数据结构
```go
// 锁在etcd中的存储结构
type LockInfo struct {
    Key        string    `json:"key"`         // 锁的键
    Token      string    `json:"token"`       // 锁的唯一标识
    Owner      string    `json:"owner"`       // 锁的拥有者（客户端ID）
    CreatedAt  time.Time `json:"created_at"`  // 创建时间
    ExpireAt   time.Time `json:"expire_at"`   // 过期时间
    LeaseID    int64     `json:"lease_id"`    // etcd租约ID
    Reentrant  bool      `json:"reentrant"`   // 是否可重入
    HoldCount  int       `json:"hold_count"`  // 持有次数（用于可重入）
}
```

## 核心接口设计

### etcd锁实现
```go
// pkg/etcd/lock/locker.go
package lock

import (
    "context"
    "fmt"
    "time"
    
    "github.com/ceyewan/genesis/pkg/lock"
    clientv3 "go.etcd.io/etcd/client/v3"
)

// Config etcd锁配置
type Config struct {
    // etcd连接配置
    Endpoints   []string      `json:"endpoints"`
    DialTimeout time.Duration `json:"dial_timeout"`
    Username    string        `json:"username"`
    Password    string        `json:"password"`
    
    // 锁配置
    DefaultTTL      time.Duration `json:"default_ttl"`       // 默认锁过期时间
    RenewInterval   time.Duration `json:"renew_interval"`    // 续期间隔
    RetryInterval   time.Duration `json:"retry_interval"`    // 重试间隔
    MaxRetries      int           `json:"max_retries"`       // 最大重试次数
    EnableReentrant bool          `json:"enable_reentrant"`  // 是否启用可重入
}

// Locker etcd分布式锁实现
type Locker struct {
    client       *clientv3.Client
    config       *Config
    leaseManager *LeaseManager
    sessionID    string // 客户端会话ID
}

// LeaseManager 租约管理器
type LeaseManager struct {
    client    *clientv3.Client
    leases    map[string]int64 // key -> leaseID
    mu        sync.RWMutex
    stopCh    chan struct{}
}
```

## 关键实现细节

### 1. 获取锁（TryLock）
```go
func (l *Locker) TryLock(ctx context.Context, key string, opts ...lock.Option) (lock.LockGuard, bool, error) {
    // 解析选项
    ttl := l.config.DefaultTTL
    for _, opt := range opts {
        if withTTL, ok := opt.(lock.WithTTL); ok {
            ttl = withTTL.Duration
        }
    }
    
    // 创建租约
    lease, err := l.client.Grant(ctx, int64(ttl.Seconds()))
    if err != nil {
        return nil, false, fmt.Errorf("failed to grant lease: %w", err)
    }
    
    // 生成锁token
    token, err := generateToken()
    if err != nil {
        l.client.Revoke(ctx, lease.ID)
        return nil, false, fmt.Errorf("failed to generate token: %w", err)
    }
    
    // 构建锁信息
    lockInfo := &LockInfo{
        Key:       key,
        Token:     token,
        Owner:     l.sessionID,
        CreatedAt: time.Now(),
        ExpireAt:  time.Now().Add(ttl),
        LeaseID:   lease.ID,
    }
    
    // 序列化锁信息
    data, err := json.Marshal(lockInfo)
    if err != nil {
        l.client.Revoke(ctx, lease.ID)
        return nil, false, fmt.Errorf("failed to marshal lock info: %w", err)
    }
    
    // 使用事务确保原子性
    txn := l.client.Txn(ctx).
        If(clientv3.Compare(clientv3.CreateRevision(lockKey), "=", 0)).
        Then(clientv3.OpPut(lockKey, string(data), clientv3.WithLease(lease.ID))).
        Else(clientv3.OpGet(lockKey))
    
    resp, err := txn.Commit()
    if err != nil {
        l.client.Revoke(ctx, lease.ID)
        return nil, false, fmt.Errorf("failed to commit transaction: %w", err)
    }
    
    // 检查是否成功获取锁
    if !resp.Succeeded {
        // 锁已被其他客户端持有
        return nil, false, nil
    }
    
    // 启动续期goroutine
    l.leaseManager.AddLease(key, lease.ID, ttl)
    
    return &Guard{
        locker:  l,
        key:     key,
        token:   token,
        leaseID: lease.ID,
    }, true, nil
}
```

### 2. 锁续期机制
```go
// LeaseManager 管理所有活跃租约的续期
func (lm *LeaseManager) AddLease(key string, leaseID int64, ttl time.Duration) {
    lm.mu.Lock()
    defer lm.mu.Unlock()
    
    lm.leases[key] = leaseID
    
    // 启动续期goroutine
    go lm.keepAlive(key, leaseID, ttl)
}

func (lm *LeaseManager) keepAlive(key string, leaseID int64, ttl time.Duration) {
    // 计算续期间隔（TTL的1/3）
    interval := ttl / 3
    
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    for {
        select {
        case <-lm.stopCh:
            return
        case <-ticker.C:
            // 检查租约是否还存在
            lm.mu.RLock()
            currentLeaseID, exists := lm.leases[key]
            lm.mu.RUnlock()
            
            if !exists || currentLeaseID != leaseID {
                return
            }
            
            // 续期租约
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            _, err := lm.client.KeepAliveOnce(ctx, leaseID)
            cancel()
            
            if err != nil {
                // 续期失败，可能是租约已过期
                lm.mu.Lock()
                delete(lm.leases, key)
                lm.mu.Unlock()
                return
            }
        }
    }
}
```

### 3. 释放锁
```go
func (g *Guard) Unlock(ctx context.Context) error {
    // 停止续期
    g.locker.leaseManager.RemoveLease(g.key)
    
    // 使用事务确保只有锁的持有者才能删除
    lockKey := fmt.Sprintf("/locks/%s", g.key)
    
    // 先获取当前锁信息
    resp, err := g.locker.client.Get(ctx, lockKey)
    if err != nil {
        return fmt.Errorf("failed to get lock info: %w", err)
    }
    
    if len(resp.Kvs) == 0 {
        return fmt.Errorf("lock not found")
    }
    
    var lockInfo LockInfo
    if err := json.Unmarshal(resp.Kvs[0].Value, &lockInfo); err != nil {
        return fmt.Errorf("failed to unmarshal lock info: %w", err)
    }
    
    // 验证token是否匹配
    if lockInfo.Token != g.token {
        return fmt.Errorf("token mismatch, not the lock owner")
    }
    
    // 删除锁
    _, err = g.locker.client.Delete(ctx, lockKey)
    if err != nil {
        return fmt.Errorf("failed to delete lock: %w", err)
    }
    
    // 撤销租约
    if g.leaseID != 0 {
        _, err = g.locker.client.Revoke(ctx, g.leaseID)
        if err != nil {
            // 记录日志但不返回错误，因为锁已经删除
            log.Printf("Failed to revoke lease %d: %v", g.leaseID, err)
        }
    }
    
    return nil
}
```

## 高级特性

### 1. 可重入锁支持
```go
func (l *Locker) TryLockReentrant(ctx context.Context, key string, opts ...lock.Option) (lock.LockGuard, bool, error) {
    if !l.config.EnableReentrant {
        return l.TryLock(ctx, key, opts...)
    }
    
    // 检查当前客户端是否已持有该锁
    existingLock, err := l.getLockInfo(ctx, key)
    if err == nil && existingLock.Owner == l.sessionID {
        // 增加持有计数
        existingLock.HoldCount++
        if err := l.updateLockInfo(ctx, existingLock); err != nil {
            return nil, false, err
        }
        
        return &ReentrantGuard{
            Guard: Guard{
                locker:  l,
                key:     key,
                token:   existingLock.Token,
                leaseID: existingLock.LeaseID,
            },
            holdCount: existingLock.HoldCount,
        }, true, nil
    }
    
    // 否则尝试获取新锁
    return l.TryLock(ctx, key, opts...)
}
```

### 2. 公平锁实现
```go
// 使用etcd的Revision机制实现公平锁
func (l *Locker) LockFair(ctx context.Context, key string, opts ...lock.Option) (lock.LockGuard, error) {
    // 创建等待队列键
    waitKey := fmt.Sprintf("/locks/wait/%s/%s", key, uuid.New().String())
    
    // 获取当前等待队列
    resp, err := l.client.Get(ctx, fmt.Sprintf("/locks/wait/%s", key), clientv3.WithPrefix())
    if err != nil {
        return nil, err
    }
    
    // 如果前面有等待者，监听前面节点的删除事件
    if len(resp.Kvs) > 0 {
        // 找到前一个等待者
        prevKey := l.findPreviousWaiter(resp.Kvs, waitKey)
        
        // 监听前一个等待者的删除
        watchCh := l.client.Watch(ctx, prevKey)
        
        // 等待前一个等待者释放
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-watchCh:
            // 前一个等待者已释放，继续尝试获取锁
        }
    }
    
    // 尝试获取锁
    return l.Lock(ctx, key, opts...)
}
```

## 错误处理和容错

### 1. 网络分区处理
- 当etcd集群发生网络分区时，客户端可能无法连接到etcd
- 实现指数退避重试机制
- 设置最大重试次数和超时时间

### 2. 租约失效处理
- 监控租约失效事件
- 及时清理无效的锁记录
- 防止死锁

### 3. 客户端崩溃处理
- 客户端崩溃时，租约会自动过期
- 其他客户端可以正常获取锁
- 无需人工干预

## 性能优化

### 1. 连接池优化
- 复用etcd客户端连接
- 减少连接建立开销
- 支持多路复用

### 2. 批量操作
- 批量续期租约
- 减少网络往返次数

### 3. 本地缓存
- 缓存锁状态信息
- 减少etcd访问次数

## 监控和观测

### 1. 指标收集
- 锁获取成功率
- 锁等待时间
- 锁持有时间
- 续期成功率

### 2. 日志记录
- 锁获取/释放事件
- 错误和异常
- 性能指标

这个etcd分布式锁设计充分利用了etcd的强一致性特性，提供了可靠的分布式锁服务，支持自动续期、可重入等高级特性。