package log

import (
	"go.uber.org/zap"
)

// Field 是 zap.Field 的别名
type Field = zap.Field

// 直接导出 zap 的字段构造函数
var (
	String   = zap.String
	Int      = zap.Int
	Int16    = zap.Int16
	Int32    = zap.Int32
	Int64    = zap.Int64
	Uint     = zap.Uint
	Uint32   = zap.Uint32
	Uint64   = zap.Uint64
	Float32  = zap.Float32
	Float64  = zap.Float64
	Bool     = zap.Bool
	Time     = zap.Time
	Duration = zap.Duration
	Any      = zap.Any
	Binary   = zap.Binary
	Strings  = zap.Strings
	Ints     = zap.Ints
	Err      = zap.Error // 别名，为了兼容性
	Stringer = zap.Stringer
)
