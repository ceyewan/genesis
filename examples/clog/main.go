package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
)

// 自定义错误类型
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation failed on %s: %s", e.Field, e.Message)
}

func main() {
	fmt.Println("=== Basic clog Examples ===")

	// 示例1: 默认配置
	basicDefaultExample()

	// 示例2: JSON 格式输出
	basicJSONExample()

	// 示例3: Console 格式输出
	basicConsoleExample()

	// 示例4: 不同日志级别
	basicLevelExample()

	// 示例5: 字段使用
	basicFieldsExample()

	// 示例6: 错误处理
	basicErrorExample()

	// 示例7: Context 使用
	basicContextExample()

	// 示例8: 命名空间使用
	basicNamespaceExample()

	// 示例9: Component vs Namespace
	componentVsNamespaceExample()

	// 示例10: SourceRoot 功能 - 默认行为（绝对路径）
	sourceRootDefaultExample()

	// 示例11: SourceRoot 功能 - genesis 路径裁剪
	sourceRootGenesisExample()
}

func basicDefaultExample() {
	fmt.Println("--- Example 1: Default Configuration ---")

	logger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, nil)

	logger.Info("application started with default configuration")
	logger.Debug("this debug message won't show (default level is info)")
	logger.Warn("this is a warning message")
	logger.Error("this is an error message")

	fmt.Println()
}

func basicJSONExample() {
	fmt.Println("--- Example 2: JSON Format ---")

	logger := clog.Must(&clog.Config{
		Level:     "debug",
		Format:    "json",
		Output:    "stdout",
		AddSource: true,
	}, nil)

	logger.Debug("debug message in JSON format")
	logger.Info("info message in JSON format",
		clog.String("service", "user-api"),
		clog.Int("port", 8080))

	fmt.Println()
}

func basicConsoleExample() {
	fmt.Println("--- Example 3: Console Format with Colors ---")

	logger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, nil)

	logger.Info("info message in console format")
	logger.Warn("warning message with colors",
		clog.String("component", "auth"),
		clog.Bool("retry", true))
	logger.Error("error message in red color")

	fmt.Println()
}

func basicLevelExample() {
	fmt.Println("--- Example 4: Different Log Levels (DEBUG) ---")

	logger := clog.Must(&clog.Config{
		Level:       "debug",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, nil)

	logger.Debug("debug level message")
	logger.Info("info level message")
	logger.Warn("warn level message")
	logger.Error("error level message")

	// 动态调整级别
	fmt.Println("\n--- After setting level to WARN ---")
	logger.SetLevel(clog.WarnLevel)

	logger.Debug("this debug won't show")
	logger.Info("this info won't show")
	logger.Warn("this warn will show")
	logger.Error("this error will show")

	fmt.Println()
}

func basicFieldsExample() {
	fmt.Println("--- Example 5: Various Field Types (Console) ---")

	logger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, nil)

	// 基础类型字段
	logger.Info("basic field types",
		clog.String("str_field", "hello world"),
		clog.Int("int_field", 42),
		clog.Int64("int64_field", 1234567890),
		clog.Float64("float_field", 3.14159),
		clog.Bool("bool_field", true),
		clog.Duration("duration_field", 5*time.Second),
		clog.Time("time_field", time.Now()),
	)

	// 常用语义字段
	logger.Info("semantic fields",
		clog.RequestID("req-123456"),
		clog.UserID("user-789"),
		clog.TraceID("trace-abc"),
		clog.Component("database"),
	)

	// Any 字段（复杂对象）
	user := map[string]any{
		"id":   123,
		"name": "Alice",
		"age":  30,
	}
	logger.Info("complex object", clog.Any("user", user))

	fmt.Println()
}

func basicErrorExample() {
	fmt.Println("--- Example 6: Error Handling (Console) ---")

	logger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, nil)

	// 简单错误
	err := errors.New("database connection failed")
	logger.Error("operation failed", clog.Error(err))

	// 带错误码的错误
	logger.Error("business logic error",
		clog.ErrorWithCode(err, "DB_CONN_001"),
		clog.String("operation", "user_query"))

	validationErr := ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	logger.Error("validation failed",
		clog.Error(validationErr),
		clog.String("user_input", "invalid@"))

	fmt.Println()
}

