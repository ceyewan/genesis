package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"github.com/ceyewan/genesis/dlock"
	"github.com/ceyewan/genesis/xerrors"
)

type product struct {
	ID   string `gorm:"primaryKey" json:"id"`
	Name string `json:"name"`
}
type cachedProduct struct {
	Value   product `json:"value"`
	Missing bool    `json:"missing"`
}

type productReader struct {
	database db.DB
	values   cache.KV
	locker   dlock.Locker
	pending  singleflight.Group
	reads    atomic.Int32
}

func (r *productReader) get(ctx context.Context, id string) (product, error) {
	key := "product:" + id
	var entry cachedProduct
	if err := r.values.Get(ctx, key, &entry); err == nil {
		return entry.result()
	} else if !xerrors.Is(err, cache.ErrMiss) {
		return product{}, err
	}
	// 单实例用请求合并，跨实例用共享缓存和各实例自己的 Locker。
	result := r.pending.DoChan(key, func() (any, error) { return r.load(ctx, key, id) })
	select {
	case <-ctx.Done():
		return product{}, ctx.Err()
	case value := <-result:
		if value.Err != nil {
			return product{}, value.Err
		}
		return value.Val.(product), nil
	}
}

func (v cachedProduct) result() (product, error) {
	if v.Missing {
		return product{}, gorm.ErrRecordNotFound
	}
	return v.Value, nil
}

func (r *productReader) load(ctx context.Context, key, id string) (value product, err error) {
	if r.locker != nil {
		if err := r.locker.Lock(ctx, key); err != nil {
			return product{}, err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			err = xerrors.Join(err, r.locker.Unlock(cleanup, key))
		}()
	}
	var entry cachedProduct
	// 等待共享锁或 singleflight 后必须再次检查，也要传播非 miss 错误。
	if err := r.values.Get(ctx, key, &entry); err == nil {
		return entry.result()
	} else if !xerrors.Is(err, cache.ErrMiss) {
		return product{}, err
	}
	r.reads.Add(1)
	err = r.database.DB(ctx).First(&value, "id = ?", id).Error
	if err != nil && !xerrors.Is(err, gorm.ErrRecordNotFound) {
		return product{}, err
	}
	entry = cachedProduct{Value: value, Missing: xerrors.Is(err, gorm.ErrRecordNotFound)}
	ttl := time.Minute
	if entry.Missing {
		ttl = 5 * time.Second
	}
	if setErr := r.values.Set(ctx, key, entry, ttl); setErr != nil {
		return product{}, setErr
	}
	return entry.result()
}

// verifyConcurrent 要求每个参与者成功且业务数据库只读取一次。
func verifyConcurrent(ctx context.Context, readers ...*productReader) error {
	var wg sync.WaitGroup
	errors := make(chan error, 100)
	start := make(chan struct{})
	for i := range 100 {
		wg.Go(func() {
			<-start
			value, err := readers[i%len(readers)].get(ctx, "p-1001")
			if err == nil && value.ID != "p-1001" {
				err = xerrors.New("unexpected product")
			}
			errors <- err
		})
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			return err
		}
	}
	var count int32
	for _, reader := range readers {
		count += reader.reads.Load()
	}
	if count != 1 {
		return xerrors.Wrapf(xerrors.New("cache rebuild assertion failed"), "database reads: %d, expected 1", count)
	}
	return nil
}

func run(ctx context.Context) (err error) {
	logger, err := clog.New(&clog.Config{Format: "console", Output: "stdout"})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, logger.Close()) }()
	sqlite, err := connector.NewSQLite(&connector.SQLiteConfig{Path: "file:" + rand.Text() + "?mode=memory&cache=shared"})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, sqlite.Close()) }()
	if err := sqlite.Connect(ctx); err != nil {
		return err
	}
	database, err := db.New(&db.Config{Driver: "sqlite"}, db.WithSQLiteConnector(sqlite), db.WithLogger(logger))
	if err != nil {
		return err
	}
	if err := database.DB(ctx).AutoMigrate(&product{}); err != nil {
		return err
	}
	if err := database.DB(ctx).Create(&product{ID: "p-1001", Name: "Genesis T-Shirt"}).Error; err != nil {
		return err
	}
	local, err := cache.NewLocal(&cache.LocalConfig{MaxEntries: 128})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, local.Close()) }()
	if err := verifyConcurrent(ctx, &productReader{database: database, values: local}); err != nil {
		return err
	}
	fmt.Println("单进程并发查询通过，数据库读取次数=1")
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	conn, err := connector.NewRedis(&connector.RedisConfig{Addr: addr})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, conn.Close()) }()
	if err := conn.Connect(ctx); err != nil {
		return xerrors.Wrap(err, "connect Redis; run make up for this example")
	}
	prefix := "cached-query:" + rand.Text() + ":"
	values, err := cache.NewDistributed(&cache.DistributedConfig{KeyPrefix: prefix}, cache.WithRedisConnector(conn))
	if err != nil {
		return err
	}
	var readers []*productReader
	for range 2 {
		locker, err := dlock.New(&dlock.Config{Driver: dlock.DriverRedis, Prefix: prefix + "lock:", DefaultTTL: 3 * time.Second}, dlock.WithRedisConnector(conn))
		if err != nil {
			return err
		}
		defer func() { err = xerrors.Join(err, locker.Close()) }()
		readers = append(readers, &productReader{database: database, values: values, locker: locker})
	}
	if err := verifyConcurrent(ctx, readers...); err != nil {
		return err
	}
	fmt.Println("共享 Redis 缓存跨实例并发查询通过，数据库读取次数=1")
	return values.Delete(ctx, "product:p-1001")
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
