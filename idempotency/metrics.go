package idempotency

// Metrics 指标常量定义
const (
	// MetricExecutionsTotal 执行总数 (Counter)
	MetricExecutionsTotal = "idempotency_executions_total"

	// MetricCacheHitsTotal 缓存命中数 (Counter)
	MetricCacheHitsTotal = "idempotency_cache_hits_total"

	// MetricCacheMissesTotal 缓存未命中数 (Counter)
	MetricCacheMissesTotal = "idempotency_cache_misses_total"

	// MetricConcurrentRequestsTotal 并发请求数 (Counter)
	MetricConcurrentRequestsTotal = "idempotency_concurrent_requests_total"

	// MetricExecutionDuration 执行耗时 (Histogram)
	MetricExecutionDuration = "idempotency_execution_duration_seconds"

	// MetricLockAcquisitionDuration 锁获取耗时 (Histogram)
	MetricLockAcquisitionDuration = "idempotency_lock_acquisition_duration_seconds"

	// MetricStorageErrors 存储错误数 (Counter)
	MetricStorageErrors = "idempotency_storage_errors_total"

	// LabelKey 幂等键标签
	LabelKey = "key"

	// LabelResult 结果标签 (success/failure/concurrent)
	LabelResult = "result"

	// LabelOperation 操作标签 (execute/cache_hit/concurrent)
	LabelOperation = "operation"
)
