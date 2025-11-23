package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/config"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/dlock"
)

// AppConfig 应用总配置
type AppConfig struct {
	App   AppSection   `mapstructure:"app"`
	Log   LogSection   `mapstructure:"log"`
	Redis RedisSection `mapstructure:"redis"`
	DLock DLockSection `mapstructure:"dlock"`
}

type AppSection struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
}

type LogSection struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

type RedisSection struct {
	Addr     string        `mapstructure:"addr"`
	Password string        `mapstructure:"password"`
	DB       int           `mapstructure:"db"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

type DLockSection struct {
	Prefix     string        `mapstructure:"prefix"`
	DefaultTTL time.Duration `mapstructure:"default_ttl"`
}

func main() {
	fmt.Println("=== Config + Container 集成示例 ===\n")

	ctx := context.Background()

	// ========================================
	// 阶段 1: Bootstrap - 在 Container 之外初始化配置
	// ========================================
	fmt.Println("📋 阶段 1: 加载配置...")

	// 1.1 创建配置管理器
	cfgMgr, err := config.New(
		config.WithConfigName("config"),
		config.WithConfigPaths("examples/config-with-container"),
		config.WithEnvPrefix("GENESIS"),
	)
	if err != nil {
		log.Fatalf("创建配置管理器失败: %v", err)
	}

	// 1.2 加载配置
	if err := cfgMgr.Load(ctx); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 1.3 解析到强类型结构体
	var appCfg AppConfig
	if err := cfgMgr.Unmarshal(&appCfg); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}

	fmt.Printf("✓ 配置加载成功: %s v%s\n\n", appCfg.App.Name, appCfg.App.Version)

	// ========================================
	// 阶段 2: 创建应用级 Logger
	// ========================================
	fmt.Println("📝 阶段 2: 初始化 Logger...")

	logConfig := &clog.Config{
		Level:  appCfg.Log.Level,
		Format: appCfg.Log.Format,
		Output: appCfg.Log.Output,
	}
	appLogger, err := clog.New(logConfig, &clog.Option{
		NamespaceParts: []string{appCfg.App.Name},
	})
	if err != nil {
		log.Fatalf("创建 Logger 失败: %v", err)
	}

	fmt.Printf("✓ Logger 初始化成功\n\n")

	// ========================================
	// 阶段 3: 创建 Container
	// ========================================
	fmt.Println("🏗️  阶段 3: 创建 Container...")

	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        appCfg.Redis.Addr,
			Password:    appCfg.Redis.Password,
			DB:          appCfg.Redis.DB,
			DialTimeout: appCfg.Redis.Timeout,
		},
		DLock: &dlock.Config{
			Backend:    dlock.BackendRedis,
			Prefix:     appCfg.DLock.Prefix,
			DefaultTTL: appCfg.DLock.DefaultTTL,
		},
	}

	app, err := container.New(containerCfg, container.WithLogger(appLogger))
	if err != nil {
		log.Fatalf("创建 Container 失败: %v", err)
	}
	defer app.Close()

	fmt.Printf("✓ Container 创建成功\n\n")

	// ========================================
	// 阶段 4: 注册 ConfigManager 到 Container
	// ========================================
	fmt.Println("🔗 阶段 4: 注册 ConfigManager...")

	app.RegisterConfigManager(cfgMgr)
	fmt.Printf("✓ ConfigManager 已注册到 Container\n\n")

	// ========================================
	// 阶段 5: 启动 Container (会自动启动 ConfigManager)
	// ========================================
	fmt.Println("🚀 阶段 5: 启动 Container...")

	if err := app.Start(ctx); err != nil {
		log.Fatalf("启动 Container 失败: %v", err)
	}

	fmt.Printf("✓ Container 启动成功 (ConfigManager 的 Watch 已启动)\n\n")

	// ========================================
	// 阶段 6: 使用组件
	// ========================================
	fmt.Println("💼 阶段 6: 使用组件...")

	if app.DLock != nil {
		fmt.Println("✓ DLock 组件可用")
	}

	fmt.Println()
	fmt.Println("✅ 所有阶段完成!")
	fmt.Println("   - Config 在 Container 之外加载")
	fmt.Println("   - Logger 通过 Option 注入")
	fmt.Println("   - ConfigManager 由 Container 托管生命周期")
	fmt.Println()
}
