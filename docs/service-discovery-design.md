# 服务发现与注册抽象接口设计

## 设计目标
- 提供统一的服务发现与注册抽象接口，支持Redis和etcd两种实现
- 支持服务的健康检查、负载均衡、故障转移
- 支持服务配置的动态更新和版本管理
- 提供服务事件监听机制（上线、下线、配置变更）
- 支持多数据中心和区域感知
- 提供丰富的服务元数据管理

## 核心接口设计

### 基础服务发现接口
```go
// pkg/common/discovery/discovery.go
package discovery

import (
    "context"
    "time"
)

// ServiceDiscovery 定义了服务发现的通用接口
type ServiceDiscovery interface {
    // 服务注册
    Register(ctx context.Context, service ServiceInfo) error
    RegisterWithTTL(ctx context.Context, service ServiceInfo, ttl time.Duration) error
    
    // 服务注销
    Deregister(ctx context.Context, serviceID string) error
    
    // 服务发现
    Discover(ctx context.Context, serviceName string) ([]ServiceInfo, error)
    DiscoverHealthy(ctx context.Context, serviceName string) ([]ServiceInfo, error)
    DiscoverByTags(ctx context.Context, serviceName string, tags []string) ([]ServiceInfo, error)
    
    // 服务监听
    Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)
    WatchHealthy(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)
    
    // 服务信息获取
    GetService(ctx context.Context, serviceID string) (*ServiceInfo, error)
    GetAllServices(ctx context.Context) ([]ServiceInfo, error)
    
    // 健康检查
    HealthCheck(ctx context.Context, serviceID string) error
    UpdateHealthStatus(ctx context.Context, serviceID string, status HealthStatus) error
    
    // 服务元数据
    UpdateMetadata(ctx context.Context, serviceID string, metadata map[string]string) error
    GetMetadata(ctx context.Context, serviceID string) (map[string]string, error)
    
    // 统计信息
    Stats(ctx context.Context) (*Stats, error)
    
    // 健康检查
    HealthCheck(ctx context.Context) error
    
    // 关闭
    Close() error
}

// ServiceInfo 服务信息
type ServiceInfo struct {
    ID          string            `json:"id"`           // 服务实例ID
    Name        string            `json:"name"`         // 服务名称
    Version     string            `json:"version"`      // 服务版本
    Address     string            `json:"address"`      // 服务地址
    Port        int               `json:"port"`         // 服务端口号
    Protocol    string            `json:"protocol"`     // 协议类型
    Tags        []string          `json:"tags"`         // 服务标签
    Metadata    map[string]string `json:"metadata"`     // 服务元数据
    HealthCheck *HealthCheckInfo  `json:"health_check"` // 健康检查配置
    Status      ServiceStatus     `json:"status"`       // 服务状态
    Region      string            `json:"region"`       // 所属区域
    Zone        string            `json:"zone"`         // 所属可用区
    Weight      int               `json:"weight"`       // 权重（用于负载均衡）
    RegisterTime time.Time        `json:"register_time"` // 注册时间
    LastHeartbeat time.Time       `json:"last_heartbeat"` // 最后心跳时间
}

// ServiceStatus 服务状态
type ServiceStatus string

const (
    StatusUnknown   ServiceStatus = "unknown"   // 未知状态
    StatusStarting  ServiceStatus = "starting"  // 启动中
    StatusHealthy   ServiceStatus = "healthy"   // 健康
    StatusUnhealthy ServiceStatus = "unhealthy" // 不健康
    StatusStopping  ServiceStatus = "stopping"  // 停止中
    StatusStopped   ServiceStatus = "stopped"   // 已停止
)

// HealthCheckInfo 健康检查信息
type HealthCheckInfo struct {
    Type        HealthCheckType   `json:"type"`        // 检查类型
    Endpoint    string            `json:"endpoint"`    // 检查端点
    Interval    time.Duration     `json:"interval"`    // 检查间隔
    Timeout     time.Duration     `json:"timeout"`     // 超时时间
    Threshold   int               `json:"threshold"`   // 失败阈值
    SuccessCodes []int            `json:"success_codes"` // 成功状态码
    Headers     map[string]string `json:"headers"`     // 请求头
}

// HealthCheckType 健康检查类型
type HealthCheckType string

const (
    HealthCheckHTTP   HealthCheckType = "http"   // HTTP检查
    HealthCheckTCP    HealthCheckType = "tcp"    // TCP检查
    HealthCheckGRPC   HealthCheckType = "grpc"   // gRPC检查
    HealthCheckScript HealthCheckType = "script" // 脚本检查
)

// HealthStatus 健康状态
type HealthStatus struct {
    ServiceID string        `json:"service_id"`
    Status    ServiceStatus `json:"status"`
    Message   string        `json:"message"`
    Timestamp time.Time     `json:"timestamp"`
    Checks    []CheckResult `json:"checks"`
}

// CheckResult 检查结果
type CheckResult struct {
    Type      HealthCheckType `json:"type"`
    Success   bool            `json:"success"`
    Message   string          `json:"message"`
    Duration  time.Duration   `json:"duration"`
    Timestamp time.Time       `json:"timestamp"`
}

// ServiceEvent 服务事件
type ServiceEvent struct {
    Type        EventType     `json:"type"`         // 事件类型
    Service     ServiceInfo   `json:"service"`      // 服务信息
    Timestamp   time.Time     `json:"timestamp"`    // 事件时间
    OldStatus   ServiceStatus `json:"old_status"`   // 旧状态
    NewStatus   ServiceStatus `json:"new_status"`   // 新状态
    Metadata    map[string]string `json:"metadata"` // 事件元数据
}

// EventType 事件类型
type EventType string

const (
    EventServiceRegistered EventType = "service_registered"   // 服务注册
    EventServiceDeregistered EventType = "service_deregistered" // 服务注销
    EventServiceUpdated EventType = "service_updated"         // 服务更新
    EventServiceHealthy EventType = "service_healthy"         // 服务变健康
    EventServiceUnhealthy EventType = "service_unhealthy"     // 服务变不健康
    EventServiceExpired EventType = "service_expired"         // 服务过期
)

// Stats 统计信息
type Stats struct {
    TotalServices    int64            `json:"total_services"`    // 总服务数
    HealthyServices  int64            `json:"healthy_services"`  // 健康服务数
    UnhealthyServices int64           `json:"unhealthy_services"` // 不健康服务数
    ServiceTypes     map[string]int64 `json:"service_types"`     // 各类型服务数
    LastUpdateTime   time.Time        `json:"last_update_time"`  // 最后更新时间
}

// LoadBalancer 负载均衡器接口
type LoadBalancer interface {
    // 选择服务实例
    Select(services []ServiceInfo) (*ServiceInfo, error)
    
    // 更新服务权重
    UpdateWeight(serviceID string, weight int) error
    
    // 获取负载均衡策略
    GetStrategy() LoadBalancerStrategy
}

// LoadBalancerStrategy 负载均衡策略
type LoadBalancerStrategy string

const (
    LBStrategyRandom       LoadBalancerStrategy = "random"        // 随机
    LBStrategyRoundRobin   LoadBalancerStrategy = "round_robin"   // 轮询
    LBStrategyWeighted     LoadBalancerStrategy = "weighted"      // 权重
    LBStrategyLeastConn    LoadBalancerStrategy = "least_conn"    // 最少连接
    LBStrategyHash         LoadBalancerStrategy = "hash"          // 哈希
    LBStrategyRegionAware  LoadBalancerStrategy = "region_aware"  // 区域感知
)
```

