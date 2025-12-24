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

// Error 将错误简化为仅包含错误消息
//
// 这是轻量级的错误字段，只输出错误消息，适用于大多数场景。
//
// 示例：
//
//	logger.Error("Operation failed", clog.Error(err))
//
// 输出：err_msg="file not found"
func Error(err error) Field {
	if err == nil {
		return slog.String("", "")
	}
	return slog.String("err_msg", err.Error())
}

// ErrorWithCode 包含错误代码的错误字段
//
// 添加业务错误码，适用于需要错误分类的场景。
// 使用 slog.Group 产生嵌套结构：error={code="ERR_INVALID_INPUT", msg="invalid email"}
//
// 示例：
//
//	logger.Error("Validation failed", clog.ErrorWithCode(err, "ERR_INVALID_INPUT"))
func ErrorWithCode(err error, code string) Field {
	if err == nil {
		return slog.Group("error", slog.String("code", code))
	}
	return slog.Group("error",
		slog.String("msg", err.Error()),
		slog.String("code", code),
	)
}

// ErrorWithStack 包含错误消息和堆栈信息的字段
//
// 适用于需要调试的场景，包含完整的堆栈信息。
// 注意：生产环境中谨慎使用，可能产生过多日志。
// 使用 slog.Group 产生嵌套结构：error={msg="file not found", type="*os.PathError", stack="..."}
//
// 示例：
//
//	logger.Error("Critical error", clog.ErrorWithStack(err))
func ErrorWithStack(err error) Field {
	if err == nil {
		return slog.String("", "")
	}
	// 跳过当前函数和 ErrorWithStack 函数
	stack := getStackTrace(3)
	if stack != "" {
		return slog.Group("error",
			slog.String("msg", err.Error()),
			slog.String("type", fmt.Sprintf("%T", err)),
			slog.String("stack", stack),
		)
	}
	return slog.Group("error",
		slog.String("msg", err.Error()),
		slog.String("type", fmt.Sprintf("%T", err)),
	)
}

// ErrorWithCodeStack 包含错误消息、错误码和堆栈信息的字段
//
// 最完整的错误字段，包含消息、类型、堆栈和错误码。
// 仅在需要最详细调试信息时使用，如系统严重错误。
// 使用 slog.Group 产生嵌套结构：error={msg="...", type="...", code="...", stack="..."}
//
// 示例：
//
//	logger.Error("System crash", clog.ErrorWithCodeStack(err, "SYS_001"))
func ErrorWithCodeStack(err error, code string) Field {
	if err == nil {
		return slog.Group("error", slog.String("code", code))
	}
	// 跳过当前函数和 ErrorWithCodeStack 函数
	stack := getStackTrace(3)
	if stack != "" {
		return slog.Group("error",
			slog.String("msg", err.Error()),
			slog.String("type", fmt.Sprintf("%T", err)),
			slog.String("code", code),
			slog.String("stack", stack),
		)
	}
	return slog.Group("error",
		slog.String("msg", err.Error()),
		slog.String("type", fmt.Sprintf("%T", err)),
		slog.String("code", code),
	)
}

// getStackTrace 获取当前调用栈信息（内部使用）
func getStackTrace(skip int) string {
	var pcs [32]uintptr
	n := runtime.Callers(skip, pcs[:])
	if n == 0 {
		return ""
	}

	var builder strings.Builder
	frames := runtime.CallersFrames(pcs[:n])

	for i := 0; i < n; i++ {
		frame, more := frames.Next()
		if i > 0 { // 跳过第一个调用（当前函数）
			builder.WriteString(fmt.Sprintf("%s:%d %s\n", frame.File, frame.Line, frame.Function))
		}
		if !more {
			break
		}
	}

	return builder.String()
}
