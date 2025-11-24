package breaker

import (
	"sync"
)

// Window 滑动窗口，使用环形缓冲区实现
type Window struct {
	size     int    // 窗口大小
	buffer   []bool // 环形缓冲区，true=成功，false=失败
	index    int    // 当前写入位置
	total    int    // 总请求数（未满窗口时 < size）
	failures int    // 失败次数
	mu       sync.Mutex
}

// NewWindow 创建新的滑动窗口
func NewWindow(size int) *Window {
	if size <= 0 {
		size = 100 // 默认大小
	}
	return &Window{
		size:   size,
		buffer: make([]bool, size),
	}
}

// Record 记录一次请求结果
func (w *Window) Record(success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 如果窗口已满，先减去即将被覆盖的值
	if w.total >= w.size && !w.buffer[w.index] {
		w.failures--
	}

	// 写入新值
	w.buffer[w.index] = success
	if !success {
		w.failures++
	}

	// 更新索引和总数
	w.index = (w.index + 1) % w.size
	if w.total < w.size {
		w.total++
	}
}

// FailureRate 计算失败率
func (w *Window) FailureRate() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.total == 0 {
		return 0
	}
	return float64(w.failures) / float64(w.total)
}

// Total 获取总请求数
func (w *Window) Total() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total
}

// Failures 获取失败次数
func (w *Window) Failures() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failures
}

// Reset 重置窗口
func (w *Window) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.index = 0
	w.total = 0
	w.failures = 0
	for i := range w.buffer {
		w.buffer[i] = false
	}
}
