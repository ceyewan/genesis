package clog

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
)

// clogHandler 封装 slog.Handler，提供动态级别和 Flush 能力。
type clogHandler struct {
	slog.Handler
	levelVar *slog.LevelVar
}

// newHandler 创建并返回一个适配 clog 配置的 slog.Handler（内部使用）。
//
// 构造顺序：writer -> handler options -> base handler -> (optional) color handler -> wrapper。
func newHandler(config *Config, options *options) (slog.Handler, error) {
	w, err := resolveWriter(config, options)
	if err != nil {
		return nil, err
	}

	levelVar := new(slog.LevelVar)
	levelVar.Set(slogLevelFromConfig(config.Level))

	replaceAttr := newReplaceAttr(config)
	opts := &slog.HandlerOptions{
		AddSource:   config.AddSource,
		Level:       levelVar,
		ReplaceAttr: replaceAttr,
	}

	format := strings.ToLower(config.Format)
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		textFactory := func(writer io.Writer) slog.Handler {
			return slog.NewTextHandler(writer, opts)
		}

		if config.EnableColor {
			handler = newColoredTextHandler(textFactory, w)
		} else {
			handler = textFactory(w)
		}
	}

	return &clogHandler{Handler: handler, levelVar: levelVar}, nil
}

// resolveWriter 根据配置创建输出 writer。
func resolveWriter(config *Config, options *options) (io.Writer, error) {
	switch strings.ToLower(config.Output) {
	case "stdout":
		return os.Stdout, nil
	case "stderr":
		return os.Stderr, nil
	case "buffer":
		if options.buffer != nil {
			return options.buffer, nil
		}
		return nil, fmt.Errorf("buffer output requires options.buffer to be set")
	default:
		f, err := os.OpenFile(config.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
}

// slogLevelFromConfig 将配置的 Level 映射为 slog.Level。
func slogLevelFromConfig(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "fatal":
		return slog.LevelError + 4
	default:
		return slog.LevelInfo
	}
}

// newReplaceAttr 统一处理 Level/Time/Source 等字段。
func newReplaceAttr(config *Config) func(groups []string, a slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		switch a.Key {
		case slog.LevelKey:
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
		case slog.TimeKey:
			if a.Value.Kind() == slog.KindTime {
				a.Value = slog.StringValue(a.Value.Time().Format(timeFormat))
			}
		case slog.SourceKey:
			if source, ok := a.Value.Any().(*slog.Source); ok {
				fileName := trimSourcePath(source.File, config.SourceRoot)
				caller := fmt.Sprintf("%s:%d", fileName, source.Line)
				return slog.String("caller", caller)
			}
		}
		return a
	}
}

// trimSourcePath 根据 sourceRoot 和项目路径裁剪调用文件路径。
func trimSourcePath(fileName, sourceRoot string) string {
	if sourceRoot != "" {
		relPath, err := filepath.Rel(sourceRoot, fileName)
		if err == nil && !strings.HasPrefix(relPath, "..") {
			return relPath
		}
	}
	if idx := strings.Index(fileName, "genesis"); idx != -1 {
		return fileName[idx:]
	}
	return fileName
}

// SetLevel 动态调整日志级别。
func (h *clogHandler) SetLevel(level Level) error {
	var slogLevel slog.Level
	switch level {
	case DebugLevel:
		slogLevel = slog.LevelDebug
	case InfoLevel:
		slogLevel = slog.LevelInfo
	case WarnLevel:
		slogLevel = slog.LevelWarn
	case ErrorLevel:
		slogLevel = slog.LevelError
	case FatalLevel:
		slogLevel = slog.LevelError + 4
	default:
		slogLevel = slog.LevelInfo
	}

	h.levelVar.Set(slogLevel)
	return nil
}

// Flush 强制同步所有缓冲区的日志 (slog 默认是同步的，这里留空)。
func (h *clogHandler) Flush() {
	// No-op for standard slog handlers
}

// ANSI 颜色常量
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m" // 暗淡效果
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m" // Key 颜色，清爽
	ansiWhite   = "\033[37m"
	ansiGray    = "\033[90m" // 深灰，用于分割线和时间
	ansiBgRed   = "\033[41m" // 红底色，用于 Fatal
)

// coloredTextHandler 为 TextHandler 添加彩色支持。
//
// 结构：coloredTextHandler -> textFactory -> slog.TextHandler
// 每次 Handle 时用临时 TextHandler 输出到 buffer，再进行着色。
type coloredTextHandler struct {
	textFactory func(io.Writer) slog.Handler
	writer      io.Writer
	attrs       []slog.Attr
	groups      []string
	mu          *sync.Mutex
}

func newColoredTextHandler(textFactory func(io.Writer) slog.Handler, writer io.Writer) slog.Handler {
	return &coloredTextHandler{
		textFactory: textFactory,
		writer:      writer,
		mu:          &sync.Mutex{},
	}
}

// Enabled 检查日志级别是否启用。
func (h *coloredTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	base := h.baseHandler(io.Discard)
	return base.Enabled(ctx, level)
}

// Handle 处理日志记录，添加颜色输出。
func (h *coloredTextHandler) Handle(ctx context.Context, r slog.Record) error {
	var buf bytes.Buffer

	base := h.baseHandler(&buf)
	if err := base.Handle(ctx, r); err != nil {
		return err
	}

	coloredOutput := h.colorizeOutput(buf.String(), r.Level)

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.writer.Write([]byte(coloredOutput))
	return err
}

