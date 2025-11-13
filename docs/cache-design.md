# 缓存组件抽象接口设计

## 设计目标
- 提供统一的缓存抽象接口，支持Redis和etcd两种实现
- 支持基本的缓存操作：获取、设置、删除、存在检查、清空
- 支持TTL（生存时间）设置
- 支持批量操作和事务
- 提供缓存统计和监控能力
- 支持缓存预热和缓存穿透保护

## 核心接口设计

### 基础缓存接口
```go
// pkg/common/cache/cache.go
package cache

import (
    "context"
    "time"
)

// Cache 定义了缓存的通用接口
type Cache interface {
    // 基本操作
    Get(ctx context.Context, key string) (interface{}, error)
    Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
    Clear(ctx context.Context) error
    
    // 批量操作
    GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error)
    SetMulti(ctx context.Context, items map[string]interface{}, ttl time.Duration) error
    DeleteMulti(ctx context.Context, keys []string) error
    
    // TTL操作
    GetTTL(ctx context.Context, key string) (time.Duration, error)
    SetTTL(ctx context.Context, key string, ttl time.Duration) error
    Persist(ctx context.Context, key string) error
    
    // 原子操作
    Increment(ctx context.Context, key string, delta int64) (int64, error)
    Decrement(ctx context.Context, key string, delta int64) (int64, error)
    
    // 统计信息
    Stats(ctx context.Context) (*Stats, error)
    
    // 健康检查
    HealthCheck(ctx context.Context) error
    
    // 关闭缓存
    Close() error
}

// Stats 缓存统计信息
type Stats struct {
    HitCount         int64         `json:"hit_count"`         // 命中次数
    MissCount        int64         `json:"miss_count"`        // 未命中次数
    HitRate          float64       `json:"hit_rate"`          // 命中率
    KeyCount         int64         `json:"key_count"`         // 键数量
    TotalSize        int64         `json:"total_size"`        // 总大小（字节）
    EvictionCount    int64         `json:"eviction_count"`    // 淘汰次数
    AverageAccessTime time.Duration `json:"average_access_time"` // 平均访问时间
    LastUpdateTime   time.Time     `json:"last_update_time"`  // 最后更新时间
}

// Item 缓存项
type Item struct {
    Key        string        `json:"key"`
    Value      interface{}   `json:"value"`
    TTL        time.Duration `json:"ttl"`
    CreateTime time.Time     `json:"create_time"`
    AccessTime time.Time     `json:"access_time"`
    HitCount   int64         `json:"hit_count"`
    Size       int64         `json:"size"`
}

// Options 缓存选项
type Options struct {
    // 默认TTL
    DefaultTTL time.Duration `json:"default_ttl"`
    
    // 最大内存限制（字节）
    MaxMemory int64 `json:"max_memory"`
    
    // 淘汰策略
    EvictionPolicy EvictionPolicy `json:"eviction_policy"`
    
    // 是否启用压缩
    EnableCompression bool `json:"enable_compression"`
    
    // 序列化方式
    Serializer Serializer `json:"serializer"`
    
    // 缓存前缀
    Prefix string `json:"prefix"`
    
    // 是否启用统计
    EnableStats bool `json:"enable_stats"`
}

// EvictionPolicy 淘汰策略
type EvictionPolicy string

const (
    EvictionLRU    EvictionPolicy = "lru"    // 最近最少使用
    EvictionLFU    EvictionPolicy = "lfu"    // 最不经常使用
    EvictionFIFO   EvictionPolicy = "fifo"   // 先进先出
    EvictionRandom EvictionPolicy = "random" // 随机淘汰
)

// Serializer 序列化器
type Serializer interface {
    Serialize(v interface{}) ([]byte, error)
    Deserialize(data []byte, v interface{}) error
    GetType() string
}
```

## 高级特性接口

