package clog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestNew 测试 Logger 创建
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		opts    []Option
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Level:  "info",
				Format: "console",
				Output: "stdout",
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "invalid level",
			config: &Config{
				Level:  "invalid",
				Format: "console",
				Output: "stdout",
			},
			wantErr: true,
		},
		{
			name: "invalid format",
			config: &Config{
				Level:  "info",
				Format: "invalid",
				Output: "stdout",
			},
			wantErr: true,
		},
		{
			name: "valid config with options",
			config: &Config{
				Level:  "debug",
				Format: "json",
				Output: "stdout",
			},
			opts: []Option{
				WithNamespace("test", "service"),
				WithContextField("trace_id", "trace_id"),
				WithContextField("user_id", "user_id"),
				WithContextField("request_id", "request_id"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.config, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && logger == nil {
				t.Error("New() returned nil logger on success")
			}
		})
	}
}

// TestLoggerLevels 测试日志级别功能
func TestLoggerLevels(t *testing.T) {
	// 创建内存缓冲区捕获输出
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	// 测试所有级别
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 4 {
		t.Errorf("Expected 4 log lines, got %d", len(lines))
	}

	// 验证每行都是有效的 JSON
	for i, line := range lines {
		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
		if logEntry["level"] == nil {
			t.Errorf("Line %d missing level field", i)
		}
		// slog 输出的是大写的级别名称
		level := logEntry["level"].(string)
		expectedLevels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
		if i < len(expectedLevels) && level != expectedLevels[i] {
			t.Errorf("Line %d level = %s, want %s", i, level, expectedLevels[i])
		}
	}
}

// TestLoggerSetLevel 测试动态设置日志级别
func TestLoggerSetLevel(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "info",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	// 记录不同级别的日志
	logger.Debug("debug message") // 不应该显示
	logger.Info("info message")   // 应该显示

	// 设置为 debug 级别
	if err := logger.SetLevel(DebugLevel); err != nil {
		t.Errorf("SetLevel() error = %v", err)
	}

	logger.Debug("debug message after set") // 现在应该显示
	logger.Info("info message after set")   // 应该显示

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 3 {
		t.Errorf("Expected 3 log lines, got %d", len(lines))
	}

	// 第一行应该是 info 级别（debug 被过滤）
	var firstEntry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &firstEntry); err != nil {
		t.Fatalf("Failed to parse first log entry: %v", err)
	}
	if firstEntry["level"] != "INFO" {
		t.Errorf("First log entry should be INFO level, got %v", firstEntry["level"])
	}
}

// TestLoggerFields 测试字段功能
func TestLoggerFields(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	err := errors.New("test error")

	logger.Info("test message",
		String("string_field", "test_value"),
		Int("int_field", 42),
		Float64("float_field", 3.14),
		Bool("bool_field", true),
		Time("time_field", testTime),
		ErrorWithStack(err),
	)

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证扁平字段
	tests := map[string]interface{}{
		"string_field": "test_value",
		"int_field":    float64(42), // JSON 数字都是 float64
		"float_field":  3.14,
		"bool_field":   true,
	}

	for key, expected := range tests {
		if value, ok := logEntry[key]; !ok {
			t.Errorf("Missing field: %s", key)
		} else if value != expected {
			t.Errorf("Field %s = %v, want %v", key, value, expected)
		}
	}

	// 验证时间字段格式
	if timeField, ok := logEntry["time_field"]; !ok {
		t.Error("Missing time_field")
	} else if timeStr, ok := timeField.(string); !ok {
		t.Errorf("time_field is not string: %T", timeField)
	} else if _, err := time.Parse(time.RFC3339Nano, timeStr); err != nil {
		t.Errorf("time_field is not valid RFC3339Nano format: %v", err)
	}

	// ErrorWithStack 现在使用 Group 产生嵌套结构：error={msg="...", type="...", stack="..."}
	errorGroup, ok := logEntry["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected error field to be a group, got %T", logEntry["error"])
	}
	if errorGroup["msg"] != "test error" {
		t.Errorf("error.msg = %v, want test error", errorGroup["msg"])
	}
	if errorGroup["type"] != "*errors.errorString" {
		t.Errorf("error.type = %v, want *errors.errorString", errorGroup["type"])
	}
	if _, ok := errorGroup["stack"]; !ok {
		t.Error("Missing stack field in error group")
	}
}

