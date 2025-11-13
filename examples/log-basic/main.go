package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	clog "github.com/ceyewan/genesis/pkg/log"
)

func main() {
	fmt.Println("=== clog 快速上手指南 ===")
	fmt.Println("本指南演示 clog 的核心功能和使用方法，帮助您快速上手")

	// 清理之前的日志文件
	cleanupLogs()

	// 示例1: 环境相关配置
	fmt.Println("\n📋 示例1: 环境相关配置")
	demoEnvironmentConfigs()

	// 示例2: 基础日志记录
	fmt.Println("\n📋 示例2: 基础日志记录")
	demoBasicLogging()

	// 示例3: 层次化命名空间
	fmt.Println("\n📋 示例3: 层次化命名空间系统")
	demoHierarchicalNamespaces()

	// 示例4: 上下文感知日志
	fmt.Println("\n📋 示例4: 上下文感知与链路追踪")
	demoContextualLogging()

	// 示例5: Options 模式
	fmt.Println("\n📋 示例5: Options 配置模式")
	demoOptionsPattern()

	fmt.Println("\n✅ 快速上手指南完成！")
	fmt.Println("💡 提示: 查看 examples/advanced/main.go 了解更高级的功能")
}

// cleanupLogs 清理日志文件
func cleanupLogs() {
	logDirs := []string{"logs", "output"}
	for _, dir := range logDirs {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("清理目录 %s 失败: %v\n", dir, err)
		}
	}
}

// demoEnvironmentConfigs 演示环境相关配置
func demoEnvironmentConfigs() {
	fmt.Println("🔧 开发环境配置:")
	devConfig := clog.GetDefaultConfig("development")
	fmt.Printf("   级别: %s, 格式: %s, 颜色: %t\n", devConfig.Level, devConfig.Format, devConfig.EnableColor)

	fmt.Println("🏭 生产环境配置:")
	prodConfig := clog.GetDefaultConfig("production")
	fmt.Printf("   级别: %s, 格式: %s, 颜色: %t\n", prodConfig.Level, prodConfig.Format, prodConfig.EnableColor)

	// 使用开发配置初始化
	if err := clog.Init(context.Background(), devConfig); err != nil {
		fmt.Printf("❌ 初始化失败: %v\n", err)
		return
	}
	clog.Info("✅ 使用开发环境配置初始化成功")
}

// demoBasicLogging 演示基础日志记录功能
func demoBasicLogging() {
	// 演示不同日志级别
	clog.Debug("🔍 这是调试信息，通常只在开发环境显示")
	clog.Info("ℹ️ 这是信息级别的日志，记录常规操作")
	clog.Warn("⚠️ 这是警告信息，表示需要注意但不影响运行")
	clog.Error("❌ 这是错误信息，记录异常情况")

	// 演示结构化字段
	clog.Info("用户登录",
		clog.String("user_id", "12345"),
		clog.String("email", "user@example.com"),
		clog.Int("login_count", 5),
	)

	// 演示错误处理
	err := errors.New("数据库连接超时")
	clog.Error("操作失败",
		clog.Err(err),
		clog.String("operation", "database_query"),
		clog.Duration("timeout", 5*time.Second),
	)
}

// demoHierarchicalNamespaces 演示层次化命名空间系统
func demoHierarchicalNamespaces() {
	// 创建模块级别的命名空间
	userLogger := clog.Namespace("user")
	orderLogger := clog.Namespace("order")
	paymentLogger := clog.Namespace("payment")

	// 使用模块日志器
	userLogger.Info("用户模块启动")
	orderLogger.Info("订单模块启动")
	paymentLogger.Info("支付模块启动")

	// 创建子模块命名空间
	authLogger := userLogger.Namespace("auth")
	dbLogger := userLogger.Namespace("database")
	processorLogger := paymentLogger.Namespace("processor")

	// 使用子模块日志器
	authLogger.Info("用户认证检查", clog.String("user_id", "12345"))
	dbLogger.Info("查询用户信息", clog.String("email", "user@example.com"))
	processorLogger.Info("处理支付请求", clog.String("order_id", "ORDER-001"))

	// 链式创建深层命名空间
	stripeProcessor := paymentLogger.Namespace("processor").Namespace("stripe")
	stripeProcessor.Info("调用 Stripe API", clog.String("amount", "99.99"))
}

