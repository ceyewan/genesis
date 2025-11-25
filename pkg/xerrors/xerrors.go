// xerrors 包为 Genesis 提供标准化的错误处理工具。
// 这是一个基础包，不依赖于 Genesis 的其他组件。
package xerrors

import (
	"errors"
	"fmt"
)

// ============================================================================
// 哨兵错误 - Genesis 组件通用的错误类型
// ============================================================================

var (
	// ErrNotFound 表示请求的资源未找到。
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists 表示资源已存在。
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidInput 表示输入参数无效。
	ErrInvalidInput = errors.New("invalid input")

	// ErrTimeout 表示操作超时。
	ErrTimeout = errors.New("timeout")

	// ErrUnavailable 表示服务或资源不可用。
	ErrUnavailable = errors.New("unavailable")

	// ErrUnauthorized 表示需要认证。
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden 表示操作不被允许。
	ErrForbidden = errors.New("forbidden")

	// ErrConflict 表示与当前状态冲突。
	ErrConflict = errors.New("conflict")

	// ErrInternal 表示内部错误。
	ErrInternal = errors.New("internal error")

	// ErrCanceled 表示操作被取消。
	ErrCanceled = errors.New("canceled")
)

// ============================================================================
// 错误包装 - 保留带上下文的错误链
// ============================================================================

// Wrap 用额外的上下文信息包装错误。
// 如果 err 为 nil，则返回 nil。
//
// 示例：
//
//	if err != nil {
//	    return xerrors.Wrap(err, "打开文件失败")
//	}
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf 用格式化的上下文信息包装错误。
// 如果 err 为 nil，则返回 nil。
//
// 示例：
//
//	if err != nil {
//	    return xerrors.Wrapf(err, "处理用户 %d 失败", userID)
//	}
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// WithCode 用错误码包装错误，便于结构化错误处理。
// 如果 err 为 nil，则返回 nil。
//
// 示例：
//
//	return xerrors.WithCode(err, "CACHE_MISS")
func WithCode(err error, code string) error {
	if err == nil {
		return nil
	}
	return &CodedError{
		Code:  code,
		Cause: err,
	}
}

// ============================================================================
// CodedError - 带有机器可读错误码的错误
// ============================================================================

// CodedError 表示带有机器可读错误码的错误。
type CodedError struct {
	Code  string // Machine-readable error code, e.g., "CACHE_MISS"
	Cause error  // Underlying error
}

// Error 实现 error 接口。
func (e *CodedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %v", e.Code, e.Cause)
	}
	return fmt.Sprintf("[%s]", e.Code)
}

// Unwrap 返回底层错误，支持 errors.Is/As。
func (e *CodedError) Unwrap() error {
	return e.Cause
}

// GetCode 从错误链中提取错误码。
// 如果未找到 CodedError，则返回空字符串。
func GetCode(err error) string {
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

// ============================================================================
// Must - 遇到错误时 panic（仅用于初始化）
// ============================================================================

// Must 如果 err 不为 nil，则 panic。仅在初始化阶段使用。
// 如果 err 为 nil，则返回 v。
//
// 示例：
//
//	cfg := xerrors.Must(config.Load("config.yaml"))
//	conn := xerrors.Must(db.Connect(cfg.DSN))
func Must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("must: %v", err))
	}
	return v
}

// MustOK 如果 ok 为 false，则 panic。用于初始化阶段的类型断言。
//
// 示例：
//
//	handler := xerrors.MustOK(service.(http.Handler))
func MustOK[T any](v T, ok bool) T {
	if !ok {
		panic(fmt.Sprintf("must: assertion failed for type %T", v))
	}
	return v
}

// ============================================================================
// Collector - 聚合多个错误
// ============================================================================

// Collector 用于收集多个操作中的错误。
// 只保留遇到的第一个错误。
type Collector struct {
	err error
}

// Collect 向收集器添加错误。
// 只保留第一个非 nil 的错误。
func (c *Collector) Collect(err error) {
	if err != nil && c.err == nil {
		c.err = err
	}
}

// Err 返回收集到的第一个错误，如果没有则返回 nil。
func (c *Collector) Err() error {
	return c.err
}

// ============================================================================
// MultiError - 合并多个错误为一个
// ============================================================================

// MultiError 用于将多个错误合并为一个错误。
type MultiError struct {
	Errors []error
}

// Error 实现 error 接口。
func (m *MultiError) Error() string {
	if len(m.Errors) == 0 {
		return "no errors"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	return fmt.Sprintf("%v (and %d more errors)", m.Errors[0], len(m.Errors)-1)
}

// Unwrap 返回错误列表，支持 errors.Is/As（Go 1.20+）。
func (m *MultiError) Unwrap() []error {
	return m.Errors
}

// Combine 将多个错误合并为一个。
// 如果所有错误都为 nil，则返回 nil。
// 如果只有一个非 nil 错误，则返回该错误。
// 如果有多个非 nil 错误，则返回 MultiError。
func Combine(errs ...error) error {
	var nonNil []error
	for _, err := range errs {
		if err != nil {
			nonNil = append(nonNil, err)
		}
	}
	switch len(nonNil) {
	case 0:
		return nil
	case 1:
		return nonNil[0]
	default:
		return &MultiError{Errors: nonNil}
	}
}

// ============================================================================
// 标准库常用方法的再导出，便于使用
// ============================================================================

var (
	// New 创建一个带指定消息的新错误。
	New = errors.New

	// Is 判断 err 的错误链中是否有错误与 target 匹配。
	Is = errors.Is

	// As 查找 err 的错误链中第一个与 target 匹配的错误。
	As = errors.As

	// Unwrap 返回对 err 调用 Unwrap 的结果。
	Unwrap = errors.Unwrap

	// Join 返回一个包装给定错误的错误（Go 1.20+）。
	Join = errors.Join
)
