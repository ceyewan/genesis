# EtcdManager 实现问题审查

## 存在的问题

### 1. **缺少连接健康检查机制** ⚠️

**问题：**
```go
func (m *EtcdManager) GetEtcdClient(config EtcdConfig) (*clientv3.Client, error) {
    if client, exists := m.clients[configHash]; exists {
        return client, nil  // 直接返回，没有检查连接是否还有效
    }
}
```

**后果：**
- 缓存的客户端可能已经断开连接或过期
- 返回无效的客户端会导致后续操作失败
- 没有自动重连机制

### 2. **缺少连接生命周期管理** 💀

**问题：**
- 没有 `Close()` 方法来关闭所有连接
- 程序退出时 etcd 连接不会被正确关闭
- 可能导致 etcd 服务端资源泄漏

### 3. **缺少连接引用计数** 📊

**问题：**
```go
m.clients[configHash] = client  // 多个地方可能使用同一个客户端
```

**后果：**
- 不知道有多少地方在使用某个客户端
- 无法安全地关闭不再使用的连接
- 可能过早关闭仍在使用的连接

### 4. **配置哈希计算不够精确** 🔍

**问题：**
```go
func (m *EtcdManager) hashConfig(config EtcdConfig) string {
    for _, endpoint := range config.Endpoints {
        h.Write([]byte(endpoint))  // 顺序不同会导致不同的哈希
    }
}
```

**后果：**
- `["127.0.0.1:2379", "127.0.0.2:2379"]` 和 `["127.0.0.2:2379", "127.0.0.1:2379"]` 会产生不同哈希
- 实际上这两个配置应该被视为相同（etcd 集群）
- 导致不必要的连接创建

### 5. **缺少错误处理和日志记录** 📝

**问题：**
- 创建客户端失败时没有日志记录
- 无法追踪连接的创建和销毁
- 调试困难

### 6. **缺少连接池配置选项** ⚙️

**问题：**
- 没有最大连接数限制
- 没有连接超时配置
- 没有空闲连接清理机制

### 7. **并发场景下的潜在问题** 🔒

**问题：**
```go
m.mu.RLock()
if client, exists := m.clients[configHash]; exists {
    m.mu.RUnlock()
    return client, nil  // 返回后，其他 goroutine 可能立即关闭这个客户端
}
```

**后果：**
- 如果添加了 `RemoveClient` 方法，可能出现竞态条件
- 返回的客户端可能在使用前就被关闭

### 8. **缺少连接测试功能** 🧪

**问题：**
- 创建客户端后没有验证连接是否真的可用
- 可能返回一个配置正确但网络不通的客户端

## 改进建议

### 完整的修复版本：