### 缓存加载器
```go
// Loader 缓存加载器，用于缓存未命中时加载数据
type Loader interface {
    Load(ctx context.Context, key string) (interface{}, error)
    LoadMulti(ctx context.Context, keys []string) (map[string]interface{}, error)
}

// LoadFunc 加载函数类型
type LoadFunc func(ctx context.Context, key string) (interface{}, error)

// CacheLoader 支持自动加载的缓存
type CacheLoader interface {
    Cache
    GetWithLoader(ctx context.Context, key string, loader Loader) (interface{}, error)
    GetWithFunc(ctx context.Context, key string, loadFunc LoadFunc) (interface{}, error)
}
```

### 缓存装饰器
```go
// Decorator 缓存装饰器，用于添加额外功能
type Decorator interface {
    // 装饰Get操作
    DecorateGet(ctx context.Context, key string, next GetFunc) (interface{}, error)
    
    // 装饰Set操作
    DecorateSet(ctx context.Context, key string, value interface{}, ttl time.Duration, next SetFunc) error
    
    // 装饰Delete操作
    DecorateDelete(ctx context.Context, key string, next DeleteFunc) error
}

// GetFunc Get操作函数类型
type GetFunc func(ctx context.Context, key string) (interface{}, error)

// SetFunc Set操作函数类型
type SetFunc func(ctx context.Context, key string, value interface{}, ttl time.Duration) error

// DeleteFunc Delete操作函数类型
type DeleteFunc func(ctx context.Context, key string) error
```

### 缓存事件
```go
// EventType 事件类型
type EventType string

const (
    EventHit      EventType = "hit"      // 缓存命中
    EventMiss     EventType = "miss"     // 缓存未命中
    EventSet      EventType = "set"      // 设置缓存
    EventDelete   EventType = "delete"   // 删除缓存
    EventExpire   EventType = "expire"   // 缓存过期
    EventEvict    EventType = "evict"    // 缓存淘汰
    EventClear    EventType = "clear"    // 清空缓存
)

// Event 缓存事件
type Event struct {
    Type      EventType     `json:"type"`
    Key       string        `json:"key"`
    Value     interface{}   `json:"value,omitempty"`
    TTL       time.Duration `json:"ttl,omitempty"`
    Timestamp time.Time     `json:"timestamp"`
    Error     error         `json:"error,omitempty"`
}

// EventListener 事件监听器
type EventListener interface {
    OnEvent(event Event)
}

// ObservableCache 可观察的缓存
type ObservableCache interface {
    Cache
    AddEventListener(listener EventListener)
    RemoveEventListener(listener EventListener)
}
```

## Redis缓存实现设计

### 配置结构
```go
// pkg/redis/cache/config.go
package cache

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/cache"
)

// RedisConfig Redis缓存配置
type RedisConfig struct {
    cache.Options
    
    // Redis连接配置
    Addr         string   `json:"addr"`
    Password     string   `json:"password"`
    DB           int      `json:"db"`
    PoolSize     int      `json:"pool_size"`
    MinIdleConns int      `json:"min_idle_conns"`
    
    // 缓存特定配置
    KeyPrefix    string        `json:"key_prefix"`     // 键前缀
    ScanCount    int           `json:"scan_count"`     // SCAN命令的COUNT参数
    PipelineSize int           `json:"pipeline_size"`  // 管道批量大小
    FlushInterval time.Duration `json:"flush_interval"` // 统计刷新间隔
}
```

