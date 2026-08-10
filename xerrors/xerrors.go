// Package xerrors 提供 Genesis 的轻量错误封装工具。
//
// 这个组件的目标不是替代标准库 errors，也不是提供完整的错误框架，而是在保持
// Go 错误链语义不变的前提下，补齐几个高频且稳定的工程能力：
//
//   - 使用 Wrap / Wrapf 为错误追加上下文，同时保留 errors.Is / errors.As 链路
//   - 使用 WithCode / GetCode 为错误补充一个轻量的机器可读错误码
//   - 使用 Collector / Combine 简化多步骤校验和多错误合并
//   - 使用 Must / MustOK 处理初始化阶段的“失败即 panic”场景
//
// xerrors 刻意保持克制。它当前不提供 stack trace、错误分类体系、并发安全的错误
// 聚合器，也不试图替应用统一建模所有协议层错误。对大多数业务代码来说，它更像
// 是“标准库 errors 的工程补充层”，而不是“另一套错误系统”。
package xerrors

import (
	"errors"
	"fmt"
)

// Wrap 用上下文信息包装错误，保留错误链。
//
// Wrap(nil, msg) 会返回 nil，这样调用方可以在 if err != nil 分支里直接返回。
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf 用格式化的上下文信息包装错误。
//
// Wrapf(nil, format, args...) 会返回 nil。
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// WithCode 用错误码包装错误。
//
// 当前的 code 模型非常轻量，只包含一个字符串错误码，不承担更复杂的错误元数据职责。
// WithCode(nil, code) 会返回 nil。
// WithCode(err, "") 会直接返回原错误，不创建新的错误包装。
func WithCode(err error, code string) error {
	if err == nil {
		return nil
	}
	if code == "" {
		return err
	}
	return &CodedError{Code: code, Cause: err}
}

// CodedError 表示带有机器可读错误码的错误。
//
// 一般建议通过 WithCode 构造，而不是直接初始化该结构体。
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
//
// 若错误链中存在多个 CodedError，返回 errors.As 命中的第一个。
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
//
// 它适合消费已有的 (T, bool) 返回值 helper。对于普通运行时分支，
// 直接显式处理 ok 往往更清晰。
func MustOK[T any](v T, ok bool) T {
	if !ok {
		panic("assertion failed")
	}
	return v
}

// Collector 收集多个错误，但只保留第一个非 nil 错误。
//
// 它适合顺序执行的多步骤校验流程，不是并发安全的错误聚合器。
type Collector struct {
	err error
}

// Collect 收集一个错误；若 c 已经保存了第一个错误，则后续错误会被忽略。
func (c *Collector) Collect(err error) {
	if err != nil && c.err == nil {
		c.err = err
	}
}

// Err 返回已收集到的第一个错误；若从未收集到非 nil 错误，则返回 nil。
func (c *Collector) Err() error {
	return c.err
}

// MultiError 表示多个错误的集合。
//
// 它实现 Unwrap() []error，因此兼容 errors.Is / errors.As 多错误匹配语义。
type MultiError struct {
	Errors []error
}

func (m *MultiError) Error() string {
	if m == nil {
		return "no errors"
	}

	var first error
	n := 0
	for _, err := range m.Errors {
		if err == nil {
			continue
		}
		if first == nil {
			first = err
		}
		n++
	}

	switch n {
	case 0:
		return "no errors"
	case 1:
		return first.Error()
	default:
		return fmt.Sprintf("%v (and %d more errors)", first, n-1)
	}
}

// Unwrap 返回非 nil 错误的副本，供 errors.Is / errors.As 遍历匹配。
func (m *MultiError) Unwrap() []error {
	if m == nil {
		return nil
	}
	errs := make([]error, 0, len(m.Errors))
	for _, err := range m.Errors {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Combine 将多个错误合并为一个。
//
// 规则如下：
//   - 全为 nil 时返回 nil
//   - 仅有一个非 nil 错误时直接返回该错误
//   - 多个非 nil 错误时返回 *MultiError
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

// New delegates to errors.New.
func New(text string) error { return errors.New(text) }

// Is delegates to errors.Is.
func Is(err, target error) bool { return errors.Is(err, target) }

// As delegates to errors.As.
func As(err error, target any) bool { return errors.As(err, target) }

// Unwrap delegates to errors.Unwrap.
func Unwrap(err error) error { return errors.Unwrap(err) }

// Join delegates to errors.Join.
func Join(errs ...error) error { return errors.Join(errs...) }
