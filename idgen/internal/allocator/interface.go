package allocator

import (
	"context"
)

// Allocator 定义 WorkerID 的分配策略
type Allocator interface {
	// Allocate 分配一个可用的 WorkerID
	// ctx: 用于控制超时
	Allocate(ctx context.Context) (int64, error)

	// Start 启动后台保活任务
	// workerID: 已分配的 ID
	// 返回: error channel。如果保活失败（租约失效），会发送 error，上层必须停止发号。
	Start(ctx context.Context, workerID int64) (<-chan error, error)
}
