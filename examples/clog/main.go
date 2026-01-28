package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/clog"
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
	fmt.Println("=== clog 推荐使用方式演示 ===")

	// 示例1：基础配置 - 推荐的生产环境配置
	basicExample()

	// 示例2：Context 字段提取 - 微服务推荐用法
	contextExample()

	// 示例3：命名空间分层 - 微服务架构推荐
	namespaceExample()

	// 示例4：错误处理 - 推荐的错误日志方式
	errorHandlingExample()

	// 示例5：彩色控制台输出 - 开发环境推荐配置
	coloredExample()

	// 示例6：预设字段 With
	presetFieldsExample()

	// 示例7：动态调整日志级别
	dynamicLevelExample()
}

// basicExample 展示基础配置，推荐用于生产环境
func basicExample() {
	fmt.Println("\n--- 示例1：生产环境推荐配置 ---")

	// 推荐的生产环境配置
	logger, _ := clog.New(&clog.Config{
		Level:     "info",   // 生产环境建议使用 info 或 warn
		Format:    "json",   // 结构化日志，便于日志收集系统处理
		Output:    "stdout", // 容器化环境中输出到 stdout
		AddSource: true,     // 便于问题排查
	})

	logger.Info("服务启动",
		clog.String("version", "v1.0.0"),
		clog.Int("port", 8080),
		clog.String("environment", "production"),
	)
}

// contextExample 展示 Context 字段提取，微服务中的推荐用法
func contextExample() {
	fmt.Println("\n--- 示例2：微服务 Context 传播 ---")

	// 推荐的微服务配置
	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	},
		clog.WithContextField("trace_id", "trace_id"),
		clog.WithContextField("user_id", "user_id"),
		clog.WithContextField("request_id", "request_id"),
	)

	// 模拟微服务调用链
	ctx := context.Background()
	ctx = context.WithValue(ctx, "trace_id", "trace-abc-123")
	ctx = context.WithValue(ctx, "user_id", "user-456")
	ctx = context.WithValue(ctx, "request_id", "req-789")

	// 在处理请求时使用 Context
	logger.InfoContext(ctx, "处理用户请求",
		clog.String("method", "GET"),
		clog.String("path", "/api/users"),
		clog.Duration("duration", 150*time.Millisecond),
	)
}

// namespaceExample 展示命名空间分层，微服务架构推荐用法
func namespaceExample() {
	fmt.Println("\n--- 示例3：微服务命名空间分层 ---")

	// 创建服务级 Logger
	serviceLogger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	},
		clog.WithNamespace("user-service"), // 服务级别命名空间
	)

	// API 层
	apiLogger := serviceLogger.WithNamespace("api")
	apiLogger.Info("创建用户",
		clog.String("email", "user@example.com"),
		clog.String("ip", "192.168.1.100"),
	)

	// Repository 层
	repoLogger := serviceLogger.WithNamespace("repository", "user")
	repoLogger.Info("保存用户到数据库",
		clog.String("table", "users"),
		clog.Duration("query_time", 25*time.Millisecond),
	)
}

// errorHandlingExample 展示推荐的错误处理方式
func errorHandlingExample() {
	fmt.Println("\n--- 示例4：分层错误处理 ---")

	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})

	// 业务错误
	err := ValidationError{
		Field:   "email",
		Message: "格式不正确",
	}

	// 1. 轻量级错误处理 - 适用于大多数业务日志
	logger.Error("用户验证失败",
		clog.Error(err), // 仅包含错误消息
		clog.String("operation", "create_user"),
		clog.String("user_input", "invalid-email"),
		clog.String("client_ip", "192.168.1.100"),
	)

	// 2. 带错误码的业务错误 - 适用于需要错误分类的场景
	logger.Error("数据库连接失败",
		clog.ErrorWithCode(errors.New("connection timeout"), "DB_CONN_001"),
		clog.String("database", "users"),
		clog.Int("retry_count", 3),
		clog.Duration("timeout", 30*time.Second),
	)

	// 3. 详细错误处理 - 适用于需要调试的场景（开发环境使用）
	logger.Error("关键系统错误",
		clog.ErrorWithStack(err), // 包含错误消息、类型和堆栈
		clog.String("component", "payment"),
		clog.String("trace_id", "trace-123"),
	)

	// 4. 最完整的错误信息 - 适用于生产环境的严重错误
	logger.Error("系统级严重错误",
		clog.ErrorWithCodeStack(errors.New("memory allocation failed"), "SYS_001"),
		clog.String("service", "payment"),
		clog.String("version", "v2.1.0"),
	)
}

// coloredExample 展示彩色控制台输出，推荐用于开发环境
func coloredExample() {
	fmt.Println("\n--- 示例5：开发环境彩色输出 ---")

	// 开发环境推荐配置
	logger, _ := clog.New(&clog.Config{
		Level:       "debug",   // 开发环境建议使用 debug
		Format:      "console", // 控制台格式
		Output:      "stdout",  // 输出到标准输出
		EnableColor: true,      // 启用彩色输出
		AddSource:   true,      // 显示调用位置
		SourceRoot:  "genesis", // 裁剪 genesis 前缀
	},
		clog.WithNamespace("my-app"), // 命名空间
	)

	// Debug 级别 - 暗灰色
	logger.Debug("调试信息",
		clog.String("detail", "检查数据库连接"),
		clog.Int("retry", 0),
	)

	// Info 级别 - 亮绿色
	logger.Info("用户登录成功",
		clog.String("user_id", "12345"),
		clog.String("username", "alice"),
		clog.String("ip", "192.168.1.100"),
	)

	// Warn 级别 - 亮黄色
	logger.Warn("响应时间较长",
		clog.Duration("duration", 2500*time.Millisecond),
		clog.String("threshold", "2s"),
	)

	// Error 级别 - 亮红色
	err := errors.New("数据库连接失败")
	logger.Error("操作失败",
		clog.ErrorWithCode(err, "DB_001"),
		clog.String("database", "users"),
	)
}

// presetFieldsExample 展示 With 预设字段用法
func presetFieldsExample() {
	fmt.Println("\n--- 示例6：预设字段 With ---")

	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	},
		clog.WithNamespace("user-service"),
	)

	// 预设公共字段
	base := logger.With(
		clog.String("component", "auth"),
		clog.String("env", "staging"),
	)

	base = base.WithNamespace("auth")

	base.Info("登录请求",
		clog.String("user_id", "u-1001"),
	)

	// 继续派生，叠加更多字段
	requestLogger := base.With(clog.String("request_id", "req-001"))
	requestLogger.Warn("风控命中",
		clog.String("rule", "ip_blacklist"),
	)
}

// dynamicLevelExample 展示动态调整日志级别
func dynamicLevelExample() {
	fmt.Println("\n--- 示例7：动态调整日志级别 ---")

	logger, _ := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		EnableColor: false,
		AddSource:   true,
		SourceRoot:  "genesis",
	})

	// 当前为 info，debug 不输出
	logger.Debug("debug: 这条不会出现")
	logger.Info("info: 默认级别输出")

	fmt.Println(">> 切换为 debug 级别")
	if err := logger.SetLevel(clog.DebugLevel); err != nil {
		fmt.Println("SetLevel failed:", err)
	}

	// 切换后 debug 生效
	logger.Debug("debug: 现在会输出")
	logger.Info("info: 仍然输出")
}
