package slogadapter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

	// 解析配置的级别字符串，直接映射到slog级别
	var slogLevel slog.Level
	switch strings.ToLower(config.Level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	case "fatal":
		slogLevel = slog.LevelError + 4 // Fatal比Error更高
	default:
		slogLevel = slog.LevelInfo // 默认info级别
	}

	levelVar.Set(slogLevel)

	opts := &slog.HandlerOptions{
		AddSource: config.AddSource,
		Level:     levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// 修复级别显示 - 正确映射slog级别到字符串
			if a.Key == slog.LevelKey {
				level := a.Value.Any().(slog.Level)
				var levelStr string
				switch {
				case level <= slog.LevelDebug:
					levelStr = "DEBUG"
				case level <= slog.LevelInfo:
					levelStr = "INFO"
				case level <= slog.LevelWarn:
					levelStr = "WARN"
				case level <= slog.LevelError:
					levelStr = "ERROR"
				default:
					levelStr = "FATAL"
				}
				a.Value = slog.StringValue(levelStr)
			}

			// 统一时间戳格式为 ISO8601
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				// 使用 ISO8601 格式：2006-01-02T15:04:05.000Z
				a.Value = slog.StringValue(a.Value.Time().Format(time.RFC3339))
			}

			// 路径裁剪和调用信息处理 - 显示为caller字段
			if a.Key == slog.SourceKey {
				if source, ok := a.Value.Any().(*slog.Source); ok {
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
		baseHandler := slog.NewTextHandler(w, opts)
		if config.EnableColor {
			handler = &coloredTextHandler{
				baseHandler: baseHandler,
				writer:      w,
				levelVar:    levelVar,
				addSource:   config.AddSource,
				sourceRoot:  config.SourceRoot,
			}
		} else {
			handler = baseHandler
		}
	}

	return &clogHandler{
		Handler:    handler,
		levelVar:   levelVar,
		sourceRoot: config.SourceRoot,
	}, nil
}

// SetLevel 动态调整日志级别
func (h *clogHandler) SetLevel(level types.Level) error {
	// 根据types.Level映射到slog.Level
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
		slogLevel = slog.LevelError + 4
	}

	h.levelVar.Set(slogLevel)
	return nil
}

// Flush 强制同步所有缓冲区的日志 (slog 默认是同步的，这里留空)
func (h *clogHandler) Flush() {
	// No-op for standard slog handlers
}

// ANSI 颜色常量
const (
	ansiReset        = "\033[0m"
	ansiDim          = "\033[2m"  // 暗淡效果，用于时间戳
	ansiResetDim     = "\033[22m" // 取消暗淡
	ansiBrightRed    = "\033[91m" // ERROR/FATAL - 亮红
	ansiBrightYellow = "\033[93m" // WARN - 亮黄
	ansiBrightGreen  = "\033[92m" // INFO - 亮绿
	ansiDimGray      = "\033[90m" // DEBUG - 暗灰
)

// coloredTextHandler 为 TextHandler 添加彩色支持
type coloredTextHandler struct {
	baseHandler slog.Handler
	writer      io.Writer
	levelVar    *slog.LevelVar
	addSource   bool
	sourceRoot  string
	mu          sync.Mutex
}

// Enabled 检查日志级别是否启用
func (h *coloredTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.baseHandler.Enabled(ctx, level)
}

// Handle 处理日志记录，添加颜色输出
func (h *coloredTextHandler) Handle(ctx context.Context, r slog.Record) error {
	// 使用缓冲区捕获基础 handler 的输出
	var buf bytes.Buffer

	// 创建临时的 TextHandler，输出到缓冲区
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		// 处理 Source 字段路径裁剪
		if a.Key == slog.SourceKey && h.addSource {
			if source, ok := a.Value.Any().(*slog.Source); ok {
				fileName := source.File
				if h.sourceRoot != "" {
					relPath, err := filepath.Rel(h.sourceRoot, fileName)
					if err == nil && !strings.HasPrefix(relPath, "..") {
						fileName = relPath
					} else {
						if idx := strings.Index(fileName, "genesis"); idx != -1 {
							fileName = fileName[idx:]
						}
					}
				}
				caller := fmt.Sprintf("%s:%d", fileName, source.Line)
				return slog.String("caller", caller)
			}
		}
		return a
	}

	baseOpts := &slog.HandlerOptions{
		AddSource:   h.addSource,
		Level:       h.levelVar,
		ReplaceAttr: replaceAttr,
	}
	tempHandler := slog.NewTextHandler(&buf, baseOpts)

	// 输出到缓冲区
	if err := tempHandler.Handle(ctx, r); err != nil {
		return err
	}

	// 获取输出内容
	output := buf.String()

	// 解析和着色输出
	coloredOutput := h.colorizeOutput(output, r.Level)

	// 写入最终输出
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.writer.Write([]byte(coloredOutput))
	return err
}

