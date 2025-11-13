package log

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// outputConfig 输出配置
type outputConfig struct {
	Type        string
	Format      string
	Filename    string
	Rotation    *rotationConfig
	EnableColor bool
}

// buildWriteSyncer 根据输出配置创建写入器
func buildWriteSyncer(output outputConfig) (zapcore.WriteSyncer, error) {
	switch output.Type {
	case "console":
		return zapcore.AddSync(os.Stdout), nil
	case "file":
		return buildFileWriteSyncer(output)
	default:
		return nil, fmt.Errorf("unsupported output type: %s", output.Type)
	}
}

// buildFileWriteSyncer 创建文件写入器
func buildFileWriteSyncer(output outputConfig) (zapcore.WriteSyncer, error) {
	if output.Filename == "" {
		return nil, fmt.Errorf("filename is required for file output")
	}

	// 确保目录存在
	dir := filepath.Dir(output.Filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory failed: %v", err)
	}

	// 如果没有轮转配置，使用普通文件
	if output.Rotation == nil {
		file, err := os.OpenFile(output.Filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		return zapcore.AddSync(file), nil
	}

	// 使用 lumberjack 进行日志轮转
	logger := &lumberjack.Logger{
		Filename:   output.Filename,
		MaxSize:    output.Rotation.MaxSize,
		MaxBackups: output.Rotation.MaxBackups,
		MaxAge:     output.Rotation.MaxAge,
		Compress:   output.Rotation.Compress,
		LocalTime:  true,
	}

	return zapcore.AddSync(logger), nil
}
