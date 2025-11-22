package main

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/dlock"
)

func main() {
	// 1. 初始化 Logger (独立模式)
	// 我们可以直接创建一个 Logger，并指定 Namespace
	logger := clog.New(clog.Config{
		Level:  "debug",
		Format: "text",
	}).WithNamespace("simple-script")

	// 2. 初始化 Connector (独立模式)
	// 直接调用 Connector 的工厂函数，注入 Logger
	redisCfg := connector.RedisConfig{
		Addr: "localhost:6379",
		DB:   0,
	}
	// 注意：这里我们需要手动注入带 Namespace 的 Logger
	redisLogger := logger.WithNamespace("redis")
	redisConn, err := connector.NewRedis(redisCfg, redisLogger)
	if err != nil {
		logger.Fatal("failed to create redis connector", clog.Err(err))
	}

	// 连接器通常需要显式 Connect (取决于具体实现，有些在 New 时就连接了)
	if err := redisConn.Connect(context.Background()); err != nil {
		logger.Fatal("failed to connect to redis", clog.Err(err))
	}
	defer redisConn.Close()

	// 3. 初始化 Component (独立模式)
	// 直接调用 Component 的工厂函数，注入 Connector 和 Logger
	dlockCfg := dlock.Config{
		Backend:    "redis",
		Prefix:     "script-lock:",
		DefaultTTL: 10 * time.Second,
	}

	// 同样，手动注入带 Namespace 的 Logger
	dlockLogger := logger.WithNamespace("dlock")

	// 使用 Option 模式注入依赖
	locker, err := dlock.New(dlockCfg,
		dlock.WithConnector(redisConn),
		dlock.WithLogger(dlockLogger),
	)
	if err != nil {
		logger.Fatal("failed to create dlock", clog.Err(err))
	}

	// 4. 使用组件
	ctx := context.Background()
	key := "my-resource"

	logger.Info("trying to acquire lock", clog.String("key", key))
	if err := locker.Lock(ctx, key); err != nil {
		logger.Error("failed to acquire lock", clog.Err(err))
		return
	}
	logger.Info("lock acquired")

	// 模拟业务处理
	time.Sleep(2 * time.Second)

	if err := locker.Unlock(ctx, key); err != nil {
		logger.Error("failed to release lock", clog.Err(err))
		return
	}
	logger.Info("lock released")
}
