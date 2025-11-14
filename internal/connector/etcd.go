// internal/connector/manager.go
package connector

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// ConnectionConfig 连接配置（仅连接相关，与业务无关）
type ConnectionConfig struct {
	Backend   string        // 后端类型: etcd, redis
	Endpoints []string      // 连接地址
	Username  string        // 认证用户（可选）
	Password  string        // 认证密码（可选）
	Timeout   time.Duration // 连接超时（可选，默认5s）
}

// Manager 连接管理器（支持配置哈希复用）
type Manager struct {
	etcdClients map[string]*clientv3.Client // 配置哈希 -> etcd客户端
	configs     map[string]ConnectionConfig // 配置哈希 -> 配置
	mu          sync.RWMutex
}

var (
	globalManager *Manager
	managerOnce   sync.Once
)

// GetManager 获取全局连接管理器（单例）
func GetManager() *Manager {
	managerOnce.Do(func() {
		globalManager = &Manager{
			etcdClients: make(map[string]*clientv3.Client),
			configs:     make(map[string]ConnectionConfig),
		}
	})
	return globalManager
}

// GetEtcdClient 根据配置获取etcd客户端（自动复用）
func (m *Manager) GetEtcdClient(config ConnectionConfig) (*clientv3.Client, error) {
	// 应用默认值
	if config.Backend == "" {
		config.Backend = "etcd"
	}
	if len(config.Endpoints) == 0 {
		config.Endpoints = []string{"127.0.0.1:2379"}
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	// 计算配置哈希
	configHash := m.hashConfig(config)

	// 检查是否已有相同配置的客户端
	m.mu.RLock()
	if client, exists := m.etcdClients[configHash]; exists {
		m.mu.RUnlock()
		return client, nil
	}
	m.mu.RUnlock()

	// 创建新客户端
	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查，避免并发重复创建
	if client, exists := m.etcdClients[configHash]; exists {
		return client, nil
	}

	// 创建新客户端
	client, err := m.createEtcdClient(config)
	if err != nil {
		return nil, err
	}

	// 缓存客户端
	m.etcdClients[configHash] = client
	m.configs[configHash] = config

	return client, nil
}

// hashConfig 计算配置哈希（用于连接复用判断）
func (m *Manager) hashConfig(config ConnectionConfig) string {
	h := sha256.New()

	// 关键配置字段参与哈希
	h.Write([]byte(config.Backend))
	for _, endpoint := range config.Endpoints {
		h.Write([]byte(endpoint))
	}
	h.Write([]byte(config.Username))
	h.Write([]byte(config.Password))
	h.Write([]byte(config.Timeout.String()))

	return hex.EncodeToString(h.Sum(nil))
}

// createEtcdClient 创建etcd客户端
func (m *Manager) createEtcdClient(config ConnectionConfig) (*clientv3.Client, error) {
	clientConfig := clientv3.Config{
		Endpoints:   config.Endpoints,
		Username:    config.Username,
		Password:    config.Password,
		DialTimeout: config.Timeout,
	}

	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return client, nil
}
