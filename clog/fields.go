package clog

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Field 是用于构建日志字段的抽象类型
//
// Field 是一个函数，它接收一个 map[string]any 并填充键值对。
// 这种设计支持延迟计算和类型安全。
//
// 示例：
//
//	logger.Info("User login",
//	    clog.String("username", "alice"),
//	    clog.Int("retry_count", 3),
//	    clog.Error(err),
//	)
type Field func(map[string]any)

// String 创建字符串字段
//
// 示例：clog.String("username", "alice")
func String(k, v string) Field {
	return func(m map[string]any) {
		m[k] = v
	}
}

// Int 创建整数字段
//
// 示例：clog.Int("retry_count", 3)
func Int(k string, v int) Field {
	return func(m map[string]any) {
		m[k] = v
	}
}

// Float64 创建浮点数字段
//
// 示例：clog.Float64("duration", 1.234)
func Float64(k string, v float64) Field {
	return func(m map[string]any) {
		m[k] = v
	}
}

// Bool 创建布尔字段
//
// 示例：clog.Bool("success", true)
func Bool(k string, v bool) Field {
	return func(m map[string]any) {
		m[k] = v
	}
}

// Time 创建时间字段，格式化为 RFC3339Nano
//
// 示例：clog.Time("created_at", time.Now())
func Time(k string, v time.Time) Field {
	return func(m map[string]any) {
		m[k] = v.Format(time.RFC3339Nano)
	}
}

// Int64 创建64位整数字段
//
// 示例：clog.Int64("timestamp", 1640995200000)
func Int64(k string, v int64) Field {
	return func(m map[string]any) {
		m[k] = v
	}
}

// Duration 创建时间长度字段
//
// 示例：clog.Duration("response_time", 150*time.Millisecond)
func Duration(k string, v time.Duration) Field {
	return func(m map[string]any) {
		m[k] = v.String()
	}
}

// Any 创建任意类型字段
//
// 当其他类型不适用时使用，支持任意类型的值。
//
// 示例：
//
//	clog.Any("payload", map[string]interface{}{
//	    "id": 123,
//	    "name": "test",
//	})
func Any(k string, v any) Field {
	return func(m map[string]any) {
		m[k] = v
	}
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
	return func(m map[string]any) {
		if err == nil {
			return
		}
		m["err_msg"] = err.Error()
	}
}

// ErrorWithCode 包含错误代码的错误字段
//
// 添加业务错误码，适用于需要错误分类的场景。
//
// 示例：
//
//	logger.Error("Validation failed", clog.ErrorWithCode(err, "ERR_INVALID_INPUT"))
//
// 输出：err_msg="invalid email" err_code="ERR_INVALID_INPUT"
func ErrorWithCode(err error, code string) Field {
	return func(m map[string]any) {
		if err == nil {
			return
		}
		m["err_msg"] = err.Error()
		m["err_code"] = code
	}
}

// ErrorWithStack 包含错误消息和堆栈信息的字段
//
// 适用于需要调试的场景，包含完整的堆栈信息。
// 注意：生产环境中谨慎使用，可能产生过多日志。
//
// 示例：
//
//	logger.Error("Critical error", clog.ErrorWithStack(err))
//
// 输出示例：
//
//	err_msg="file not found" err_type="*os.PathError" err_stack="main.go:42 main()"
func ErrorWithStack(err error) Field {
	return func(m map[string]any) {
		if err == nil {
			return
		}
		m["err_msg"] = err.Error()
		m["err_type"] = fmt.Sprintf("%T", err)
		// 添加错误堆栈信息 - 跳过当前函数和ErrorWithStack函数
		stack := getStackTrace(3)
		if stack != "" {
			m["err_stack"] = stack
		}
	}
}

// ErrorWithCodeStack 包含错误消息、错误码和堆栈信息的字段
//
// 最完整的错误字段，包含消息、类型、堆栈和错误码。
// 仅在需要最详细调试信息时使用，如系统严重错误。
//
// 示例：
//
//	logger.Error("System crash", clog.ErrorWithCodeStack(err, "SYS_001"))
//
// 输出示例：
//
//	err_msg="connection timeout" err_type="*net.OpError"
//	err_code="SYS_001" err_stack="main.go:45 main()"
func ErrorWithCodeStack(err error, code string) Field {
	return func(m map[string]any) {
		if err == nil {
			return
		}
		m["err_msg"] = err.Error()
		m["err_type"] = fmt.Sprintf("%T", err)
		m["err_code"] = code
		// 添加错误堆栈信息 - 跳过当前函数和ErrorWithCodeStack函数
		stack := getStackTrace(3)
		if stack != "" {
			m["err_stack"] = stack
		}
	}
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
