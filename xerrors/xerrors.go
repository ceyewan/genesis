// Package xerrors 为 Genesis 框架提供标准化的错误处理工具。
// 这是一个基础包，不依赖于 Genesis 的其他组件。
//
// 特性：
//   - 零依赖设计：不依赖任何 Genesis 组件，避免循环依赖
//   - 错误链兼容：完全兼容 Go 1.13+ 的 errors.Is、errors.As、errors.Unwrap
//   - Sentinel Errors：提供 10 个预定义的通用错误类型
//   - 错误码支持：机器可读的错误码，便于 API 错误映射
//   - 泛型支持：Go 1.18+ 的泛型 Must 函数
//   - 智能错误聚合：Collector 和 Combine 支持多错误处理
//
// 基本使用：
//
//	// 错误包装
//	file, err := os.Open("config.yaml")
//	if err != nil {
//		return nil, xerrors.Wrap(err, "open config file")
//	}
//
//	// 带错误码的错误
//	user, err := getUserFromDB(123)
//	if err != nil {
//		return nil, xerrors.WithCode(err, "USER_NOT_FOUND")
//	}
//
//	// Sentinel Errors 检查
//	result, err := cache.Get(ctx, key)
//	if errors.Is(err, cache.ErrCacheMiss) {
//		// 缓存未命中，从数据库加载
//		result, err = db.FindByID(ctx, id)
//	}
//
//	// 错误码提取
//	err := someOperation()
//	code := xerrors.GetCode(err)
//	if code != "" {
//		return HTTPError(codeToHTTPStatus(code), code)
//	}
package xerrors

import (
	"errors"
	"fmt"
)

// ============================================================================
// 哨兵错误 - Genesis 组件通用的错误类型
// ============================================================================
// 这些是预定义的 Sentinel Errors，用于快速判断错误类型。
// 所有 Genesis 组件应该定义自己的错误，然后在必要时使用这些基础错误或创建新的。
// 使用 errors.Is() 进行比较，支持错误链。
//
// 示例：
//   err := someOperation()
//   if errors.Is(err, ErrNotFound) {
//       // 处理未找到的情况
//   }

var (
	// ErrNotFound 表示请求的资源未找到。
	// HTTP 映射: 404 Not Found
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists 表示资源已存在。
	// HTTP 映射: 409 Conflict
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidInput 表示输入参数无效。
	// HTTP 映射: 400 Bad Request
	ErrInvalidInput = errors.New("invalid input")

	// ErrTimeout 表示操作超时。
	// HTTP 映射: 504 Gateway Timeout
	ErrTimeout = errors.New("timeout")

	// ErrUnavailable 表示服务或资源不可用。
	// HTTP 映射: 503 Service Unavailable
	ErrUnavailable = errors.New("unavailable")

	// ErrUnauthorized 表示需要认证。
	// HTTP 映射: 401 Unauthorized
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden 表示操作不被允许。
	// HTTP 映射: 403 Forbidden
	ErrForbidden = errors.New("forbidden")

	// ErrConflict 表示与当前状态冲突。
	// HTTP 映射: 409 Conflict
	ErrConflict = errors.New("conflict")

	// ErrInternal 表示内部错误。
	// HTTP 映射: 500 Internal Server Error
	ErrInternal = errors.New("internal error")

	// ErrCanceled 表示操作被取消。
	// HTTP 映射: 499 Client Closed Request (非标准)
	ErrCanceled = errors.New("canceled")
)

// ============================================================================
// 错误包装 - 保留带上下文的错误链
// ============================================================================
// 错误包装是构建可读错误消息的关键。使用 Wrap/Wrapf 保留错误链，
// 使得调用方可以使用 errors.Is/As 检查底层错误。
//
// 原则：
//   1. 每层函数都应该 Wrap 错误，添加上下文
//   2. 使用 Wrap 而非 fmt.Errorf，确保 %w 保留链
//   3. nil 检查：如果 err 为 nil，不要包装

