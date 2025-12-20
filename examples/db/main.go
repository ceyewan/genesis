package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault 获取环境变量并转换为 int，如果不存在或转换失败则返回默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// Order 订单模型（分片表）
type Order struct {
	ID        uint64    `gorm:"primaryKey"`
	UserID    int64     `gorm:"index"` // 分片键
	ProductID int64     `gorm:"index"`
	Amount    float64   `gorm:"type:decimal(10,2)"`
	Status    string    `gorm:"type:varchar(50)"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// Product 产品模型（非分片表）
type Product struct {
	ID    uint64  `gorm:"primaryKey"`
	Name  string  `gorm:"type:varchar(100)"`
	Price float64 `gorm:"type:decimal(10,2)"`
}

func main() {
	fmt.Println("=== Genesis DB Component Example (Go Native DI) ===")

	// 0. 加载环境变量（从根目录）
	if err := godotenv.Load("/Users/ceyewan/CodeField/genesis/.env"); err != nil {
		log.Printf("Warning: could not load .env file: %v", err)
	}

	// 1. 初始化连接器和组件
	mysqlConn, database := initComponents()
	if mysqlConn == nil || database == nil {
		fmt.Println("Example exited due to missing MySQL connection")
		return
	}
	defer mysqlConn.Close()

	// 2. 自动迁移表结构
	migrateTables(database)

	// 3. 演示：插入分片数据
	demoInsertShardingData(database)

	// 4. 演示：查询分片数据
	demoQueryShardingData(database)

	// 5. 演示：事务操作
	demoTransaction(database)

	// 6. 演示：错误处理
	demoErrorHandling(database)
}

func initComponents() (connector.MySQLConnector, db.DB) {
	fmt.Println("\n--- 1. Initializing Components (Go Native DI) ---")

	// 1. 初始化 Logger
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout", // 添加输出配置
	}, &clog.Option{})
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	// 2. 创建 MySQL 连接器
	mysqlConn, err := connector.NewMySQL(&connector.MySQLConfig{
		BaseConfig: connector.BaseConfig{
			Name: "mysql-db-example",
		},
		Host:         getEnvOrDefault("MYSQL_HOST", "localhost"),
		Port:         getEnvIntOrDefault("MYSQL_PORT", 3306),
		Username:     getEnvOrDefault("MYSQL_USER", "root"),
		Password:     getEnvOrDefault("MYSQL_PASSWORD", "password"),
		Database:     getEnvOrDefault("MYSQL_DATABASE", "genesis_db"),
		Charset:      "utf8mb4",
		Timeout:      10 * time.Second,
		MaxIdleConns: 10,
		MaxOpenConns: 100,
		MaxLifetime:  time.Hour,
	}, connector.WithLogger(logger))
	if err != nil {
		fmt.Printf("⚠️  MySQL connector creation failed (expected if MySQL is not running): %v\n", err)
		fmt.Printf("💡 To run this example, please:\n")
		fmt.Printf("   1. Start MySQL server\n")
		fmt.Printf("   2. Create database 'genesis_db'\n")
		fmt.Printf("   3. Set environment variables (MYSQL_HOST, MYSQL_PASSWORD, etc.)\n")
		fmt.Printf("   4. Run this example again\n")
		return nil, nil // 返回 nil，让 main 函数正常退出
	}

	// 3. 连接到数据库
	if err := mysqlConn.Connect(context.Background()); err != nil {
		log.Fatalf("failed to connect to mysql: %v", err)
	}

	// 4. 创建 DB 组件
	database, err := db.New(mysqlConn, &db.Config{
		EnableSharding: true,
		ShardingRules: []db.ShardingRule{
			{
				ShardingKey:    "user_id",
				NumberOfShards: 64, // 将创建 orders_00 到 orders_63 共 64 张表
				Tables:         []string{"orders"},
			},
		},
	}, db.WithLogger(logger))
	if err != nil {
		log.Fatalf("failed to create db component: %v", err)
	}

	fmt.Println("Components initialized successfully")
	return mysqlConn, database
}

func migrateTables(database db.DB) {
	fmt.Println("\n--- 2. Migrating Tables ---")
	ctx := context.Background()
	gormDB := database.DB(ctx)

	// gorm.io/sharding 会自动拦截 AutoMigrate 并为每个分片创建表
	if err := gormDB.AutoMigrate(&Order{}, &Product{}); err != nil {
		fmt.Printf("Table migration failed: %v\n", err)
		return
	}
	fmt.Println("Tables migrated successfully (including 64 sharded 'orders' tables)")
}

func demoInsertShardingData(database db.DB) {
	fmt.Println("\n--- 3. Demo: Insert Sharded Data ---")
	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 先清理可能存在的旧数据，避免重复插入
	userID := int64(12345)
	if err := gormDB.Where("user_id = ?", userID).Delete(&Order{}).Error; err != nil {
		fmt.Printf("Failed to clean existing orders: %v\n", err)
		return
	}

	// 插入 UserID = 12345 的订单
	// 分片计算: 12345 % 64 = 57
	// 数据应该存储在 orders_57 表中
	shardIndex := userID % 64

	order := &Order{
		UserID:    userID,
		ProductID: 1001,
		Amount:    99.99,
		Status:    "pending",
	}

	if err := gormDB.Create(order).Error; err != nil {
		fmt.Printf("Failed to create order: %v\n", err)
	} else {
		fmt.Printf("Order created successfully: orderID=%v, userID=%d, target_table=%s\n",
			order.ID, order.UserID, fmt.Sprintf("orders_%02d", shardIndex))
		fmt.Printf(">>> Please check table 'orders_%02d' in your database to verify data.\n", shardIndex)
	}
}

func demoQueryShardingData(database db.DB) {
	fmt.Println("\n--- 4. Demo: Query Sharded Data ---")
	ctx := context.Background()
	gormDB := database.DB(ctx)

	userID := int64(12345)
	var orders []Order

	// 查询必须包含分片键 (user_id)
	if err := gormDB.Where("user_id = ?", userID).Find(&orders).Error; err != nil {
		fmt.Printf("Failed to query orders: %v\n", err)
	} else {
		fmt.Printf("Query successful: count=%d, userID=%d\n", len(orders), userID)
		for _, o := range orders {
			fmt.Printf("Found Order: ID=%d, Amount=%.2f\n", o.ID, o.Amount)
		}
	}
}

func demoTransaction(database db.DB) {
	fmt.Println("\n--- 5. Demo: Transaction ---")
	ctx := context.Background()

	err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
		// 1. 插入订单 (分片表)
		newOrder := &Order{
			UserID:    67890, // 67890 % 64 = 50 -> orders_50
			ProductID: 1002,
			Amount:    199.99,
			Status:    "paid",
		}
		if err := tx.Create(newOrder).Error; err != nil {
			return err
		}
		fmt.Printf("Transaction: Order created, userID=%d\n", newOrder.UserID)

		// 2. 插入产品 (普通表)
		newProduct := &Product{
			Name:  "Premium Widget",
			Price: 299.99,
		}
		if err := tx.Create(newProduct).Error; err != nil {
			return err
		}
		fmt.Printf("Transaction: Product created, name=%s\n", newProduct.Name)

		return nil
	})

	if err != nil {
		fmt.Printf("Transaction failed: %v\n", err)
	} else {
		fmt.Println("Transaction committed successfully")
	}
}

func demoErrorHandling(database db.DB) {
	fmt.Println("\n--- 6. Demo: Error Handling (Missing Sharding Key) ---")
	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 尝试不带分片键查询分片表
	var orders []Order
	err := gormDB.Where("product_id = ?", 1001).Find(&orders).Error
	if err != nil {
		fmt.Printf("Expected error caught: %v\n", err)
	} else {
		fmt.Println("Unexpected success: Query should have failed without sharding key")
	}
}
