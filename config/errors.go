package config

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrValidationFailed 配置验证失败
	ErrValidationFailed = xerrors.New("configuration validation failed")

	// ErrNotLoaded 配置尚未加载
	ErrNotLoaded = xerrors.New("configuration not loaded")
)
