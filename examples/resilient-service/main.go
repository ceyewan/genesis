package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/ceyewan/genesis/breaker"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/idem"
	"github.com/ceyewan/genesis/ratelimit"
	"github.com/ceyewan/genesis/xerrors"
)

type gateway struct {
	router      *gin.Engine
	inventory   *inventoryServer
	server      *grpc.Server
	conn        *grpc.ClientConn
	limiter     ratelimit.Limiter
	idempotency idem.Idempotency
	breaker     breaker.Breaker
	orders      atomic.Int32
}

func newGateway(logger clog.Logger, limit ratelimit.Limit, cfg *breaker.Config) (g *gateway, err error) {
	g = &gateway{inventory: &inventoryServer{}}
	defer func() {
		if err != nil {
			err = xerrors.Join(err, g.close())
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return g, err
	}
	g.server = grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g.server, g.inventory)
	go func() {
		if err := g.server.Serve(listener); err != nil {
			logger.Error("Inventory server failed", clog.Error(err))
		}
	}()
	g.limiter, err = ratelimit.New(&ratelimit.Config{Driver: ratelimit.DriverStandalone}, ratelimit.WithLogger(logger))
	if err != nil {
		return g, err
	}
	g.idempotency, err = idem.New(&idem.Config{Driver: idem.DriverMemory, Prefix: "resilient:", DefaultTTL: time.Minute}, idem.WithLogger(logger))
	if err != nil {
		return g, err
	}
	g.breaker, err = breaker.New(cfg, breaker.WithLogger(logger))
	if err != nil {
		return g, err
	}
	g.conn, err = grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(g.breaker.UnaryClientInterceptor()))
	if err != nil {
		return g, err
	}
	g.router = gin.New()
	g.router.Use(gin.Recovery())
	limiter := ratelimit.GinMiddleware(g.limiter, &ratelimit.GinMiddlewareOptions{KeyFunc: func(c *gin.Context) string { return c.ClientIP() + ":" + c.FullPath() }, LimitFunc: func(*gin.Context) ratelimit.Limit { return limit }})
	// 若增加认证，必须置于幂等之前并提供可信 identity scope。
	g.router.POST("/orders", limiter, g.idempotency.GinMiddleware(), g.createOrder)
	g.router.POST("/debug/inventory/:mode", func(c *gin.Context) {
		switch c.Param("mode") {
		case "available":
			g.inventory.mode.Store(0)
		case "unavailable":
			g.inventory.mode.Store(1)
		case "business-error":
			g.inventory.mode.Store(2)
		default:
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})
	g.router.GET("/debug/breaker", func(c *gin.Context) {
		state, err := g.breaker.State(g.conn.Target())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"state": state.String()})
	})
	return g, nil
}

func (g *gateway) close() error {
	var err error
	if g.conn != nil {
		err = xerrors.Join(err, g.conn.Close())
	}
	if g.idempotency != nil {
		err = xerrors.Join(err, g.idempotency.Close())
	}
	if g.limiter != nil {
		err = xerrors.Join(err, g.limiter.Close())
	}
	if g.server != nil {
		g.server.Stop()
	}
	return err
}

func (g *gateway) createOrder(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	_, err := grpc_health_v1.NewHealthClient(g.conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		code := http.StatusServiceUnavailable
		if status.Code(err) == codes.FailedPrecondition {
			code = http.StatusConflict
		}
		c.JSON(code, gin.H{"error": status.Code(err).String()})
		return
	}
	g.orders.Add(1)
	c.JSON(http.StatusCreated, gin.H{"order_id": "order-" + c.GetHeader("X-Idempotency-Key"), "status": "created"})
}

type inventoryServer struct {
	grpc_health_v1.UnimplementedHealthServer
	mode  atomic.Int32
	calls atomic.Int32
}

func (s *inventoryServer) Check(context.Context, *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	s.calls.Add(1)
	switch s.mode.Load() {
	case 1:
		return nil, status.Error(codes.Unavailable, "inventory unavailable")
	case 2:
		return nil, status.Error(codes.FailedPrecondition, "inventory rejected order")
	}
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func run(ctx context.Context) (err error) {
	logger, err := clog.New(&clog.Config{Format: "console", Output: "stdout"})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, logger.Close()) }()
	g, err := newGateway(logger, ratelimit.Limit{Rate: 2, Burst: 3}, &breaker.Config{MinimumRequests: 3, FailureRatio: 0.5, Timeout: 3 * time.Second})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, g.close()) }()
	listener, err := net.Listen("tcp", "127.0.0.1:8083")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: g.router, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Println("Gateway: http://127.0.0.1:8083 (local debug endpoints only)")
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(cleanup); err != nil {
		return xerrors.Join(err, server.Close())
	}
	if err := <-done; !xerrors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
