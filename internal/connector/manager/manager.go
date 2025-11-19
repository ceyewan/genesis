// internal/connector/manager/manager.go
package manager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
)

// ManagedInstance 管理的连接器实例
type ManagedInstance[T connector.Connector] struct {
	instance  T
	refCount  int32
	createdAt time.Time
	lastCheck time.Time
	healthy   bool
}

// Manager 负责管理某一类连接器的所有实例
type Manager[T connector.Connector] struct {
	mu            sync.RWMutex
	instances     map[string]*ManagedInstance[T] // key 为配置的 hash
	factory       func(config any) (T, error)    // 创建具体连接器的工厂函数
	healthChecker *time.Ticker
	stopChan      chan struct{}
	checkInterval time.Duration
	maxInstances  int
}

// ManagerOptions 管理器配置选项
type ManagerOptions struct {
	CheckInterval time.Duration // 健康检查间隔，0 表示不检查
	MaxInstances  int           // 最大实例数，0 表示无限制
}

// NewManager 创建新的连接管理器
func NewManager[T connector.Connector](factory func(config any) (T, error), opts ManagerOptions) *Manager[T] {
	m := &Manager[T]{
		instances:     make(map[string]*ManagedInstance[T]),
		factory:       factory,
		stopChan:      make(chan struct{}),
		checkInterval: opts.CheckInterval,
		maxInstances:  opts.MaxInstances,
	}

	// 启动健康检查
	if opts.CheckInterval > 0 {
		m.startHealthCheck()
	}

	return m
}

// Get 获取或创建连接器实例
func (m *Manager[T]) Get(config any) (T, error) {
	// 计算配置哈希
	configHash := m.hashConfig(config)

	// 尝试复用现有实例
	m.mu.RLock()
	if instance, exists := m.instances[configHash]; exists && instance.healthy {
		instance.refCount++
		m.mu.RUnlock()
		return instance.instance, nil
	}
	m.mu.RUnlock()

	// 创建新实例
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if instance, exists := m.instances[configHash]; exists && instance.healthy {
		instance.refCount++
		return instance.instance, nil
	}

	// 检查实例数量限制
	if m.maxInstances > 0 && len(m.instances) >= m.maxInstances {
		var zero T
		return zero, fmt.Errorf("达到最大实例数限制: %d", m.maxInstances)
	}

	// 调用工厂函数创建新实例
	instance, err := m.factory(config)
	if err != nil {
		var zero T
		return zero, err
	}

	// 建立连接
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := instance.Connect(ctx); err != nil {
		var zero T
		return zero, err
	}

	// 存储实例
	m.instances[configHash] = &ManagedInstance[T]{
		instance:  instance,
		refCount:  1,
		createdAt: time.Now(),
		lastCheck: time.Now(),
		healthy:   true,
	}

	return instance, nil
}

// Release 释放连接器实例引用
func (m *Manager[T]) Release(config any) error {
	configHash := m.hashConfig(config)

	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[configHash]
	if !exists {
		return fmt.Errorf("实例不存在")
	}

	instance.refCount--
	if instance.refCount <= 0 {
		// 引用计数归零，关闭实例
		if err := instance.instance.Close(); err != nil {
			return err
		}
		delete(m.instances, configHash)
	}

	return nil
}

// Close 关闭所有实例
func (m *Manager[T]) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 停止健康检查
	if m.healthChecker != nil {
		m.healthChecker.Stop()
		if m.stopChan != nil {
			select {
			case <-m.stopChan:
				// 通道已关闭，不需要再次关闭
			default:
				close(m.stopChan)
			}
		}
	}

	var lastErr error
	for hash, instance := range m.instances {
		if err := instance.instance.Close(); err != nil {
			lastErr = err
		}
		delete(m.instances, hash)
	}

	return lastErr
}

// startHealthCheck 启动健康检查
func (m *Manager[T]) startHealthCheck() {
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
func (m *Manager[T]) performHealthCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	for _, instance := range m.instances {
		// 跳过最近检查过的
		if now.Sub(instance.lastCheck) < m.checkInterval {
			continue
		}

		if err := instance.instance.HealthCheck(ctx); err != nil {
			instance.healthy = false
		} else {
			instance.healthy = true
			instance.lastCheck = now
		}
	}
}

// hashConfig 计算配置哈希
func (m *Manager[T]) hashConfig(config any) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%+v", config)))
	return hex.EncodeToString(h.Sum(nil))
}

// GetStats 获取管理器统计信息
func (m *Manager[T]) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"total_instances": len(m.instances),
		"max_instances":   m.maxInstances,
		"instances":       []map[string]interface{}{},
	}

	for hash, instance := range m.instances {
		instanceInfo := map[string]interface{}{
			"hash":       hash[:8],
			"ref_count":  instance.refCount,
			"created_at": instance.createdAt,
			"last_check": instance.lastCheck,
			"healthy":    instance.healthy,
		}
		stats["instances"] = append(stats["instances"].([]map[string]interface{}), instanceInfo)
	}

	return stats
}