### 核心实现
```go
// pkg/redis/cache/cache.go
package cache

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    "github.com/ceyewan/genesis/pkg/common/cache"
    "github.com/redis/go-redis/v9"
)

// RedisCache Redis缓存实现
type RedisCache struct {
    client   *redis.Client
    config   *RedisConfig
    stats    *cache.Stats
    mu       sync.RWMutex
    listeners []cache.EventListener
}

// Get 获取缓存值
func (c *RedisCache) Get(ctx context.Context, key string) (interface{}, error) {
    fullKey := c.getFullKey(key)
    
    // 发送事件
    c.emitEvent(cache.Event{
        Type:      cache.EventMiss,
        Key:       key,
        Timestamp: time.Now(),
    })
    
    // 获取值
    data, err := c.client.Get(ctx, fullKey).Bytes()
    if err != nil {
        if err == redis.Nil {
            c.updateStats(false)
            return nil, nil
        }
        c.updateStats(false)
        return nil, fmt.Errorf("failed to get key %s: %w", key, err)
    }
    
    // 反序列化
    var value interface{}
    if err := c.config.Serializer.Deserialize(data, &value); err != nil {
        c.updateStats(false)
        return nil, fmt.Errorf("failed to deserialize value: %w", err)
    }
    
    c.updateStats(true)
    
    // 发送命中事件
    c.emitEvent(cache.Event{
        Type:      cache.EventHit,
        Key:       key,
        Value:     value,
        Timestamp: time.Now(),
    })
    
    return value, nil
}

// Set 设置缓存值
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
    fullKey := c.getFullKey(key)
    
    // 序列化值
    data, err := c.config.Serializer.Serialize(value)
    if err != nil {
        return fmt.Errorf("failed to serialize value: %w", err)
    }
    
    // 设置值
    err = c.client.Set(ctx, fullKey, data, ttl).Err()
    if err != nil {
        return fmt.Errorf("failed to set key %s: %w", key, err)
    }
    
    // 发送事件
    c.emitEvent(cache.Event{
        Type:      cache.EventSet,
        Key:       key,
        Value:     value,
        TTL:       ttl,
        Timestamp: time.Now(),
    })
    
    return nil
}

// GetMulti 批量获取
func (c *RedisCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
    if len(keys) == 0 {
        return make(map[string]interface{}), nil
    }
    
    // 构建完整键名
    fullKeys := make([]string, len(keys))
    keyMap := make(map[string]string) // fullKey -> originalKey
    for i, key := range keys {
        fullKey := c.getFullKey(key)
        fullKeys[i] = fullKey
        keyMap[fullKey] = key
    }
    
    // 使用pipeline批量获取
    pipe := c.client.Pipeline()
    cmds := make([]*redis.StringCmd, len(fullKeys))
    for i, fullKey := range fullKeys {
        cmds[i] = pipe.Get(ctx, fullKey)
    }
    
    _, err := pipe.Exec(ctx)
    if err != nil && err != redis.Nil {
        return nil, fmt.Errorf("failed to execute pipeline: %w", err)
    }
    
    // 处理结果
    result := make(map[string]interface{})
    for i, cmd := range cmds {
        if cmd.Err() == redis.Nil {
            continue
        }
        if cmd.Err() != nil {
            continue
        }
        
        data, err := cmd.Bytes()
        if err != nil {
            continue
        }
        
        var value interface{}
        if err := c.config.Serializer.Deserialize(data, &value); err != nil {
            continue
        }
        
        originalKey := keyMap[fullKeys[i]]
        result[originalKey] = value
    }
    
    return result, nil
}
```

## etcd缓存实现设计

### 配置结构
```go
// pkg/etcd/cache/config.go
package cache

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/cache"
)

// EtcdConfig etcd缓存配置
type EtcdConfig struct {
    cache.Options
    
    // etcd连接配置
    Endpoints   []string      `json:"endpoints"`
    DialTimeout time.Duration `json:"dial_timeout"`
    Username    string        `json:"username"`
    Password    string        `json:"password"`
    
    // 缓存特定配置
    KeyPrefix     string        `json:"key_prefix"`      // 键前缀
    LeaseTTL      time.Duration `json:"lease_ttl"`       // 租约TTL
    CompactInterval time.Duration `json:"compact_interval"` // 压缩间隔
    MaxTxnOps     int           `json:"max_txn_ops"`     // 最大事务操作数
}
```

