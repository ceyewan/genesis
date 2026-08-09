package clog

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrInvalidConfig 表示 logger 配置或构造 option 无效。
	ErrInvalidConfig = xerrors.New("clog: invalid config")
	// ErrInvalidLevel 表示日志级别不受支持。
	ErrInvalidLevel = xerrors.New("clog: invalid level")
)
