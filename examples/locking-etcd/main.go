package main

import (
	"context"
	"fmt"
	"log"
	"time"

	lockpkg "github.com/ceyewan/genesis/pkg/lock"
	etcdlock "github.com/ceyewan/genesis/pkg/lock/etcd"
)

func main() {
	cfg := &etcdlock.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	}

	opts := lockpkg.DefaultLockOptions()
	opts.TTL = 10 * time.Second

	locker, err := etcdlock.New(cfg, opts)
	if err != nil {
		log.Fatalf("Failed to create locker: %v", err)
	}
	defer locker.Close()

	ctx := context.Background()
	lockKey := "/locks/my-resource"

	fmt.Println("=== 演示阻塞式加锁 ===")
	if err := locker.Lock(ctx, lockKey); err != nil {
		log.Fatalf("Failed to lock: %v", err)
	}
	fmt.Println("✓ 成功获取锁")

	time.Sleep(2 * time.Second)
	fmt.Println("✓ 执行业务逻辑中...")

	if err := locker.Unlock(ctx, lockKey); err != nil {
		log.Fatalf("Failed to unlock: %v", err)
	}
	fmt.Println("✓ 成功释放锁")
}
