package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ceyewan/genesis/config"
)

// AppConfig 应用配置结构体
type AppConfig struct {
	App struct {
		Name        string `mapstructure:"name"`
		Version     string `mapstructure:"version"`
		Environment string `mapstructure:"environment"`
		Debug       bool   `mapstructure:"debug"`
	} `mapstructure:"app"`

	MySQL struct {
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
		Database string `mapstructure:"database"`
		Charset  string `mapstructure:"charset"`
		Timeout  string `mapstructure:"timeout"`
	} `mapstructure:"mysql"`

	Redis struct {
		Addr     string `mapstructure:"addr"`
		Password string `mapstructure:"password"`
		DB       int    `mapstructure:"db"`
	} `mapstructure:"redis"`

	Logger struct {
		Level       string `mapstructure:"level"`
		Format      string `mapstructure:"format"`
		Output      string `mapstructure:"output"`
		EnableColor bool   `mapstructure:"enable_color"`
		AddSource   bool   `mapstructure:"add_source"`
	} `mapstructure:"clog"`
}

// MySQL 配置结构体（用于部分解析）
type MySQLConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

func main() {
	fmt.Println("=== Genesis 配置管理系统示例 ===")
	fmt.Println()

	// 示例 1: 基础配置加载 - 演示多源配置加载和优先级
	basicConfigExample()

	// 示例 2: 演示不同的解析用法（全量、结构体、单个字段）
	usageExamples()

	// 示例 3: 配置监听与热更新
	configWatchExample()

	fmt.Println("=== 所有示例演示完成 ===")
}

// basicConfigExample 基础配置加载示例 - 演示多源配置加载和优先级
func basicConfigExample() {
	fmt.Println("=== 示例 1: 多源配置加载与优先级演示 ===")
	fmt.Println()

	// 设置环境变量（最高优先级）- 只设置 app 配置
	os.Setenv("GENESIS_APP_NAME", "Genesis 生产应用")
	os.Setenv("GENESIS_APP_VERSION", "1.0.0-prod")
	os.Setenv("GENESIS_APP_ENVIRONMENT", "production")
	os.Setenv("GENESIS_APP_DEBUG", "false")
	defer func() {
		os.Unsetenv("GENESIS_APP_NAME")
		os.Unsetenv("GENESIS_APP_VERSION")
		os.Unsetenv("GENESIS_APP_ENVIRONMENT")
		os.Unsetenv("GENESIS_APP_DEBUG")
	}()

	// 设置环境变量来加载开发环境配置
	os.Setenv("GENESIS_ENV", "dev")
	defer os.Unsetenv("GENESIS_ENV")

	ctx := context.Background()

	// 创建配置加载器
	loader, err := config.New(&config.Config{
		Name:      "config",
		Paths:     []string{"."},
		FileType:  "yaml",
		EnvPrefix: "GENESIS",
	})
	if err != nil {
		log.Fatalf("创建配置加载器失败: %v", err)
	}

	// 加载配置
	if err := loader.Load(ctx); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 解析到结构体
	var cfg AppConfig
	if err := loader.Unmarshal(&cfg); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}

	// 输出配置信息，展示配置来源分析
	fmt.Printf("✓ 配置加载成功！\n\n")

	fmt.Printf("=== 配置来源分析 ===\n")
	fmt.Printf("按四组配置分别展示：mysql、redis、log、app\n\n")

	fmt.Printf("【APP 配置组】\n")
	fmt.Printf("1. 环境变量 (最高优先级):\n")
	fmt.Printf("   - 应用名称: %s (来自 GENESIS_APP_NAME)\n", cfg.App.Name)
	fmt.Printf("   - 应用版本: %s (来自 GENESIS_APP_VERSION)\n", cfg.App.Version)
	fmt.Printf("   - 应用环境: %s (来自 GENESIS_APP_ENVIRONMENT)\n", cfg.App.Environment)
	fmt.Printf("   - 调试模式: %t (来自 GENESIS_APP_DEBUG)\n\n", cfg.App.Debug)

	fmt.Printf("【LOG 配置组】\n")
	fmt.Printf("1. .env 文件 (高优先级):\n")
	fmt.Printf("   - 日志级别: %s (来自 GENESIS_CLOG_LEVEL)\n", cfg.Logger.Level)
	fmt.Printf("   - 日志格式: %s (来自 GENESIS_CLOG_FORMAT)\n", cfg.Logger.Format)
	fmt.Printf("   - 日志输出: %s (来自 GENESIS_CLOG_OUTPUT)\n", cfg.Logger.Output)
	fmt.Printf("   - 彩色输出: %t (来自 GENESIS_CLOG_ENABLE_COLOR)\n\n", cfg.Logger.EnableColor)

	fmt.Printf("【REDIS 配置组】\n")
	fmt.Printf("1. 环境特定配置 (config.dev.yaml):\n")
	fmt.Printf("   - Redis 地址: %s (来自 config.dev.yaml)\n", cfg.Redis.Addr)
	fmt.Printf("   - Redis DB: %d (来自 config.dev.yaml)\n\n", cfg.Redis.DB)

	fmt.Printf("【MYSQL 配置组】\n")
	fmt.Printf("1. 基础配置 (config.yaml):\n")
	fmt.Printf("   - MySQL 主机: %s (来自 config.yaml)\n", cfg.MySQL.Host)
	fmt.Printf("   - MySQL 端口: %d (来自 config.yaml)\n", cfg.MySQL.Port)
	fmt.Printf("   - MySQL 用户名: %s (来自 config.yaml)\n", cfg.MySQL.Username)
	fmt.Printf("   - MySQL 数据库: %s (来自 config.yaml)\n", cfg.MySQL.Database)
	fmt.Printf("   - MySQL 字符集: %s (来自 config.yaml)\n", cfg.MySQL.Charset)
	fmt.Printf("   - MySQL 超时: %s (来自 config.yaml)\n", cfg.MySQL.Timeout)

	fmt.Printf("\n=== 配置优先级总结 ===\n")
	fmt.Printf("✓ 环境变量: 只设置 APP 配置组\n")
	fmt.Printf("✓ .env 文件: 只设置 LOG 配置组\n")
	fmt.Printf("✓ config.dev.yaml: 只设置 REDIS 配置组\n")
	fmt.Printf("✓ config.yaml: 设置 MYSQL 配置组（及其他默认值）\n")
	fmt.Printf("\n展示了：环境变量 > .env 文件 > 环境特定配置 > 基础配置\n")
	fmt.Println()
}

