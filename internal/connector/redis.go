// internal/connector/redis.go
package connector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig Redis连接配置
type RedisConfig struct {
	Addr         string        // 连接地址，如 "127.0.0.1:6379"
	Password     string        // 认证密码（可选）
	DB           int           // 数据库编号（默认0）
	PoolSize     int           // 连接池大小（默认10）
	MinIdleConns int           // 最小空闲连接数（默认5）
	MaxRetries   int           // 最大重试次数（默认3）
	DialTimeout  time.Duration // 连接超时（默认5s）
	ReadTimeout  time.Duration // 读取超时（默认3s）
	WriteTimeout time.Duration // 写入超时（默认3s）
}

// Redis 的 DB 可以理解成类似于关系型数据库中的“数据库”或“Schema”，
// 它用于在同一个 Redis 服务实例中，逻辑隔离不同应用或模块的数据。
// Redis 默认提供多个编号的数据库，通常是 16 个，编号从 0 到 15。

// redisEntry Redis客户端条目
type redisEntry struct {
	client    *redis.Client
	config    RedisConfig
	refCount  int       // 引用计数
	createdAt time.Time // 创建时间
	lastCheck time.Time // 最后健康检查时间
}

// RedisManager Redis连接管理器
type RedisManager struct {
	clients       map[string]*redisEntry // 配置哈希 -> 客户端条目
	mu            sync.RWMutex
	healthChecker *time.Ticker  // 健康检查定时器
	stopChan      chan struct{} // 停止信号
	maxClients    int           // 最大连接数
	checkInterval time.Duration // 健康检查间隔
}

var (
	globalRedisManager *RedisManager
	redisManagerOnce   sync.Once
)

// RedisManagerOptions Redis管理器配置选项
type RedisManagerOptions struct {
	MaxClients    int           // 最大连接数，0表示无限制
	CheckInterval time.Duration // 健康检查间隔，0表示不检查
}

// GetRedisManager 获取全局Redis连接管理器（单例，带默认配置）
func GetRedisManager() *RedisManager {
	return GetRedisManagerWithOptions(RedisManagerOptions{
		MaxClients:    10,
		CheckInterval: 30 * time.Second,
	})
}

// GetRedisManagerWithOptions 使用自定义选项获取Redis管理器
func GetRedisManagerWithOptions(opts RedisManagerOptions) *RedisManager {
	redisManagerOnce.Do(func() {
		globalRedisManager = &RedisManager{
			clients:       make(map[string]*redisEntry),
			stopChan:      make(chan struct{}),
			maxClients:    opts.MaxClients,
			checkInterval: opts.CheckInterval,
		}

		// 启动健康检查
		if opts.CheckInterval > 0 {
			globalRedisManager.startHealthCheck()
		}
	})
	return globalRedisManager
}

// GetRedisClient 根据配置获取Redis客户端（自动复用）
func (m *RedisManager) GetRedisClient(config RedisConfig) (*redis.Client, error) {
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
			log.Printf("[RedisManager] 复用现有客户端, hash=%s, 引用计数=%d", configHash[:8], entry.refCount)
			return entry.client, nil
		}
		// 客户端不健康，需要重建
		log.Printf("[RedisManager] 客户端不健康，将重新创建, hash=%s", configHash[:8])
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
		return nil, fmt.Errorf("达到最大连接数限制: %d", m.maxClients)
	}

	// 创建新客户端
	client, err := m.createRedisClient(config)
	if err != nil {
		return nil, err
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), config.DialTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("连接测试失败: %w", err)
	}

	// 缓存客户端
	m.clients[configHash] = &redisEntry{
		client:    client,
		config:    config,
		refCount:  1,
		createdAt: time.Now(),
		lastCheck: time.Now(),
	}

	log.Printf("[RedisManager] 创建新客户端, hash=%s, 地址=%s", configHash[:8], config.Addr)
	return client, nil
}

// ReleaseClient 释放客户端引用
func (m *RedisManager) ReleaseClient(client *redis.Client) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 查找客户端
	for hash, entry := range m.clients {
		if entry.client == client {
			entry.refCount--
			log.Printf("[RedisManager] 释放客户端引用, hash=%s, 引用计数=%d", hash[:8], entry.refCount)

			// 如果引用计数为0，关闭连接
			if entry.refCount <= 0 {
				return m.closeClientUnsafe(hash)
			}
			return nil
		}
	}

	return fmt.Errorf("在管理器中未找到客户端")
}

