package log

import (
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

// buildEncoderConfig 根据格式创建编码器配置
func buildEncoderConfig(format string, enableColor bool, rootPath string, addSource bool) zapcore.EncoderConfig {
	config := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     customTimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
	}

	// 根据 addSource 配置决定是否包含 caller 信息
	if addSource {
		config.CallerKey = "caller"
		config.EncodeCaller = customCallerEncoder(rootPath)
	} else {
		config.CallerKey = zapcore.OmitKey
	}

	// Console 格式特殊处理
	if format == "console" {
		if enableColor {
			config.EncodeLevel = zapcore.CapitalColorLevelEncoder
		} else {
			config.EncodeLevel = zapcore.CapitalLevelEncoder
		}
	}

	return config
}

// customTimeEncoder 自定义时间编码格式
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

// customCallerEncoder 自定义调用者编码器，支持 rootPath 配置
func customCallerEncoder(rootPath string) zapcore.CallerEncoder {
	return func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
		if !caller.Defined {
			enc.AppendString("undefined")
			return
		}

		// 如果没有设置 rootPath，使用默认的短路径显示（最后两层）
		if rootPath == "" {
			zapcore.ShortCallerEncoder(caller, enc)
			return
		}

		// 获取文件的绝对路径
		fullPath := caller.File

		// 检查路径是否包含 rootPath
		if strings.Contains(fullPath, rootPath) {
			// 找到 rootPath 在路径中的位置
			if idx := strings.Index(fullPath, rootPath); idx != -1 {
				// 截取 rootPath 后的部分
				relativePath := fullPath[idx+len(rootPath):]
				// 移除开头的路径分隔符
				relativePath = strings.TrimPrefix(relativePath, string(filepath.Separator))
				// 格式化输出：相对路径:行号
				enc.AppendString(relativePath + ":" + caller.String()[strings.LastIndex(caller.String(), ":")+1:])
				return
			}
		}

		// 如果 rootPath 不在路径中，显示绝对路径
		enc.AppendString(caller.String())
	}
}

// createEncoder 根据格式创建编码器
func createEncoder(format string, config zapcore.EncoderConfig) zapcore.Encoder {
	switch format {
	case "json":
		return zapcore.NewJSONEncoder(config)
	case "console":
		return zapcore.NewConsoleEncoder(config)
	default:
		return zapcore.NewJSONEncoder(config)
	}
}
