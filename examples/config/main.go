package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ceyewan/genesis/pkg/config"
)

// AppConfig 应用配置
type AppConfig struct {
	Name    string `mapstructure:"name"`
	Version string `mapstructure:"version"`
	Debug   bool   `mapstructure:"debug"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Driver   string `mapstructure:"driver"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Database string `mapstructure:"database"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr string `mapstructure:"addr"`
	DB   int    `mapstructure:"db"`
}

// Config 总配置结构体
type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
}

func main() {
	// 设置环境变量以模拟不同环境
	os.Setenv("GENESIS_ENV", "dev")
	// 模拟通过环境变量覆盖配置
	os.Setenv("GENESIS_DATABASE_HOST", "db.example.com")

	ctx := context.Background()

	fmt.Println("🚀 Starting Config Example...")

	// 1. 初始化配置管理器
	cfgMgr, err := config.New(
		config.WithConfigName("config"),
		config.WithConfigPaths("examples/config"), // 覆盖默认路径，只在 examples/config 查找
		config.WithEnvPrefix("GENESIS"),           // 设置环境变量前缀
	)
	if err != nil {
		log.Fatalf("Failed to create config manager: %v", err)
	}

	// 2. 加载配置
	// 这将按顺序加载：
	// - .env (如果存在)
	// - config.yaml
	// - config.dev.yaml (因为 GENESIS_ENV=dev)
	// - 环境变量 (GENESIS_*)
	if err := cfgMgr.Load(ctx); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	fmt.Println("✅ Configuration loaded successfully")

	// 3. 解析配置到结构体
	var cfg Config
	if err := cfgMgr.Unmarshal(&cfg); err != nil {
		log.Fatalf("Failed to unmarshal config: %v", err)
	}

	// 4. 打印配置展示加载优先级
	printConfig(&cfg)

	// 5. 演示动态获取配置
	fmt.Println("\n🔍 Dynamic Config Access:")
	fmt.Printf("App Name: %v\n", cfgMgr.Get("app.name"))
	fmt.Printf("Redis DB: %v\n", cfgMgr.Get("redis.db"))

	// 6. 演示配置监听 (Watch)
	fmt.Println("\n👀 Starting Watcher...")
	if err := cfgMgr.Start(ctx); err != nil {
		log.Fatalf("Failed to start watcher: %v", err)
	}

	// 监听 app.driver 的变化
	debugCh, err := cfgMgr.Watch(ctx, "app.driver")
	if err != nil {
		log.Fatalf("Failed to watch app.driver: %v", err)
	}
	// 监听 server.port (shadowed)
	portCh, err := cfgMgr.Watch(ctx, "server.port")
	if err != nil {
		log.Fatalf("Failed to watch server.port: %v", err)
	}
	// 监听 app.name (non-shadowed)
	nameCh, err := cfgMgr.Watch(ctx, "app.name")
	if err != nil {
		log.Fatalf("Failed to watch app.name: %v", err)
	}

	go func() {
		fmt.Println("   (Waiting for config changes... Try modifying examples/config/config.yaml)")
		for {
			select {
			case event := <-debugCh:
				printEvent(event)
			case event := <-portCh:
				printEvent(event)
			case event := <-nameCh:
				printEvent(event)
			}
		}
	}()

	// 模拟运行一段时间
	time.Sleep(60 * time.Second)

	// 7. 停止配置管理器
	if err := cfgMgr.Stop(ctx); err != nil {
		log.Printf("Failed to stop config manager: %v", err)
	}
	fmt.Println("\n👋 Config Example Finished")
}

func printEvent(event config.Event) {
	fmt.Printf("\n🔔 Config Changed: %s\n", event.Key)
	fmt.Printf("   Old Value: %v\n", event.OldValue)
	fmt.Printf("   New Value: %v\n", event.Value)
	fmt.Printf("   Source: %s\n", event.Source)
}

func printConfig(cfg *Config) {
	fmt.Println("\n📊 Current Configuration:")
	fmt.Println("--------------------------------------------------")

	fmt.Printf("[App]\n")
	fmt.Printf("  Name:    %s (from config.yaml)\n", cfg.App.Name)
	fmt.Printf("  Version: %s (from .env override)\n", cfg.App.Version)
	fmt.Printf("  Debug:   %v (from config.dev.yaml override)\n", cfg.App.Debug)

	fmt.Printf("\n[Server]\n")
	fmt.Printf("  Port:    %d (from config.dev.yaml override)\n", cfg.Server.Port)
	fmt.Printf("  Host:    %s (from config.yaml)\n", cfg.Server.Host)

	fmt.Printf("\n[Database]\n")
	fmt.Printf("  Host:    %s (from ENV GENESIS_DATABASE_HOST)\n", cfg.Database.Host)
	fmt.Printf("  DB Name: %s (from config.dev.yaml override)\n", cfg.Database.Database)
	fmt.Printf("  Driver:  %s (from config.yaml)\n", cfg.Database.Driver)

	fmt.Printf("\n[Redis]\n")
	fmt.Printf("  Addr:    %s (from config.yaml)\n", cfg.Redis.Addr)
	fmt.Printf("  DB:      %d (from .env override)\n", cfg.Redis.DB)
	fmt.Println("--------------------------------------------------")
}
