// pkg/container/lifecycle.go
package container

import (
	"context"
	"fmt"
)

// Phase 定义启动阶段的常量
const (
	PhaseLogger    = 0  // 日志阶段
	PhaseConnector = 10 // 连接器阶段
	PhaseComponent = 20 // 组件阶段
	PhaseService   = 30 // 服务阶段
)

// Lifecycle 定义了可由容器管理生命周期的对象的行为
type Lifecycle interface {
	// Start 启动服务，Phase 越小越先启动
	Start(ctx context.Context) error
	// Stop 关闭服务，按启动的逆序调用
	Stop(ctx context.Context) error
	// Phase 返回启动阶段，用于排序
	Phase() int
}

// LifecycleManager 生命周期管理器
type LifecycleManager struct {
	items []LifecycleItem
}

// LifecycleItem 生命周期项目
type LifecycleItem struct {
	Name     string
	Instance Lifecycle
}

// NewLifecycleManager 创建新的生命周期管理器
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		items: make([]LifecycleItem, 0),
	}
}

// Register 注册生命周期对象
func (m *LifecycleManager) Register(name string, instance Lifecycle) {
	m.items = append(m.items, LifecycleItem{
		Name:     name,
		Instance: instance,
	})
}

// StartAll 按阶段顺序启动所有生命周期对象
func (m *LifecycleManager) StartAll(ctx context.Context) error {
	// 按阶段排序
	m.sortByPhase()

	// 顺序启动
	for _, item := range m.items {
		if err := item.Instance.Start(ctx); err != nil {
			return &LifecycleError{
				Phase: item.Instance.Phase(),
				Name:  item.Name,
				Cause: err,
			}
		}
	}

	return nil
}

// StopAll 按逆序停止所有生命周期对象
func (m *LifecycleManager) StopAll(ctx context.Context) {
	// 逆序停止
	for i := len(m.items) - 1; i >= 0; i-- {
		_ = m.items[i].Instance.Stop(ctx)
	}
}

// sortByPhase 按阶段排序
func (m *LifecycleManager) sortByPhase() {
	// 使用简单的冒泡排序
	for i := 0; i < len(m.items)-1; i++ {
		for j := 0; j < len(m.items)-i-1; j++ {
			if m.items[j].Instance.Phase() > m.items[j+1].Instance.Phase() {
				m.items[j], m.items[j+1] = m.items[j+1], m.items[j]
			}
		}
	}
}

// GetItems 获取所有生命周期项目
func (m *LifecycleManager) GetItems() []LifecycleItem {
	return m.items
}

// LifecycleError 生命周期错误
type LifecycleError struct {
	Phase int
	Name  string
	Cause error
}

// Error 实现 error 接口
func (e *LifecycleError) Error() string {
	return fmt.Sprintf("lifecycle error in phase %d [%s]: %v", e.Phase, e.Name, e.Cause)
}

// Unwrap 支持错误链
func (e *LifecycleError) Unwrap() error {
	return e.Cause
}
