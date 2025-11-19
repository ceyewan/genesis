package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/db"
	"gorm.io/gorm"
)

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
	fmt.Println("=== Genesis DB Component Example ===")

	// 1. 初始化容器
	app := initContainer()
	defer app.Close()

	// 2. 自动迁移表结构
	migrateTables(app)

	// 3. 演示：插入分片数据
	demoInsertShardingData(app)

	// 4. 演示：查询分片数据
	demoQueryShardingData(app)

	// 5. 演示：事务操作
	demoTransaction(app)

	// 6. 演示：错误处理
	demoErrorHandling(app)
}

func initContainer() *container.Container {
	fmt.Println("\n--- 1. Initializing Container ---")

	cfg := &container.Config{
		// MySQL 连接配置
		MySQL: &connector.MySQLConfig{
			Host:         "127.0.0.1",
			Port:         3306,
			Username:     "root",
			Password:     "your_root_password", // 请替换为实际密码
			Database:     "app_db",
			Charset:      "utf8mb4",
			Timeout:      10 * time.Second,
			MaxIdleConns: 10,
			MaxOpenConns: 100,
			MaxLifetime:  time.Hour,
		},
		// DB 组件配置
		DB: &db.Config{
			EnableSharding: true,
			ShardingRules: []db.ShardingRule{
				{
					ShardingKey:    "user_id",
					NumberOfShards: 64, // 将创建 orders_00 到 orders_63 共 64 张表
					Tables:         []string{"orders"},
				},
			},
		},
	}

	app, err := container.New(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize container: %v", err))
	}
	fmt.Println()
	fmt.Println("Container initialized successfully")
	return app
}

func migrateTables(app *container.Container) {
	fmt.Println("\n--- 2. Migrating Tables ---")
	ctx := context.Background()
	gormDB := app.DB.DB(ctx)

	// gorm.io/sharding 会自动拦截 AutoMigrate 并为每个分片创建表
	if err := gormDB.AutoMigrate(&Order{}, &Product{}); err != nil {
		fmt.Printf("Table migration failed: %v\n", err)
		return
	}
	fmt.Println("Tables migrated successfully (including 64 sharded 'orders' tables)")
}

func demoInsertShardingData(app *container.Container) {
	fmt.Println("\n--- 3. Demo: Insert Sharded Data ---")
	ctx := context.Background()
	gormDB := app.DB.DB(ctx)

	// 插入 UserID = 12345 的订单
	// 分片计算: 12345 % 64 = 57
	// 数据应该存储在 orders_57 表中
	userID := int64(12345)
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

func demoQueryShardingData(app *container.Container) {
	fmt.Println("\n--- 4. Demo: Query Sharded Data ---")
	ctx := context.Background()
	gormDB := app.DB.DB(ctx)

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

func demoTransaction(app *container.Container) {
	fmt.Println("\n--- 5. Demo: Transaction ---")
	ctx := context.Background()

	err := app.DB.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
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

func demoErrorHandling(app *container.Container) {
	fmt.Println("\n--- 6. Demo: Error Handling (Missing Sharding Key) ---")
	ctx := context.Background()
	gormDB := app.DB.DB(ctx)

	// 尝试不带分片键查询分片表
	var orders []Order
	err := gormDB.Where("product_id = ?", 1001).Find(&orders).Error
	if err != nil {
		fmt.Printf("Expected error caught: %v\n", err)
	} else {
		fmt.Println("Unexpected success: Query should have failed without sharding key")
	}
}
