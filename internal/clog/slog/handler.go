package slogadapter

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
	level, _ := types.ParseLevel(config.Level)
	levelVar.Set(slog.Level(level))

	opts := &slog.HandlerOptions{
		AddSource: config.AddSource,
		Level:     levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// 确保时间戳格式化 (简化格式，不包含时区)
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				a.Value = slog.StringValue(a.Value.Time().Format("2006-01-02 15:04:05.000"))
			}

			// 路径裁剪
			if a.Key == slog.SourceKey {
				if source, ok := a.Value.Any().(*slog.Source); ok {
					if config.SourceRoot != "" {
						relPath, err := filepath.Rel(config.SourceRoot, source.File)
						if err == nil && !strings.HasPrefix(relPath, "..") {
							source.File = relPath
						}
					}
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
	h.levelVar.Set(slog.Level(level))
	return nil
}

// Flush 强制同步所有缓冲区的日志 (slog 默认是同步的，这里留空)
func (h *clogHandler) Flush() {
	// No-op for standard slog handlers
}
