package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"github.com/ceyewan/genesis/dlock"
)

type product struct {
	ID   string `gorm:"primaryKey" json:"id"`
	Name string `json:"name"`
}

func main() {
	ctx := context.Background()
	logger, err := clog.New(&clog.Config{Level: "info", Format: "console", Output: "stdout"})
	if err != nil {
		log.Fatal(err)
	}

	sqliteConn, err := connector.NewSQLite(&connector.SQLiteConfig{Name: "cached-query", Path: "file:cached-query?mode=memory&cache=shared"}, connector.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer sqliteConn.Close()
	if err := sqliteConn.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	database, err := db.New(&db.Config{Driver: "sqlite"}, db.WithSQLiteConnector(sqliteConn), db.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	if err := database.DB(ctx).AutoMigrate(&product{}); err != nil {
		log.Fatal(err)
	}
	if err := database.DB(ctx).Create(&product{ID: "p-1001", Name: "Genesis T-Shirt"}).Error; err != nil {
		log.Fatal(err)
	}

	redisConn, err := connector.NewRedis(&connector.RedisConfig{Addr: "127.0.0.1:6379"}, connector.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer redisConn.Close()
	if err := redisConn.Connect(ctx); err != nil {
		log.Fatalf("连接 Redis 失败；请先执行 make up: %v", err)
	}
	locker, err := dlock.New(&dlock.Config{Driver: dlock.DriverRedis, Prefix: "cached-query:", DefaultTTL: 3 * time.Second}, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer locker.Close()
	local, err := cache.NewLocal(&cache.LocalConfig{MaxEntries: 128, DefaultTTL: time.Minute}, cache.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer local.Close()

	var databaseReads atomic.Int32
	getProduct := func(id string) (product, error) {
		key := "product:" + id
		var value product
		if err := local.Get(ctx, key, &value); err == nil {
			return value, nil
		} else if !errors.Is(err, cache.ErrMiss) {
			return product{}, err
		}
		if err := locker.Lock(ctx, key); err != nil {
			return product{}, err
		}
		defer locker.Unlock(ctx, key)
		if err := local.Get(ctx, key, &value); err == nil { // 获取锁后必须再次检查缓存。
			return value, nil
		}
		databaseReads.Add(1)
		if err := database.DB(ctx).First(&value, "id = ?", id).Error; err != nil {
			return product{}, err
		}
		if err := local.Set(ctx, key, value, time.Minute); err != nil {
			return product{}, err
		}
		return value, nil
	}

	for range 3 {
		value, err := getProduct("p-1001")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("读取商品: %s (%s)\n", value.Name, value.ID)
	}
	fmt.Printf("数据库读取次数=%d（期望为 1）\n", databaseReads.Load())
}