```go
package connector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdConfig etcd连接配置
type EtcdConfig struct {
	Endpoints        []string      // 连接地址
	Username         string        // 认证用户（可选）
	Password         string        // 认证密码（可选）
	Timeout          time.Duration // 连接超时（可选，默认5s）
	KeepAliveTime    time.Duration // 心跳间隔（可选，默认10s）
	KeepAliveTimeout time.Duration // 心跳超时（可选，默认3s）
}

// clientEntry 客户端条目
type clientEntry struct {
	client    *clientv3.Client
	config    EtcdConfig
	refCount  int       // 引用计数
	createdAt time.Time // 创建时间
	lastCheck time.Time // 最后健康检查时间
}

// EtcdManager etcd连接管理器
type EtcdManager struct {
	clients       map[string]*clientEntry // 配置哈希 -> 客户端条目
	mu            sync.RWMutex
	healthChecker *time.Ticker // 健康检查定时器
	stopChan      chan struct{}
	maxClients    int           // 最大连接数
	checkInterval time.Duration // 健康检查间隔
}

var (
	globalEtcdManager *EtcdManager
	etcdManagerOnce   sync.Once
)

// ManagerOptions 管理器配置选项
type ManagerOptions struct {
	MaxClients    int           // 最大连接数，0表示无限制
	CheckInterval time.Duration // 健康检查间隔，0表示不检查
}

// GetEtcdManager 获取全局etcd连接管理器（单例）
func GetEtcdManager() *EtcdManager {
	return GetEtcdManagerWithOptions(ManagerOptions{
		MaxClients:    10,
		CheckInterval: 30 * time.Second,
	})
}

// GetEtcdManagerWithOptions 使用自定义选项获取管理器
func GetEtcdManagerWithOptions(opts ManagerOptions) *EtcdManager {
	etcdManagerOnce.Do(func() {
		globalEtcdManager = &EtcdManager{
			clients:       make(map[string]*clientEntry),
			stopChan:      make(chan struct{}),
			maxClients:    opts.MaxClients,
			checkInterval: opts.CheckInterval,
		}

		// 启动健康检查
		if opts.CheckInterval > 0 {
			globalEtcdManager.startHealthCheck()
		}
	})
	return globalEtcdManager
}

// GetEtcdClient 根据配置获取etcd客户端（自动复用）
func (m *EtcdManager) GetEtcdClient(config EtcdConfig) (*clientv3.Client, error) {
	// 应用默认值
	m.applyDefaults(&config)

	// 计算配置哈希
	configHash := m.hashConfig(config)

	// 检查是否已有相同配置的客户端
	m.mu.RLock()
	if entry, exists := m.clients[configHash]; exists {
		// 检查客户端健康状态
		if m.isClientHealthy(entry.client) {
			entry.refCount++
			m.mu.RUnlock()
			log.Printf("[EtcdManager] Reusing existing client, hash=%s, refCount=%d", configHash[:8], entry.refCount)
			return entry.client, nil
		}
		// 客户端不健康，需要重建
		log.Printf("[EtcdManager] Client unhealthy, will recreate, hash=%s", configHash[:8])
	}
	m.mu.RUnlock()

	// 创建新客户端
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if entry, exists := m.clients[configHash]; exists {
		if m.isClientHealthy(entry.client) {
			entry.refCount++
			return entry.client, nil
		}
		// 清理不健康的客户端
		m.closeClientUnsafe(configHash)
	}

	// 检查连接数限制
	if m.maxClients > 0 && len(m.clients) >= m.maxClients {
		return nil, fmt.Errorf("max clients limit reached: %d", m.maxClients)
	}

	// 创建新客户端
	client, err := m.createEtcdClient(config)
	if err != nil {
		return nil, err
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()
	if _, err := client.Status(ctx, config.Endpoints[0]); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to verify connection: %w", err)
	}

	// 缓存客户端
	m.clients[configHash] = &clientEntry{
		client:    client,
		config:    config,
		refCount:  1,
		createdAt: time.Now(),
		lastCheck: time.Now(),
	}

	log.Printf("[EtcdManager] Created new client, hash=%s, endpoints=%v", configHash[:8], config.Endpoints)
	return client, nil
}

// ReleaseClient 释放客户端引用
func (m *EtcdManager) ReleaseClient(client *clientv3.Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 查找客户端
	for hash, entry := range m.clients {
		if entry.client == client {
			entry.refCount--
			log.Printf("[EtcdManager] Released client, hash=%s, refCount=%d", hash[:8], entry.refCount)

			// 如果引用计数为0，可以选择立即关闭或延迟关闭
			if entry.refCount <= 0 {
				return m.closeClientUnsafe(hash)
			}
			return nil
		}
	}

	return fmt.Errorf("client not found in manager")
}

// closeClientUnsafe 关闭客户端（不加锁，内部使用）
func (m *EtcdManager) closeClientUnsafe(hash string) error {
	entry, exists := m.clients[hash]
	if !exists {
		return nil
	}

	log.Printf("[EtcdManager] Closing client, hash=%s", hash[:8])
	err := entry.client.Close()
	delete(m.clients, hash)
	return err
}

// isClientHealthy 检查客户端健康状态
func (m *EtcdManager) isClientHealthy(client *clientv3.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 尝试获取集群状态
	_, err := client.Status(ctx, client.Endpoints()[0])
	return err == nil
}

// startHealthCheck 启动健康检查
func (m *EtcdManager) startHealthCheck() {
	m.healthChecker = time.NewTicker(m.checkInterval)
	go func() {
		for {
			select {
			case <-m.healthChecker.C:
				m.performHealthCheck()
			case <-m.stopChan:
				return
			}
		}
	}()
}

// performHealthCheck 执行健康检查
func (m *EtcdManager) performHealthCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for hash, entry := range m.clients {
		// 跳过最近检查过的
		if now.Sub(entry.lastCheck) < m.checkInterval {
			continue
		}

		if !m.isClientHealthy(entry.client) {
			log.Printf("[EtcdManager] Health check failed, hash=%s, will recreate on next use", hash[:8])
			// 标记为不健康，下次使用时会重建
			entry.lastCheck = time.Time{} // 设置为零值表示不健康
		} else {
			entry.lastCheck = now
		}
	}
}

// applyDefaults 应用默认配置
func (m *EtcdManager) applyDefaults(config *EtcdConfig) {
	if len(config.Endpoints) == 0 {
		config.Endpoints = []string{"127.0.0.1:2379"}
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.KeepAliveTime == 0 {
		config.KeepAliveTime = 10 * time.Second
	}
	if config.KeepAliveTimeout == 0 {
		config.KeepAliveTimeout = 3 * time.Second
	}
}

// hashConfig 计算配置哈希（用于连接复用判断）
func (m *EtcdManager) hashConfig(config EtcdConfig) string {
	h := sha256.New()

	// 对 endpoints 排序后再哈希，确保顺序不影响结果
	sortedEndpoints := make([]string, len(config.Endpoints))
	copy(sortedEndpoints, config.Endpoints)
	sort.Strings(sortedEndpoints)

	for _, endpoint := range sortedEndpoints {
		h.Write([]byte(endpoint))
		h.Write([]byte("|")) // 分隔符
	}
	h.Write([]byte(config.Username))
	h.Write([]byte("|"))
	h.Write([]byte(config.Password))
	h.Write([]byte("|"))
	h.Write([]byte(config.Timeout.String()))

	return hex.EncodeToString(h.Sum(nil))
}

// createEtcdClient 创建etcd客户端
func (m *EtcdManager) createEtcdClient(config EtcdConfig) (*clientv3.Client, error) {
	clientConfig := clientv3.Config{
		Endpoints:            config.Endpoints,
		Username:             config.Username,
		Password:             config.Password,
		DialTimeout:          config.Timeout,
		DialKeepAliveTime:    config.KeepAliveTime,
		DialKeepAliveTimeout: config.KeepAliveTimeout,
	}

	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return client, nil
}

// Close 关闭所有连接
func (m *EtcdManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 停止健康检查
	if m.healthChecker != nil {
		m.healthChecker.Stop()
		close(m.stopChan)
	}

	var lastErr error
	for hash, entry := range m.clients {
		log.Printf("[EtcdManager] Closing client on shutdown, hash=%s, refCount=%d", hash[:8], entry.refCount)
		if err := entry.client.Close(); err != nil {
			lastErr = err
			log.Printf("[EtcdManager] Error closing client: %v", err)
		}
	}

	m.clients = make(map[string]*clientEntry)
	return lastErr
}

// GetStats 获取管理器统计信息
func (m *EtcdManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"total_clients": len(m.clients),
		"max_clients":   m.maxClients,
		"clients":       []map[string]interface{}{},
	}

	for hash, entry := range m.clients {
		clientInfo := map[string]interface{}{
			"hash":       hash[:8],
			"endpoints":  entry.config.Endpoints,
			"ref_count":  entry.refCount,
			"created_at": entry.createdAt,
			"last_check": entry.lastCheck,
		}
		stats["clients"] = append(stats["clients"].([]map[string]interface{}), clientInfo)
	}

	return stats
}
```

## 关键改进点总结

1. ✅ **添加健康检查机制**：定期检查连接状态，自动重建不健康的连接
2. ✅ **引用计数管理**：跟踪每个客户端的使用情况，支持安全释放
3. ✅ **生命周期管理**：提供 `Close()` 方法正确关闭所有连接
4. ✅ **配置哈希优化**：对 endpoints 排序，避免顺序导致的重复连接
5. ✅ **连接验证**：创建后立即测试连接可用性
6. ✅ **日志记录**：完整的操作日志，便于调试
7. ✅ **连接数限制**：防止连接数过多
8. ✅ **统计信息**：提供 `GetStats()` 方法查看管理器状态