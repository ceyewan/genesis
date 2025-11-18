package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
)

// 自定义错误类型
type DatabaseError struct {
	Operation string
	Table     string
	Cause     error
}

func (e DatabaseError) Error() string {
	return fmt.Sprintf("database %s failed on table %s: %v", e.Operation, e.Table, e.Cause)
}

type ValidationError struct {
	Field   string
	Value   any
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error on field %s (value: %v): %s", e.Field, e.Value, e.Message)
}

func main() {
	fmt.Println("=== Advanced clog Examples ===\n")

	// 高级示例1: 完整微服务配置
	advancedMicroserviceExample()

	// 高级示例2: 自定义Context字段提取
	advancedContextFieldsExample()

	// 高级示例3: 性能测试和批量日志
	advancedPerformanceExample()

	// 高级示例4: 文件输出和Source信息
	advancedFileOutputExample()

	// 高级示例5: 动态配置和运行时调整
	advancedDynamicConfigExample()

	// 高级示例6: With方法和预设字段
	advancedWithFieldsExample()

	// 高级示例7: 错误链和堆栈跟踪
	advancedErrorHandlingExample()

	// 高级示例8: 复杂命名空间场景
	advancedNamespaceExample()

	// 高级示例9: SourceRoot 路径裁剪功能
	advancedSourceRootExample()
}

func advancedMicroserviceExample() {
	fmt.Println("--- Advanced Example 1: Complete Microservice Setup ---")

	// 模拟完整的微服务日志配置
	config := &clog.Config{
		Level:       "debug",
		Format:      "json",
		Output:      "stdout",
		AddSource:   true,
		SourceRoot:  "/go/src/app", // 在容器中的典型路径
		EnableColor: false,         // 生产环境通常关闭
	}

	option := &clog.Option{
		NamespaceParts: []string{"order-service"},
		ContextFields: []clog.ContextField{
			{Key: "request_id", FieldName: "request_id", Required: false},
			{Key: "user_id", FieldName: "user_id", Required: false},
			{Key: "trace_id", FieldName: "trace_id", Required: false},
			{Key: "span_id", FieldName: "span_id", Required: false},
			{Key: "tenant_id", FieldName: "tenant_id", Required: false},
		},
		ContextPrefix:   "ctx.",
		NamespaceJoiner: ".",
	}

	logger := clog.Must(config, option)

	// 模拟微服务启动
	logger.Info("microservice starting",
		clog.String("version", "v1.2.3"),
		clog.String("env", "production"),
		clog.Int("port", 8080),
		clog.Any("config", map[string]any{
			"db_pool_size":    10,
			"cache_enabled":   true,
			"metrics_enabled": true,
		}))

	// 模拟请求处理
	ctx := context.Background()
	ctx = context.WithValue(ctx, "request_id", "req-"+generateID())
	ctx = context.WithValue(ctx, "user_id", "user-67890")
	ctx = context.WithValue(ctx, "trace_id", "trace-"+generateID())
	ctx = context.WithValue(ctx, "tenant_id", "tenant-001")

	// Handler层
	handlerLogger := logger.WithNamespace("handler", "orders")
	handlerLogger.InfoContext(ctx, "processing create order request",
		clog.Component("http-handler"),
		clog.String("method", "POST"),
		clog.String("path", "/api/v1/orders"))

	// Service层
	serviceLogger := handlerLogger.WithNamespace("service")
	serviceLogger.InfoContext(ctx, "validating order data",
		clog.Component("business-logic"),
		clog.Any("order_data", map[string]any{
			"product_id": "prod-123",
			"quantity":   2,
			"amount":     99.99,
		}))

	// Repository层
	repoLogger := serviceLogger.WithNamespace("repo")
	repoLogger.InfoContext(ctx, "saving order to database",
		clog.Component("database"),
		clog.String("table", "orders"),
		clog.Duration("query_time", 15*time.Millisecond))

	// Cache层
	cacheLogger := serviceLogger.WithNamespace("cache")
	cacheLogger.InfoContext(ctx, "updating order cache",
		clog.Component("redis"),
		clog.String("key", "order:12345"),
		clog.Duration("cache_time", 2*time.Millisecond))

	fmt.Println()
}

