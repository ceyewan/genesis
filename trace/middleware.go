package trace

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc/stats"
)

// GinMiddleware 返回一个可重用的 Gin 跟踪中间件
func GinMiddleware(serviceName string) gin.HandlerFunc {
	return otelgin.Middleware(serviceName)
}

// GRPCServerStatsHandler 返回一个可重用的 gRPC 服务器状态处理程序用于跟踪
func GRPCServerStatsHandler() stats.Handler {
	return otelgrpc.NewServerHandler()
}

// GRPCClientStatsHandler 返回一个可重用的 gRPC 客户端状态处理程序用于跟踪
func GRPCClientStatsHandler() stats.Handler {
	return otelgrpc.NewClientHandler()
}
