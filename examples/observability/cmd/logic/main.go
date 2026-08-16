package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"github.com/ceyewan/genesis/examples/observability/internal/bootstrap"
	"github.com/ceyewan/genesis/examples/observability/internal/order"
	"github.com/ceyewan/genesis/examples/observability/internal/proto"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	grpcAddr        = ":9090"
	natsEndpoint    = "nats://localhost:4222"
	sqlitePath      = "./examples/observability/logic.sqlite"
	orderSubject    = "orders.created"
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

type orderService struct {
	proto.UnimplementedOrderServiceServer

	logger clog.Logger
	db     db.DB
	mq     mq.MQ
}

func (s *orderService) CreateOrder(ctx context.Context, req *proto.CreateOrderRequest) (*proto.CreateOrderResponse, error) {
	if req.GetUserId() == "" || req.GetProductId() == "" {
		return nil, xerrors.New("user_id and product_id are required")
	}

	span := oteltrace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("order.user_id", req.UserId),
		attribute.String("order.product_id", req.ProductId),
	)

	orderID := "ORD-" + req.UserId + "-" + time.Now().Format("150405.000")
	s.logger.InfoContext(ctx, "Logic creating order",
		clog.String("order_id", orderID),
		clog.String("user_id", req.UserId),
		clog.String("product_id", req.ProductId),
	)

	if err := s.db.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
		if err := tx.Create(&order.Order{
			OrderID:   orderID,
			UserID:    req.UserId,
			ProductID: req.ProductId,
			Status:    "CREATED",
		}).Error; err != nil {
			return xerrors.Wrap(err, "insert order")
		}
		return nil
	}); err != nil {
		return nil, err
	}

	ev := orderCreatedEvent{OrderID: orderID, UserID: req.UserId, ProductID: req.ProductId}
	data, err := json.Marshal(ev)
	if err != nil {
		return nil, xerrors.Wrap(err, "marshal event")
	}

	tracer := otel.Tracer("obs-logic")
	pubCtx, pubSpan, headers := trace.StartProducerSpan(
		ctx,
		tracer,
		trace.SpanNameMQPublish(orderSubject),
		trace.MessagingMeta{
			System:      trace.MessagingSystemNATS,
			Destination: orderSubject,
			Operation:   trace.MessagingOperationPublish,
		},
		attribute.String("order.id", orderID),
	)
	defer pubSpan.End()

	if err := s.mq.Publish(pubCtx, orderSubject, data, mq.WithHeaders(headers)); err != nil {
		trace.MarkSpanError(pubSpan, err)
		return nil, xerrors.Wrap(err, "publish order event")
	}

	return &proto.CreateOrderResponse{OrderId: orderID, Status: "CREATED"}, nil
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

	obs, err := bootstrap.Init("obs-logic", 9102)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			obs.Logger.Fatal("Logic stopped with error", clog.Error(retErr))
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		retErr = xerrors.Combine(retErr, obs.Shutdown(shutdownCtx))
	}()

	grpcMetrics, err := metrics.NewGRPCServerMetrics(obs.Meter, metrics.DefaultGRPCServerMetricsConfig("obs-logic"))
	if err != nil {
		return xerrors.Wrap(err, "create grpc metrics")
	}

	natsConnCfg := &connector.NATSConfig{URL: getenv("NATS_URL", natsEndpoint)}
	natsConn, err := connector.NewNATS(natsConnCfg, connector.WithLogger(obs.Logger))
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

	sqliteConn, err := connector.NewSQLite(
		&connector.SQLiteConfig{Path: getenv("SQLITE_PATH", sqlitePath)},
		connector.WithLogger(obs.Logger),
	)
	if err != nil {
		return xerrors.Wrap(err, "new sqlite connector")
	}
	defer func() { _ = sqliteConn.Close() }()
	if err := sqliteConn.Connect(appCtx); err != nil {
		return xerrors.Wrap(err, "connect sqlite")
	}

	database, err := db.New(
		&db.Config{Driver: "sqlite"},
		db.WithSQLiteConnector(sqliteConn),
		db.WithLogger(obs.Logger),
		db.WithTracer(otel.GetTracerProvider()),
	)
	if err != nil {
		return xerrors.Wrap(err, "new db")
	}
	if err := database.DB(appCtx).AutoMigrate(&order.Order{}); err != nil {
		return xerrors.Wrap(err, "auto migrate")
	}

	lis, err := net.Listen("tcp", getenv("LOGIC_GRPC_ADDR", grpcAddr))
	if err != nil {
		return xerrors.Wrap(err, "listen grpc")
	}
	defer func() { _ = lis.Close() }()

	server := grpc.NewServer(
		grpc.StatsHandler(trace.GRPCServerStatsHandler()),
		grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
	)
	proto.RegisterOrderServiceServer(server, &orderService{logger: obs.Logger, db: database, mq: mqClient})

	serveErrors := make(chan error, 1)
	go func() {
		obs.Logger.Info("Logic grpc listening", clog.String("addr", lis.Addr().String()))
		if serveErr := server.Serve(lis); serveErr != nil && !xerrors.Is(serveErr, grpc.ErrServerStopped) {
			serveErrors <- xerrors.Wrap(serveErr, "serve logic grpc")
		}
	}()

	select {
	case <-appCtx.Done():
		obs.Logger.Info("Logic shutdown requested")
	case serveErr := <-serveErrors:
		retErr = serveErr
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return xerrors.Combine(retErr, stopGRPC(shutdownCtx, server))
}

func stopGRPC(ctx context.Context, server *grpc.Server) error {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		server.Stop()
		return xerrors.Wrap(ctx.Err(), "shutdown logic grpc")
	}
}
