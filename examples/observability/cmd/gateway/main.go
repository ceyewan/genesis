package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/examples/observability/internal/bootstrap"
	"github.com/ceyewan/genesis/examples/observability/proto"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	httpAddr         = ":8080"
	callbackGRPCAddr = ":9091"
	logicTarget      = "localhost:9090"
)

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

type callbackServer struct {
	proto.UnimplementedGatewayCallbackServiceServer

	logger clog.Logger

	mu      sync.Mutex
	results map[string]string
}

func (s *callbackServer) PushResult(ctx context.Context, req *proto.PushResultRequest) (*proto.PushResultResponse, error) {
	if req.GetResult() == nil {
		return nil, xerrors.New("missing result")
	}
	s.logger.InfoContext(ctx, "gateway received task result",
		clog.String("order_id", req.Result.OrderId),
		clog.String("status", req.Result.Status),
	)

	s.mu.Lock()
	if s.results == nil {
		s.results = make(map[string]string)
	}
	s.results[req.Result.OrderId] = req.Result.Status
	s.mu.Unlock()

	return &proto.PushResultResponse{Ok: true}, nil
}

func main() {
	ctx := context.Background()

	obs, shutdowns, err := bootstrap.Init(ctx, "obs-gateway", 9101)
	if err != nil {
		panic(err)
	}
	for i := len(shutdowns) - 1; i >= 0; i-- {
		defer func(fn bootstrap.Shutdown) { _ = fn(ctx) }(shutdowns[i])
	}

	httpMetricsCfg := metrics.DefaultHTTPServerMetricsConfig("obs-gateway")
	httpMetricsCfg.RequestDurationName = "http_request_duration_seconds"
	httpMetrics, err := metrics.NewHTTPServerMetrics(obs.Meter, httpMetricsCfg)
	if err != nil {
		obs.Logger.Fatal("create http metrics failed", clog.Error(err))
	}

	grpcMetrics, err := metrics.NewGRPCServerMetrics(obs.Meter, metrics.DefaultGRPCServerMetricsConfig("obs-gateway"))
	if err != nil {
		obs.Logger.Fatal("create grpc metrics failed", clog.Error(err))
	}

	logicConn, err := grpc.NewClient(
		getenv("LOGIC_GRPC_TARGET", logicTarget),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
	)
	if err != nil {
		obs.Logger.Fatal("connect logic failed", clog.Error(err))
	}
	defer func() { _ = logicConn.Close() }()
	logicClient := proto.NewOrderServiceClient(logicConn)

	cbLis, err := net.Listen("tcp", getenv("GATEWAY_CALLBACK_GRPC_ADDR", callbackGRPCAddr))
	if err != nil {
		obs.Logger.Fatal("listen callback grpc failed", clog.Error(err))
	}

	cbSrv := grpc.NewServer(
		grpc.StatsHandler(trace.GRPCServerStatsHandler()),
		grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
	)
	proto.RegisterGatewayCallbackServiceServer(cbSrv, &callbackServer{logger: obs.Logger})
	go func() {
		obs.Logger.Info("gateway callback grpc listening", clog.String("addr", cbLis.Addr().String()))
		if err := cbSrv.Serve(cbLis); err != nil {
			obs.Logger.Error("callback grpc serve failed", clog.Error(err))
		}
	}()

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(trace.GinMiddleware("obs-gateway"))
	r.Use(metrics.GinHTTPMiddleware(httpMetrics))

	r.POST("/orders", func(c *gin.Context) {
		ctx := c.Request.Context()

		auth := c.GetHeader("Authorization")
		if auth != "Bearer demo-token" {
			obs.Logger.WarnContext(ctx, "unauthorized request", clog.String("authorization", auth))
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		var req struct {
			UserID    string `json:"user_id"`
			ProductID string `json:"product_id"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		obs.Logger.InfoContext(ctx, "gateway received order request",
			clog.String("user_id", req.UserID),
			clog.String("product_id", req.ProductID),
		)

		resp, err := logicClient.CreateOrder(ctx, &proto.CreateOrderRequest{
			UserId:    req.UserID,
			ProductId: req.ProductID,
		})
		if err != nil {
			obs.Logger.ErrorContext(ctx, "logic grpc failed", clog.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"order_id": resp.OrderId,
			"status":   resp.Status,
			"hint":     "task result will be pushed back to gateway via gRPC",
		})
	})

	srv := &http.Server{
		Addr:              httpAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	obs.Logger.Info("gateway http listening", clog.String("addr", httpAddr))
	if err := srv.ListenAndServe(); err != nil && !xerrors.Is(err, http.ErrServerClosed) {
		obs.Logger.Fatal("gateway http failed", clog.Error(err))
	}
}
