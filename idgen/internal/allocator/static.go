package allocator

import (
	"context"
	"fmt"
)

// StaticAllocator 静态分配器
type StaticAllocator struct {
	workerID int64
}

// NewStatic 创建静态分配器
func NewStatic(workerID int64) *StaticAllocator {
	return &StaticAllocator{workerID: workerID}
}

// Allocate 直接返回配置的 WorkerID
func (a *StaticAllocator) Allocate(ctx context.Context) (int64, error) {
	if a.workerID < 0 || a.workerID > 1023 {
		return 0, fmt.Errorf("invalid worker id: %d (must be 0-1023)", a.workerID)
	}
	return a.workerID, nil
}

// Start 静态分配器无需保活，返回一个永不关闭的 channel
func (a *StaticAllocator) Start(ctx context.Context, workerID int64) (<-chan error, error) {
	ch := make(chan error)
	// 永不关闭，表示永不失效
	return ch, nil
}