// WithAttrs 返回带有附加属性的新 handler。
func (h *coloredTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &coloredTextHandler{
		textFactory: h.textFactory,
		writer:      h.writer,
		attrs:       append(append([]slog.Attr(nil), h.attrs...), attrs...),
		groups:      append([]string(nil), h.groups...),
		mu:          h.mu,
	}
}

// WithGroup 返回带有分组的新 handler。
func (h *coloredTextHandler) WithGroup(name string) slog.Handler {
	return &coloredTextHandler{
		textFactory: h.textFactory,
		writer:      h.writer,
		attrs:       append([]slog.Attr(nil), h.attrs...),
		groups:      append(append([]string(nil), h.groups...), name),
		mu:          h.mu,
	}
}

// baseHandler 构建带 attrs/groups 的基础 TextHandler。
func (h *coloredTextHandler) baseHandler(writer io.Writer) slog.Handler {
	base := h.textFactory(writer)
	if len(h.attrs) > 0 {
		base = base.WithAttrs(h.attrs)
	}
	for _, group := range h.groups {
		base = base.WithGroup(group)
	}
	return base
}

// colorizeOutput 为日志输出添加 ANSI 颜色。
func (h *coloredTextHandler) colorizeOutput(output string, level slog.Level) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return output + "\n"
	}

	fields := h.parseKeyValuePairs(output)
	var sb strings.Builder

	// 临时存储解析出的核心字段
	var (
		timeStr   string
		levelStr  string
		callerStr string
		msgStr    string
		attrs     []string // 剩余的 kv 属性
	)

	// 第一遍扫描：分离核心字段和业务属性
	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]

		switch key {
		case "time":
			// 缩短时间戳，只显示时间部分
			// 原始: 2025-12-24T15:48:17.340+08:00
			// 截取: 15:48:17.340
			if len(val) > 23 {
				timeStr = val[11:23]
			} else {
				timeStr = val
			}
		case "level":
			levelStr = val
		case "caller":
			callerStr = strings.TrimPrefix(val, "genesis/")
		case "msg":
			msgStr = val
		default:
			// 存储业务属性 key=value
			attrs = append(attrs, field)
		}
	}

	// --- 重新组装日志行 (Layout) ---

	// 1. 时间戳 (深灰色，低调)
	if timeStr != "" {
		sb.WriteString(fmt.Sprintf("%s%s%s ", ansiGray, timeStr, ansiReset))
	}

	// 2. 级别 (带颜色，固定宽度对齐)
	levelColor := h.getLevelColor(level)
	paddedLevel := fmt.Sprintf("%-5s", levelStr)
	sb.WriteString(fmt.Sprintf("%s%s%s%s ", ansiBold, levelColor, paddedLevel, ansiReset))

	// 3. 分隔符 (竖线，增加层次感)
	sb.WriteString(fmt.Sprintf("%s|%s ", ansiGray, ansiReset))

	// 4. 调用处 (可选：放在消息前)
	if callerStr != "" {
		sb.WriteString(fmt.Sprintf("%s%s%s ", ansiGray, callerStr, ansiReset))
		sb.WriteString(fmt.Sprintf("%s>%s ", ansiCyan, ansiReset)) // 一个小箭头
	}

	// 5. 消息主体 (最重要！白色高亮)
	sb.WriteString(fmt.Sprintf("%s%s%s ", ansiWhite, msgStr, ansiReset))

	// 6. 业务属性 (放在最后，Key 青色，Value 默认色)
	if len(attrs) > 0 {
		sb.WriteString("\t") // 与消息稍微隔开一点
		for i, attr := range attrs {
			if i > 0 {
				sb.WriteString(" ")
			}
			parts := strings.SplitN(attr, "=", 2)
			k, v := parts[0], parts[1]

			// 格式: Key(青色)=Value(默认)
			sb.WriteString(fmt.Sprintf("%s%s%s%s=%s%s",
				ansiCyan, k, ansiReset,
				ansiGray, ansiReset, v))
		}
	}

	return sb.String() + "\n"
}

// parseKeyValuePairs 解析 "key1=value1 key2=value2 ..." 格式的字符串。
// 处理引号的值（可能包含空格）。
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
				pair := current.String()
				// 处理 key==value 格式，替换为 key=value
				pair = strings.Replace(pair, "==", "=", 1)
				// 移除 %!(EXTRA 等特殊标记
				if idx := strings.Index(pair, "%!(EXTRA"); idx != -1 {
					pair = pair[:idx]
				}
				pairs = append(pairs, pair)
				current.Reset()
			}
		} else {
			current.WriteByte(char)
		}
		i++
	}

	if current.Len() > 0 {
		pair := current.String()
		pair = strings.Replace(pair, "==", "=", 1)
		if idx := strings.Index(pair, "%!(EXTRA"); idx != -1 {
			pair = pair[:idx]
		}
		pairs = append(pairs, pair)
	}

	return pairs
}

// getLevelColor 根据日志级别返回对应的颜色代码。
func (h *coloredTextHandler) getLevelColor(level slog.Level) string {
	switch {
	case level <= slog.LevelDebug:
		return ansiMagenta // Debug 用紫色，显眼但不刺眼
	case level <= slog.LevelInfo:
		return ansiGreen // Info 保持绿色，代表正常
	case level <= slog.LevelWarn:
		return ansiYellow // Warn 黄色
	case level <= slog.LevelError:
		return ansiBold + ansiRed // Error 使用加粗亮红色
	default:
		return ansiBgRed + ansiWhite + ansiBold // Fatal 红底白字
	}
}