func basicContextExample() {
	fmt.Println("--- Example 7: Context Integration (Console) ---")

	logger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, &clog.Option{
		ContextFields: []clog.ContextField{
			{
				Key:       "request_id",
				FieldName: "request_id",
				Required:  false,
			},
			{
				Key:       "user_id",
				FieldName: "user_id",
				Required:  false,
			},
			{
				Key:       "trace_id",
				FieldName: "trace_id",
				Required:  false,
			},
		},
		ContextPrefix: "ctx.",
	})

	// 创建带有Context数据的Context
	ctx := context.Background()
	ctx = context.WithValue(ctx, "request_id", "req-12345")
	ctx = context.WithValue(ctx, "user_id", "user-67890")
	ctx = context.WithValue(ctx, "trace_id", "trace-abcde")

	// 使用Context方法，自动提取Context字段
	logger.InfoContext(ctx, "user login successful",
		clog.String("method", "password"),
		clog.Duration("duration", 150*time.Millisecond))

	// 普通方法不会提取Context字段
	logger.Info("user login successful (no context)",
		clog.String("method", "password"))

	fmt.Println()
}

func basicNamespaceExample() {
	fmt.Println("--- Example 8: Namespace Usage (Console) ---")

	// 主服务logger
	mainLogger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, &clog.Option{
		NamespaceParts:  []string{"user-service"},
		NamespaceJoiner: ".",
	})

	mainLogger.Info("main service started")

	// Handler层
	handlerLogger := mainLogger.WithNamespace("handler")
	handlerLogger.Info("handling user request")

	// 更深层级
	authLogger := handlerLogger.WithNamespace("auth")
	authLogger.Info("authenticating user", clog.UserID("123"))

	// Repository层
	repoLogger := mainLogger.WithNamespace("repo", "user")
	repoLogger.Info("querying user database",
		clog.String("query", "SELECT * FROM users WHERE id = ?"))

	fmt.Println()
}

func componentVsNamespaceExample() {
	fmt.Println("--- Example 9: Component vs Namespace (Console) ---")

	// 创建主服务logger
	serviceLogger := clog.Must(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, &clog.Option{
		NamespaceParts: []string{"user-service", "handler"},
	})

	// 在handler中使用数据库组件
	serviceLogger.Info("querying user data",
		clog.Component("database"),
		clog.String("table", "users"),
		clog.String("operation", "SELECT"))

	// 使用Redis缓存
	cacheLogger := serviceLogger.WithNamespace("cache")
	cacheLogger.Info("caching user data",
		clog.Component("redis"),
		clog.String("key", "user:123"),
		clog.Duration("ttl", 1*time.Hour))

	// 发送消息队列
	cacheLogger.Info("publishing user event",
		clog.Component("message-queue"),
		clog.String("topic", "user.updated"),
		clog.Any("payload", map[string]any{
			"user_id": 123,
			"action":  "profile_updated",
		}))

	fmt.Println()
}

func sourceRootDefaultExample() {
	fmt.Println("--- Example 10: SourceRoot = nil (Absolute Path - Console) ---")

	logger := clog.Must(&clog.Config{
		Level:       "debug",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		EnableColor: true,
	}, nil)

	logger.Debug("debug with absolute path")
	logger.Info("info  with absolute path", clog.String("module", "test"))
	logger.Warn("warn  with absolute path")

	fmt.Println()
}

func sourceRootGenesisExample() {
	fmt.Println("--- Example 11: SourceRoot = \"genesis\" (Path Trimming) ---")

	logger := clog.Must(&clog.Config{
		Level:       "debug",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "genesis",
		EnableColor: true,
	}, nil)

	logger.Debug("debug path from genesis")
	logger.Info("info  path from genesis", clog.String("component", "source-root"))
	logger.Warn("warn  path from genesis", clog.String("feature", "path-trimming"))
	logger.Error("error path from genesis")

	fmt.Println()
}
