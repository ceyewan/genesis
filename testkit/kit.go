package testkit

import (
	"context"
	"testing"
	"time"

	"os"
	"path/filepath"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

func init() {
	// 尝试加载 .env 文件，向上查找最多 5 层
	dir, err := os.Getwd()
	if err == nil {
		for i := 0; i < 5; i++ {
			envFile := filepath.Join(dir, ".env")
			if _, err := os.Stat(envFile); err == nil {
				_ = godotenv.Load(envFile)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
}

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

// NewLogger 返回一个用于测试的 no-op logger
// 未来如果需要，可以配置为输出到 t.Log
func NewLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("genesis"))
	if err != nil {
		return clog.Discard()
	}
	return logger
}

// NewTestLogger 返回一个写入 testing.TB 的 logger
// 注意：这需要实现一个适配器或者使用 clog.New 配合自定义 writer
// 目前为了安全起见，我们返回 Discard
func NewTestLogger(t testing.TB) clog.Logger {
	// TODO: 实现写入 t.Log 的 logger
	return clog.Discard()
}

// NewMeter 返回一个用于测试的 no-op meter
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

// NewID 返回一个唯一的测试 ID (UUID v4)
// 用于生成唯一的 Key、Topic 或表名后缀，避免测试间数据冲突
func NewID() string {
	return uuid.New().String()[0:8]
}
