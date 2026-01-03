package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"gorm.io/gorm"
)

// Order 订单模型（分片表）
type Order struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    int64  `gorm:"index"` // 分片键
	ProductID int64  `gorm:"index"`
	Amount    float64
	Status    string
}

// Product 产品模型（非分片表）
type Product struct {
	ID    uint64  `gorm:"primaryKey"`
	Name  string
	Price float64
}

func main() {
	fmt.Println("=== Genesis DB Component Example (SQLite + Sharding) ===")

	// 1. 初始化 Logger（开发环境配置：INFO 级别，彩色控制台输出）
	logger, err := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		EnableColor: true,
		AddSource:   true,
		SourceRoot:  "genesis",
	})
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	// 2. 创建 SQLite 连接器（内存数据库，方便测试）
	sqliteConn, err := connector.NewSQLite(&connector.SQLiteConfig{
		Path: "file:genesis_db?mode=memory&cache=shared",
	}, connector.WithLogger(logger))
	if err != nil {
		log.Fatalf("failed to create sqlite connector: %v", err)
	}
	defer sqliteConn.Close()

	// 3. 连接到数据库
	if err := sqliteConn.Connect(context.Background()); err != nil {
		log.Fatalf("failed to connect to sqlite: %v", err)
	}

	// 4. 创建 DB 组件（配置驱动 API + 分片）
	database, err := db.New(&db.Config{
		Driver:         "sqlite",
		EnableSharding: true,
		ShardingRules: []db.ShardingRule{
			{
				ShardingKey:    "user_id",
				NumberOfShards: 4, // SQLite 测试用 4 个分片即可
				Tables:         []string{"orders"},
			},
		},
	},
		db.WithLogger(logger),
		db.WithSQLiteConnector(sqliteConn),
	)
	if err != nil {
		log.Fatalf("failed to create db component: %v", err)
	}

	fmt.Println("\n--- 1. Migrating Tables ---")
	ctx := context.Background()
	gormDB := database.DB(ctx)
	if err := gormDB.AutoMigrate(&Order{}, &Product{}); err != nil {
		log.Fatalf("Table migration failed: %v", err)
	}
	fmt.Println("Tables migrated successfully (including 4 sharded 'orders' tables)")

	fmt.Println("\n--- 2. Insert Sharded Data ---")
	userID := int64(12345)
	order := &Order{
		UserID:    userID,
		ProductID: 1001,
		Amount:    99.99,
		Status:    "pending",
	}
	if err := gormDB.Create(order).Error; err != nil {
		log.Fatalf("Failed to create order: %v", err)
	}
	shardIndex := userID % 4
	fmt.Printf("Order created: ID=%d, userID=%d, target_table=orders_%02d\n", order.ID, order.UserID, shardIndex)

	fmt.Println("\n--- 3. Query Sharded Data ---")
	var orders []Order
	if err := gormDB.Where("user_id = ?", userID).Find(&orders).Error; err != nil {
		log.Fatalf("Failed to query orders: %v", err)
	}
	fmt.Printf("Query successful: count=%d\n", len(orders))

	fmt.Println("\n--- 4. Transaction ---")
	err = database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
		if err := tx.Create(&Order{UserID: 67890, ProductID: 1002, Amount: 199.99, Status: "paid"}).Error; err != nil {
			return err
		}
		return tx.Create(&Product{Name: "Premium Widget", Price: 299.99}).Error
	})
	if err != nil {
		log.Fatalf("Transaction failed: %v", err)
	}
	fmt.Println("Transaction committed successfully")

	fmt.Println("\n--- 5. Error Handling (Missing Sharding Key) ---")
	var allOrders []Order
	err = gormDB.Where("product_id = ?", 1001).Find(&allOrders).Error
	fmt.Printf("Expected error: %v\n", err)

	fmt.Println("\n=== Example completed successfully ===")
}