func advancedContextFieldsExample() {
	fmt.Println("--- Advanced Example 2: Custom Context Field Extraction ---")

	// 自定义提取函数示例
	logger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}, &clog.Option{
		ContextFields: []clog.ContextField{
			// 简单提取
			{
				Key:       "simple_field",
				FieldName: "simple",
				Required:  false,
			},
			// 自定义提取函数 - 提取结构体字段
			{
				Key:       "user_info",
				FieldName: "user_name",
				Required:  false,
				Extract: func(val any) (any, bool) {
					if userInfo, ok := val.(map[string]any); ok {
						if name, exists := userInfo["name"]; exists {
							return name, true
						}
					}
					return nil, false
				},
			},
			// 自定义提取函数 - 格式化时间
			{
				Key:       "request_time",
				FieldName: "formatted_time",
				Required:  false,
				Extract: func(val any) (any, bool) {
					if t, ok := val.(time.Time); ok {
						return t.Format("2006-01-02 15:04:05"), true
					}
					return nil, false
				},
			},
		},
		ContextPrefix: "meta.",
	})

	// 创建复杂的Context
	ctx := context.Background()
	ctx = context.WithValue(ctx, "simple_field", "simple_value")
	ctx = context.WithValue(ctx, "user_info", map[string]any{
		"id":   123,
		"name": "Alice Johnson",
		"role": "admin",
	})
	ctx = context.WithValue(ctx, "request_time", time.Now())

	logger.InfoContext(ctx, "processing request with custom context extraction",
		clog.String("action", "user_profile_update"))

	fmt.Println()
}

func advancedPerformanceExample() {
	fmt.Println("--- Advanced Example 3: Performance Testing ---")

	logger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}, &clog.Option{
		NamespaceParts: []string{"perf-test"},
	})

	// 批量日志性能测试
	start := time.Now()
	count := 1000

	fmt.Printf("Writing %d log entries...\n", count)

	for i := 0; i < count; i++ {
		logger.Info("performance test log entry",
			clog.Int("iteration", i),
			clog.String("batch_id", "batch-001"),
			clog.Time("timestamp", time.Now()),
			clog.Duration("elapsed", time.Since(start)))

		// 每100条打印一次进度
		if i%100 == 0 && i > 0 {
			fmt.Printf("Progress: %d/%d entries written\n", i, count)
		}
	}

	elapsed := time.Since(start)
	logger.Info("performance test completed",
		clog.Int("total_entries", count),
		clog.Duration("total_time", elapsed),
		clog.Float64("entries_per_second", float64(count)/elapsed.Seconds()))

	// 测试With方法的性能影响
	fmt.Println("\n--- Testing With() method performance ---")

	baseLogger := logger.WithNamespace("with-test")
	presetLogger := baseLogger.With(
		clog.String("service", "user-api"),
		clog.String("version", "v1.0.0"),
		clog.Component("performance-test"))

	start = time.Now()
	for i := 0; i < 100; i++ {
		presetLogger.Info("test with preset fields", clog.Int("iteration", i))
	}
	elapsedWithPreset := time.Since(start)

	start = time.Now()
	for i := 0; i < 100; i++ {
		baseLogger.Info("test without preset fields",
			clog.Int("iteration", i),
			clog.String("service", "user-api"),
			clog.String("version", "v1.0.0"),
			clog.Component("performance-test"))
	}
	elapsedWithoutPreset := time.Since(start)

	logger.Info("with() method performance comparison",
		clog.Duration("with_preset", elapsedWithPreset),
		clog.Duration("without_preset", elapsedWithoutPreset),
		clog.Float64("improvement_ratio", float64(elapsedWithoutPreset)/float64(elapsedWithPreset)))

	fmt.Println()
}

