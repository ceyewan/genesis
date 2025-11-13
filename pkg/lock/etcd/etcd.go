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

	internalCfg := &connector.EtcdConfig{
		Endpoints:   cfg.Endpoints,
		Username:    cfg.Username,
		Password:    cfg.Password,
		DialTimeout: cfg.DialTimeout,
	}

	if err := connector.InitGlobalManager(internalCfg); err != nil {
		return nil, fmt.Errorf("failed to init etcd manager: %w", err)
	}

	locker, err := internallock.NewEtcdLocker(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd locker: %w", err)
	}

	return locker, nil
}
