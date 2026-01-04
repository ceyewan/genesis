// examples/connector/main.go
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/joho/godotenv"
)

// 本示例演示了 Genesis Connector 的标准使用模式：
// 1. 加载配置（环境变量或配置文件）
// 2. 初始化 L0 组件（日志、指标）
// 3. 按照 "New -> defer Close -> Connect" 模式使用连接器
// 4. 获取客户端执行具体操作

func main() {
	ctx := context.Background()

	// 0. 加载环境变量
	// 尝试从当前目录或上级目录加载 .env，不强制要求路径
	if err := godotenv.Load(); err != nil {
		log.Printf("提示: 未能加载 .env 文件，将使用系统环境变量或默认值")
	}

	// 1. 创建基础组件 (Level 0)
	logger := initLogger()
	meter := initMetrics(ctx, logger)
	defer meter.Shutdown(ctx)

	logger.Info("=== Genesis Connector 综合示例演示启动 ===")

	// 2. 演示各种连接器的标准生命周期管理
	// 我们推荐的模式是：先创建（验证配置），再 defer Close（确保释放），最后 Connect（执行连接）

	runRedisExample(ctx, logger, meter)
	runMySQLExample(ctx, logger, meter)
	runSQLiteExample(ctx, logger, meter)
	runEtcdExample(ctx, logger, meter)
	runNATSExample(ctx, logger, meter)
	runKafkaExample(ctx, logger, meter)

	logger.Info("=== 示例演示完成 ===")
	log.Println("指标可在以下地址查看: http://localhost:9092/metrics")
}

// runRedisExample 演示 Redis 连接器的标准用法
func runRedisExample(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("--- [Redis] 示例开始 ---")

	// 1. 准备配置
	cfg := &connector.RedisConfig{
		Name:     "redis-demo",
		Addr:     getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		Password: getEnvOrDefault("REDIS_PASSWORD", ""),
		PoolSize: 10,
	}

	// 2. 创建实例 (Fail-fast: 验证配置)
	conn, err := connector.NewRedis(cfg, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 Redis 连接器失败", clog.Error(err))
		return
	}

	// 3. 注册释放资源 (谁创建谁负责)
	// 在真实的 main 函数中，这通常位于 defer 链的末端
	defer conn.Close()

	// 4. 建立物理连接 (执行网络 I/O)
	if err := conn.Connect(ctx); err != nil {
		logger.Error("Redis 连接失败", clog.Error(err))
		return
	}

	// 5. 获取原生客户端执行操作
	client := conn.GetClient()
	if err := client.Set(ctx, "genesis_demo", "active", time.Minute).Err(); err != nil {
		logger.Warn("Redis 操作失败", clog.Error(err))
	} else {
		logger.Info("Redis SET 成功", clog.String("key", "genesis_demo"))
	}

	// 6. 检查状态
	if conn.IsHealthy() {
		logger.Info("Redis 状态检查: 健康")
	}
}

