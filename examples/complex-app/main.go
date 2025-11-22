package main

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/config"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/db"
	"github.com/ceyewan/genesis/pkg/dlock"
	"github.com/ceyewan/genesis/pkg/idgen"
)

// 模拟业务逻辑
func runBusinessLogic(ctx context.Context, c *container.Container) {
	logger := c.Log.WithNamespace("business")

	// 1. 使用 IDGen 生成 ID
	orderID, err := c.IDGen.NextID(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to generate order id", clog.Err(err))
		return
	}
	logger.InfoContext(ctx, "generated order id", clog.Int64("order_id", orderID))

	// 2. 使用 DLock 获取分布式锁
	lockKey := "lock:order:" + string(orderID)
	if err := c.DLock.Lock(ctx, lockKey, dlock.WithTTL(5*time.Second)); err != nil {
		logger.ErrorContext(ctx, "failed to acquire lock", clog.Err(err))
		return
	}
	defer func() {
		if err := c.DLock.Unlock(ctx, lockKey); err != nil {
			logger.ErrorContext(ctx, "failed to release lock", clog.Err(err))
		}
	}()
	logger.InfoContext(ctx, "acquired lock", clog.String("key", lockKey))

	// 3. 使用 DB 执行事务
	err = c.DB.Transaction(ctx, func(ctx context.Context, tx db.Tx) error {
		// 模拟数据库操作
		// tx.Create(&Order{ID: orderID, ...})
		logger.InfoContext(ctx, "executing db transaction", clog.Int64("order_id", orderID))
		return nil
	})
	if err != nil {
		logger.ErrorContext(ctx, "transaction failed", clog.Err(err))
		return
	}

	// 4. 使用 Cache (假设 Container 中有 Cache 组件)
	// err = c.Cache.Set(ctx, "order:"+string(orderID), []byte("data"), time.Minute)
	// ...
}

func main() {
	// 1. 加载配置
	// 在实际场景中，这里会从 config.yaml 或环境变量加载
	// 这里为了演示方便，手动构造一个 AppConfig
	cfg := &config.AppConfig{
		App: config.AppInfo{
			Name:      "complex-app",
			Namespace: "complex-app",
			Env:       "dev",
		},
		Log: clog.Config{
			Level:  "info",
			Format: "json",
		},
		// 假设这里配置了 Connectors 和 Components
		// Connectors: ...
		// Components: ...
	}

	// 2. 初始化容器
	// Container 会负责：
	// - 初始化 Logger
	// - 初始化 Connectors (Redis, MySQL 等)
	// - 初始化 Components (DB, DLock, IDGen 等) 并注入依赖
	c, err := container.New(cfg)
	if err != nil {
		panic(err)
	}

	// 3. 启动容器 (启动所有组件的生命周期任务，如后台清理、心跳等)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := c.Start(ctx); err != nil {
		panic(err)
	}
	defer c.Stop(ctx) // 确保优雅关闭

	// 4. 运行业务逻辑
	runBusinessLogic(ctx, c)

	// 模拟运行一段时间
	time.Sleep(1 * time.Second)
}
