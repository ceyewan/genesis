package idempotency

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("idempotency: connector is nil")

	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("idempotency: config is nil")

	// ErrKeyEmpty 幂等键为空
	ErrKeyEmpty = xerrors.New("idempotency: key is empty")

	// ErrConcurrentRequest 并发请求（当 WaitTimeout=0 或等待超时）
	ErrConcurrentRequest = xerrors.New("idempotency: concurrent request detected")

	// ErrResultNotFound 结果未找到（内部使用）
	ErrResultNotFound = xerrors.New("idempotency: result not found")

	// ErrStoreFailed 存储后端故障
	ErrStoreFailed = xerrors.New("idempotency: store operation failed")

	// ErrWaitTimeout 等待结果超时
	ErrWaitTimeout = xerrors.New("idempotency: wait for result timeout")

	// ErrExecutionFailed 执行失败
	ErrExecutionFailed = xerrors.New("idempotency: execution failed")
)
