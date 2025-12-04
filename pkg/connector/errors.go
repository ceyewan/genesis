// pkg/connector/errors.go
package connector

import (
	"github.com/ceyewan/genesis/pkg/xerrors"
)

// ErrorType 错误类型枚举
type ErrorType int

const (
	TypeConnection  ErrorType = iota // 连接建立失败
	TypeTimeout                      // 操作超时
	TypeConfig                       // 配置错误
	TypeHealthCheck                  // 健康检查失败
	TypeClosed                       // 连接已关闭
)

// Sentinel Errors - 连接器专用的哨兵错误
var (
	ErrNotConnected  = xerrors.New("connector: not connected")
	ErrAlreadyClosed = xerrors.New("connector: already closed")
	ErrConnection    = xerrors.New("connector: connection failed")
	ErrTimeout       = xerrors.New("connector: timeout")
	ErrConfig        = xerrors.New("connector: invalid config")
	ErrHealthCheck   = xerrors.New("connector: health check failed")
)

// Error 连接器统一错误类型
type Error struct {
	Type      ErrorType // 错误类型
	Connector string    // 出错的连接器名称
	Cause     error     // 原始错误
	Retryable bool      // 是否可重试
}

// Error 实现 error 接口
func (e *Error) Error() string {
	return xerrors.Wrapf(e.Cause, "connector[%s] error: type=%d, retryable=%v",
		e.Connector, e.Type, e.Retryable).Error()
}

// Unwrap 支持错误链
func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError 创建新的连接器错误
func NewError(connectorName string, errType ErrorType, cause error, retryable bool) *Error {
	return &Error{
		Type:      errType,
		Connector: connectorName,
		Cause:     cause,
		Retryable: retryable,
	}
}

// WrapError 用连接器上下文包装错误
func WrapError(connectorName string, err error, retryable bool) error {
	if err == nil {
		return nil
	}

	// 检查是否是已知的哨兵错误
	if xerrors.Is(err, ErrNotConnected) ||
		xerrors.Is(err, ErrAlreadyClosed) ||
		xerrors.Is(err, ErrConnection) ||
		xerrors.Is(err, ErrTimeout) ||
		xerrors.Is(err, ErrConfig) ||
		xerrors.Is(err, ErrHealthCheck) {
		return xerrors.Wrapf(err, "connector[%s]", connectorName)
	}

	// 根据错误类型判断是否可重试
	isRetryable := retryable
	if xerrors.Is(err, xerrors.ErrTimeout) || xerrors.Is(err, xerrors.ErrUnavailable) {
		isRetryable = true
	}

	return NewError(connectorName, TypeConnection, err, isRetryable)
}