func advancedFileOutputExample() {
	fmt.Println("--- Advanced Example 4: File Output and Source Info ---")

	// 创建临时日志文件
	logFile := "/tmp/clog_advanced_test.log"

	logger := clog.Must(&clog.Config{
		Level:      "debug",
		Format:     "json",
		Output:     logFile,
		AddSource:  true,
		SourceRoot: "/go/src/app", // 模拟容器环境
	}, &clog.Option{
		NamespaceParts: []string{"file-output-test"},
	})

	logger.Info("logging to file with source information",
		clog.String("log_file", logFile),
		clog.Bool("source_enabled", true))

	// 多层函数调用测试Source信息
	testSourceInfo(logger)

	// 强制刷新缓冲区
	logger.Flush()

	// 读取并显示文件内容
	if content, err := os.ReadFile(logFile); err == nil {
		fmt.Printf("Log file content:\n%s\n", string(content))
	} else {
		fmt.Printf("Error reading log file: %v\n", err)
	}

	// 清理临时文件
	os.Remove(logFile)

	fmt.Println()
}

func testSourceInfo(logger clog.Logger) {
	nestedLogger := logger.WithNamespace("nested-function")
	nestedLogger.Debug("this log shows nested function source info",
		clog.String("function", "testSourceInfo"))

	// 更深层嵌套
	deeperFunction(nestedLogger)
}

func deeperFunction(logger clog.Logger) {
	logger.Warn("deeper nested function call",
		clog.String("function", "deeperFunction"),
		clog.Int("depth", 2))
}

func advancedDynamicConfigExample() {
	fmt.Println("--- Advanced Example 5: Dynamic Configuration ---")

	logger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, &clog.Option{
		NamespaceParts: []string{"dynamic-config"},
	})

	logger.Info("initial configuration - level: INFO")
	logger.Debug("this debug message won't show")
	logger.Warn("this warning will show")

	// 动态调整到DEBUG级别
	fmt.Println("\n--- Changing log level to DEBUG ---")
	if err := logger.SetLevel(clog.DebugLevel); err != nil {
		logger.Error("failed to set log level", clog.Error(err))
	} else {
		logger.Info("log level changed to DEBUG")
	}

	logger.Debug("now debug messages will show")
	logger.Info("info messages still show")

	// 动态调整到ERROR级别
	fmt.Println("\n--- Changing log level to ERROR ---")
	if err := logger.SetLevel(clog.ErrorLevel); err != nil {
		logger.Error("failed to set log level", clog.Error(err))
	} else {
		// 这条消息可能不会显示，因为级别已经是ERROR了
		logger.Error("log level changed to ERROR")
	}

	logger.Debug("debug won't show")
	logger.Info("info won't show")
	logger.Warn("warn won't show")
	logger.Error("only error and fatal will show")

	fmt.Println()
}

func advancedWithFieldsExample() {
	fmt.Println("--- Advanced Example 6: With() Method and Preset Fields ---")

	baseLogger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}, &clog.Option{
		NamespaceParts: []string{"with-fields-test"},
	})

	// 创建带有预设字段的logger
	serviceLogger := baseLogger.With(
		clog.String("service", "user-management"),
		clog.String("version", "v2.1.0"),
		clog.Component("service-layer"))

	serviceLogger.Info("service operation started")

	// 创建更多预设字段的子logger
	requestLogger := serviceLogger.With(
		clog.RequestID("req-"+generateID()),
		clog.UserID("user-12345"))

	requestLogger.Info("processing user request")

	// 再创建更具体的子logger
	dbLogger := requestLogger.With(
		clog.Component("database"),
		clog.String("table", "users"))

	dbLogger.Info("executing database query",
		clog.String("sql", "SELECT * FROM users WHERE id = ?"),
		clog.Duration("query_time", 25*time.Millisecond))

	// 演示字段继承链
	dbLogger.Info("query completed successfully",
		clog.Int("rows_affected", 1),
		clog.Bool("cache_updated", true))

	// 使用Context和预设字段
	ctx := context.WithValue(context.Background(), "trace_id", "trace-"+generateID())

	contextLogger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}, &clog.Option{
		NamespaceParts: []string{"context-with-test"},
		ContextFields: []clog.ContextField{
			{Key: "trace_id", FieldName: "trace_id", Required: false},
		},
	}).With(clog.String("preset_field", "preset_value"))

	contextLogger.InfoContext(ctx, "combining context extraction and preset fields")

	fmt.Println()
}

