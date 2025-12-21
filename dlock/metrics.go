package dlock

// Metrics 指标常量定义
const (
	// MetricLockAcquired 锁获取成功次数 (Counter)
	MetricLockAcquired = "dlock_lock_acquired_total"

	// MetricLockFailed 锁获取失败次数 (Counter)
	MetricLockFailed = "dlock_lock_failed_total"

	// MetricLockReleased 锁释放次数 (Counter)
	MetricLockReleased = "dlock_lock_released_total"

	// MetricLockHoldDuration 锁持有时长 (Histogram)
	MetricLockHoldDuration = "dlock_lock_hold_duration_seconds"

	// LabelBackend 后端类型标签
	LabelBackend = "backend"

	// LabelOperation 操作类型标签
	LabelOperation = "operation"

	// LabelKey 锁键标签
	LabelKey = "key"
)