// 定义 Context 键类型避免冲突
type contextKey string

// TestLoggerWithContext 测试 Context 功能
func TestLoggerWithContext(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	},
		withBuffer(&buf),
		WithContextField(contextKey("trace_id"), "trace_id"),
		WithContextField(contextKey("user_id"), "user_id"),
	)

	ctx := context.Background()
	ctx = context.WithValue(ctx, contextKey("trace_id"), "trace-123")
	ctx = context.WithValue(ctx, contextKey("user_id"), "user-456")

	logger.InfoContext(ctx, "message with context")

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证 Context 字段被提取
	if logEntry["trace_id"] != "trace-123" {
		t.Errorf("Expected trace_id = trace-123, got %v", logEntry["trace_id"])
	}
	if logEntry["user_id"] != "user-456" {
		t.Errorf("Expected user_id = user-456, got %v", logEntry["user_id"])
	}
}

// TestLoggerWithNamespace 测试命名空间功能
func TestLoggerWithNamespace(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	},
		withBuffer(&buf),
		WithNamespace("service"),
	)

	// 测试 WithNamespace
	namespacedLogger := logger.WithNamespace("api", "v1")
	namespacedLogger.Info("namespaced message")

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证命名空间
	namespace, ok := logEntry["namespace"].(string)
	if !ok {
		t.Error("Missing or invalid namespace field")
	} else if namespace != "service.api.v1" {
		t.Errorf("Expected namespace = service.api.v1, got %s", namespace)
	}
}

// TestLoggerWith 测试 With 功能
func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	// 创建带有预设字段的子 Logger
	childLogger := logger.With(
		String("component", "test"),
		Int("version", 1),
	)

	childLogger.Info("message with preset fields")

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证预设字段
	if logEntry["component"] != "test" {
		t.Errorf("Expected component = test, got %v", logEntry["component"])
	}
	if logEntry["version"] != float64(1) {
		t.Errorf("Expected version = 1, got %v", logEntry["version"])
	}
}

func TestLoggerWith_DerivedLoggerDoesNotMutateSiblings(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	// 先构造一个 base，使其 baseAttrs 具备“多余 cap”的可能，
	// 以覆盖“append 复用底层数组导致兄弟 Logger 字段互相污染”的场景。
	base := logger.With(
		String("k1", "v1"),
		String("k2", "v2"),
		String("k3", "v3"),
		String("k4", "v4"),
	).With(String("k5", "v5"))

	a := base.With(String("x", "A"))
	_ = base.With(String("x", "B"))

	a.Info("msg")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("Expected 1 log line, got %d: %q", len(lines), buf.String())
	}

	var logEntry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	if logEntry["x"] != "A" {
		t.Fatalf("Expected x = A, got %v", logEntry["x"])
	}
}

// TestConfigValidation 测试配置验证
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		wantOk bool
	}{
		{
			name: "valid config",
			config: Config{
				Level:  "info",
				Format: "json",
				Output: "stdout",
			},
			wantOk: true,
		},
		{
			name: "invalid level",
			config: Config{
				Level:  "invalid",
				Format: "json",
				Output: "stdout",
			},
			wantOk: false,
		},
		{
			name: "invalid format",
			config: Config{
				Level:  "info",
				Format: "invalid",
				Output: "stdout",
			},
			wantOk: false,
		},
		{
			name: "empty output",
			config: Config{
				Level:  "info",
				Format: "json",
				Output: "",
			},
			wantOk: true, // 现在空输出会设置为默认值 "stdout"
		},
		{
			name: "console format with color",
			config: Config{
				Level:       "info",
				Format:      "console",
				Output:      "stdout",
				EnableColor: true,
			},
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			ok := err == nil
			if ok != tt.wantOk {
				t.Errorf("Config.validate() = %v, wantOk %v", err, tt.wantOk)
			}
		})
	}
}

// TestParseLevel 测试日志级别解析
func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
		wantErr  bool
	}{
		{"debug", DebugLevel, false},
		{"info", InfoLevel, false},
		{"warn", WarnLevel, false},
		{"error", ErrorLevel, false},
		{"fatal", FatalLevel, false},
		{"DEBUG", DebugLevel, false}, // 测试大小写不敏感
		{"Info", InfoLevel, false},
		{"invalid", InfoLevel, true}, // 默认返回 info 级别但报错
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level, err := ParseLevel(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLevel(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && level != tt.expected {
				t.Errorf("ParseLevel(%s) = %v, want %v", tt.input, level, tt.expected)
			}
		})
	}
}

