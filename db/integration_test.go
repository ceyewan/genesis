package db

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/internal/testkit"
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
		require.NotZero(t, user.ID)
	})

	t.Run("Read", func(t *testing.T) {
		user := TestUser{Name: "Bob", Age: 25}
		require.NoError(t, gormDB.Create(&user).Error)

		var fetched TestUser
		err := gormDB.First(&fetched, user.ID).Error
		require.NoError(t, err)
		require.Equal(t, "Bob", fetched.Name)
		require.Equal(t, 25, fetched.Age)
	})

	t.Run("Update", func(t *testing.T) {
		user := TestUser{Name: "Charlie", Age: 35}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Model(&user).Update("age", 36).Error
		require.NoError(t, err)

		var fetched TestUser
		gormDB.First(&fetched, user.ID)
		require.Equal(t, 36, fetched.Age)
	})

	t.Run("Delete", func(t *testing.T) {
		user := TestUser{Name: "David", Age: 40}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Delete(&user).Error
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("id = ?", user.ID).Count(&count)
		require.Equal(t, int64(0), count)
	})

	t.Run("Transaction_Success", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.Create(&TestUser{Name: "TxUser", Age: 50}).Error
		})
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "TxUser").Count(&count)
		require.Equal(t, int64(1), count)
	})

	t.Run("Transaction_Rollback", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			tx.Create(&TestUser{Name: "ShouldRollback", Age: 99})
			return errors.New("force rollback")
		})
		require.Error(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "ShouldRollback").Count(&count)
		require.Equal(t, int64(0), count)
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
		require.NotZero(t, user.ID)
	})

	t.Run("Read", func(t *testing.T) {
		user := TestUser{Name: "Bob", Age: 25}
		require.NoError(t, gormDB.Create(&user).Error)

		var fetched TestUser
		err := gormDB.First(&fetched, user.ID).Error
		require.NoError(t, err)
		require.Equal(t, "Bob", fetched.Name)
		require.Equal(t, 25, fetched.Age)
	})

	t.Run("Update", func(t *testing.T) {
		user := TestUser{Name: "Charlie", Age: 35}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Model(&user).Update("age", 36).Error
		require.NoError(t, err)

		var fetched TestUser
		gormDB.First(&fetched, user.ID)
		require.Equal(t, 36, fetched.Age)
	})

	t.Run("Transaction", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.Create(&TestUser{Name: "TxUser", Age: 50}).Error
		})
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "TxUser").Count(&count)
		require.Equal(t, int64(1), count)
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
		require.NotZero(t, user.ID)
	})

	t.Run("Read", func(t *testing.T) {
		user := TestUser{Name: "Bob", Age: 25}
		require.NoError(t, gormDB.Create(&user).Error)

		var fetched TestUser
		err := gormDB.First(&fetched, user.ID).Error
		require.NoError(t, err)
		require.Equal(t, "Bob", fetched.Name)
	})

	t.Run("Update", func(t *testing.T) {
		user := TestUser{Name: "Charlie", Age: 35}
		require.NoError(t, gormDB.Create(&user).Error)

		err := gormDB.Model(&user).Update("age", 36).Error
		require.NoError(t, err)

		var fetched TestUser
		gormDB.First(&fetched, user.ID)
		require.Equal(t, 36, fetched.Age)
	})

	t.Run("Transaction", func(t *testing.T) {
		err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
			return tx.Create(&TestUser{Name: "TxUser", Age: 50}).Error
		})
		require.NoError(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "TxUser").Count(&count)
		require.Equal(t, int64(1), count)
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
		require.Error(t, err)
	})

	t.Run("缺少 MySQL 连接器", func(t *testing.T) {
		_, err := New(&Config{Driver: "mysql"})
		require.ErrorIs(t, err, ErrMySQLConnectorRequired)
	})

	t.Run("缺少 PostgreSQL 连接器", func(t *testing.T) {
		_, err := New(&Config{Driver: "postgresql"})
		require.ErrorIs(t, err, ErrPostgreSQLConnectorRequired)
	})

	t.Run("缺少 SQLite 连接器", func(t *testing.T) {
		_, err := New(&Config{Driver: "sqlite"})
		require.ErrorIs(t, err, ErrSQLiteConnectorRequired)
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
	require.NoError(t, err)

	// 再次 Close 也应该没问题
	err = database.Close()
	require.NoError(t, err)
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
			return errors.New("force rollback")
		})
		require.Error(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "ShouldRollback").Count(&count)
		require.Equal(t, int64(0), count)
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
			return errors.New("force rollback")
		})
		require.Error(t, err)

		var count int64
		gormDB.Model(&TestUser{}).Where("name = ?", "ShouldRollback").Count(&count)
		require.Equal(t, int64(0), count)
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
	require.Equal(t, "SilentUser", fetched.Name)
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
	require.Error(t, err)
}