func advancedErrorHandlingExample() {
	fmt.Println("--- Advanced Example 7: Advanced Error Handling ---")

	logger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}, &clog.Option{
		NamespaceParts: []string{"error-handling"},
	})

	// 模拟错误链
	originalErr := fmt.Errorf("connection timeout")
	dbErr := DatabaseError{
		Operation: "INSERT",
		Table:     "users",
		Cause:     originalErr,
	}

	logger.Error("database operation failed",
		clog.Error(dbErr),
		clog.ErrorWithCode(dbErr, "DB_INSERT_001"),
		clog.String("operation", "create_user"),
		clog.Duration("timeout", 30*time.Second))

	// 验证错误
	validationErr := ValidationError{
		Field:   "email",
		Value:   "invalid-email",
		Message: "must be a valid email address",
	}

	logger.Error("input validation failed",
		clog.Error(validationErr),
		clog.ErrorWithCode(validationErr, "VALIDATION_001"),
		clog.String("user_input", "invalid-email"),
		clog.String("expected_format", "user@domain.com"))

	// 业务逻辑错误
	businessErr := fmt.Errorf("insufficient balance: required %.2f, available %.2f", 100.0, 50.0)
	logger.Error("business rule violation",
		clog.Error(businessErr),
		clog.ErrorWithCode(businessErr, "BUSINESS_001"),
		clog.Float64("required_amount", 100.0),
		clog.Float64("available_balance", 50.0),
		clog.UserID("user-12345"))

	fmt.Println()
}

func advancedNamespaceExample() {
	fmt.Println("--- Advanced Example 8: Complex Namespace Scenarios ---")

	// 模拟大型微服务架构
	logger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}, &clog.Option{
		NamespaceParts:  []string{"e-commerce", "order-service"},
		NamespaceJoiner: "::", // 使用不同的连接符
	})

	logger.Info("order service initialized")

	// API网关层
	gatewayLogger := logger.WithNamespace("gateway")
	gatewayLogger.Info("request received at gateway",
		clog.Component("api-gateway"),
		clog.String("endpoint", "/api/v1/orders"))

	// 认证中间件
	authLogger := gatewayLogger.WithNamespace("middleware", "auth")
	authLogger.Info("authenticating request",
		clog.Component("auth-middleware"),
		clog.String("token_type", "JWT"))

	// 路由到具体handler
	handlerLogger := authLogger.WithNamespace("handler", "create-order")
	handlerLogger.Info("routing to order creation handler",
		clog.Component("http-handler"))

	// 业务逻辑层
	serviceLogger := handlerLogger.WithNamespace("service")
	serviceLogger.Info("executing business logic",
		clog.Component("order-service"))

	// 并行调用多个下游服务

	// 库存服务调用
	inventoryLogger := serviceLogger.WithNamespace("downstream", "inventory")
	inventoryLogger.Info("checking product inventory",
		clog.Component("http-client"),
		clog.String("target_service", "inventory-service"),
		clog.String("product_id", "prod-123"))

	// 用户服务调用
	userLogger := serviceLogger.WithNamespace("downstream", "user")
	userLogger.Info("validating user information",
		clog.Component("http-client"),
		clog.String("target_service", "user-service"),
		clog.UserID("user-456"))

	// 支付服务调用
	paymentLogger := serviceLogger.WithNamespace("downstream", "payment")
	paymentLogger.Info("processing payment",
		clog.Component("http-client"),
		clog.String("target_service", "payment-service"),
		clog.Float64("amount", 199.99))

	// 数据持久化层
	persistenceLogger := serviceLogger.WithNamespace("persistence")

	// 主数据库
	dbLogger := persistenceLogger.WithNamespace("database", "primary")
	dbLogger.Info("saving order to primary database",
		clog.Component("database"),
		clog.String("db_type", "postgresql"),
		clog.String("table", "orders"))

	// 缓存层
	cacheLogger := persistenceLogger.WithNamespace("cache", "redis")
	cacheLogger.Info("caching order data",
		clog.Component("redis"),
		clog.String("cache_key", "order:789"),
		clog.Duration("ttl", 1*time.Hour))

	// 消息队列
	mqLogger := persistenceLogger.WithNamespace("messaging", "kafka")
	mqLogger.Info("publishing order event",
		clog.Component("message-queue"),
		clog.String("topic", "orders.created"),
		clog.String("partition_key", "user-456"))

	// 监控和指标
	metricsLogger := logger.WithNamespace("monitoring", "metrics")
	metricsLogger.Info("recording performance metrics",
		clog.Component("metrics"),
		clog.Duration("total_processing_time", 250*time.Millisecond),
		clog.Int("downstream_calls", 3))

	fmt.Println()
}

