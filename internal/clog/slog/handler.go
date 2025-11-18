package slogadapter

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ceyewan/genesis/pkg/clog/types"
)

// clogHandler 封装了底层的 slog.Handler，并处理 Source 路径裁剪和动态级别调整。
type clogHandler struct {
	slog.Handler
	levelVar   *slog.LevelVar // 用于动态调整级别
	sourceRoot string
}

// NewHandler 创建并返回一个适配 clog 配置的 slog.Handler。
func NewHandler(config *types.Config, option *types.Option) (slog.Handler, error) {
	var w io.Writer
	switch strings.ToLower(config.Output) {
	case "stdout":
		w = os.Stdout
	case "stderr":
		w = os.Stderr
	default:
		// 假设是文件路径
		f, err := os.OpenFile(config.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, err
		}
		w = f
	}

	levelVar := new(slog.LevelVar)
	level, err := types.ParseLevel(config.Level)
	if err != nil {
		return nil, fmt.Errorf("parse level failed: %w", err)
	}

	// 正确设置slog级别 - slog的级别常量与我们的定义不同
	var slogLevel slog.Level
	switch level {
	case types.DebugLevel:
		slogLevel = slog.LevelDebug
	case types.InfoLevel:
		slogLevel = slog.LevelInfo
	case types.WarnLevel:
		slogLevel = slog.LevelWarn
	case types.ErrorLevel:
		slogLevel = slog.LevelError
	case types.FatalLevel:
		slogLevel = slog.LevelError + 1 // Fatal比Error更高
	}

	levelVar.Set(slogLevel)

	opts := &slog.HandlerOptions{
		AddSource: config.AddSource,
		Level:     levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// 修复级别显示 - 将数字级别映射为正确的级别名称
			if a.Key == slog.LevelKey {
				level := a.Value.Any().(slog.Level)
				var levelStr string
				switch {
				case level == -4: // DebugLevel
					levelStr = "DEBUG"
				case level == -3: // InfoLevel
					levelStr = "INFO"
				case level == -2: // WarnLevel
					levelStr = "WARN"
				case level == -1: // ErrorLevel
					levelStr = "ERROR"
				default:
					levelStr = fmt.Sprintf("LEVEL_%d", level)
				}
				a.Value = slog.StringValue(levelStr)
			}

			// 确保时间戳格式化 - 简化时间格式，去掉时区信息
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				// 使用简洁的时间格式：YYYY-MM-DD HH:MM:SS
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02 15:04:05"))
			}

			// 路径裁剪和调用信息处理 - 显示为caller字段
			if a.Key == slog.SourceKey {
				if source, ok := a.Value.Any().(*slog.Source); ok {
					// 使用runtime获取更准确的调用信息
					_, file, line, ok := runtime.Caller(6) // 跳过足够的调用栈层级
					if ok {
						fileName := file
						if config.SourceRoot != "" {
							// 如果指定了SourceRoot，尝试从该路径开始裁剪
							relPath, err := filepath.Rel(config.SourceRoot, fileName)
							if err == nil && !strings.HasPrefix(relPath, "..") {
								fileName = relPath
							} else {
								// 如果SourceRoot无效，尝试查找包含"genesis"的路径并裁剪
								if idx := strings.Index(fileName, "genesis"); idx != -1 {
									fileName = fileName[idx:]
								}
							}
						}
						// 如果没有设置SourceRoot，显示完整路径（默认行为）
						// 创建caller字段，格式：文件名:行号
						caller := fmt.Sprintf("%s:%d", fileName, line)
						// 返回caller属性而不是修改source
						return slog.String("caller", caller)
					}

					// 如果runtime.Caller失败，回退到source信息
					fileName := source.File
					if config.SourceRoot != "" {
						// 如果指定了SourceRoot，尝试从该路径开始裁剪
						relPath, err := filepath.Rel(config.SourceRoot, fileName)
						if err == nil && !strings.HasPrefix(relPath, "..") {
							fileName = relPath
						} else {
							// 如果SourceRoot无效，尝试查找包含"genesis"的路径并裁剪
							if idx := strings.Index(fileName, "genesis"); idx != -1 {
								fileName = fileName[idx:]
							}
						}
					}
					// 如果没有设置SourceRoot，显示完整路径（默认行为）
					// 创建caller字段，格式：文件名:行号
					caller := fmt.Sprintf("%s:%d", fileName, source.Line)
					// 返回caller属性而不是修改source
					return slog.String("caller", caller)
				}
			}
			return a
		},
	}

	var handler slog.Handler
	format := strings.ToLower(config.Format)
	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		// console 格式
		handler = slog.NewTextHandler(w, opts)
	}

	// TODO: 如果需要颜色支持，可能需要自定义 TextHandler 或使用第三方库。
	// 暂时忽略 EnableColor 字段，使用默认 TextHandler。

	return &clogHandler{
		Handler:    handler,
		levelVar:   levelVar,
		sourceRoot: config.SourceRoot,
	}, nil
}

// SetLevel 动态调整日志级别
func (h *clogHandler) SetLevel(level types.Level) error {
	// 正确设置slog级别 - slog的级别常量与我们的定义不同
	var slogLevel slog.Level
	switch level {
	case types.DebugLevel:
		slogLevel = slog.LevelDebug
	case types.InfoLevel:
		slogLevel = slog.LevelInfo
	case types.WarnLevel:
		slogLevel = slog.LevelWarn
	case types.ErrorLevel:
		slogLevel = slog.LevelError
	case types.FatalLevel:
		slogLevel = slog.LevelError + 1 // Fatal比Error更高
	}

	h.levelVar.Set(slogLevel)
	return nil
}

// Flush 强制同步所有缓冲区的日志 (slog 默认是同步的，这里留空)
func (h *clogHandler) Flush() {
	// No-op for standard slog handlers
}