## 高级特性接口

### 服务网格支持
```go
// ServiceMesh 服务网格接口
type ServiceMesh interface {
    ServiceDiscovery
    
    // 服务路由
    Route(ctx context.Context, serviceName string, headers map[string]string) (*ServiceInfo, error)
    
    // 流量管理
    SetTrafficPolicy(ctx context.Context, serviceName string, policy TrafficPolicy) error
    GetTrafficPolicy(ctx context.Context, serviceName string) (*TrafficPolicy, error)
    
    // 熔断器
    EnableCircuitBreaker(ctx context.Context, serviceName string, config CircuitBreakerConfig) error
    DisableCircuitBreaker(ctx context.Context, serviceName string) error
    
    // 重试策略
    SetRetryPolicy(ctx context.Context, serviceName string, policy RetryPolicy) error
}

// TrafficPolicy 流量策略
type TrafficPolicy struct {
    ServiceName   string          `json:"service_name"`
    Split         []TrafficSplit  `json:"split"`          // 流量分配
    Timeout       time.Duration   `json:"timeout"`        // 超时时间
    RetryPolicy   *RetryPolicy    `json:"retry_policy"`   // 重试策略
    CircuitBreaker *CircuitBreakerConfig `json:"circuit_breaker"` // 熔断器配置
}

// TrafficSplit 流量分配
type TrafficSplit struct {
    Version string `json:"version"` // 目标版本
    Weight  int    `json:"weight"`  // 权重百分比
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
    Enabled          bool          `json:"enabled"`
    FailureThreshold int           `json:"failure_threshold"` // 失败阈值
    SuccessThreshold int           `json:"success_threshold"` // 成功阈值
    Timeout          time.Duration `json:"timeout"`           // 熔断超时时间
    WindowSize       time.Duration `json:"window_size"`       // 时间窗口
}

// RetryPolicy 重试策略
type RetryPolicy struct {
    MaxAttempts    int           `json:"max_attempts"`     // 最大重试次数
    InitialBackoff time.Duration `json:"initial_backoff"`  // 初始退避时间
    MaxBackoff     time.Duration `json:"max_backoff"`      // 最大退避时间
    BackoffFactor  float64       `json:"backoff_factor"`   // 退避因子
    RetryOn        []string      `json:"retry_on"`         // 重试条件
}
```

