package registry

import (
	internalregistry "github.com/ceyewan/genesis/internal/registry/etcd"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/registry/types"
)

// 重新导出类型，方便使用
type (
	Registry        = types.Registry
	ServiceInstance = types.ServiceInstance
	ServiceEvent    = types.ServiceEvent
	EventType       = types.EventType
	Config          = types.Config
)

// 重新导出常量
const (
	EventTypePut    = types.EventTypePut
	EventTypeDelete = types.EventTypeDelete
)

// 重新导出错误
var (
	ErrServiceNotFound          = types.ErrServiceNotFound
	ErrServiceAlreadyRegistered = types.ErrServiceAlreadyRegistered
	ErrInvalidServiceInstance   = types.ErrInvalidServiceInstance
	ErrLeaseExpired             = types.ErrLeaseExpired
	ErrWatchClosed              = types.ErrWatchClosed
	ErrConnectionFailed         = types.ErrConnectionFailed
)

// New 创建 Registry 实例（基于 Etcd）
// conn: Etcd 连接器
// cfg: 组件配置
// opts: 可选参数 (Logger, Meter, Tracer)
func New(conn connector.EtcdConnector, cfg Config, opts ...Option) (Registry, error) {
	// 应用选项
	opt := defaultOptions()
	for _, o := range opts {
		o(opt)
	}

	// 调用内部实现
	return internalregistry.New(conn, cfg, opt.logger, opt.meter, opt.tracer)
}
