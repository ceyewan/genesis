package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/examples/observability/internal/bootstrap"
	"github.com/ceyewan/genesis/examples/observability/proto"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	natsEndpoint   = "nats://localhost:4222"
	orderSubject   = "orders.created"
	callbackTarget = "localhost:9091"
)

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

type orderCreatedEvent struct {
	OrderID   string `json:"order_id"`
	UserID    string `json:"user_id"`
	ProductID string `json:"product_id"`
}

func main() {
	ctx := context.Background()

	obs, shutdowns, err := bootstrap.Init(ctx, "obs-task", 9103)
	if err != nil {
		panic(err)
	}
	for i := len(shutdowns) - 1; i >= 0; i-- {
		defer func(fn bootstrap.Shutdown) { _ = fn(ctx) }(shutdowns[i])
	}

	natsConn, err := connector.NewNATS(&connector.NATSConfig{URL: getenv("NATS_URL", natsEndpoint)}, connector.WithLogger(obs.Logger))
	if err != nil {
		obs.Logger.Fatal("new nats connector failed", clog.Error(err))
	}
	defer func() { _ = natsConn.Close() }()
	if err := natsConn.Connect(ctx); err != nil {
		obs.Logger.Fatal("connect nats failed", clog.Error(err))
	}

	mqClient, err := mq.New(&mq.Config{Driver: mq.DriverNatsCore}, mq.WithNATSConnector(natsConn), mq.WithLogger(obs.Logger), mq.WithMeter(obs.Meter))
	if err != nil {
		obs.Logger.Fatal("new mq failed", clog.Error(err))
	}
	defer func() { _ = mqClient.Close() }()

	cbConn, err := grpc.NewClient(
		getenv("GATEWAY_CALLBACK_TARGET", callbackTarget),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
	)
	if err != nil {
		obs.Logger.Fatal("connect callback grpc failed", clog.Error(err))
	}
	defer func() { _ = cbConn.Close() }()
	cbClient := proto.NewGatewayCallbackServiceClient(cbConn)

	tracer := otel.Tracer("obs-task")

	sub, err := mqClient.Subscribe(ctx, orderSubject, func(ctx context.Context, msg mq.Message) error {
		consumeCtx, consumeSpan := trace.StartConsumerSpanFromHeaders(
			msg.Context(),
			tracer,
			trace.SpanNameMQConsume(orderSubject),
			msg.Headers(),
			trace.MessagingMeta{
				System:        trace.MessagingSystemNATS,
				Destination:   orderSubject,
				Operation:     trace.MessagingOperationProcess,
				ConsumerGroup: "order-task-workers",
				TraceRelation: trace.MessagingTraceRelationChildOf,
			},
		)
		defer consumeSpan.End()

		ev := orderCreatedEvent{}
		if err := json.Unmarshal(msg.Data(), &ev); err != nil {
			trace.MarkSpanError(consumeSpan, err)
			obs.Logger.ErrorContext(consumeCtx, "unmarshal order event failed", clog.Error(err))
			return err
		}
		consumeSpan.SetAttributes(
			attribute.String("order.id", ev.OrderID),
			attribute.String("order.user_id", ev.UserID),
			attribute.String("order.product_id", ev.ProductID),
		)

		handledCtx, span := tracer.Start(consumeCtx, "task.handle_order_created")
		defer span.End()

		obs.Logger.InfoContext(handledCtx, "task received order event",
			clog.String("order_id", ev.OrderID),
			clog.String("user_id", ev.UserID),
			clog.String("product_id", ev.ProductID),
		)

		time.Sleep(30 * time.Millisecond)

		_, err := cbClient.PushResult(handledCtx, &proto.PushResultRequest{
			Result: &proto.OrderResult{
				OrderId: ev.OrderID,
				Status:  "DONE",
			},
		})
		if err != nil {
			obs.Logger.ErrorContext(handledCtx, "push result to gateway failed", clog.Error(err))
			trace.MarkSpanError(consumeSpan, err)
			return err
		}

		obs.Logger.InfoContext(handledCtx, "task pushed result to gateway", clog.String("order_id", ev.OrderID))
		return nil
	}, mq.WithQueueGroup("order-task-workers"))
	if err != nil {
		obs.Logger.Fatal("subscribe failed", clog.Error(err))
	}
	defer func() { _ = sub.Unsubscribe() }()

	obs.Logger.Info("task worker started", clog.String("subject", orderSubject))
	select {}
}