### 多数据中心支持
```go
// MultiDataCenter 多数据中心接口
type MultiDataCenter interface {
    ServiceDiscovery
    
    // 数据中心管理
    GetDataCenters(ctx context.Context) ([]DataCenter, error)
    GetCurrentDataCenter(ctx context.Context) (*DataCenter, error)
    
    // 跨数据中心服务发现
    DiscoverCrossDC(ctx context.Context, serviceName string, dataCenters []string) (map[string][]ServiceInfo, error)
    
    // 区域感知路由
    RouteWithRegion(ctx context.Context, serviceName string, region string) (*ServiceInfo, error)
}

// DataCenter 数据中心信息
type DataCenter struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Region      string            `json:"region"`
    Zone        string            `json:"zone"`
    Address     string            `json:"address"`
    Metadata    map[string]string `json:"metadata"`
    Status      DataCenterStatus  `json:"status"`
    LastSyncTime time.Time        `json:"last_sync_time"`
}

// DataCenterStatus 数据中心状态
type DataCenterStatus string

const (
    DCStatusActive   DataCenterStatus = "active"   // 活跃
    DCStatusInactive DataCenterStatus = "inactive" // 不活跃
    DCStatusSyncing  DataCenterStatus = "syncing"  // 同步中
)
```

## Redis实现设计

### 配置结构
```go
// pkg/redis/discovery/config.go
package discovery

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/discovery"
)

// RedisConfig Redis服务发现配置
type RedisConfig struct {
    // Redis连接配置
    Addr     string `json:"addr"`
    Password string `json:"password"`
    DB       int    `json:"db"`
    
    // 服务发现配置
    KeyPrefix        string        `json:"key_prefix"`         // 键前缀
    HeartbeatInterval time.Duration `json:"heartbeat_interval"` // 心跳间隔
    ServiceTTL       time.Duration `json:"service_ttl"`        // 服务TTL
    CleanupInterval  time.Duration `json:"cleanup_interval"`   // 清理间隔
    WatchBufferSize  int           `json:"watch_buffer_size"`  // 监听缓冲区大小
    
    // 负载均衡配置
    DefaultLBStrategy discovery.LoadBalancerStrategy `json:"default_lb_strategy"` // 默认负载均衡策略
}
```