// runMySQLExample 演示 MySQL 连接器的标准用法
func runMySQLExample(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("--- [MySQL] 示例开始 ---")

	cfg := &connector.MySQLConfig{
		Name:         "mysql-demo",
		Host:         getEnvOrDefault("MYSQL_HOST", "localhost"),
		Port:         getEnvIntOrDefault("MYSQL_PORT", 3306),
		Username:     getEnvOrDefault("MYSQL_USER", "root"),
		Password:     getEnvOrDefault("MYSQL_PASSWORD", "password"),
		Database:     getEnvOrDefault("MYSQL_DATABASE", "genesis_db"),
		MaxIdleConns: 5,
		MaxOpenConns: 20,
	}

	conn, err := connector.NewMySQL(cfg, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 MySQL 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	if err := conn.Connect(ctx); err != nil {
		logger.Error("MySQL 连接失败", clog.Error(err))
		return
	}

	// 使用 GORM 客户端
	db := conn.GetClient()
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil {
		logger.Warn("MySQL 查询失败", clog.Error(err))
	} else {
		logger.Info("MySQL 查询成功", clog.String("version", version))
	}
}

// runEtcdExample 演示 Etcd 连接器的标准用法
func runEtcdExample(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("--- [Etcd] 示例开始 ---")

	cfg := &connector.EtcdConfig{
		Name:        "etcd-demo",
		Endpoints:   []string{getEnvOrDefault("ETCD_ENDPOINTS", "localhost:2379")},
		DialTimeout: 5 * time.Second,
	}

	conn, err := connector.NewEtcd(cfg, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 Etcd 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	if err := conn.Connect(ctx); err != nil {
		logger.Error("Etcd 连接失败", clog.Error(err))
		return
	}

	client := conn.GetClient()
	_, err = client.Put(ctx, "/genesis/status", "online")
	if err != nil {
		logger.Warn("Etcd 操作失败", clog.Error(err))
	} else {
		logger.Info("Etcd PUT 成功")
	}
}

// runNATSExample 演示 NATS 连接器的标准用法
func runNATSExample(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("--- [NATS] 示例开始 ---")

	cfg := &connector.NATSConfig{
		Name:    "nats-demo",
		URL:     getEnvOrDefault("NATS_URL", "nats://localhost:4222"),
		Timeout: 5 * time.Second,
	}

	conn, err := connector.NewNATS(cfg, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 NATS 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	if err := conn.Connect(ctx); err != nil {
		logger.Error("NATS 连接失败", clog.Error(err))
		return
	}

	client := conn.GetClient()
	logger.Info("NATS 连接状态", clog.String("status", client.Status().String()))
}

// runKafkaExample 演示 Kafka 连接器的标准用法
func runKafkaExample(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("--- [Kafka] 示例开始 ---")

	cfg := &connector.KafkaConfig{
		Name:           "kafka-demo",
		Seed:           []string{getEnvOrDefault("KAFKA_BROKERS", "127.0.0.1:9092")},
		ConnectTimeout: 10 * time.Second,
		RequestTimeout: 5 * time.Second,
	}

	conn, err := connector.NewKafka(cfg, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 Kafka 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	if err := conn.Connect(ctx); err != nil {
		logger.Error("Kafka 连接失败", clog.Error(err))
		return
	}

	logger.Info("Kafka 连接成功", clog.String("brokers", getEnvOrDefault("KAFKA_BROKERS", "localhost:9092")))
}

// runSQLiteExample 演示 SQLite 连接器的标准用法
func runSQLiteExample(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("--- [SQLite] 示例开始 ---")

	// 使用内存数据库进行演示
	cfg := &connector.SQLiteConfig{
		Path: "file::memory:?cache=shared",
	}

	conn, err := connector.NewSQLite(cfg, connector.WithLogger(logger))
	if err != nil {
		logger.Error("创建 SQLite 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	if err := conn.Connect(ctx); err != nil {
		logger.Error("SQLite 连接失败", clog.Error(err))
		return
	}

	// 使用 GORM 客户端
	db := conn.GetClient()

	// 创建示例表
	if err := db.Exec("CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT)").Error; err != nil {
		logger.Warn("创建表失败", clog.Error(err))
		return
	}

	// 插入数据
	if err := db.Exec("INSERT INTO users (name) VALUES (?)", "Genesis User").Error; err != nil {
		logger.Warn("插入数据失败", clog.Error(err))
		return
	}

	// 查询数据
	var name string
	if err := db.Raw("SELECT name FROM users WHERE id = 1").Scan(&name).Error; err != nil {
		logger.Warn("查询数据失败", clog.Error(err))
	} else {
		logger.Info("SQLite 查询成功", clog.String("name", name))
	}
}

// --- 辅助初始化函数 ---

func initLogger() clog.Logger {
	l, err := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		EnableColor: true,
	})
	if err != nil {
		log.Fatalf("初始化日志组件失败: %v", err)
	}
	return l
}

func initMetrics(_ context.Context, logger clog.Logger) metrics.Meter {
	m, err := metrics.New(&metrics.Config{
		ServiceName: "connector-example",
		Version:     "1.0.0",
		Port:        9092,
		Path:        "/metrics",
	})
	if err != nil {
		logger.Error("初始化指标组件失败", clog.Error(err))
		os.Exit(1)
	}
	return m
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
