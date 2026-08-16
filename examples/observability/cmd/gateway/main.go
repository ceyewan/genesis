package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/examples/observability/internal/bootstrap"
	"github.com/ceyewan/genesis/examples/observability/internal/proto"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	httpAddr         = ":8080"
	callbackGRPCAddr = ":9091"
	logicTarget      = "localhost:9090"
	shutdownTimeout  = 10 * time.Second
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
	s.logger.InfoContext(ctx, "Gateway received task result",
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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (retErr error) {
	appCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	obs, err := bootstrap.Init("obs-gateway", 9101)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			obs.Logger.Fatal("Gateway stopped with error", clog.Error(retErr))
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		retErr = xerrors.Combine(retErr, obs.Shutdown(shutdownCtx))
	}()

	httpMetricsCfg := metrics.DefaultHTTPServerMetricsConfig("obs-gateway")
	httpMetricsCfg.RequestDurationName = "http_request_duration_seconds"
	httpMetrics, err := metrics.NewHTTPServerMetrics(obs.Meter, httpMetricsCfg)
	if err != nil {
		return xerrors.Wrap(err, "create http metrics")
	}

	grpcMetrics, err := metrics.NewGRPCServerMetrics(obs.Meter, metrics.DefaultGRPCServerMetricsConfig("obs-gateway"))
	if err != nil {
		return xerrors.Wrap(err, "create grpc metrics")
	}

	logicConn, err := grpc.NewClient(
		getenv("LOGIC_GRPC_TARGET", logicTarget),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
	)
	if err != nil {
		return xerrors.Wrap(err, "create logic grpc client")
	}
	defer func() { _ = logicConn.Close() }()
	logicClient := proto.NewOrderServiceClient(logicConn)

	cbLis, err := net.Listen("tcp", getenv("GATEWAY_CALLBACK_GRPC_ADDR", callbackGRPCAddr))
	if err != nil {
		return xerrors.Wrap(err, "listen callback grpc")
	}
	defer func() { _ = cbLis.Close() }()

	cbSrv := grpc.NewServer(
		grpc.StatsHandler(trace.GRPCServerStatsHandler()),
		grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
	)
	proto.RegisterGatewayCallbackServiceServer(cbSrv, &callbackServer{logger: obs.Logger})

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(trace.GinMiddleware("obs-gateway"))
	r.Use(metrics.GinHTTPMiddleware(httpMetrics))
	r.Use(gin.Recovery())
	r.POST("/orders", func(c *gin.Context) {
		ctx := c.Request.Context()

		if c.GetHeader("Authorization") != "Bearer demo-token" {
			obs.Logger.WarnContext(ctx, "Unauthorized request")
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

		obs.Logger.InfoContext(ctx, "Gateway received order request",
			clog.String("user_id", req.UserID),
			clog.String("product_id", req.ProductID),
		)

		resp, err := logicClient.CreateOrder(ctx, &proto.CreateOrderRequest{
			UserId:    req.UserID,
			ProductId: req.ProductID,
		})
		if err != nil {
			obs.Logger.ErrorContext(ctx, "Logic grpc failed", clog.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"order_id": resp.OrderId,
			"status":   resp.Status,
			"hint":     "task result will be pushed back to gateway via gRPC",
		})
	})

	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErrors := make(chan error, 2)
	go func() {
		obs.Logger.Info("Gateway callback grpc listening", clog.String("addr", cbLis.Addr().String()))
		if serveErr := cbSrv.Serve(cbLis); serveErr != nil && !xerrors.Is(serveErr, grpc.ErrServerStopped) {
			serveErrors <- xerrors.Wrap(serveErr, "serve callback grpc")
		}
	}()
	go func() {
		obs.Logger.Info("Gateway http listening", clog.String("addr", httpAddr))
		if serveErr := httpServer.ListenAndServe(); serveErr != nil && !xerrors.Is(serveErr, http.ErrServerClosed) {
			serveErrors <- xerrors.Wrap(serveErr, "serve gateway http")
		}
	}()

	select {
	case <-appCtx.Done():
		obs.Logger.Info("Gateway shutdown requested")
	case serveErr := <-serveErrors:
		retErr = serveErr
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	httpErr := httpServer.Shutdown(shutdownCtx)
	grpcErr := stopGRPC(shutdownCtx, cbSrv)
	return xerrors.Combine(retErr, xerrors.Wrap(httpErr, "shutdown gateway http"), grpcErr)
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
		return xerrors.Wrap(ctx.Err(), "shutdown callback grpc")
	}
}