### 核心实现
```go
// pkg/redis/discovery/discovery.go
package discovery

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    "github.com/ceyewan/genesis/pkg/common/discovery"
    "github.com/redis/go-redis/v9"
)

// RedisDiscovery Redis服务发现实现
type RedisDiscovery struct {
    client        *redis.Client
    config        *RedisConfig
    services      map[string]*discovery.ServiceInfo
    mu            sync.RWMutex
    watchers      map[string]chan discovery.ServiceEvent
    loadBalancers map[string]discovery.LoadBalancer
    stopCh        chan struct{}
}

// Register 注册服务
func (d *RedisDiscovery) Register(ctx context.Context, service discovery.ServiceInfo) error {
    service.RegisterTime = time.Now()
    service.LastHeartbeat = time.Now()
    service.Status = discovery.StatusHealthy
    
    // 序列化服务信息
    data, err := json.Marshal(service)
    if err != nil {
        return fmt.Errorf("failed to marshal service info: %w", err)
    }
    
    // 构建键名
    serviceKey := d.getServiceKey(service.ID)
    
    // 使用SET命令存储服务信息，设置TTL
    err = d.client.Set(ctx, serviceKey, data, d.config.ServiceTTL).Err()
    if err != nil {
        return fmt.Errorf("failed to register service: %w", err)
    }
    
    // 添加到服务集合
    servicesKey := d.getServicesKey(service.Name)
    err = d.client.SAdd(ctx, servicesKey, service.ID).Err()
    if err != nil {
        return fmt.Errorf("failed to add service to set: %w", err)
    }
    
    // 启动心跳
    go d.heartbeat(ctx, service.ID)
    
    // 发送注册事件
    d.emitEvent(discovery.ServiceEvent{
        Type:      discovery.EventServiceRegistered,
        Service:   service,
        Timestamp: time.Now(),
    })
    
    return nil
}

// Discover 发现服务
func (d *RedisDiscovery) Discover(ctx context.Context, serviceName string) ([]discovery.ServiceInfo, error) {
    // 获取服务ID列表
    servicesKey := d.getServicesKey(serviceName)
    serviceIDs, err := d.client.SMembers(ctx, servicesKey).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get service IDs: %w", err)
    }
    
    // 获取服务详细信息
    services := make([]discovery.ServiceInfo, 0, len(serviceIDs))
    for _, serviceID := range serviceIDs {
        service, err := d.GetService(ctx, serviceID)
        if err != nil {
            continue // 跳过无效的服务
        }
        services = append(services, *service)
    }
    
    return services, nil
}

// Watch 监听服务变化
func (d *RedisDiscovery) Watch(ctx context.Context, serviceName string) (<-chan discovery.ServiceEvent, error) {
    d.mu.Lock()
    defer d.mu.Unlock()
    
    // 创建事件通道
    eventCh := make(chan discovery.ServiceEvent, d.config.WatchBufferSize)
    
    // 存储监听器
    if d.watchers == nil {
        d.watchers = make(map[string]chan discovery.ServiceEvent)
    }
    d.watchers[serviceName] = eventCh
    
    return eventCh, nil
}

// heartbeat 心跳机制
func (d *RedisDiscovery) heartbeat(ctx context.Context, serviceID string) {
    ticker := time.NewTicker(d.config.HeartbeatInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-d.stopCh:
            return
        case <-ticker.C:
            // 更新心跳时间
            serviceKey := d.getServiceKey(serviceID)
            
            // 获取当前服务信息
            data, err := d.client.Get(ctx, serviceKey).Bytes()
            if err != nil {
                if err == redis.Nil {
                    // 服务已过期，停止心跳
                    return
                }
                continue
            }
            
            var service discovery.ServiceInfo
            if err := json.Unmarshal(data, &service); err != nil {
                continue
            }
            
            // 更新心跳时间
            service.LastHeartbeat = time.Now()
            
            // 重新序列化并存储
            newData, err := json.Marshal(service)
            if err != nil {
                continue
            }
            
            // 更新服务信息，重置TTL
            err = d.client.Set(ctx, serviceKey, newData, d.config.ServiceTTL).Err()
            if err != nil {
                continue
            }
        }
    }
}
```

## etcd实现设计

