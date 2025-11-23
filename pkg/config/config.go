package config

import (
	"github.com/ceyewan/genesis/internal/config/viper"
	"github.com/ceyewan/genesis/pkg/config/types"
)

// 重新导出 types 包中的类型，方便用户使用
type (
	Manager       = types.Manager
	Event         = types.Event
	Option        = types.Option
	Options       = types.Options
	RemoteOptions = types.RemoteOptions
	Error         = types.Error
	ErrorType     = types.ErrorType
)

// 重新导出 Option 函数
var (
	WithConfigName  = types.WithConfigName
	WithConfigPaths = types.WithConfigPaths
	WithConfigPath  = types.WithConfigPath
	WithConfigType  = types.WithConfigType
	WithEnvPrefix   = types.WithEnvPrefix
	WithRemote      = types.WithRemote
)

// New 创建一个新的配置管理器
func New(opts ...Option) (Manager, error) {
	options := types.DefaultOptions()
	for _, o := range opts {
		o(options)
	}

	// 目前只支持 Viper 实现
	return viper.NewManager(options)
}
