package allocator

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdAllocator Etcd 分配器
type EtcdAllocator struct {
	client    connector.EtcdConnector
	keyPrefix string
	ttl       time.Duration
}

// NewEtcd 创建 Etcd 分配器
func NewEtcd(conn connector.EtcdConnector, keyPrefix string, ttl int) *EtcdAllocator {
	if keyPrefix == "" {
		keyPrefix = "genesis:idgen:worker"
	}
	if ttl <= 0 {
		ttl = 30
	}
	return &EtcdAllocator{
		client:    conn,
		keyPrefix: keyPrefix,
		ttl:       time.Duration(ttl) * time.Second,
	}
}

// Allocate 使用 GetPrefix + Txn 分配 WorkerID
func (a *EtcdAllocator) Allocate(ctx context.Context) (int64, error) {
	// 1. 获取当前所有占用
	resp, err := a.client.GetClient().Get(ctx, a.keyPrefix, clientv3.WithPrefix())
	if err != nil {
		return 0, fmt.Errorf("etcd get prefix failed: %w", err)
	}
	// 2. 构建占用集合
	occupied := make(map[int64]bool)
	for _, kv := range resp.Kvs {
		// key 格式: genesis:idgen:worker:123
		var id int64
		if _, err := fmt.Sscanf(string(kv.Key), a.keyPrefix+":%d", &id); err == nil {
			occupied[id] = true
		}
	}
	// 3. 找第一个空闲 ID
	var workerID int64 = -1
	for i := int64(0); i <= 1023; i++ {
		if !occupied[i] {
			workerID = i
			break
		}
	}
	if workerID < 0 {
		return 0, fmt.Errorf("no available worker id")
	}

	// 4. 创建租约并抢占
	lease, err := a.client.GetClient().Grant(ctx, int64(a.ttl.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("etcd grant lease failed: %w", err)
	}
	key := fmt.Sprintf("%s:%d", a.keyPrefix, workerID)
	value := fmt.Sprintf("host:%d", time.Now().UnixNano())

	// 使用事务确保原子性
	txn := a.client.GetClient().Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, value, clientv3.WithLease(lease.ID))).
		Else()
	resp2, err := txn.Commit()
	if err != nil {
		return 0, fmt.Errorf("etcd txn failed: %w", err)
	}
	if !resp2.Succeeded {
		return 0, fmt.Errorf("worker id %d already taken", workerID)
	}

	return workerID, nil
}

// Start 启动保活任务 (使用 Etcd 的 KeepAlive)
func (a *EtcdAllocator) Start(ctx context.Context, workerID int64) (<-chan error, error) {
	failCh := make(chan error, 1)
	key := fmt.Sprintf("%s:%d", a.keyPrefix, workerID)

	// 获取当前 key 的 lease id
	resp, err := a.client.GetClient().Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("etcd get key failed: %w", err)
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	leaseID := clientv3.LeaseID(resp.Kvs[0].Lease)

	// 启动 KeepAlive
	kaCh, err := a.client.GetClient().KeepAlive(ctx, leaseID)
	if err != nil {
		return nil, fmt.Errorf("etcd keep alive failed: %w", err)
	}

	// 监听 KeepAlive 响应
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case resp, ok := <-kaCh:
				if !ok || resp == nil {
					// KeepAlive 失败
					select {
					case failCh <- fmt.Errorf("keep alive channel closed"):
					default:
					}
					return
				}
			}
		}
	}()

	return failCh, nil
}