// demoContextualLogging 演示上下文感知日志和链路追踪
func demoContextualLogging() {
	// 模拟请求处理场景
	traceID := "req-123456"
	ctx := clog.WithTraceID(context.Background(), traceID)

	// 从上下文获取带链路追踪的日志器
	logger := clog.WithContext(ctx)

	// 记录请求处理过程
	logger.Info("开始处理用户请求",
		clog.String("method", "POST"),
		clog.String("path", "/api/users"),
	)

	// 模拟业务处理
	processUserRequest(ctx)

	logger.Info("请求处理完成")
}

// processUserRequest 模拟用户请求处理
func processUserRequest(ctx context.Context) {
	// 获取带链路追踪的日志器
	logger := clog.WithContext(ctx).Namespace("service")

	logger.Info("开始用户注册流程",
		clog.String("email", "newuser@example.com"),
	)

	// 验证步骤
	validationLogger := logger.Namespace("validation")
	validationLogger.Info("验证用户数据")

	// 数据库操作
	dbLogger := logger.Namespace("database")
	dbLogger.Info("保存用户到数据库")

	// 发送邮件
	emailLogger := logger.Namespace("email")
	emailLogger.Info("发送确认邮件")

	logger.Info("用户注册流程完成")
}

// demoOptionsPattern 演示 Options 配置模式
func demoOptionsPattern() {
	fmt.Println("🎯 Options 模式演示:")

	// 1. 使用 WithNamespace 初始化全局 logger
	fmt.Println("   1. 全局 logger + 命名空间配置:")
	config1 := clog.GetDefaultConfig("production")

	if err := clog.Init(context.Background(), config1, clog.WithNamespace("api-gateway")); err != nil {
		fmt.Printf("   ❌ 初始化失败: %v\n", err)
		return
	}

	clog.Info("✅ API 网关服务启动")
	clog.Namespace("auth").Info("认证模块初始化")
	clog.Namespace("user").Namespace("profile").Info("用户资料模块初始化")

	// 2. 创建独立的 logger 实例
	fmt.Println("   2. 独立 logger 实例:")
	config2 := &clog.Config{
		Level:     "debug",
		Format:    "json",
		Output:    "stdout",
		AddSource: true,
	}

	// 创建专用的支付服务 logger
	paymentLogger, err := clog.New(context.Background(), config2,
		clog.WithNamespace("payment-service"),
	)
	if err != nil {
		fmt.Printf("   ❌ 创建 logger 失败: %v\n", err)
		return
	}

	paymentLogger.Info("支付服务初始化")
	paymentLogger.Namespace("processor").Info("支付处理器启动")

	// 3. 演示不同输出格式
	fmt.Println("   3. 文件输出演示:")
	demoFileOutput()
}

// demoFileOutput 演示文件输出功能
func demoFileOutput() {
	// 创建输出目录
	outputDir := "output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("   ❌ 创建输出目录失败: %v\n", err)
		return
	}

	// 配置文件输出
	config := &clog.Config{
		Level:     "info",
		Format:    "json",
		Output:    filepath.Join(outputDir, "getting-started.log"),
		AddSource: true,
	}

	fileLogger, err := clog.New(context.Background(), config,
		clog.WithNamespace("file-demo"),
	)
	if err != nil {
		fmt.Printf("   ❌ 创建文件 logger 失败: %v\n", err)
		return
	}

	fileLogger.Info("文件输出测试",
		clog.String("filename", "getting-started.log"),
		clog.String("status", "success"),
	)

	fmt.Printf("   ✅ 日志已写入到: %s\n", config.Output)
}
