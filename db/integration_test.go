package db

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/testkit"
)

// TestUser 测试用的用户模型
type TestUser struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:100"`
	Age  int
}

// =============================================================================
// MySQL 集成测试
// =============================================================================

func TestDBMySQL(t *testing.T) {
	conn := testkit.NewMySQLConnector(t)
	defer conn.Close()

	database, err := New(&Config{Driver: "mysql"},
		WithMySQLConnector(conn),
		WithLogger(testkit.NewLogger()),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	t.Run("Create", func(t *testing.T) {
		user := TestUser{Name: "Alice", Age: 30}
		err := gormDB.Create(&user).Error
		require.NoError(t, err)
		assert.NotZero(t, user.ID)
	})

	t.Run("Read", func(t *testing.T) {
		user := TestUser{Name: "Bob", Age: 25}
		require.NoError(t, gormDB.Create(&user).Error)

		var fetched TestUser
		err := gormDB.First(&fetched, user.ID).Error
		require.NoError(t, err)
		assert.Equal(t, "Bob", fetched.Name)
		assert.Equal(t, 25, fetched.Age)
	})

	t.Run("Update", func(t *testing.T) {
		user := TestUser{Name: "Charlie", Age: 35}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Model(&user).Update("age", 36).Error
		require.NoError(t, err)

		var fetched TestUser
		gormDB.First(&fetched, user.ID)
		assert.Equal(t, 36, fetched.Age)
	})

	t.Run("Delete", func(t *testing.T) {
		user := TestUser{Name: "David", Age: 40}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Delete(&user).Error
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("id = ?", user.ID).Count(&count)
		assert.Equal(t, int64(0), count)
	})

	t.Run("Transaction_Success", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.Create(&TestUser{Name: "TxUser", Age: 50}).Error
		})
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "TxUser").Count(&count)
		assert.Equal(t, int64(1), count)
	})

	t.Run("Transaction_Rollback", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			tx.Create(&TestUser{Name: "ShouldRollback", Age: 99})
			return assert.AnError
		})
		assert.Error(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "ShouldRollback").Count(&count)
		assert.Equal(t, int64(0), count)
	})
}

// =============================================================================
// PostgreSQL 集成测试
// =============================================================================

func TestDBPostgreSQL(t *testing.T) {
	conn := testkit.NewPostgreSQLConnector(t)
	defer conn.Close()

	database, err := New(&Config{Driver: "postgresql"},
		WithPostgreSQLConnector(conn),
		WithLogger(testkit.NewLogger()),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	t.Run("Create", func(t *testing.T) {
		user := TestUser{Name: "Alice", Age: 30}
		err := gormDB.Create(&user).Error
		require.NoError(t, err)
		assert.NotZero(t, user.ID)
	})

	t.Run("Read", func(t *testing.T) {
		user := TestUser{Name: "Bob", Age: 25}
		require.NoError(t, gormDB.Create(&user).Error)

		var fetched TestUser
		err := gormDB.First(&fetched, user.ID).Error
		require.NoError(t, err)
		assert.Equal(t, "Bob", fetched.Name)
		assert.Equal(t, 25, fetched.Age)
	})

	t.Run("Update", func(t *testing.T) {
		user := TestUser{Name: "Charlie", Age: 35}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Model(&user).Update("age", 36).Error
		require.NoError(t, err)

		var fetched TestUser
		gormDB.First(&fetched, user.ID)
		assert.Equal(t, 36, fetched.Age)
	})

	t.Run("Transaction", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.Create(&TestUser{Name: "TxUser", Age: 50}).Error
		})
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "TxUser").Count(&count)
		assert.Equal(t, int64(1), count)
	})
}

// =============================================================================
// SQLite 集成测试
// =============================================================================

func TestDBSQLite(t *testing.T) {
	conn := testkit.NewSQLiteConnector(t)
	defer conn.Close()

	database, err := New(&Config{Driver: "sqlite"},
		WithSQLiteConnector(conn),
		WithLogger(testkit.NewLogger()),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	t.Run("Create", func(t *testing.T) {
		user := TestUser{Name: "Alice", Age: 30}
		err := gormDB.Create(&user).Error
		require.NoError(t, err)
		assert.NotZero(t, user.ID)
	})

	t.Run("Read", func(t *testing.T) {
		user := TestUser{Name: "Bob", Age: 25}
		require.NoError(t, gormDB.Create(&user).Error)

		var fetched TestUser
		err := gormDB.First(&fetched, user.ID).Error
		require.NoError(t, err)
		assert.Equal(t, "Bob", fetched.Name)
	})

	t.Run("Update", func(t *testing.T) {
		user := TestUser{Name: "Charlie", Age: 35}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Model(&user).Update("age", 36).Error
		require.NoError(t, err)

		var fetched TestUser
		gormDB.First(&fetched, user.ID)
		assert.Equal(t, 36, fetched.Age)
	})

	t.Run("Transaction", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.Create(&TestUser{Name: "TxUser", Age: 50}).Error
		})
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "TxUser").Count(&count)
		assert.Equal(t, int64(1), count)
	})
}

