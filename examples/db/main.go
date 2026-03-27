package main

import (
	"context"
	"fmt"
	"log"

	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
)

// Order 订单模型
type Order struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    int64  `gorm:"index"`
	ProductID int64  `gorm:"index"`
	Amount    float64
	Status    string
}

// Product 产品模型
type Product struct {
	ID    uint64 `gorm:"primaryKey"`
	Name  string
	Price float64
}

func main() {
	fmt.Println("=== Genesis DB Component Example (SQLite) ===")

	// 1. 初始化 Logger
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

	// 2. 创建 SQLite 连接器（内存数据库）
	sqliteConn, err := connector.NewSQLite(&connector.SQLiteConfig{
		Path: "file:genesis_db?mode=memory&cache=shared",
	}, connector.WithLogger(logger))
	if err != nil {
		log.Fatalf("failed to create sqlite connector: %v", err)
	}
	defer sqliteConn.Close()

	if err := sqliteConn.Connect(context.Background()); err != nil {
		log.Fatalf("failed to connect to sqlite: %v", err)
	}

	// 3. 创建 DB 组件
	database, err := db.New(&db.Config{Driver: "sqlite"},
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
		log.Fatalf("table migration failed: %v", err)
	}
	fmt.Println("Tables migrated successfully")

	fmt.Println("\n--- 2. Insert Data ---")
	order := &Order{UserID: 12345, ProductID: 1001, Amount: 99.99, Status: "pending"}
	if err := gormDB.Create(order).Error; err != nil {
		log.Fatalf("failed to create order: %v", err)
	}
	fmt.Printf("Order created: ID=%d, userID=%d\n", order.ID, order.UserID)

	fmt.Println("\n--- 3. Query Data ---")
	var orders []Order
	if err := gormDB.Where("user_id = ?", int64(12345)).Find(&orders).Error; err != nil {
		log.Fatalf("failed to query orders: %v", err)
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
		log.Fatalf("transaction failed: %v", err)
	}
	fmt.Println("Transaction committed successfully")

	fmt.Println("\n--- 5. Transaction Rollback ---")
	err = database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
		tx.Create(&Order{UserID: 99999, Amount: 1.0, Status: "should-rollback"})
		return fmt.Errorf("intentional rollback")
	})
	fmt.Printf("Expected error: %v\n", err)

	fmt.Println("\n=== Example completed successfully ===")
}
