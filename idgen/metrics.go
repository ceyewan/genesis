package idgen

// Metrics 指标常量定义
const (
	// MetricIDGenerated ID 生成总数 (Counter)
	MetricIDGenerated = "idgen_generated_total"

	// MetricIDGenerationErrors ID 生成错误数 (Counter)
	MetricIDGenerationErrors = "idgen_generation_errors_total"

	// MetricWorkerIDAllocationErrors WorkerID 分配错误数 (Counter)
	MetricWorkerIDAllocationErrors = "idgen_worker_id_allocation_errors_total"

	// MetricClockBackwardsCount 时钟回拨次数 (Counter)
	MetricClockBackwardsCount = "idgen_clock_backwards_total"

	// MetricSequenceGenerated 序列号生成总数 (Counter)
	MetricSequenceGenerated = "idgen_sequence_generated_total"

	// MetricSequenceGenerationErrors 序列号生成错误数 (Counter)
	MetricSequenceGenerationErrors = "idgen_sequence_generation_errors_total"

	// LabelMethod 方法标签 (e.g., "static", "redis", "etcd")
	LabelMethod = "method"

	// LabelVersion 版本标签 (e.g., "v4", "v7")
	LabelVersion = "version"

	// LabelKey 键标签
	LabelKey = "key"

	// LabelErrorType 错误类型标签
	LabelErrorType = "error_type"
)
