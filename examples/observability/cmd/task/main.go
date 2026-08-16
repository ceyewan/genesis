package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/examples/observability/internal/bootstrap"
	"github.com/ceyewan/genesis/examples/observability/internal/proto"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	natsEndpoint    = "nats://localhost:4222"
	orderSubject    = "orders.created"
	callbackTarget  = "localhost:9091"
	shutdownTimeout = 10 * time.Second
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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (retErr error) {
	appCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	obs, err := bootstrap.Init("obs-task", 9103)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			obs.Logger.Fatal("Task stopped with error", clog.Error(retErr))
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		retErr = xerrors.Combine(retErr, obs.Shutdown(shutdownCtx))
	}()

	natsConn, err := connector.NewNATS(
		&connector.NATSConfig{URL: getenv("NATS_URL", natsEndpoint)},
		connector.WithLogger(obs.Logger),
	)
	if err != nil {
		return xerrors.Wrap(err, "new nats connector")
	}
	defer func() { _ = natsConn.Close() }()
	if err := natsConn.Connect(appCtx); err != nil {
		return xerrors.Wrap(err, "connect nats")
	}

	mqClient, err := mq.New(
		&mq.Config{Driver: mq.DriverNATSJetStream, JetStream: &mq.JetStreamConfig{AutoCreateStream: true}},
		mq.WithNATSConnector(natsConn),
		mq.WithLogger(obs.Logger),
		mq.WithMeter(obs.Meter),
	)
	if err != nil {
		return xerrors.Wrap(err, "new mq")
	}
	defer func() { _ = mqClient.Close() }()

	callbackConn, err := grpc.NewClient(
		getenv("GATEWAY_CALLBACK_TARGET", callbackTarget),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
	)
	if err != nil {
		return xerrors.Wrap(err, "create callback grpc client")
	}
	defer func() { _ = callbackConn.Close() }()
	callbackClient := proto.NewGatewayCallbackServiceClient(callbackConn)
	tracer := otel.Tracer("obs-task")

	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	subscription, err := mqClient.Subscribe(workerCtx, orderSubject, func(msg mq.Message) error {
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

		event := orderCreatedEvent{}
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			trace.MarkSpanError(consumeSpan, err)
			obs.Logger.ErrorContext(consumeCtx, "Unmarshal order event failed", clog.Error(err))
			return err
		}
		consumeSpan.SetAttributes(
			attribute.String("order.id", event.OrderID),
			attribute.String("order.user_id", event.UserID),
			attribute.String("order.product_id", event.ProductID),
		)

		handledCtx, handleSpan := tracer.Start(consumeCtx, "task.handle_order_created")
		defer handleSpan.End()
		obs.Logger.InfoContext(handledCtx, "Task received order event",
			clog.String("order_id", event.OrderID),
			clog.String("user_id", event.UserID),
			clog.String("product_id", event.ProductID),
		)

		time.Sleep(30 * time.Millisecond)
		_, err := callbackClient.PushResult(handledCtx, &proto.PushResultRequest{
			Result: &proto.OrderResult{OrderId: event.OrderID, Status: "DONE"},
		})
		if err != nil {
			obs.Logger.ErrorContext(handledCtx, "Push result to gateway failed", clog.Error(err))
			trace.MarkSpanError(handleSpan, err)
			trace.MarkSpanError(consumeSpan, err)
			return err
		}

		obs.Logger.InfoContext(handledCtx, "Task pushed result to gateway", clog.String("order_id", event.OrderID))
		return nil
	}, mq.WithQueueGroup("order-task-workers"), mq.WithAutoAck())
	if err != nil {
		return xerrors.Wrap(err, "subscribe")
	}
	defer func() { _ = subscription.Unsubscribe() }()

	obs.Logger.Info("Task worker started", clog.String("subject", orderSubject))
	<-appCtx.Done()
	obs.Logger.Info("Task shutdown requested")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return xerrors.Wrap(mqClient.Drain(shutdownCtx), "drain mq")
}
