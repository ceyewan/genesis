package config

import (
	"testing"

	"github.com/ceyewan/genesis/xerrors"
)

// TestIsNotFound 测试 IsNotFound 函数
func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ErrNotFound",
			err:  xerrors.ErrNotFound,
			want: true,
		},
		{
			name: "wrapped ErrNotFound",
			err:  xerrors.Wrap(xerrors.ErrNotFound, "config not found"),
			want: true,
		},
		{
			name: "ErrTimeout",
			err:  xerrors.ErrTimeout,
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "custom error",
			err:  xerrors.New("custom error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsInvalidInput 测试 IsInvalidInput 函数
func TestIsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ErrInvalidInput",
			err:  xerrors.ErrInvalidInput,
			want: true,
		},
		{
			name: "wrapped ErrInvalidInput",
			err:  xerrors.Wrap(xerrors.ErrInvalidInput, "validation failed"),
			want: true,
		},
		{
			name: "ErrNotFound",
			err:  xerrors.ErrNotFound,
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "custom error",
			err:  xerrors.New("custom error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsInvalidInput(tt.err); got != tt.want {
				t.Errorf("IsInvalidInput() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWrapValidationError 测试 WrapValidationError 函数
func TestWrapValidationError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "non-nil error",
			err:     xerrors.New("validation failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := WrapValidationError(tt.err)
			if (wrapped != nil) != tt.wantErr {
				t.Errorf("WrapValidationError() error = %v, wantErr %v", wrapped, tt.wantErr)
				return
			}

			if tt.wantErr {
				// 验证可以使用 IsInvalidInput 检查
				if !IsInvalidInput(wrapped) {
					t.Error("WrapValidationError() result should be detectable by IsInvalidInput")
				}

				// 验证错误消息包含原始错误信息
				expectedMsg := "validation failed: invalid input"
				if wrapped.Error() != expectedMsg {
					t.Errorf("WrapValidationError() message = %v, want %v", wrapped.Error(), expectedMsg)
				}
			}
		})
	}
}

// TestWrapLoadError 测试 WrapLoadError 函数
func TestWrapLoadError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		message string
		wantErr bool
	}{
		{
			name:    "nil error",
			err:     nil,
			message: "test message",
			wantErr: false,
		},
		{
			name:    "non-nil error",
			err:     xerrors.New("file not found"),
			message: "config.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := WrapLoadError(tt.err, tt.message)
			if (wrapped != nil) != tt.wantErr {
				t.Errorf("WrapLoadError() error = %v, wantErr %v", wrapped, tt.wantErr)
				return
			}

			if tt.wantErr {
				// 验证错误消息包含预期的格式
				expectedMsg := "failed to load config: " + tt.message + ": file not found"
				if wrapped.Error() != expectedMsg {
					t.Errorf("WrapLoadError() message = %v, want %v", wrapped.Error(), expectedMsg)
				}

				// 验证可以使用 IsNotFound 检查（假设原错误是 ErrNotFound）
				if xerrors.Is(tt.err, xerrors.ErrNotFound) && !IsNotFound(wrapped) {
					t.Error("WrapLoadError() result should preserve original error type")
				}
			}
		})
	}
}

// TestErrValidationFailed 测试 ErrValidationFailed 变量
func TestErrValidationFailed(t *testing.T) {
	// 验证 ErrValidationFailed 不为 nil
	if ErrValidationFailed == nil {
		t.Error("ErrValidationFailed should not be nil")
	}

	// 验证错误消息
	expectedMsg := "configuration validation failed"
	if ErrValidationFailed.Error() != expectedMsg {
		t.Errorf("ErrValidationFailed.Error() = %v, want %v", ErrValidationFailed.Error(), expectedMsg)
	}

	// 验证可以使用 xerrors.Is 检查
	if !xerrors.Is(ErrValidationFailed, ErrValidationFailed) {
		t.Error("xerrors.Is(ErrValidationFailed, ErrValidationFailed) should return true")
	}

	// 验证错误类型
	var customErr *CustomValidationError
	if xerrors.As(ErrValidationFailed, &customErr) {
		t.Error("ErrValidationFailed should not be convertible to CustomValidationError")
	}
}

// CustomValidationError 自定义验证错误类型（用于测试）
type CustomValidationError struct {
	Message string
}

func (e *CustomValidationError) Error() string {
	return e.Message
}

// TestErrorIntegration 测试错误处理的集成场景
func TestErrorIntegration(t *testing.T) {
	// 场景1: 配置文件未找到
	fileNotFoundErr := xerrors.Wrap(xerrors.ErrNotFound, "config.yaml not found")
	if !IsNotFound(fileNotFoundErr) {
		t.Error("IsNotFound should detect wrapped ErrNotFound")
	}

	// 场景2: 配置验证失败
	validationErr := WrapValidationError(xerrors.New("required field missing"))
	if !IsInvalidInput(validationErr) {
		t.Error("IsInvalidInput should detect wrapped validation error")
	}

	// 场景3: 加载错误
	loadErr := WrapLoadError(xerrors.ErrTimeout, "timeout while loading config")
	if !xerrors.Is(loadErr, xerrors.ErrTimeout) {
		t.Error("Load error should preserve original error type")
	}

	// 场景4: 错误链检查
	if IsNotFound(validationErr) {
		t.Error("Validation error should not be detected as not found")
	}
	if IsInvalidInput(fileNotFoundErr) {
		t.Error("Not found error should not be detected as invalid input")
	}
}
