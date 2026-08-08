package trace

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc/stats"
)

// HTTPHandler 为标准库 HTTP handler 创建服务端 span 并提取 W3C trace context。
func HTTPHandler(handler http.Handler, operation string, opts ...otelhttp.Option) http.Handler {
	return otelhttp.NewHandler(handler, operation, opts...)
}

// HTTPTransport 为标准库 HTTP client 注入 W3C trace context 并创建客户端 span。
// base 为 nil 时使用 http.DefaultTransport。
func HTTPTransport(base http.RoundTripper, opts ...otelhttp.Option) *otelhttp.Transport {
	return otelhttp.NewTransport(base, opts...)
}

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
