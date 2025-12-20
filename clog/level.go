package clog

import (
	"fmt"
	"strings"
)

// Level 日志级别类型
//
// 支持5个级别，按严重程度递增：
//
//	DebugLevel: 调试信息，通常只在开发环境使用
//	InfoLevel:  一般信息，记录正常的业务流程
//	WarnLevel:  警告信息，表示潜在问题
//	ErrorLevel: 错误信息，表示程序出错但可恢复
//	FatalLevel: 致命错误，程序会退出
//
// 级别数值越小优先级越高，DebugLevel 最低。
type Level int

const (
	DebugLevel Level = iota - 4 // 调试级别
	InfoLevel                   // 信息级别
	WarnLevel                   // 警告级别
	ErrorLevel                  // 错误级别
	FatalLevel                  // 致命级别
)

// String 返回 Level 的字符串表示
//
// 示例：
//
//	clog.InfoLevel.String() // "info"
//	clog.ErrorLevel.String() // "error"
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	case FatalLevel:
		return "fatal"
	default:
		return fmt.Sprintf("level(%d)", l)
	}
}

// ParseLevel 将字符串解析为 Level
//
// 支持的字符串（不区分大小写）：
//
//	"debug", "info", "warn", "error", "fatal"
//
// 如果无法解析，会返回 InfoLevel 和错误信息。
//
// 示例：
//
//	level, err := clog.ParseLevel("INFO")
//	level, err := clog.ParseLevel("debug")
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return DebugLevel, nil
	case "info":
		return InfoLevel, nil
	case "warn":
		return WarnLevel, nil
	case "error":
		return ErrorLevel, nil
	case "fatal":
		return FatalLevel, nil
	default:
		return InfoLevel, fmt.Errorf("unknown log level: %s", s)
	}
}