// TestLevelString 测试级别字符串表示
func TestLevelString(t *testing.T) {
	tests := map[Level]string{
		DebugLevel: "debug",
		InfoLevel:  "info",
		WarnLevel:  "warn",
		ErrorLevel: "error",
		FatalLevel: "fatal",
	}

	for level, expected := range tests {
		t.Run(expected, func(t *testing.T) {
			if got := level.String(); got != expected {
				t.Errorf("Level.String() = %v, want %v", got, expected)
			}
		})
	}
}

// TestFieldFunctions 测试字段构造函数
func TestFieldFunctions(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	testTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)

	logger.Info("test fields",
		String("key1", "value1"),
		Int("key2", 42),
		Float64("key3", 3.14),
		Bool("key4", true),
		Time("key5", testTime),
		Any("key6", map[string]string{"nested": "value"}),
	)

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证字段
	tests := map[string]interface{}{
		"key1": "value1",
		"key2": float64(42), // JSON 数字都是 float64
		"key3": 3.14,
		"key4": true,
	}

	for key, expected := range tests {
		if value, ok := logEntry[key]; !ok {
			t.Errorf("Missing field: %s", key)
		} else if value != expected {
			t.Errorf("Field %s = %v, want %v", key, value, expected)
		}
	}

	// 验证时间字段
	if timeField, ok := logEntry["key5"]; !ok {
		t.Error("Missing time field")
	} else if _, ok := timeField.(string); !ok {
		t.Errorf("Time field is not string: %T", timeField)
	}

	// 验证 Any 字段
	if anyField, ok := logEntry["key6"]; !ok {
		t.Error("Missing any field")
	} else if nested, ok := anyField.(map[string]interface{}); !ok {
		t.Errorf("Any field is not map[string]interface{}: %T", anyField)
	} else if nested["nested"] != "value" {
		t.Errorf("Nested value = %v, want value", nested["nested"])
	}
}

// TestErrorField 测试轻量级错误字段
func TestErrorField(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	err := errors.New("test error")
	logger.Error("test error message", Error(err))

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证错误字段 - 只应该包含 err_msg
	if logEntry["err_msg"] != "test error" {
		t.Errorf("err_msg = %v, want test error", logEntry["err_msg"])
	}
	// 确保不包含其他字段
	if _, ok := logEntry["err_type"]; ok {
		t.Error("Error() should not include err_type field")
	}
	if _, ok := logEntry["err_stack"]; ok {
		t.Error("Error() should not include err_stack field")
	}
	if _, ok := logEntry["error"]; ok {
		t.Error("Error() should not include error group field")
	}
}

// TestErrorWithCodeField 测试带错误码的错误字段
func TestErrorWithCodeField(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	err := errors.New("test error")
	logger.Error("test error with code", ErrorWithCode(err, "ERR_001"))

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// ErrorWithCode 使用 Group 产生嵌套结构：error={code="...", msg="..."}
	errorGroup, ok := logEntry["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected error field to be a group, got %T", logEntry["error"])
	}

	// 验证嵌套字段
	if errorGroup["msg"] != "test error" {
		t.Errorf("error.msg = %v, want test error", errorGroup["msg"])
	}
	if errorGroup["code"] != "ERR_001" {
		t.Errorf("error.code = %v, want ERR_001", errorGroup["code"])
	}
	// 确保不包含其他字段
	if _, ok := errorGroup["type"]; ok {
		t.Error("ErrorWithCode() should not include type field")
	}
	if _, ok := errorGroup["stack"]; ok {
		t.Error("ErrorWithCode() should not include stack field")
	}
}

// TestErrorWithStackField 测试带堆栈的错误字段
func TestErrorWithStackField(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	err := errors.New("test error")
	logger.Error("test error with stack", ErrorWithStack(err))

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// ErrorWithStack 使用 Group 产生嵌套结构：error={msg="...", type="...", stack="..."}
	errorGroup, ok := logEntry["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected error field to be a group, got %T", logEntry["error"])
	}

	// 验证嵌套字段
	if errorGroup["msg"] != "test error" {
		t.Errorf("error.msg = %v, want test error", errorGroup["msg"])
	}
	if errorGroup["type"] != "*errors.errorString" {
		t.Errorf("error.type = %v, want *errors.errorString", errorGroup["type"])
	}
	if _, ok := errorGroup["stack"]; !ok {
		t.Error("Missing stack field in error group")
	}
	// 确保不包含错误码
	if _, ok := errorGroup["code"]; ok {
		t.Error("ErrorWithStack() should not include code field")
	}
}

