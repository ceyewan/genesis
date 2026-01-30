package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/dlock"
	"github.com/ceyewan/genesis/testkit"
)

func main() {
	ctx := context.Background()
	logger := initLogger()

	cases := []struct {
		name string
		init func(context.Context, clog.Logger) (dlock.Locker, func(), error)
	}{
		{name: "redis", init: initRedis},
		{name: "etcd", init: initEtcd},
	}

	for _, c := range cases {
		logger.Info("=== Genesis DLock Example ===", clog.String("driver", c.name))

		locker, cleanup, err := c.init(ctx, logger)
		if err != nil {
			logger.Error("初始化 dlock 失败", clog.String("driver", c.name), clog.Error(err))
			continue
		}

		runDemo(ctx, locker, logger)
		cleanup()

		logger.Info("=== 示例演示完成 ===", clog.String("driver", c.name))
	}
}

func initLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("genesis"))
	if err != nil {
		return clog.Discard()
	}
	return logger.WithNamespace("dlock-example")
}

func initRedis(ctx context.Context, logger clog.Logger) (dlock.Locker, func(), error) {
	addr := getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379")
	password := getEnvOrDefault("REDIS_PASSWORD", "")

	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:     addr,
		Password: password,
		DB:       0,
		PoolSize: 10,
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	if err := redisConn.Connect(ctx); err != nil {
		return nil, func() { _ = redisConn.Close() }, err
	}

	locker, err := dlock.New(&dlock.Config{
		Driver:        dlock.DriverRedis,
		Prefix:        "dlock:",
		DefaultTTL:    10 * time.Second,
		RetryInterval: 100 * time.Millisecond,
	}, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))
	if err != nil {
		_ = redisConn.Close()
		return nil, nil, err
	}

	return locker, func() { _ = redisConn.Close() }, nil
}

func initEtcd(ctx context.Context, logger clog.Logger) (dlock.Locker, func(), error) {
	endpoints := strings.Split(getEnvOrDefault("ETCD_ENDPOINTS", "127.0.0.1:2379"), ",")

	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	if err := etcdConn.Connect(ctx); err != nil {
		return nil, func() { _ = etcdConn.Close() }, err
	}

	locker, err := dlock.New(&dlock.Config{
		Driver:        dlock.DriverEtcd,
		Prefix:        "dlock:",
		DefaultTTL:    10 * time.Second,
		RetryInterval: 100 * time.Millisecond,
	}, dlock.WithEtcdConnector(etcdConn), dlock.WithLogger(logger))
	if err != nil {
		_ = etcdConn.Close()
		return nil, nil, err
	}

	return locker, func() { _ = etcdConn.Close() }, nil
}

func runDemo(ctx context.Context, locker dlock.Locker, logger clog.Logger) {
	key := "resource:" + testkit.NewID()

	logger.Info("尝试加锁", clog.String("key", key))
	if err := locker.Lock(ctx, key); err != nil {
		logger.Error("加锁失败", clog.String("key", key), clog.Error(err))
		return
	}
	logger.Info("加锁成功", clog.String("key", key))

	logger.Info("执行业务逻辑", clog.String("key", key))
	time.Sleep(500 * time.Millisecond)

	logger.Info("尝试释放锁", clog.String("key", key))
	if err := locker.Unlock(ctx, key); err != nil {
		logger.Error("释放锁失败", clog.String("key", key), clog.Error(err))
		return
	}
	logger.Info("释放锁成功", clog.String("key", key))

	logger.Info("测试 TryLock", clog.String("key", key))
	ok, err := locker.TryLock(ctx, key)
	if err != nil {
		logger.Error("TryLock 失败", clog.String("key", key), clog.Error(err))
		return
	}
	if ok {
		logger.Info("TryLock 成功", clog.String("key", key))
		_ = locker.Unlock(ctx, key)
	} else {
		logger.Info("TryLock 未获取到锁", clog.String("key", key))
	}

	logger.Info("测试 WithTTL", clog.String("key", key))
	if err := locker.Lock(ctx, key, dlock.WithTTL(2*time.Second)); err != nil {
		logger.Error("WithTTL 加锁失败", clog.String("key", key), clog.Error(err))
		return
	}
	logger.Info("WithTTL 加锁成功", clog.String("key", key))
	_ = locker.Unlock(ctx, key)

	logger.Info("测试多次重试", clog.String("key", key))
	_ = locker.Lock(ctx, key)
	go func() {
		if err := locker.Lock(ctx, key); err != nil {
			logger.Info("重试加锁失败（预期）", clog.String("key", key), clog.Error(err))
			return
		}
		logger.Info("重试加锁成功", clog.String("key", key))
		_ = locker.Unlock(ctx, key)
	}()
	time.Sleep(time.Second)
	_ = locker.Unlock(ctx, key)
	time.Sleep(500 * time.Millisecond)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
