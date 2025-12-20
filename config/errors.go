package config

import "github.com/ceyewan/genesis/xerrors"

// IsNotFound 检查错误是否为配置未找到
func IsNotFound(err error) bool {
	return xerrors.Is(err, xerrors.ErrNotFound)
}

// IsInvalidInput 检查错误是否为配置格式无效或验证失败
func IsInvalidInput(err error) bool {
	return xerrors.Is(err, xerrors.ErrInvalidInput)
}

// ErrValidationFailed 验证失败
var ErrValidationFailed = xerrors.New("configuration validation failed")

// WrapValidationError 包装验证错误
func WrapValidationError(err error) error {
	if err == nil {
		return nil
	}
	return xerrors.Wrapf(err, "validation failed")
}

// WrapLoadError 包装加载错误
func WrapLoadError(err error, message string) error {
	if err == nil {
		return nil
	}
	return xerrors.Wrapf(err, "failed to load config: %s", message)
}
