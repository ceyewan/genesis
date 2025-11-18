package types

import (
	"fmt"
	"strings"
)

// Level 是日志级别
type Level int

const (
	DebugLevel Level = iota - 4
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// String 返回 Level 的字符串表示
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
