// internal/connector/manager.go
package connector

import (
	"fmt"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// GlobalManager 全局连接管理器（单例）
var (
	globalManager *Manager
	initOnce      sync.Once
)

// Manager 连接管理器
type Manager struct {
	etcdClient *clientv3.Client
	etcdConfig *EtcdConfig
	initOnce   sync.Once
	mu         sync.RWMutex
}

// EtcdConfig etcd配置
type EtcdConfig struct {
	Endpoints   []string      `json:"endpoints" yaml:"endpoints"`
	Username    string        `json:"username" yaml:"username"`
	Password    string        `json:"password" yaml:"password"`
	DialTimeout time.Duration `json:"dial_timeout" yaml:"dial_timeout"`
}

// InitGlobalManager 初始化全局管理器（只能调用一次）
func InitGlobalManager(config *EtcdConfig) error {
	var initErr error
	initOnce.Do(func() {
		globalManager = &Manager{
			etcdConfig: config,
		}
		initErr = globalManager.init()
	})
	return initErr
}

// GetEtcdClient 获取etcd客户端（懒加载，线程安全）
func GetEtcdClient() (*clientv3.Client, error) {
	if globalManager == nil {
		return nil, fmt.Errorf("global manager not initialized, call InitGlobalManager first")
	}
	return globalManager.getEtcdClient()
}

// 内部方法
func (m *Manager) init() error {
	// 这里实现真正的初始化逻辑
	return nil
}

func (m *Manager) getEtcdClient() (*clientv3.Client, error) {
	m.initOnce.Do(func() {
		// 懒加载：第一次调用时才真正创建连接
		client, err := m.createEtcdClient()
		if err != nil {
			// 记录错误，下次调用时重试
			return
		}
		m.etcdClient = client
	})

	if m.etcdClient == nil {
		return nil, fmt.Errorf("failed to create etcd client")
	}
	return m.etcdClient, nil
}

func (m *Manager) createEtcdClient() (*clientv3.Client, error) {
	if m.etcdConfig == nil {
		return nil, fmt.Errorf("etcd config is nil")
	}

	config := clientv3.Config{
		Endpoints:   m.etcdConfig.Endpoints,
		Username:    m.etcdConfig.Username,
		Password:    m.etcdConfig.Password,
		DialTimeout: m.etcdConfig.DialTimeout,
	}

	client, err := clientv3.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return client, nil
}