// usageExamples 演示不同的配置解析用法
func usageExamples() {
	fmt.Println("=== 示例 2: 不同解析用法演示 ===")
	fmt.Println()

	ctx := context.Background()

	loader, err := config.New(&config.Config{
		Name:      "config",
		Paths:     []string{"."},
		FileType:  "yaml",
		EnvPrefix: "GENESIS",
	})
	if err != nil {
		log.Fatalf("创建配置加载器失败: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 用法 1: 全量解析到结构体
	fmt.Println("用法 1: 全量解析到结构体")
	var fullConfig AppConfig
	if err := loader.Unmarshal(&fullConfig); err != nil {
		log.Fatalf("全量解析失败: %v", err)
	}
	fmt.Printf("✓ 应用名称: %s\n", fullConfig.App.Name)
	fmt.Printf("✓ MySQL 主机: %s\n", fullConfig.MySQL.Host)
	fmt.Printf("✓ Redis 地址: %s\n", fullConfig.Redis.Addr)
	fmt.Println()

	// 用法 2: 只解析 MySQL 配置结构体
	fmt.Println("用法 2: 提取特定结构体 (仅 MySQL 配置)")
	var mysqlConfig MySQLConfig
	if err := loader.UnmarshalKey("mysql", &mysqlConfig); err != nil {
		log.Fatalf("MySQL 配置解析失败: %v", err)
	}
	fmt.Printf("✓ MySQL 配置提取成功:\n")
	fmt.Printf("  - 主机: %s\n", mysqlConfig.Host)
	fmt.Printf("  - 端口: %d\n", mysqlConfig.Port)
	fmt.Printf("  - 数据库: %s\n", mysqlConfig.Database)
	fmt.Printf("  - 用户名: %s\n", mysqlConfig.Username)
	fmt.Println()

	// 用法 3: 获取单个字段值
	fmt.Println("用法 3: 获取单个字段值")
	appName := loader.Get("app.name")
	appVersion := loader.Get("app.version")
	mysqlPort := loader.Get("mysql.port")
	redisDb := loader.Get("redis.db")

	fmt.Printf("✓ 应用名称: %v (类型: %T)\n", appName, appName)
	fmt.Printf("✓ 应用版本: %v (类型: %T)\n", appVersion, appVersion)
	fmt.Printf("✓ MySQL 端口: %v (类型: %T)\n", mysqlPort, mysqlPort)
	fmt.Printf("✓ Redis DB: %v (类型: %T)\n", redisDb, redisDb)
	fmt.Println()

	// 用法 4: 检查配置是否存在
	fmt.Println("用法 4: 检查配置项是否存在")
	if loader.Get("mysql.host") != nil {
		fmt.Printf("✓ mysql.host 配置项存在: %v\n", loader.Get("mysql.host"))
	}
	if loader.Get("nonexistent.key") != nil {
		fmt.Printf("✓ nonexistent.key 配置项存在\n")
	} else {
		fmt.Printf("✓ nonexistent.key 配置项不存在 (符合预期)\n")
	}
	fmt.Println()
}

// configWatchExample 配置监听与热更新示例
func configWatchExample() {
	fmt.Println("=== 示例 3: 配置监听与热更新演示 ===")
	fmt.Println()

	// 设置较短的上下文超时，便于演示
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	loader, err := config.New(&config.Config{
		Name:      "config",
		Paths:     []string{"."},
		FileType:  "yaml",
		EnvPrefix: "GENESIS",
	})
	if err != nil {
		log.Fatalf("创建配置加载器失败: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 注意：Load() 只负责加载配置；第一次调用 Watch() 时才会启动文件监听

	// 监听多个配置项的变化
	mysqlHostCh, err := loader.Watch(ctx, "mysql.host")
	if err != nil {
		log.Fatalf("监听 mysql.host 失败: %v", err)
	}

	loggerLevelCh, err := loader.Watch(ctx, "clog.level")
	if err != nil {
		log.Fatalf("监听 clog.level 失败: %v", err)
	}

	appDebugCh, err := loader.Watch(ctx, "app.debug")
	if err != nil {
		log.Fatalf("监听 app.debug 失败: %v", err)
	}

	fmt.Printf("✓ 配置监听服务启动成功\n")
	fmt.Printf("✓ 正在监听以下配置项变化:\n")
	fmt.Printf("  - mysql.host (当前值: %v)\n", loader.Get("mysql.host"))
	fmt.Printf("  - clog.level (当前值: %v)\n", loader.Get("clog.level"))
	fmt.Printf("  - app.debug (当前值: %v)\n", loader.Get("app.debug"))
	fmt.Println()

	fmt.Printf("🔍 监听提示：\n")
	fmt.Printf("请在另一个终端中修改配置文件，然后观察配置变化：\n")
	fmt.Printf("  - 修改 config.yaml 中的 mysql.host 值\n")
	fmt.Printf("  - 修改 config.yaml 中的 clog.level 值\n")
	fmt.Printf("  - 修改 config.yaml 中的 app.debug 值\n")
	fmt.Println()

	// 在 goroutine 中处理配置变化事件
	go func() {
		fmt.Println("开始监听配置变化...")
		for {
			select {
			case event, ok := <-mysqlHostCh:
				if !ok {
					fmt.Println("mysql.host 监听通道已关闭")
					return
				}
				fmt.Printf("🔄 [MySQL] 配置已更新: %s = %v (原值: %v, 来源: %s)\n",
					event.Key, event.Value, event.OldValue, event.Source)
				fmt.Printf("    更新时间: %s\n", event.Timestamp.Format("15:04:05"))
			case event, ok := <-loggerLevelCh:
				if !ok {
					fmt.Println("clog.level 监听通道已关闭")
					return
				}
				fmt.Printf("🔄 [日志] 配置已更新: %s = %v (原值: %v, 来源: %s)\n",
					event.Key, event.Value, event.OldValue, event.Source)
				fmt.Printf("    更新时间: %s\n", event.Timestamp.Format("15:04:05"))
			case event, ok := <-appDebugCh:
				if !ok {
					fmt.Println("app.debug 监听通道已关闭")
					return
				}
				fmt.Printf("🔄 [应用] 配置已更新: %s = %v (原值: %v, 来源: %s)\n",
					event.Key, event.Value, event.OldValue, event.Source)
				fmt.Printf("    更新时间: %s\n", event.Timestamp.Format("15:04:05"))
			case <-ctx.Done():
				fmt.Println("⏰ 配置监听超时 (15秒)")
				return
			}
		}
	}()

	// 等待一段时间让用户可以观察监听效果
	time.Sleep(10 * time.Second)

	fmt.Println("✅ 配置监听演示完成")
	fmt.Println()
}
