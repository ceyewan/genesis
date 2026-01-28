package clog

import (
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"
)

// Field 是 slog.Attr 的类型别名，实现零内存分配
type Field = slog.Attr

// String 创建字符串字段
func String(k, v string) Field {
	return slog.String(k, v)
}

// Int 创建整数字段
func Int(k string, v int) Field {
	return slog.Int(k, v)
}

// Uint64 创建64位无符号整数字段
func Uint64(k string, v uint64) Field {
	return slog.Uint64(k, v)
}

// Float64 创建浮点数字段
func Float64(k string, v float64) Field {
	return slog.Float64(k, v)
}

// Bool 创建布尔字段
func Bool(k string, v bool) Field {
	return slog.Bool(k, v)
}

// Time 创建时间字段
func Time(k string, v time.Time) Field {
	return slog.Time(k, v)
}

// Int64 创建64位整数字段
func Int64(k string, v int64) Field {
	return slog.Int64(k, v)
}

// Duration 创建时间长度字段
func Duration(k string, v time.Duration) Field {
	return slog.Duration(k, v)
}

// Any 创建任意类型字段
func Any(k string, v any) Field {
	return slog.Any(k, v)
}

// Group 创建嵌套字段组
func Group(k string, fields ...any) Field {
	return slog.Group(k, fields...)
}

const (
	errorKey          = "error"
	errorMsgKey       = "msg"
	errorMsgSimpleKey = "err_msg"
	errorCodeKey      = "code"
	errorTypeKey      = "type"
	errorStackKey     = "stack"
)

// Error 将错误简化为仅包含错误消息
//
// 这是轻量级的错误字段，只输出错误消息，适用于大多数场景。
func Error(err error) Field {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String(errorMsgSimpleKey, err.Error())
}

// ErrorWithCode 包含错误代码的错误字段
//
// 添加业务错误码，适用于需要错误分类的场景。
// 使用 slog.Group 产生嵌套结构：error={code="ERR_INVALID_INPUT", msg="invalid email"}
func ErrorWithCode(err error, code string) Field {
	if err == nil {
		return slog.Attr{}
	}
	return slog.Group(errorKey,
		slog.String(errorMsgKey, err.Error()),
		slog.String(errorCodeKey, code),
	)
}

// ErrorWithStack 包含错误消息和堆栈信息的字段
//
// 适用于需要调试的场景，包含完整的堆栈信息。
// 注意：生产环境中谨慎使用，可能产生过多日志。
// 使用 slog.Group 产生嵌套结构：error={msg="file not found", type="*os.PathError", stack="..."}
func ErrorWithStack(err error) Field {
	if err == nil {
		return slog.Attr{}
	}
	// skip=3: 跳过 runtime.Callers, getStackTrace, ErrorWithStack
	stack := getStackTrace(3)
	if stack != "" {
		return slog.Group(errorKey,
			slog.String(errorMsgKey, err.Error()),
			slog.String(errorTypeKey, fmt.Sprintf("%T", err)),
			slog.String(errorStackKey, stack),
		)
	}
	return slog.Group(errorKey,
		slog.String(errorMsgKey, err.Error()),
		slog.String(errorTypeKey, fmt.Sprintf("%T", err)),
	)
}

// ErrorWithCodeStack 包含错误消息、错误码和堆栈信息的字段
//
// 最完整的错误字段，包含消息、类型、堆栈和错误码。
// 仅在需要最详细调试信息时使用，如系统严重错误。
// 使用 slog.Group 产生嵌套结构：error={msg="...", type="...", code="...", stack="..."}
func ErrorWithCodeStack(err error, code string) Field {
	if err == nil {
		return slog.Attr{}
	}
	// skip=3: 跳过 runtime.Callers, getStackTrace, ErrorWithCodeStack
	stack := getStackTrace(3)
	if stack != "" {
		return slog.Group(errorKey,
			slog.String(errorMsgKey, err.Error()),
			slog.String(errorTypeKey, fmt.Sprintf("%T", err)),
			slog.String(errorCodeKey, code),
			slog.String(errorStackKey, stack),
		)
	}
	return slog.Group(errorKey,
		slog.String(errorMsgKey, err.Error()),
		slog.String(errorTypeKey, fmt.Sprintf("%T", err)),
		slog.String(errorCodeKey, code),
	)
}

// getStackTrace 获取当前调用栈信息（内部使用）
func getStackTrace(skip int) string {
	var pcs [32]uintptr
	n := runtime.Callers(skip, pcs[:]) // 跳过指定数量的调用栈帧
	if n == 0 {
		return ""
	}

	// runtime.CallersFrames 用于解析程序计数器（PC）为函数调用信息
	// frames.File, frames.Line, frames.Function 分别表示文件名、行号和函数名
	// frames.Next() 用于迭代调用栈帧
	var builder strings.Builder
	frames := runtime.CallersFrames(pcs[:n])

	for i := 0; i < n; i++ {
		frame, more := frames.Next()

		// 忽略 Go 运行时的入口点，保持堆栈信息清晰
		if frame.Function == "runtime.main" || frame.Function == "runtime.goexit" {
			break
		}

		builder.WriteString(fmt.Sprintf("%s:%d %s \n ", frame.File, frame.Line, frame.Function))
		if !more {
			break
		}
	}

	return builder.String()
}

// 为什么是 skip 3？
// 因为调用栈大致如下：
// 0. runtime.Callers
// 1. getStackTrace
// 2. ErrorWithStack / ErrorWithCodeStack
// 3. 调用者函数 (业务代码)
//
// runtime.Callers(skip, ...) 的 skip 参数指的是跳过多少个栈帧。
// skip=0: 包含 runtime.Callers 自身
// skip=1: 从 getStackTrace 开始
// skip=2: 从 ErrorWithStack 开始
// skip=3: 从 调用者函数 开始
//
// 因此使用 skip=3 可以直接定位到业务代码，无需再手动跳过 frames.Next() 的第一个结果。
