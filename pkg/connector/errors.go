// pkg/connector/errors.go
package connector

import "fmt"

// ErrorType 错误类型枚举
type ErrorType int

const (
	ErrConnection  ErrorType = iota // 连接建立失败
	ErrTimeout                      // 操作超时
	ErrConfig                       // 配置错误
	ErrHealthCheck                  // 健康检查失败
	ErrClosed                       // 连接已关闭
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
	return fmt.Sprintf("connector[%s] error: type=%d, retryable=%v, cause=%v",
		e.Connector, e.Type, e.Retryable, e.Cause)
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