// =============================================================================
// 配置验证测试
// =============================================================================

func TestDBConfigValidation(t *testing.T) {
	t.Run("无效的 Driver", func(t *testing.T) {
		conn := testkit.NewSQLiteConnector(t)
		defer conn.Close()

		_, err := New(&Config{Driver: "invalid"},
			WithSQLiteConnector(conn),
		)
		assert.Error(t, err)
	})

	t.Run("缺少 MySQL 连接器", func(t *testing.T) {
		_, err := New(&Config{Driver: "mysql"})
		assert.ErrorIs(t, err, ErrMySQLConnectorRequired)
	})

	t.Run("缺少 PostgreSQL 连接器", func(t *testing.T) {
		_, err := New(&Config{Driver: "postgresql"})
		assert.ErrorIs(t, err, ErrPostgreSQLConnectorRequired)
	})

	t.Run("缺少 SQLite 连接器", func(t *testing.T) {
		_, err := New(&Config{Driver: "sqlite"})
		assert.ErrorIs(t, err, ErrSQLiteConnectorRequired)
	})

	t.Run("分片启用但无规则", func(t *testing.T) {
		conn := testkit.NewSQLiteConnector(t)
		defer conn.Close()

		_, err := New(&Config{
			Driver:         "sqlite",
			EnableSharding: true,
		},
			WithSQLiteConnector(conn),
		)
		assert.Error(t, err)
	})

	t.Run("分片规则验证 - 空 ShardingKey", func(t *testing.T) {
		conn := testkit.NewSQLiteConnector(t)
		defer conn.Close()

		_, err := New(&Config{
			Driver:         "sqlite",
			EnableSharding: true,
			ShardingRules: []ShardingRule{
				{ShardingKey: "", NumberOfShards: 2, Tables: []string{"users"}},
			},
		},
			WithSQLiteConnector(conn),
		)
		assert.Error(t, err)
	})

	t.Run("分片规则验证 - NumberOfShards 为 0", func(t *testing.T) {
		conn := testkit.NewSQLiteConnector(t)
		defer conn.Close()

		_, err := New(&Config{
			Driver:         "sqlite",
			EnableSharding: true,
			ShardingRules: []ShardingRule{
				{ShardingKey: "user_id", NumberOfShards: 0, Tables: []string{"users"}},
			},
		},
			WithSQLiteConnector(conn),
		)
		assert.Error(t, err)
	})

	t.Run("分片规则验证 - 空表列表", func(t *testing.T) {
		conn := testkit.NewSQLiteConnector(t)
		defer conn.Close()

		_, err := New(&Config{
			Driver:         "sqlite",
			EnableSharding: true,
			ShardingRules: []ShardingRule{
				{ShardingKey: "user_id", NumberOfShards: 2, Tables: []string{}},
			},
		},
			WithSQLiteConnector(conn),
		)
		assert.Error(t, err)
	})
}

// =============================================================================
// Close 测试
// =============================================================================

func TestDBClose(t *testing.T) {
	conn := testkit.NewSQLiteConnector(t)
	defer conn.Close()

	database, err := New(&Config{Driver: "sqlite"},
		WithSQLiteConnector(conn),
	)
	require.NoError(t, err)

	// db 组件采用借用模型，Close 是 no-op
	err = database.Close()
	assert.NoError(t, err)

	// 再次 Close 也应该没问题
	err = database.Close()
	assert.NoError(t, err)
}

// =============================================================================
// PostgreSQL 事务回滚测试（补充）
// =============================================================================

func TestDBPostgreSQL_TransactionRollback(t *testing.T) {
	conn := testkit.NewPostgreSQLConnector(t)
	defer conn.Close()

	database, err := New(&Config{Driver: "postgresql"},
		WithPostgreSQLConnector(conn),
		WithLogger(testkit.NewLogger()),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	t.Run("Transaction_Rollback", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			tx.Create(&TestUser{Name: "ShouldRollback", Age: 99})
			return assert.AnError
		})
		assert.Error(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "ShouldRollback").Count(&count)
		assert.Equal(t, int64(0), count)
	})
}