// closeClientUnsafe 关闭客户端（不加锁，内部使用）
func (m *RedisManager) closeClientUnsafe(hash string) error {
	entry, exists := m.clients[hash]
	if !exists {
		return nil
	}

	log.Printf("[RedisManager] 关闭客户端, hash=%s", hash[:8])
	err := entry.client.Close()
	delete(m.clients, hash)
	return err
}

// isClientHealthy 检查客户端健康状态
func (m *RedisManager) isClientHealthy(client *redis.Client) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 尝试执行PING命令
	return client.Ping(ctx).Err() == nil
}

// startHealthCheck 启动健康检查
func (m *RedisManager) startHealthCheck() {
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
func (m *RedisManager) performHealthCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for hash, entry := range m.clients {
		// 跳过最近检查过的
		if now.Sub(entry.lastCheck) < m.checkInterval {
			continue
		}

		if !m.isClientHealthy(entry.client) {
			log.Printf("[RedisManager] 健康检查失败, hash=%s, 下次使用时会重新创建", hash[:8])
			// 标记为不健康，下次使用时会重建
			entry.lastCheck = time.Time{} // 设置为零值表示不健康
		} else {
			entry.lastCheck = now
		}
	}
}

// applyDefaults 应用默认配置
func (m *RedisManager) applyDefaults(config *RedisConfig) {
	if config.Addr == "" {
		config.Addr = "127.0.0.1:6379"
	}
	if config.DB < 0 {
		config.DB = 0
	}
	if config.PoolSize <= 0 {
		config.PoolSize = 10
	}
	if config.MinIdleConns < 0 {
		config.MinIdleConns = 5
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 3
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = 5 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 3 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 3 * time.Second
	}
}

// hashConfig 计算配置哈希（用于连接复用判断）
func (m *RedisManager) hashConfig(config RedisConfig) string {
	h := sha256.New()

	// 地址
	h.Write([]byte(config.Addr))
	h.Write([]byte("|"))

	// 数据库
	h.Write([]byte(fmt.Sprintf("%d", config.DB)))
	h.Write([]byte("|"))

	// 密码
	h.Write([]byte(config.Password))
	h.Write([]byte("|"))

	// 连接池配置
	h.Write([]byte(fmt.Sprintf("%d", config.PoolSize)))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", config.MinIdleConns)))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", config.MaxRetries)))
	h.Write([]byte("|"))

	// 超时配置
	h.Write([]byte(config.DialTimeout.String()))
	h.Write([]byte("|"))
	h.Write([]byte(config.ReadTimeout.String()))
	h.Write([]byte("|"))
	h.Write([]byte(config.WriteTimeout.String()))

	return hex.EncodeToString(h.Sum(nil))
}

// createRedisClient 创建Redis客户端
func (m *RedisManager) createRedisClient(config RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	})

	return client, nil
}

// Close 关闭所有连接（生命周期管理）
func (m *RedisManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 停止健康检查
	if m.healthChecker != nil {
		m.healthChecker.Stop()
		close(m.stopChan)
	}

	var lastErr error
	for hash, entry := range m.clients {
		log.Printf("[RedisManager] 关闭客户端（程序退出）, hash=%s, 引用计数=%d", hash[:8], entry.refCount)
		if err := entry.client.Close(); err != nil {
			lastErr = err
			log.Printf("[RedisManager] 关闭客户端错误: %v", err)
		}
	}

	m.clients = make(map[string]*redisEntry)
	return lastErr
}

// GetStats 获取管理器统计信息
func (m *RedisManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"总客户端数": len(m.clients),
		"最大连接数": m.maxClients,
		"客户端列表": []map[string]interface{}{},
	}

	for hash, entry := range m.clients {
		clientInfo := map[string]interface{}{
			"哈希":   hash[:8],
			"地址":   entry.config.Addr,
			"数据库":  entry.config.DB,
			"引用计数": entry.refCount,
			"创建时间": entry.createdAt,
			"最后检查": entry.lastCheck,
		}
		stats["客户端列表"] = append(stats["客户端列表"].([]map[string]interface{}), clientInfo)
	}

	return stats
}