// TestErrorWithCodeStackField 测试带错误码和堆栈的错误字段
func TestErrorWithCodeStackField(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	err := errors.New("test error")
	logger.Error("test error with code and stack", ErrorWithCodeStack(err, "ERR_001"))

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// ErrorWithCodeStack 使用 Group 产生嵌套结构：error={msg="...", type="...", code="...", stack="..."}
	errorGroup, ok := logEntry["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected error field to be a group, got %T", logEntry["error"])
	}

	// 验证嵌套字段
	if errorGroup["msg"] != "test error" {
		t.Errorf("error.msg = %v, want test error", errorGroup["msg"])
	}
	if errorGroup["code"] != "ERR_001" {
		t.Errorf("error.code = %v, want ERR_001", errorGroup["code"])
	}
	if errorGroup["type"] != "*errors.errorString" {
		t.Errorf("error.type = %v, want *errors.errorString", errorGroup["type"])
	}
	if _, ok := errorGroup["stack"]; !ok {
		t.Error("Missing stack field in error group")
	}
}

// TestErrorFieldWithNil 测试 nil 错误处理
func TestErrorFieldWithNil(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "debug",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	logger.Error("nil error", Error(nil))
	logger.Error("nil error with code", ErrorWithCode(nil, "ERR_001"))

	output := buf.String()
	var logEntry1 map[string]interface{}
	var logEntry2 map[string]interface{}

	lines := strings.Split(strings.TrimSpace(output), "\n")

	if err := json.Unmarshal([]byte(lines[0]), &logEntry1); err != nil {
		t.Fatalf("Failed to parse first log entry: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &logEntry2); err != nil {
		t.Fatalf("Failed to parse second log entry: %v", err)
	}

	// Error(nil) 应该返回空的 key="" 字段，不影响日志
	if _, ok := logEntry1["err_msg"]; ok {
		t.Error("Error(nil) should not add err_msg field")
	}

	// ErrorWithCode(nil) 应该只返回 code
	if errGroup, ok := logEntry2["error"].(map[string]interface{}); ok {
		if errGroup["code"] != "ERR_001" {
			t.Errorf("ErrorWithCode(nil) should have code = ERR_001, got %v", errGroup["code"])
		}
		if _, ok := errGroup["msg"]; ok {
			t.Error("ErrorWithCode(nil) should not have msg field")
		}
	}
}

// TestConsoleFormat 测试控制台格式
func TestConsoleFormat(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:       "info",
		Format:      "console",
		Output:      "buffer",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: false, // 关闭颜色以便测试
	}, withBuffer(&buf))

	logger.Info("console message",
		String("key", "value"),
		Int("count", 1),
	)

	output := buf.String()

	// 验证输出包含关键信息
	if !strings.Contains(output, "console message") {
		t.Error("Output doesn't contain message")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("Output doesn't contain field")
	}
	if !strings.Contains(output, "count=1") {
		t.Error("Output doesn't contain count field")
	}
}

// TestAddSource 测试源码位置功能
func TestAddSource(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:     "debug",
		Format:    "json",
		Output:    "buffer",
		AddSource: true,
	}, withBuffer(&buf))

	logger.Debug("message with source")

	output := buf.String()
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &logEntry); err != nil {
		t.Fatalf("Failed to parse log entry: %v", err)
	}

	// 验证源码字段 ( slog 使用 "source" 字段名)
	if _, ok := logEntry["source"]; !ok {
		if _, ok := logEntry["caller"]; !ok {
			t.Error("Missing source or caller field")
		}
	}
}

// TestLoggerFlush 测试 Flush 功能
func TestLoggerFlush(t *testing.T) {
	var buf bytes.Buffer
	logger, _ := New(&Config{
		Level:  "info",
		Format: "json",
		Output: "buffer",
	}, withBuffer(&buf))

	logger.Info("message before flush")
	logger.Flush()

	// Flush 不应该出错，这里主要是确保不会 panic
	output := buf.String()
	if output == "" {
		t.Error("Expected log output after flush")
	}
}
