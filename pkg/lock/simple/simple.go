package simple

import (
	"fmt"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	internallock "github.com/ceyewan/genesis/internal/lock"
	"github.com/ceyewan/genesis/pkg/lock"
)

// Locker 分布式锁接口
type Locker interface {
	lock.Locker
}

// New 创建分布式锁（Go规范：config必需，option可选）
func New(config *Config, option *Option) (Locker, error) {
	// 处理nil参数
	if config == nil {
		config = DefaultConfig()
	}
	if option == nil {
		option = DefaultOption()
	}

	// 验证必需参数
	if config.Backend == "" {
		return nil, fmt.Errorf("backend is required")
	}
	if len(config.Endpoints) == 0 {
		return nil, fmt.Errorf("endpoints is required")
	}

	// 根据后端类型创建相应实例
	switch config.Backend {
	case "etcd":
		return newEtcdLocker(config, option)
	case "redis":
		return newRedisLocker(config, option)
	default:
		return nil, fmt.Errorf("unsupported backend: %s", config.Backend)
	}
}

func newEtcdLocker(config *Config, option *Option) (Locker, error) {
	// 转换到etcd连接配置（移除多余的Backend字段）
	connConfig := connector.EtcdConfig{
		Endpoints: config.Endpoints,
		Username:  config.Username,
		Password:  config.Password,
		Timeout:   config.Timeout,
	}

	// 应用默认值
	if connConfig.Timeout == 0 {
		connConfig.Timeout = 5 * time.Second
	}

	// 使用连接管理器获取复用连接
	manager := connector.GetEtcdManager()
	client, err := manager.GetEtcdClient(connConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	// 创建锁选项
	opts := &lock.LockOptions{
		TTL:           option.TTL,
		RetryInterval: option.RetryInterval,
		AutoRenew:     option.AutoRenew,
	}

	// 创建etcd锁（复用现有内部实现）
	return internallock.NewEtcdLockerWithClient(client, opts)
}

func newRedisLocker(config *Config, option *Option) (Locker, error) {
	// TODO: 实现Redis锁支持
	return nil, fmt.Errorf("redis backend not implemented yet")
}
