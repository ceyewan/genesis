package metrics

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrInvalidConfig 表示 metrics 配置无效。
	ErrInvalidConfig = xerrors.New("metrics: invalid config")
	// ErrListen 表示 Prometheus HTTP 端点监听失败。
	ErrListen = xerrors.New("metrics: listen failed")
)
