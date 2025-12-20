package types

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Field 是用于构建日志字段的抽象类型。
type Field func(*LogBuilder)

// LogBuilder 用于在日志记录前收集和处理所有字段。
type LogBuilder struct {
	// Data 用于收集键值对
	Data map[string]any
}

// 基础类型构造函数 (设计文档 5.1)

func String(k, v string) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

func Int(k string, v int) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

func Int64(k string, v int64) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

func Float64(k string, v float64) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

func Bool(k string, v bool) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

func Duration(k string, v time.Duration) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

func Time(k string, v time.Time) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v.Format(time.RFC3339Nano)
	}
}

func Any(k string, v any) Field {
	return func(b *LogBuilder) {
		b.Data[k] = v
	}
}

// 错误处理标准化 (设计文档 5.2)

// getStackTrace 获取当前调用栈信息
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

// Error 将错误结构化为 err_msg、err_type 和 err_stack 字段。
func Error(err error) Field {
	return func(b *LogBuilder) {
		if err == nil {
			return
		}
		b.Data["err_msg"] = err.Error()
		b.Data["err_type"] = fmt.Sprintf("%T", err)
		// 添加错误堆栈信息 - 跳过当前函数和Error函数
		stack := getStackTrace(3)
		if stack != "" {
			b.Data["err_stack"] = stack
		}
	}
}

// ErrorWithCode 包含可选的业务错误码和堆栈信息。
func ErrorWithCode(err error, code string) Field {
	return func(b *LogBuilder) {
		if err == nil {
			return
		}
		b.Data["err_msg"] = err.Error()
		b.Data["err_type"] = fmt.Sprintf("%T", err)
		b.Data["err_code"] = code
		// 添加错误堆栈信息 - 跳过当前函数和ErrorWithCode函数
		stack := getStackTrace(3)
		if stack != "" {
			b.Data["err_stack"] = stack
		}
	}
}

// 常用语义字段 (设计文档 5.3)

func RequestID(id string) Field {
	return String("request_id", id)
}

func UserID(id string) Field {
	return String("user_id", id)
}

func TraceID(id string) Field {
	return String("trace_id", id)
}

func Component(name string) Field {
	return String("component", name)
}
