package types

import "fmt"

// ErrorType 定义错误类型
type ErrorType int

const (
	ErrorUnknown          ErrorType = iota
	ErrorFormatInvalid              // 格式无效
	ErrorValidationFailed           // 验证失败
	ErrorNotFound                   // 配置未找到
)

// Error 自定义配置错误
type Error struct {
	Type    ErrorType
	Key     string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("config error: %s (key: %s): %v", e.Message, e.Key, e.Cause)
	}
	return fmt.Sprintf("config error: %s (key: %s)", e.Message, e.Key)
}

func (e *Error) Unwrap() error {
	return e.Cause
}
