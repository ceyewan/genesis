package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idem"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/xerrors"
)

type task struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}
type taskWorker struct {
	store      idem.Idempotency
	failOnce   atomic.Bool
	executions atomic.Int32
	completed  chan string
}

func (w *taskWorker) handle(msg mq.Message) error {
	var value task
	if err := json.Unmarshal(msg.Data(), &value); err != nil || value.ID == "" {
		// 永久解码错误由应用明确终止；Ack 失败必须向上传播。
		return msg.Ack()
	}
	if w.failOnce.CompareAndSwap(true, false) {
		return xerrors.New("temporary downstream failure")
	}
	_, err := w.store.Consume(msg.Context(), "task:"+value.ID, time.Minute, func(context.Context) error { w.executions.Add(1); return nil })
	if err != nil {
		return err
	}
	select {
	case w.completed <- value.ID:
		return nil
	case <-msg.Context().Done():
		return msg.Context().Err()
	}
}

func verifyPhase(ctx context.Context, queue mq.MQ, store idem.Idempotency, topic, group string, failOnce bool, expected int32) (err error) {
	worker := &taskWorker{store: store, completed: make(chan string, 8)}
	worker.failOnce.Store(failOnce)
	sub, err := queue.Subscribe(ctx, topic, worker.handle, mq.WithQueueGroup(group), mq.WithAutoAck())
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, sub.Unsubscribe()) }()
	payload, err := json.Marshal(task{ID: "task-001", Kind: "send-email"})
	if err != nil {
		return err
	}
	for range 2 {
		if err := queue.Publish(ctx, topic, payload); err != nil {
			return err
		}
	}
	// 两条成功处理都必须结束，不能在第一次执行业务后提前退出。
	for range 2 {
		select {
		case id := <-worker.completed:
			if id != "task-001" {
				return xerrors.New("unexpected retained task")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := sub.Drain(ctx); err != nil {
		return err
	}
	if worker.executions.Load() != expected {
		return xerrors.Wrapf(xerrors.New("idempotent execution assertion failed"), "got %d, expected %d", worker.executions.Load(), expected)
	}
	return nil
}

func run(ctx context.Context) (err error) {
	logger, err := clog.New(&clog.Config{Format: "console", Output: "stdout"})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, logger.Close()) }()
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}
	natsConn, err := connector.NewNATS(&connector.NATSConfig{URL: natsURL}, connector.WithLogger(logger))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, natsConn.Close()) }()
	if err := natsConn.Connect(ctx); err != nil {
		return xerrors.Wrap(err, "connect NATS; run make up for this example")
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	redisConn, err := connector.NewRedis(&connector.RedisConfig{Addr: redisAddr})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, redisConn.Close()) }()
	if err := redisConn.Connect(ctx); err != nil {
		return xerrors.Wrap(err, "connect Redis; run make up for this example")
	}
	queue, err := mq.New(&mq.Config{Driver: mq.DriverNATSJetStream, JetStream: &mq.JetStreamConfig{AutoCreateStream: true, AckWait: 2 * time.Second, MaxDeliver: 5, MaxAge: time.Minute}}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, queue.Close()) }()
	identity := rand.Text()
	topic := "tasks." + identity
	group := "workers-" + identity
	cfg := &idem.Config{Driver: idem.DriverRedis, Prefix: "async:" + identity + ":", DefaultTTL: time.Minute, LockTTL: 10 * time.Second}
	store, err := idem.New(cfg, idem.WithRedisConnector(redisConn))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, store.Close()) }()
	if err := verifyPhase(ctx, queue, store, topic, group, true, 1); err != nil {
		return err
	}
	fmt.Println("临时失败重投与重复消费通过，业务执行次数=1")
	if err := store.Close(); err != nil {
		return err
	}
	restarted, err := idem.New(cfg, idem.WithRedisConnector(redisConn))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, restarted.Close()) }()
	if err := verifyPhase(ctx, queue, restarted, topic, group, false, 0); err != nil {
		return err
	}
	fmt.Println("重新创建消费者与幂等组件后重复任务未再次执行")
	return nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
