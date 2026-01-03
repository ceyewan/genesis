package config

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrNotFound 配置文件未找到
	ErrNotFound = xerrors.New("config file not found")

	// ErrValidationFailed 配置验证失败
	ErrValidationFailed = xerrors.New("configuration validation failed")
)
