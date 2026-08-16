package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idem"
	"github.com/ceyewan/genesis/mq"
)

type task struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger, err := clog.New(&clog.Config{Level: "info", Format: "console", Output: "stdout"})
	if err != nil {
		log.Fatal(err)
	}

	natsConn, err := connector.NewNATS(&connector.NATSConfig{URL: "nats://127.0.0.1:4222", Name: "async-task-example"}, connector.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer natsConn.Close()
	if err := natsConn.Connect(ctx); err != nil {
		log.Fatalf("连接 NATS 失败；请先执行 make up: %v", err)
	}

	queue, err := mq.New(&mq.Config{Driver: mq.DriverNATSJetStream, JetStream: &mq.JetStreamConfig{AutoCreateStream: true, AckWait: 2 * time.Second, MaxDeliver: 3}}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer queue.Close()

	idempotency, err := idem.New(&idem.Config{Driver: idem.DriverMemory, Prefix: "async:", DefaultTTL: time.Minute, LockTTL: 10 * time.Second}, idem.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer idempotency.Close()

	var executions atomic.Int32
	var failOnce atomic.Bool
	failOnce.Store(true)
	done := make(chan struct{}, 1)
	sub, err := queue.Subscribe(ctx, "tasks.created", func(msg mq.Message) error {
		var value task
		if err := json.Unmarshal(msg.Data(), &value); err != nil {
			_ = msg.Ack() // 不可解析的消息不能无限重试。
			return nil
		}
		if failOnce.CompareAndSwap(true, false) {
			logger.Warn("模拟临时失败，消息将重投", clog.String("task_id", value.ID))
			return errors.New("temporary downstream failure")
		}
		executed, err := idempotency.Consume(msg.Context(), "task:"+value.ID, time.Minute, func(context.Context) error {
			executions.Add(1)
			logger.Info("执行业务任务", clog.String("task_id", value.ID), clog.String("kind", value.Kind))
			return nil
		})
		if err != nil {
			return err // AutoAck 模式会请求重投，适合临时错误。
		}
		if executed {
			select {
			case done <- struct{}{}:
			default:
			}
		} else {
			logger.Info("跳过重复消息", clog.String("task_id", value.ID))
		}
		return nil
	}, mq.WithQueueGroup("async-task-workers"), mq.WithAutoAck())
	if err != nil {
		log.Fatal(err)
	}
	defer sub.Unsubscribe()

	payload, _ := json.Marshal(task{ID: "task-001", Kind: "send-email"})
	for range 2 { // 模拟 at-least-once 的重复投递。
		if err := queue.Publish(ctx, "tasks.created", payload); err != nil {
			log.Fatal(err)
		}
	}
	select {
	case <-done:
		fmt.Printf("任务处理完成，业务执行次数=%d（期望为 1）\n", executions.Load())
	case <-ctx.Done():
		log.Fatal("等待任务处理超时")
	}
}
