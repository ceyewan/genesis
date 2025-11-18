package types

import (
	"fmt"
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

// Error 将错误结构化为 err_msg 和 err_type 字段。
func Error(err error) Field {
	return func(b *LogBuilder) {
		if err == nil {
			return
		}
		b.Data["err_msg"] = err.Error()
		b.Data["err_type"] = fmt.Sprintf("%T", err)
		// TODO: 错误堆栈（如果可用）
	}
}

// ErrorWithCode 包含可选的业务错误码。
func ErrorWithCode(err error, code string) Field {
	return func(b *LogBuilder) {
		if err == nil {
			return
		}
		b.Data["err_msg"] = err.Error()
		b.Data["err_type"] = fmt.Sprintf("%T", err)
		b.Data["err_code"] = code
		// TODO: 错误堆栈（如果可用）
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