// WithAttrs 返回带有附加属性的新 handler
func (h *coloredTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &coloredTextHandler{
		baseHandler: h.baseHandler.WithAttrs(attrs),
		writer:      h.writer,
		levelVar:    h.levelVar,
		addSource:   h.addSource,
		sourceRoot:  h.sourceRoot,
	}
}

// WithGroup 返回带有分组的新 handler
func (h *coloredTextHandler) WithGroup(name string) slog.Handler {
	return &coloredTextHandler{
		baseHandler: h.baseHandler.WithGroup(name),
		writer:      h.writer,
		levelVar:    h.levelVar,
		addSource:   h.addSource,
		sourceRoot:  h.sourceRoot,
	}
}

// colorizeOutput 为日志输出添加 ANSI 颜色
func (h *coloredTextHandler) colorizeOutput(output string, level slog.Level) string {
	// 示例输出格式: "time=... level=... msg=... key=value ...\n"
	// 解析 key=value 格式并添加颜色

	output = strings.TrimSpace(output)
	if output == "" {
		return output + "\n"
	}

	levelColor := h.getLevelColor(level)

	var result strings.Builder
	fields := h.parseKeyValuePairs(output)

	for i, field := range fields {
		if i > 0 {
			result.WriteString(" ")
		}

		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			result.WriteString(field)
			continue
		}

		key := parts[0]
		value := parts[1]

		// 根据 key 应用颜色和格式
		switch key {
		case "time":
			// 时间戳：暗淡效果，key 和 = 符号都暗淡
			result.WriteString(fmt.Sprintf("%s%s%s%s%s%s%s", ansiDim, key, ansiReset, ansiDim, "=", ansiReset, value))
		case "level":
			// 级别：固定 5 字符宽度，后跟 tab 对齐，key 和 = 符号同色
			levelStr := value
			for len(levelStr) < 5 {
				levelStr += " "
			}
			result.WriteString(fmt.Sprintf("%s%s%s%s%s%s%s%s\t", levelColor, key, ansiReset, levelColor, "=", ansiReset, levelStr, ansiReset))
		case "caller":
			// caller：处理 genesis 前缀去掉，key 和 = 符号暗淡
			if strings.HasPrefix(value, "genesis/") {
				value = strings.TrimPrefix(value, "genesis/")
			}
			result.WriteString(fmt.Sprintf("%s%s%s%s%s%s%s", ansiDim, key, ansiReset, ansiDim, "=", ansiReset, value))
		case "msg":
			// 消息：根据日志级别着色，key 和 = 符号同色
			result.WriteString(fmt.Sprintf("%s%s%s%s%s%s%s", levelColor, key, ansiReset, levelColor, "=", ansiReset, value))
		default:
			// 其他字段：key 和 = 符号暗淡，value 正常
			result.WriteString(fmt.Sprintf("%s%s%s%s%s%s%s", ansiDim, key, ansiReset, ansiDim, "=", ansiReset, value))
		}
	}

	return result.String() + "\n"
}

// parseKeyValuePairs 解析 "key1=value1 key2=value2 ..." 格式的字符串
// 处理引号的值（可能包含空格）
func (h *coloredTextHandler) parseKeyValuePairs(line string) []string {
	var pairs []string
	var current strings.Builder
	inQuotes := false

	i := 0
	for i < len(line) {
		char := line[i]

		if char == '"' {
			inQuotes = !inQuotes
			current.WriteByte(char)
		} else if char == ' ' && !inQuotes {
			if current.Len() > 0 {
				pairs = append(pairs, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(char)
		}
		i++
	}

	if current.Len() > 0 {
		pairs = append(pairs, current.String())
	}

	return pairs
}

// getLevelColor 根据日志级别返回对应的颜色代码
func (h *coloredTextHandler) getLevelColor(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return ansiDimGray
	case level <= slog.LevelInfo:
		return ansiBrightGreen
	case level <= slog.LevelWarn:
		return ansiBrightYellow
	default:
		return ansiBrightRed
	}
}
