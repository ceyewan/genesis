package clog

import (
	"fmt"

	"github.com/ceyewan/genesis/internal/clog"
	"github.com/ceyewan/genesis/pkg/clog/types"
)

// 重新导出types包中的类型，方便使用
type (
	Logger       = types.Logger
	Config       = types.Config
	Option       = types.Option
	Field        = types.Field
	Level        = types.Level
	LogBuilder   = types.LogBuilder
	ContextField = types.ContextField
)

// 重新导出级别常量
const (
	DebugLevel = types.DebugLevel
	InfoLevel  = types.InfoLevel
	WarnLevel  = types.WarnLevel
	ErrorLevel = types.ErrorLevel
	FatalLevel = types.FatalLevel
)

// 重新导出字段构造函数
var (
	String        = types.String
	Int           = types.Int
	Int64         = types.Int64
	Float64       = types.Float64
	Bool          = types.Bool
	Duration      = types.Duration
	Time          = types.Time
	Any           = types.Any
	Error         = types.Error
	ErrorWithCode = types.ErrorWithCode
	RequestID     = types.RequestID
	UserID        = types.UserID
	TraceID       = types.TraceID
	Component     = types.Component
	ParseLevel    = types.ParseLevel
)

// New 创建一个新的Logger实例
func New(config *Config, option *Option) (Logger, error) {
	if config == nil {
		config = &Config{}
	}
	if option == nil {
		option = &Option{}
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	option.SetDefaults()

	// 调用internal包的实现
	return clog.NewLogger(config, option)
}

// Default 创建一个默认配置的Logger
func Default() Logger {
	logger, _ := New(&Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,  // 默认启用caller信息
		EnableColor: false, // 默认不启用颜色
	}, &Option{})
	return logger
}

// Must 类似New，但在出错时panic
func Must(config *Config, option *Option) Logger {
	logger, err := New(config, option)
	if err != nil {
		panic(err)
	}
	return logger
}