### 配置结构
```go
// pkg/etcd/discovery/config.go
package discovery

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/discovery"
)

// EtcdConfig etcd服务发现配置
type EtcdConfig struct {
    // etcd连接配置
    Endpoints   []string      `json:"endpoints"`
    DialTimeout time.Duration `json:"dial_timeout"`
    Username    string        `json:"username"`
    Password    string        `json:"password"`
    
    // 服务发现配置
    KeyPrefix       string        `json:"key_prefix"`        // 键前缀
    LeaseTTL        time.Duration `json:"lease_ttl"`         // 租约TTL
    WatchBufferSize int           `json:"watch_buffer_size"` // 监听缓冲区大小
    
    // 负载均衡配置
    DefaultLBStrategy discovery.LoadBalancerStrategy `json:"default_lb_strategy"` // 默认负载均衡策略
}
```

### 核心实现
```go
// pkg/etcd/discovery/discovery.go
package discovery

import (
    "context"
    "encoding/json"
    "fmt"
    "path"
    "time"
    
    "github.com/ceyewan/genesis/pkg/common/discovery"
    clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdDiscovery etcd服务发现实现
type EtcdDiscovery struct {
    client        *clientv3.Client
    config        *EtcdConfig
    services      map[string]*discovery.ServiceInfo
    mu            sync.RWMutex
    watchers      map[string]chan discovery.ServiceEvent
    loadBalancers map[string]discovery.LoadBalancer
    leases        map[string]int64 // serviceID -> leaseID
    stopCh        chan struct{}
}

// Register 注册服务
func (d *EtcdDiscovery) Register(ctx context.Context, service discovery.ServiceInfo) error {
    service.RegisterTime = time.Now()
    service.LastHeartbeat = time.Now()
    service.Status = discovery.StatusHealthy
    
    // 创建租约
    lease, err := d.client.Grant(ctx, int64(d.config.LeaseTTL.Seconds()))
    if err != nil {
        return fmt.Errorf("failed to grant lease: %w", err)
    }
    
    // 序列化服务信息
    data, err := json.Marshal(service)
    if err != nil {
        d.client.Revoke(ctx, lease.ID)
        return fmt.Errorf("failed to marshal service info: %w", err)
    }
    
    // 构建键名
    serviceKey := d.getServiceKey(service.ID)
    
    // 存储服务信息
    _, err = d.client.Put(ctx, serviceKey, string(data), clientv3.WithLease(lease.ID))
    if err != nil {
        d.client.Revoke(ctx, lease.ID)
        return fmt.Errorf("failed to register service: %w", err)
    }
    
    // 记录租约ID
    d.mu.Lock()
    d.leases[service.ID] = lease.ID
    d.mu.Unlock()
    
    // 启动续期
    go d.keepAlive(ctx, service.ID, lease.ID)
    
    // 发送注册事件
    d.emitEvent(discovery.ServiceEvent{
        Type:      discovery.EventServiceRegistered,
        Service:   service,
        Timestamp: time.Now(),
    })
    
    return nil
}

// keepAlive 租约续期
func (d *EtcdDiscovery) keepAlive(ctx context.Context, serviceID string, leaseID int64) {
    // 创建续期通道
    keepAliveCh, err := d.client.KeepAlive(ctx, leaseID)
    if err != nil {
        return
    }
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-d.stopCh:
            return
        case ka := <-keepAliveCh:
            if ka == nil {
                // 续期失败，租约已过期
                d.mu.Lock()
                delete(d.leases, serviceID)
                d.mu.Unlock()
                return
            }
        }
    }
}

// Watch 监听服务变化
func (d *EtcdDiscovery) Watch(ctx context.Context, serviceName string) (<-chan discovery.ServiceEvent, error) {
    d.mu.Lock()
    defer d.mu.Unlock()
    
    // 创建事件通道
    eventCh := make(chan discovery.ServiceEvent, d.config.WatchBufferSize)
    
    // 存储监听器
    if d.watchers == nil {
        d.watchers = make(map[string]chan discovery.ServiceEvent)
    }
    d.watchers[serviceName] = eventCh
    
    // 启动etcd监听
    go d.watchEtcd(ctx, serviceName, eventCh)
    
    return eventCh, nil
}

// watchEtcd 监听etcd变化
func (d *EtcdDiscovery) watchEtcd(ctx context.Context, serviceName string, eventCh chan<- discovery.ServiceEvent) {
    // 构建监听前缀
    prefix := path.Join(d.config.KeyPrefix, "services", serviceName)
    
    // 创建监听器
    watchCh := d.client.Watch(ctx, prefix, clientv3.WithPrefix())
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-d.stopCh:
            return
        case resp := <-watchCh:
            if resp.Err() != nil {
                continue
            }
            
            for _, event := range resp.Events {
                var serviceEvent discovery.ServiceEvent
                
                switch event.Type {
                case clientv3.EventTypePut:
                    // 服务注册或更新
                    var service discovery.ServiceInfo
                    if err := json.Unmarshal(event.Kv.Value, &service); err != nil {
                        continue
                    }
                    
                    // 判断是注册还是更新
                    if event.IsCreate() {
                        serviceEvent.Type = discovery.EventServiceRegistered
                    } else {
                        serviceEvent.Type = discovery.EventServiceUpdated
                    }
                    serviceEvent.Service = service
                    
                case clientv3.EventTypeDelete:
                    // 服务注销
                    serviceEvent.Type = discovery.EventServiceDeregistered
                    // 从键名中提取服务信息
                    serviceID := path.Base(string(event.Kv.Key))
                    serviceEvent.Service = discovery.ServiceInfo{ID: serviceID}
                }
                
                serviceEvent.Timestamp = time.Now()
                
                // 发送事件
                select {
                case eventCh <- serviceEvent:
                default:
                    // 通道满，丢弃事件
                }
            }
        }
    }
}
```

