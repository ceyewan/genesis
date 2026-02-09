package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
	"github.com/ceyewan/genesis/examples/observability/internal/bootstrap"
	"github.com/ceyewan/genesis/examples/observability/internal/order"
	"github.com/ceyewan/genesis/examples/observability/proto"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

const (
	grpcAddr     = ":9090"
	natsEndpoint = "nats://localhost:4222"
	sqlitePath   = "./examples/observability/logic.sqlite"
	orderSubject = "orders.created"
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

	s.logger.InfoContext(ctx, "logic creating order",
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

	ev := orderCreatedEvent{
		OrderID:   orderID,
		UserID:    req.UserId,
		ProductID: req.ProductId,
	}
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

	return &proto.CreateOrderResponse{
		OrderId: orderID,
		Status:  "CREATED",
	}, nil
}

func main() {
	ctx := context.Background()

	obs, shutdowns, err := bootstrap.Init(ctx, "obs-logic", 9102)
	if err != nil {
		panic(err)
	}
	for i := len(shutdowns) - 1; i >= 0; i-- {
		defer func(fn bootstrap.Shutdown) { _ = fn(ctx) }(shutdowns[i])
	}

	grpcMetrics, err := metrics.NewGRPCServerMetrics(obs.Meter, metrics.DefaultGRPCServerMetricsConfig("obs-logic"))
	if err != nil {
		obs.Logger.Fatal("create grpc metrics failed", clog.Error(err))
	}

	natsConnCfg := &connector.NATSConfig{URL: getenv("NATS_URL", natsEndpoint)}
	natsConn, err := connector.NewNATS(natsConnCfg, connector.WithLogger(obs.Logger))
	if err != nil {
		obs.Logger.Fatal("new nats connector failed", clog.Error(err))
	}
	defer func() { _ = natsConn.Close() }()
	if err := natsConn.Connect(ctx); err != nil {
		obs.Logger.Fatal("connect nats failed", clog.Error(err))
	}

	mqClient, err := mq.New(&mq.Config{Driver: mq.DriverNATSCore}, mq.WithNATSConnector(natsConn), mq.WithLogger(obs.Logger), mq.WithMeter(obs.Meter))
	if err != nil {
		obs.Logger.Fatal("new mq failed", clog.Error(err))
	}
	defer func() { _ = mqClient.Close() }()

	sqliteConn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: getenv("SQLITE_PATH", sqlitePath)}, connector.WithLogger(obs.Logger))
	if err != nil {
		obs.Logger.Fatal("new sqlite connector failed", clog.Error(err))
	}
	defer func() { _ = sqliteConn.Close() }()
	if err := sqliteConn.Connect(ctx); err != nil {
		obs.Logger.Fatal("connect sqlite failed", clog.Error(err))
	}

	database, err := db.New(&db.Config{Driver: "sqlite"},
		db.WithSQLiteConnector(sqliteConn),
		db.WithLogger(obs.Logger),
		db.WithTracer(otel.GetTracerProvider()),
	)
	if err != nil {
		obs.Logger.Fatal("new db failed", clog.Error(err))
	}

	if err := database.DB(ctx).AutoMigrate(&order.Order{}); err != nil {
		obs.Logger.Fatal("auto migrate failed", clog.Error(err))
	}

	lis, err := net.Listen("tcp", getenv("LOGIC_GRPC_ADDR", grpcAddr))
	if err != nil {
		obs.Logger.Fatal("listen grpc failed", clog.Error(err))
	}

	srv := grpc.NewServer(
		grpc.StatsHandler(trace.GRPCServerStatsHandler()),
		grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
	)
	proto.RegisterOrderServiceServer(srv, &orderService{
		logger: obs.Logger,
		db:     database,
		mq:     mqClient,
	})

	obs.Logger.Info("logic grpc listening", clog.String("addr", lis.Addr().String()))
	if err := srv.Serve(lis); err != nil {
		obs.Logger.Fatal("logic grpc serve failed", clog.Error(err))
	}
}
