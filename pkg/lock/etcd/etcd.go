package etcd

import (
	"fmt"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	internallock "github.com/ceyewan/genesis/internal/lock"
	lockpkg "github.com/ceyewan/genesis/pkg/lock"
)

// Config carries the etcd connection details exposed through the public API.
type Config struct {
	Endpoints   []string
	Username    string
	Password    string
	DialTimeout time.Duration
}

// New wires the public etcd configuration to the internal implementation and
// returns a distributed locker that only exposes the lockpkg.Locker interface.
//
// External callers stay decoupled from internal packages, so switching the
// backend (for example to Redis) remains transparent to them.
func New(cfg *Config, opts *lockpkg.LockOptions) (lockpkg.Locker, error) {
	if cfg == nil {
		return nil, fmt.Errorf("etcd config is nil")
	}

	// 转换到新的连接配置
	connConfig := connector.ConnectionConfig{
		Backend:   "etcd",
		Endpoints: cfg.Endpoints,
		Username:  cfg.Username,
		Password:  cfg.Password,
		Timeout:   cfg.DialTimeout,
	}

	// 使用连接管理器获取客户端
	manager := connector.GetManager()
	client, err := manager.GetEtcdClient(connConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	// 使用内部实现创建锁
	locker, err := internallock.NewEtcdLockerWithClient(client, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd locker: %w", err)
	}

	return locker, nil
}
