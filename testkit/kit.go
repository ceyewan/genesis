package testkit

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

const dockerProbeTimeout = 10 * time.Second

// RequireDocker verifies Docker before entering testcontainers, whose provider
// discovery may panic when the daemon socket is inaccessible. Integration tests
// deliberately fail (rather than silently skip) because a release gate must
// prove that its containers really ran.
func RequireDocker(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), dockerProbeTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err == nil {
		return
	}

	detail := strings.TrimSpace(string(output))
	if detail == "" {
		detail = err.Error()
	}
	if ctx.Err() != nil {
		detail = "probe timed out after " + dockerProbeTimeout.String() + ": " + detail
	}
	t.Fatalf("Docker is required for this integration test but is unavailable: %s", detail)
}

// Kit 包含通用的测试依赖。
type Kit struct {
	Ctx    context.Context
	Logger clog.Logger
	Meter  metrics.Meter
}

// NewKit 返回一个包含默认依赖的测试工具包。
func NewKit(t *testing.T) *Kit {
	t.Helper()

	logger := NewLogger()
	t.Cleanup(func() {
		_ = logger.Close()
	})

	return &Kit{
		Ctx:    context.Background(),
		Logger: logger,
		Meter:  NewMeter(),
	}
}

// NewLogger 返回一个用于测试的 logger。
// 输出到开发环境格式，适合本地调试。
func NewLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("genesis"))
	if err != nil {
		return clog.Discard()
	}
	return logger
}

// NewMeter 返回一个用于测试的 meter。
// 使用 Discard 模式，不实际输出指标。
func NewMeter() metrics.Meter {
	return metrics.Discard()
}

// NewContext 返回一个带有超时的测试上下文。
func NewContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx, cancel
}

// NewID 返回一个唯一的测试 ID（UUID v4 前 8 位）。
// 用于生成唯一的 Key、Topic 或表名后缀，避免测试间数据冲突。
func NewID() string {
	return uuid.New().String()[0:8]
}
