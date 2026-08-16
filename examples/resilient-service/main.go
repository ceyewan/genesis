package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync/atomic"
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
)

func main() {
	logger, err := clog.New(&clog.Config{Level: "info", Format: "console", Output: "stdout"})
	if err != nil {
		log.Fatal(err)
	}

	inventory, inventoryServer, inventoryAddr := startInventory()
	defer inventoryServer.Stop()

	limiter, err := ratelimit.New(&ratelimit.Config{Driver: ratelimit.DriverStandalone}, ratelimit.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer limiter.Close()

	idempotency, err := idem.New(&idem.Config{Driver: idem.DriverMemory, Prefix: "resilient:", DefaultTTL: time.Minute, LockTTL: 10 * time.Second}, idem.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer idempotency.Close()

	brk, err := breaker.New(&breaker.Config{MinimumRequests: 3, FailureRatio: 0.5, Timeout: 3 * time.Second}, breaker.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.NewClient(inventoryAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(ratelimit.GinMiddleware(limiter, &ratelimit.GinMiddlewareOptions{
		KeyFunc:   func(c *gin.Context) string { return c.ClientIP() + ":" + c.FullPath() },
		LimitFunc: func(*gin.Context) ratelimit.Limit { return ratelimit.Limit{Rate: 2, Burst: 3} },
	}))
	router.POST("/orders", idempotency.GinMiddleware(), createOrder(conn, brk))
	router.POST("/debug/inventory/:mode", func(c *gin.Context) {
		inventory.unavailable.Store(c.Param("mode") == "unavailable")
		c.Status(http.StatusNoContent)
	})
	router.GET("/debug/breaker", func(c *gin.Context) {
		state, stateErr := brk.State(inventoryAddr)
		if stateErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": stateErr.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"state": state.String()})
	})

	addr := "127.0.0.1:8083"
	fmt.Printf("Gateway: http://%s\n", addr)
	fmt.Println("正常请求: curl -i -X POST http://127.0.0.1:8083/orders -H 'X-Idempotency-Key: order-1'")
	fmt.Println("下游故障: curl -X POST http://127.0.0.1:8083/debug/inventory/unavailable")
	if err := router.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func createOrder(conn *grpc.ClientConn, brk breaker.Breaker) gin.HandlerFunc {
	client := grpc_health_v1.NewHealthClient(conn)
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		if _, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err != nil {
			state, _ := brk.State(conn.Target())
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inventory unavailable", "breaker": state.String()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"order_id": "order-" + c.GetHeader("X-Idempotency-Key"), "status": "created"})
	}
}

type inventoryServer struct {
	grpc_health_v1.UnimplementedHealthServer
	unavailable atomic.Bool
}

func (s *inventoryServer) Check(context.Context, *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if s.unavailable.Load() {
		return nil, status.Error(codes.Unavailable, "inventory is unavailable")
	}
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func startInventory() (*inventoryServer, *grpc.Server, string) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	server := grpc.NewServer()
	inventory := &inventoryServer{}
	grpc_health_v1.RegisterHealthServer(server, inventory)
	go func() { _ = server.Serve(lis) }()
	return inventory, server, lis.Addr().String()
}