## 负载均衡器实现

### 轮询负载均衡器
```go
// pkg/common/discovery/loadbalancer/round_robin.go
package loadbalancer

import (
    "sync"
    "sync/atomic"
    "github.com/ceyewan/genesis/pkg/common/discovery"
)

// RoundRobinLoadBalancer 轮询负载均衡器
type RoundRobinLoadBalancer struct {
    counter uint64
    mu      sync.RWMutex
}

func (lb *RoundRobinLoadBalancer) Select(services []discovery.ServiceInfo) (*discovery.ServiceInfo, error) {
    if len(services) == 0 {
        return nil, fmt.Errorf("no available services")
    }
    
    // 过滤健康的服务
    healthyServices := make([]discovery.ServiceInfo, 0, len(services))
    for _, service := range services {
        if service.Status == discovery.StatusHealthy {
            healthyServices = append(healthyServices, service)
        }
    }
    
    if len(healthyServices) == 0 {
        return nil, fmt.Errorf("no healthy services available")
    }
    
    // 轮询选择
    counter := atomic.AddUint64(&lb.counter, 1)
    index := int(counter % uint64(len(healthyServices)))
    
    return &healthyServices[index], nil
}
```

### 权重负载均衡器
```go
// pkg/common/discovery/loadbalancer/weighted.go
package loadbalancer

import (
    "math/rand"
    "github.com/ceyewan/genesis/pkg/common/discovery"
)

// WeightedLoadBalancer 权重负载均衡器
type WeightedLoadBalancer struct {
    rng *rand.Rand
}

func (lb *WeightedLoadBalancer) Select(services []discovery.ServiceInfo) (*discovery.ServiceInfo, error) {
    if len(services) == 0 {
        return nil, fmt.Errorf("no available services")
    }
    
    // 过滤健康的服务
    healthyServices := make([]discovery.ServiceInfo, 0, len(services))
    totalWeight := 0
    
    for _, service := range services {
        if service.Status == discovery.StatusHealthy {
            healthyServices = append(healthyServices, service)
            totalWeight += service.Weight
        }
    }
    
    if len(healthyServices) == 0 {
        return nil, fmt.Errorf("no healthy services available")
    }
    
    if totalWeight == 0 {
        // 如果所有权重的为0，使用随机选择
        index := lb.rng.Intn(len(healthyServices))
        return &healthyServices[index], nil
    }
    
    // 根据权重随机选择
    randomWeight := lb.rng.Intn(totalWeight)
    currentWeight := 0
    
    for _, service := range healthyServices {
        currentWeight += service.Weight
        if randomWeight < currentWeight {
            return &service, nil
        }
    }
    
    // 不应该到达这里
    return &healthyServices[len(healthyServices)-1], nil
}
```

这个服务发现与注册设计提供了完整的抽象接口和丰富的功能特性，支持健康检查、负载均衡、事件监听等高级功能，同时提供了Redis和etcd两种实现方案。