func advancedSourceRootExample() {
	fmt.Println("--- Advanced Example 9: SourceRoot Path Trimming ---")

	// 示例1: 默认行为（绝对路径）
	fmt.Println("--- Default Behavior (Absolute Path) ---")
	defaultLogger := clog.Must(&clog.Config{
		Level:     "info",
		Format:    "json",
		Output:    "stdout",
		AddSource: true,
	}, &clog.Option{
		NamespaceParts: []string{"source-root-test"},
	})

	defaultLogger.Info("default behavior shows absolute path")
	defaultLogger.Info("useful for development and debugging",
		clog.String("environment", "development"))

	fmt.Println()

	// 示例2: 使用 genesis 进行路径裁剪
	fmt.Println("--- SourceRoot = \"genesis\" (Path Trimming) ---")
	genesisLogger := clog.Must(&clog.Config{
		Level:      "info",
		Format:     "console",
		Output:     "stdout",
		AddSource:  true,
		SourceRoot: "genesis", // 从 genesis 开始裁剪路径
	}, &clog.Option{
		NamespaceParts: []string{"source-root-test"},
	})

	genesisLogger.Info("genesis SourceRoot trims the path")
	genesisLogger.Info("clean and concise path display",
		clog.String("feature", "path-trimming"),
		clog.String("benefit", "cleaner-logs"))

	fmt.Println()

	// 示例3: 在微服务环境中使用 SourceRoot
	fmt.Println("--- Microservice Environment with SourceRoot ---")
	microserviceLogger := clog.Must(&clog.Config{
		Level:      "info",
		Format:     "json",
		Output:     "stdout",
		AddSource:  true,
		SourceRoot: "genesis", // 适用于容器化环境
	}, &clog.Option{
		NamespaceParts: []string{"user-service", "api"},
		ContextFields: []clog.ContextField{
			{Key: "request_id", FieldName: "request_id", Required: false},
			{Key: "user_id", FieldName: "user_id", Required: false},
		},
		ContextPrefix: "ctx.",
	})

	// 模拟微服务请求处理
	ctx := context.Background()
	ctx = context.WithValue(ctx, "request_id", "req-abc123")
	ctx = context.WithValue(ctx, "user_id", "user-456")

	microserviceLogger.InfoContext(ctx, "processing API request with trimmed paths",
		clog.Component("api-gateway"),
		clog.String("endpoint", "/api/v1/users/profile"),
		clog.String("method", "GET"))

	microserviceLogger.InfoContext(ctx, "database query executed",
		clog.Component("database"),
		clog.String("table", "users"),
		clog.Duration("query_time", 15*time.Millisecond))

	fmt.Println()
}

// 辅助函数
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
}