// =============================================================================
// SQLite 事务回滚测试（补充）
// =============================================================================

func TestDBSQLite_TransactionRollback(t *testing.T) {
	conn := testkit.NewSQLiteConnector(t)
	defer conn.Close()

	database, err := New(&Config{Driver: "sqlite"},
		WithSQLiteConnector(conn),
		WithLogger(testkit.NewLogger()),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	t.Run("Transaction_Rollback", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			tx.Create(&TestUser{Name: "ShouldRollback", Age: 99})
			return assert.AnError
		})
		assert.Error(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "ShouldRollback").Count(&count)
		assert.Equal(t, int64(0), count)
	})
}

// =============================================================================
// 静默模式测试
// =============================================================================

func TestDBSilentMode(t *testing.T) {
	conn := testkit.NewSQLiteConnector(t)
	defer conn.Close()

	// 使用 WithSilentMode 禁用 SQL 日志
	database, err := New(&Config{Driver: "sqlite"},
		WithSQLiteConnector(conn),
		WithSilentMode(),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	// 执行一些操作，验证静默模式下不会 panic
	user := TestUser{Name: "SilentUser", Age: 30}
	err = gormDB.Create(&user).Error
	require.NoError(t, err)

	var fetched TestUser
	err = gormDB.First(&fetched, user.ID).Error
	require.NoError(t, err)
	assert.Equal(t, "SilentUser", fetched.Name)
}

// =============================================================================
// GormLogger 测试
// =============================================================================

func TestGormLogger(t *testing.T) {
	conn := testkit.NewSQLiteConnector(t)
	defer conn.Close()

	// 使用自定义 logger
	logger := testkit.NewLogger()
	database, err := New(&Config{Driver: "sqlite"},
		WithSQLiteConnector(conn),
		WithLogger(logger),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建测试表
	err = gormDB.Migrator().CreateTable(&TestUser{})
	require.NoError(t, err)
	defer gormDB.Migrator().DropTable(&TestUser{})

	// 执行 CRUD 操作，验证日志记录器正常工作
	user := TestUser{Name: "LoggerTest", Age: 25}
	err = gormDB.Create(&user).Error
	require.NoError(t, err)

	var fetched TestUser
	err = gormDB.First(&fetched, user.ID).Error
	require.NoError(t, err)

	// 测试错误日志（查询不存在的记录）
	var notFound TestUser
	err = gormDB.First(&notFound, 99999).Error
	assert.Error(t, err)
}

// =============================================================================
// 分片功能集成测试
// =============================================================================

// ShardedOrder 分片表模型
type ShardedOrder struct {
	ID     int64  `gorm:"primaryKey"`
	UserID int64  `gorm:"column:user_id;index"`
	Amount int    `gorm:"column:amount"`
	Status string `gorm:"column:status;size:50"`
}

func (ShardedOrder) TableName() string {
	return "orders"
}

func TestDBSharding_MySQL(t *testing.T) {
	conn := testkit.NewMySQLConnector(t)
	defer conn.Close()

	// 启用分片
	database, err := New(&Config{
		Driver:         "mysql",
		EnableSharding: true,
		ShardingRules: []ShardingRule{
			{
				ShardingKey:    "user_id",
				NumberOfShards: 4,
				Tables:         []string{"orders"},
			},
		},
	},
		WithMySQLConnector(conn),
		WithLogger(testkit.NewLogger()),
		WithSilentMode(),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建分片表（需要创建 orders_0, orders_1, orders_2, orders_3）
	for i := 0; i < 4; i++ {
		tableName := fmt.Sprintf("orders_%d", i)
		err := gormDB.Exec(fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				id BIGINT PRIMARY KEY,
				user_id BIGINT NOT NULL,
				amount INT NOT NULL,
				status VARCHAR(50) NOT NULL,
				INDEX idx_user_id (user_id)
			)
		`, tableName)).Error
		require.NoError(t, err, "failed to create table %s", tableName)
	}

	// 清理
	defer func() {
		for i := 0; i < 4; i++ {
			gormDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS orders_%d", i))
		}
	}()

	t.Run("Insert_With_Sharding", func(t *testing.T) {
		// 插入多条记录，使用不同的 user_id
		orders := []ShardedOrder{
			{UserID: 1001, Amount: 100, Status: "pending"},
			{UserID: 1002, Amount: 200, Status: "completed"},
			{UserID: 1003, Amount: 300, Status: "pending"},
			{UserID: 1004, Amount: 400, Status: "completed"},
		}

		for _, order := range orders {
			err := gormDB.Create(&order).Error
			require.NoError(t, err)
			// 注意：sharding 插件使用 Snowflake 生成 ID，但不会自动回填到结构体
			// 这里我们只验证插入成功
		}

		// 验证数据已插入
		for _, order := range orders {
			var fetched ShardedOrder
			err := gormDB.Model(&ShardedOrder{}).Where("user_id = ?", order.UserID).First(&fetched).Error
			require.NoError(t, err, "should find order with user_id %d", order.UserID)
			assert.Equal(t, order.Amount, fetched.Amount)
		}
	})

	t.Run("Query_With_ShardingKey", func(t *testing.T) {
		// 插入测试数据
		order := ShardedOrder{UserID: 2001, Amount: 500, Status: "new"}
		err := gormDB.Create(&order).Error
		require.NoError(t, err)

		// 使用分片键查询
		var fetched ShardedOrder
		err = gormDB.Model(&ShardedOrder{}).Where("user_id = ?", 2001).First(&fetched).Error
		require.NoError(t, err)
		assert.Equal(t, int64(2001), fetched.UserID)
		assert.Equal(t, 500, fetched.Amount)
	})

	t.Run("Update_With_ShardingKey", func(t *testing.T) {
		// 插入测试数据
		order := ShardedOrder{UserID: 3001, Amount: 600, Status: "pending"}
		err := gormDB.Create(&order).Error
		require.NoError(t, err)

		// 更新记录
		err = gormDB.Model(&ShardedOrder{}).Where("user_id = ?", 3001).Update("status", "shipped").Error
		require.NoError(t, err)

		// 验证更新
		var updated ShardedOrder
		err = gormDB.Model(&ShardedOrder{}).Where("user_id = ?", 3001).First(&updated).Error
		require.NoError(t, err)
		assert.Equal(t, "shipped", updated.Status)
	})

	t.Run("Delete_With_ShardingKey", func(t *testing.T) {
		// 插入测试数据
		order := ShardedOrder{UserID: 4001, Amount: 700, Status: "cancelled"}
		err := gormDB.Create(&order).Error
		require.NoError(t, err)

		// 删除记录
		err = gormDB.Where("user_id = ?", 4001).Delete(&ShardedOrder{}).Error
		require.NoError(t, err)

		// 验证删除
		var deleted ShardedOrder
		err = gormDB.Model(&ShardedOrder{}).Where("user_id = ?", 4001).First(&deleted).Error
		assert.Error(t, err)
	})
}

func TestDBSharding_SQLite(t *testing.T) {
	conn := testkit.NewSQLiteConnector(t)
	defer conn.Close()

	// 启用分片
	database, err := New(&Config{
		Driver:         "sqlite",
		EnableSharding: true,
		ShardingRules: []ShardingRule{
			{
				ShardingKey:    "user_id",
				NumberOfShards: 2,
				Tables:         []string{"orders"},
			},
		},
	},
		WithSQLiteConnector(conn),
		WithLogger(testkit.NewLogger()),
		WithSilentMode(),
	)
	require.NoError(t, err)
	defer database.Close()

	ctx := context.Background()
	gormDB := database.DB(ctx)

	// 创建分片表
	for i := 0; i < 2; i++ {
		tableName := fmt.Sprintf("orders_%d", i)
		err := gormDB.Exec(fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s (
				id INTEGER PRIMARY KEY,
				user_id INTEGER NOT NULL,
				amount INTEGER NOT NULL,
				status TEXT NOT NULL
			)
		`, tableName)).Error
		require.NoError(t, err, "failed to create table %s", tableName)
	}

	t.Run("Basic_Sharding_Operations", func(t *testing.T) {
		// 插入数据
		order1 := ShardedOrder{UserID: 100, Amount: 1000, Status: "new"}
		err := gormDB.Create(&order1).Error
		require.NoError(t, err)

		order2 := ShardedOrder{UserID: 101, Amount: 2000, Status: "processing"}
		err = gormDB.Create(&order2).Error
		require.NoError(t, err)

		// 查询数据
		var fetched1 ShardedOrder
		err = gormDB.Model(&ShardedOrder{}).Where("user_id = ?", 100).First(&fetched1).Error
		require.NoError(t, err)
		assert.Equal(t, 1000, fetched1.Amount)

		var fetched2 ShardedOrder
		err = gormDB.Model(&ShardedOrder{}).Where("user_id = ?", 101).First(&fetched2).Error
		require.NoError(t, err)
		assert.Equal(t, 2000, fetched2.Amount)
	})
}
