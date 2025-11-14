package etcd

import (
	"fmt"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	internallock "github.com/ceyewan/genesis/internal/lock"
	lockpkg "github.com/ceyewan/genesis/pkg/lock"
)

// Config 对外暴露的 etcd 连接配置
// 包含建立 etcd 连接所需的所有参数
type Config struct {
	Endpoints        []string      // etcd 集群节点地址列表，如 ["127.0.0.1:2379", "127.0.0.2:2379"]
	Username         string        // 认证用户名（可选，如果 etcd 启用了认证）
	Password         string        // 认证密码（可选，如果 etcd 启用了认证）
	DialTimeout      time.Duration // 连接超时时间（可选，默认 5s）
	KeepAliveTime    time.Duration // 心跳间隔时间（可选，默认 10s）
	KeepAliveTimeout time.Duration // 心跳超时时间（可选，默认 3s）
}

// New 创建 etcd 分布式锁实例
//
// 功能说明：
//   - 将公开的 etcd 配置转换为内部连接器配置
//   - 通过连接管理器获取复用的 etcd 客户端
//   - 使用内部锁实现创建分布式锁
//   - 返回的锁实例实现了 lockpkg.Locker 接口
//
// 参数说明：
//   - cfg: etcd 连接配置，不能为 nil
//   - opts: 锁行为配置（TTL、重试间隔等），可为 nil（使用默认值）
//
// 返回值：
//   - 成功：返回实现了 Locker 接口的分布式锁实例
//   - 失败：返回错误，可能原因包括配置错误、连接失败等
//
// 使用示例：
//   locker, err := etcd.New(&etcd.Config{
//       Endpoints: []string{"127.0.0.1:2379"},
//       Username:  "user",
//       Password:  "pass",
//   }, nil)
//
// 架构优势：
//   - 外部调用者与内部实现解耦，便于后续切换后端（如 Redis）
//   - 自动连接复用，相同配置共享同一个 etcd 客户端
//   - 支持连接健康检查和自动重连
func New(cfg *Config, opts *lockpkg.LockOptions) (lockpkg.Locker, error) {
	// 参数校验：配置不能为空
	if cfg == nil {
		return nil, fmt.Errorf("etcd 配置不能为空")
	}

	// 将公开配置转换为内部连接器配置
	// 注意：这里会应用默认值，所以即使 cfg 中的某些字段为空也能正常工作
	connConfig := connector.EtcdConfig{
		Endpoints:        cfg.Endpoints,
		Username:         cfg.Username,
		Password:         cfg.Password,
		Timeout:          cfg.DialTimeout,
		KeepAliveTime:    cfg.KeepAliveTime,
		KeepAliveTimeout: cfg.KeepAliveTimeout,
	}

	// 获取全局 etcd 连接管理器（单例）
	// 管理器会负责连接的复用、健康检查和生命周期管理
	manager := connector.GetEtcdManager()

	// 从管理器获取 etcd 客户端
	// 相同配置的客户端会被自动复用，不同配置会创建新连接
	client, err := manager.GetEtcdClient(connConfig)
	if err != nil {
		return nil, fmt.Errorf("获取 etcd 客户端失败: %w", err)
	}

	// 使用内部实现创建 etcd 分布式锁
	// 传入复用的客户端和锁配置选项
	locker, err := internallock.NewEtcdLockerWithClient(client, opts)
	if err != nil {
		return nil, fmt.Errorf("创建 etcd 锁失败: %w", err)
	}

	// 返回实现了 Locker 接口的分布式锁实例
	return locker, nil
}

// NewWithManagerOptions 使用自定义管理器选项创建 etcd 分布式锁
//
// 功能说明：
//   - 与 New 类似，但允许自定义连接管理器的行为
//   - 可以设置最大连接数、健康检查间隔等高级选项
//
// 参数说明：
//   - cfg: etcd 连接配置，不能为 nil
//   - opts: 锁行为配置（可为 nil）
//   - managerOpts: 连接管理器配置选项
//
// 使用场景：
//   - 需要限制最大连接数的场景
//   - 需要调整健康检查频率的场景
//   - 需要更精细控制连接行为的场景
func NewWithManagerOptions(cfg *Config, opts *lockpkg.LockOptions, managerOpts connector.ManagerOptions) (lockpkg.Locker, error) {
	// 参数校验
	if cfg == nil {
		return nil, fmt.Errorf("etcd 配置不能为空")
	}

	// 转换配置
	connConfig := connector.EtcdConfig{
		Endpoints:        cfg.Endpoints,
		Username:         cfg.Username,
		Password:         cfg.Password,
		Timeout:          cfg.DialTimeout,
		KeepAliveTime:    cfg.KeepAliveTime,
		KeepAliveTimeout: cfg.KeepAliveTimeout,
	}

	// 使用自定义选项获取管理器
	manager := connector.GetEtcdManagerWithOptions(managerOpts)

	// 获取客户端并创建锁
	client, err := manager.GetEtcdClient(connConfig)
	if err != nil {
		return nil, fmt.Errorf("获取 etcd 客户端失败: %w", err)
	}

	locker, err := internallock.NewEtcdLockerWithClient(client, opts)
	if err != nil {
		return nil, fmt.Errorf("创建 etcd 锁失败: %w", err)
	}

	return locker, nil
}
