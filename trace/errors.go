package trace

import "github.com/ceyewan/genesis/xerrors"

// ErrInvalidConfig 表示 trace 初始化配置无效。
var ErrInvalidConfig = xerrors.New("trace: invalid config")
