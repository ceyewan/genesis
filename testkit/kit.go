package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Kit 包含通用的测试依赖
type Kit struct {
	Ctx    context.Context
	Logger clog.Logger
	Meter  metrics.Meter
}

// NewKit 返回一个包含默认依赖的测试工具包
func NewKit(t *testing.T) *Kit {
	return &Kit{
		Ctx:    context.Background(),
		Logger: NewLogger(),
		Meter:  NewMeter(),
	}
}

// NewLogger 返回一个用于测试的 logger
// 输出到开发环境格式，适合本地调试
func NewLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("genesis"))
	if err != nil {
		return clog.Discard()
	}
	return logger
}

// NewMeter 返回一个用于测试的 meter
// 使用 Discard 模式，不实际输出指标
func NewMeter() metrics.Meter {
	meter, err := metrics.New(metrics.NewDevDefaultConfig("test"))
	if err != nil {
		return metrics.Discard()
	}
	return meter
}

// NewContext 返回一个带有超时的测试上下文
func NewContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// NewID 返回一个唯一的测试 ID (UUID v4 前 8 位)
// 用于生成唯一的 Key、Topic 或表名后缀，避免测试间数据冲突
func NewID() string {
	return uuid.New().String()[0:8]
}
