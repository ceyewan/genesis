// Package xerrors 提供标准化错误处理工具。
package xerrors

import (
	"errors"
	"fmt"
)

// Wrap 用上下文信息包装错误，保留错误链。
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf 用格式化的上下文信息包装错误。
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// WithCode 用错误码包装错误。
func WithCode(err error, code string) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Cause: err}
}

// CodedError 带有机器可读错误码的错误。
type CodedError struct {
	Code  string
	Cause error
}

func (e *CodedError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %v", e.Code, e.Cause)
	}
	return fmt.Sprintf("[%s]", e.Code)
}

func (e *CodedError) Unwrap() error {
	return e.Cause
}

// GetCode 从错误链中提取错误码。
func GetCode(err error) string {
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

// Must 如果 err 不为 nil，则 panic。仅用于初始化阶段。
func Must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("must: %v", err))
	}
	return v
}

// MustOK 如果 ok 为 false，则 panic。
func MustOK[T any](v T, ok bool) T {
	if !ok {
		panic("assertion failed")
	}
	return v
}

// Collector 收集多个错误，保留第一个。
type Collector struct {
	err error
}

func (c *Collector) Collect(err error) {
	if err != nil && c.err == nil {
		c.err = err
	}
}

func (c *Collector) Err() error {
	return c.err
}

// MultiError 合并多个错误。
type MultiError struct {
	Errors []error
}

func (m *MultiError) Error() string {
	if len(m.Errors) == 0 {
		return "no errors"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	return fmt.Sprintf("%v (and %d more errors)", m.Errors[0], len(m.Errors)-1)
}

func (m *MultiError) Unwrap() []error {
	return m.Errors
}

// Combine 将多个错误合并为一个。
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

// 标准库函数再导出
var (
	New    = errors.New
	Is     = errors.Is
	As     = errors.As
	Unwrap = errors.Unwrap
	Join   = errors.Join
)