### 核心实现
```go
// pkg/etcd/cache/cache.go
package cache

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    "github.com/ceyewan/genesis/pkg/common/cache"
    clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdCache etcd缓存实现
type EtcdCache struct {
    client   *clientv3.Client
    config   *EtcdConfig
    stats    *cache.Stats
    mu       sync.RWMutex
    listeners []cache.EventListener
}

// Get 获取缓存值
func (c *EtcdCache) Get(ctx context.Context, key string) (interface{}, error) {
    fullKey := c.getFullKey(key)
    
    resp, err := c.client.Get(ctx, fullKey)
    if err != nil {
        c.updateStats(false)
        return nil, fmt.Errorf("failed to get key %s: %w", key, err)
    }
    
    if len(resp.Kvs) == 0 {
        c.updateStats(false)
        c.emitEvent(cache.Event{
            Type:      cache.EventMiss,
            Key:       key,
            Timestamp: time.Now(),
        })
        return nil, nil
    }
    
    // 反序列化值
    var value interface{}
    if err := c.config.Serializer.Deserialize(resp.Kvs[0].Value, &value); err != nil {
        c.updateStats(false)
        return nil, fmt.Errorf("failed to deserialize value: %w", err)
    }
    
    c.updateStats(true)
    
    // 发送命中事件
    c.emitEvent(cache.Event{
        Type:      cache.EventHit,
        Key:       key,
        Value:     value,
        Timestamp: time.Now(),
    })
    
    return value, nil
}

// Set 设置缓存值
func (c *EtcdCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
    fullKey := c.getFullKey(key)
    
    // 序列化值
    data, err := c.config.Serializer.Serialize(value)
    if err != nil {
        return fmt.Errorf("failed to serialize value: %w", err)
    }
    
    // 创建租约
    lease, err := c.client.Grant(ctx, int64(ttl.Seconds()))
    if err != nil {
        return fmt.Errorf("failed to grant lease: %w", err)
    }
    
    // 设置值
    _, err = c.client.Put(ctx, fullKey, string(data), clientv3.WithLease(lease.ID))
    if err != nil {
        return fmt.Errorf("failed to set key %s: %w", key, err)
    }
    
    // 发送事件
    c.emitEvent(cache.Event{
        Type:      cache.EventSet,
        Key:       key,
        Value:     value,
        TTL:       ttl,
        Timestamp: time.Now(),
    })
    
    return nil
}
```

## 缓存装饰器实现

### 缓存穿透保护
```go
// pkg/common/cache/decorator/bloom_filter.go
package decorator

import (
    "context"
    "github.com/ceyewan/genesis/pkg/common/cache"
)

// BloomFilterDecorator 布隆过滤器装饰器，防止缓存穿透
type BloomFilterDecorator struct {
    bloomFilter *BloomFilter
    cache       cache.Cache
}

func (d *BloomFilterDecorator) DecorateGet(ctx context.Context, key string, next cache.GetFunc) (interface{}, error) {
    // 检查布隆过滤器
    if !d.bloomFilter.MightContain(key) {
        // 布隆过滤器认为key不存在，直接返回nil
        return nil, nil
    }
    
    // 继续执行原Get操作
    return next(ctx, key)
}
```

### 缓存雪崩保护
```go
// pkg/common/cache/decorator/jitter.go
package decorator

import (
    "context"
    "math/rand"
    "time"
    "github.com/ceyewan/genesis/pkg/common/cache"
)

// JitterDecorator 随机TTL装饰器，防止缓存雪崩
type JitterDecorator struct {
    jitterRange time.Duration
}

func (d *JitterDecorator) DecorateSet(ctx context.Context, key string, value interface{}, ttl time.Duration, next cache.SetFunc) error {
    // 添加随机抖动
    jitter := time.Duration(rand.Int63n(int64(d.jitterRange)))
    newTTL := ttl + jitter
    
    return next(ctx, key, value, newTTL)
}
```

这个缓存组件设计提供了完整的抽象接口和丰富的功能特性，支持Redis和etcd两种后端实现，同时提供了装饰器模式来扩展功能。