// Wrap 用额外的上下文信息包装错误。
// 如果 err 为 nil，则返回 nil。
// 保留错误链，使得 errors.Is/As/Unwrap 继续工作。
//
// 示例：
//
//	file, err := os.Open("config.yaml")
//	if err != nil {
//	    return nil, xerrors.Wrap(err, "open config file")
//	}
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf 用格式化的上下文信息包装错误。
// 如果 err 为 nil，则返回 nil。
// 格式化信息在前，原始错误在后，便于阅读。
//
// 示例：
//
//	user, err := db.FindByID(ctx, userID)
//	if err != nil {
//	    return nil, xerrors.Wrapf(err, "find user %d", userID)
//	}
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// WithCode 用错误码包装错误，便于结构化错误处理。
// 返回的错误包含机器可读的错误码和人类可读的错误消息。
// 如果 err 为 nil，则返回 nil。
//
// 错误码用途：
//  1. API 错误响应中的 code 字段
//  2. 监控告警中的错误分类
//  3. 调用方快速判断错误类型
//
// 示例：
//
//	user, err := db.FindByID(ctx, id)
//	if errors.Is(err, sql.ErrNoRows) {
//	    return nil, xerrors.WithCode(ErrNotFound, "USER_NOT_FOUND")
//	}
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
// 使用 errors.As 遍历错误链，查找第一个 CodedError。
// 如果未找到，返回空字符串。
// 支持多层包装的错误链。
//
// 示例：
//
//	err := someOperation()
//	code := xerrors.GetCode(err)
//	if code != "" {
//	    return HTTPError(codeToHTTPStatus(code), code)
//	}
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
// ⚠️  警告：仅在 main() 中的初始化阶段使用！
// 在运行时业务逻辑中使用 Must 会导致服务 panic，这是错误的。
//
// 适用场景：
//   - 加载配置文件
//   - 创建数据库连接
//   - 初始化日志系统
//
// 不适用场景：
//   - 处理 HTTP 请求
//   - 处理数据库查询
//   - 任何可能失败的业务操作
//
// 示例：
//
//	func main() {
//	    cfg := xerrors.Must(config.Load("config.yaml"))
//	    logger := xerrors.Must(clog.New(&cfg.Log))
//	    conn := xerrors.Must(connector.NewRedis(&cfg.Redis))
//	    defer conn.Close()
//	}
func Must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("must: %v", err))
	}
	return v
}

// MustOK 如果 ok 为 false，则 panic。用于初始化阶段的类型断言。
// 与 Must 相同，仅在初始化时使用。
//
// 示例：
//
//	handler := xerrors.MustOK(service.(http.Handler), ok)
func MustOK[T any](v T, ok bool) T {
	if !ok {
		panic(fmt.Sprintf("must: assertion failed for type %T", v))
	}
	return v
}

// ============================================================================
// Collector - 聚合多个错误
// ============================================================================
// Collector 是一个简单的错误收集器，用于在多步骤操作中收集错误。
// 它只保留第一个遇到的错误，适用于：
//   - 表单验证（多字段验证，返回第一个错误）
//   - 批量关闭资源（多个 Close 操作）
//   - 顺序执行的初始化步骤

// Collector 用于收集多个操作中的错误。
// 只保留遇到的第一个错误。
// 零值可用（无需初始化）。
//
// 示例：
//
//	var errs xerrors.Collector
//	errs.Collect(validateName(u.Name))
//	errs.Collect(validateEmail(u.Email))
//	errs.Collect(validateAge(u.Age))
//	return errs.Err()  // 返回第一个错误或 nil
type Collector struct {
	err error
}

// Collect 向收集器添加错误。
// 只保留第一个非 nil 的错误，后续错误被忽略。
// nil 错误自动被忽略。
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
// MultiError 将多个错误合并为单个错误，支持错误链检查。
// 适用于聚合多个独立操作的错误，例如：
//   - 批量删除操作（多个删除失败）
//   - 并发操作（多个 goroutine 的错误）
//   - errors.Join 的结果

// MultiError 用于将多个错误合并为一个错误。
// 通过 Unwrap() 返回错误列表，支持 errors.Is/As 的错误链遍历。
type MultiError struct {
	// Errors 包含所有被合并的错误
	Errors []error
}

// Error 实现 error 接口。
// 格式为：第一个错误 (and N more errors)
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
// 调用方可以使用 errors.Is(multiErr, targetErr) 来检查
// targetErr 是否在错误列表中。
func (m *MultiError) Unwrap() []error {
	return m.Errors
}

// Combine 将多个错误合并为一个。
// 智能合并逻辑：
//   - 所有错误都为 nil → 返回 nil
//   - 仅一个非 nil 错误 → 返回该错误（不包装）
//   - 多个非 nil 错误 → 返回 MultiError
//
// 示例：
//
//	err1 := operation1()
//	err2 := operation2()
//	err3 := operation3()
//	return xerrors.Combine(err1, err2, err3)
//
//	// 使用时
//	if errors.Is(combined, someError) {
//	    // someError 在 combined 中
//	}
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
// 为了方便用户，将标准库 errors 包的常用函数再导出。
// 这样用户可以从 xerrors 中导入所有错误处理函数，无需额外导入 errors。

var (
	// New 创建一个带指定消息的新错误。
	// 与 errors.New 等价。
	New = errors.New

	// Is 判断 err 的错误链中是否有错误与 target 匹配。
	// 支持错误链遍历，用于 Sentinel Error 检查。
	// 详见 https://pkg.go.dev/errors#Is
	Is = errors.Is

	// As 查找 err 的错误链中第一个与 target 匹配的错误。
	// 用于类型断言和提取具体错误类型。
	// 详见 https://pkg.go.dev/errors#As
	As = errors.As

	// Unwrap 返回对 err 调用 Unwrap 的结果。
	// 用于获取错误链中的下一个错误。
	// 详见 https://pkg.go.dev/errors#Unwrap
	Unwrap = errors.Unwrap

	// Join 返回一个包装给定错误的错误（Go 1.20+）。
	// 与 Combine 类似，但来自标准库。
	// 详见 https://pkg.go.dev/errors#Join
	Join = errors.Join
)
