package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// InstrumentationName 是用于此遥测工具的名称。
	InstrumentationName = "genesis/telemetry"
)

var (
	// tracer 是拦截器使用的全局追踪实例。
	tracer = otel.Tracer(InstrumentationName)
	// meter 是拦截器使用的全局仪表实例。
	meter = otel.Meter(InstrumentationName)

	// gRPC 服务器指标
	grpcServerRequests metric.Int64Counter
	grpcServerDuration metric.Float64Histogram

	// gRPC 客户端指标
	grpcClientRequests metric.Int64Counter
	grpcClientDuration metric.Float64Histogram

	// HTTP 服务器指标
	httpServerRequests metric.Int64Counter
	httpServerDuration metric.Float64Histogram
)

func init() {
	var err error
	// 初始化指标
	// 注意：在真实场景中，我们可能想更好地处理错误，
	// 但 init() 无法返回错误。我们记录它们。

	grpcServerRequests, err = meter.Int64Counter("rpc.server.requests.count", metric.WithDescription("收到的 gRPC 请求数。"))
	if err != nil {
		slog.Error("failed to create grpc server requests counter", "error", err)
	}

	grpcServerDuration, err = meter.Float64Histogram("rpc.server.duration", metric.WithDescription("gRPC 请求的持续时间（秒）。"), metric.WithUnit("s"))
	if err != nil {
		slog.Error("failed to create grpc server duration histogram", "error", err)
	}

	grpcClientRequests, err = meter.Int64Counter("rpc.client.requests.count", metric.WithDescription("发送的 gRPC 请求数。"))
	if err != nil {
		slog.Error("failed to create grpc client requests counter", "error", err)
	}

	grpcClientDuration, err = meter.Float64Histogram("rpc.client.duration", metric.WithDescription("gRPC 客户端请求的持续时间（秒）。"), metric.WithUnit("s"))
	if err != nil {
		slog.Error("failed to create grpc client duration histogram", "error", err)
	}

	httpServerRequests, err = meter.Int64Counter("http.server.requests.count", metric.WithDescription("收到的 HTTP 请求数。"))
	if err != nil {
		slog.Error("failed to create http server requests counter", "error", err)
	}

	httpServerDuration, err = meter.Float64Histogram("http.server.duration", metric.WithDescription("HTTP 请求的持续时间（秒）。"), metric.WithUnit("s"))
	if err != nil {
		slog.Error("failed to create http server duration histogram", "error", err)
	}
}

// GRPCServerInterceptor 返回一个用于遥测的 gRPC 一元服务器拦截器。
// 它自动为传入的 gRPC 调用创建 span、记录指标并传播追踪上下文。
func GRPCServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 提取追踪上下文
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		ctx = otel.GetTextMapPropagator().Extract(ctx, &grpcMetadataCarrier{MD: md})

		// 启动 span
		spanCtx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.RPCSystemGRPC,
				semconv.RPCServiceKey.String(info.FullMethod)))
		defer span.End()

		startTime := time.Now()
		resp, err := handler(spanCtx, req)
		duration := time.Since(startTime)
		statusCode := status.Code(err)

		// 记录指标
		attrs := attribute.NewSet(
			semconv.RPCSystemGRPC,
			semconv.RPCServiceKey.String(info.FullMethod),
			semconv.RPCGRPCStatusCodeKey.Int(int(statusCode)),
		)
		if grpcServerRequests != nil {
			grpcServerRequests.Add(spanCtx, 1, metric.WithAttributeSet(attrs))
		}
		if grpcServerDuration != nil {
			grpcServerDuration.Record(spanCtx, duration.Seconds(), metric.WithAttributeSet(attrs))
		}

		// 设置 span 状态
		sCode, sMsg := statusCodeToSpanStatus(statusCode)
		span.SetStatus(sCode, sMsg)
		if err != nil {
			span.RecordError(err)
		}

		// 日志记录（简化版）
		if err != nil {
			slog.Warn("gRPC request failed", "method", info.FullMethod, "status", statusCode, "error", err, "duration", duration)
		} else if duration > 500*time.Millisecond {
			slog.Warn("gRPC request slow", "method", info.FullMethod, "duration", duration)
		}

		return resp, err
	}
}

// GRPCClientInterceptor 返回一个用于遥测的 gRPC 一元客户端拦截器。
// 它自动为传出的 gRPC 调用创建 span、记录指标并注入追踪上下文。
func GRPCClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		spanCtx, span := tracer.Start(ctx, method,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				semconv.RPCSystemGRPC,
				semconv.RPCServiceKey.String(method)))
		defer span.End()

		// 注入追踪上下文
		md, ok := metadata.FromOutgoingContext(spanCtx)
		if !ok {
			md = metadata.New(nil)
		} else {
			md = md.Copy()
		}
		otel.GetTextMapPropagator().Inject(spanCtx, &grpcMetadataCarrier{MD: md})
		spanCtx = metadata.NewOutgoingContext(spanCtx, md)

		startTime := time.Now()
		err := invoker(spanCtx, method, req, reply, cc, opts...)
		duration := time.Since(startTime)
		statusCode := status.Code(err)

		// 记录指标
		attrs := attribute.NewSet(
			semconv.RPCSystemGRPC,
			semconv.RPCServiceKey.String(method),
			semconv.RPCGRPCStatusCodeKey.Int(int(statusCode)),
		)
		if grpcClientRequests != nil {
			grpcClientRequests.Add(spanCtx, 1, metric.WithAttributeSet(attrs))
		}
		if grpcClientDuration != nil {
			grpcClientDuration.Record(spanCtx, duration.Seconds(), metric.WithAttributeSet(attrs))
		}

		sCode, sMsg := statusCodeToSpanStatus(statusCode)
		span.SetStatus(sCode, sMsg)
		if err != nil {
			span.RecordError(err)
		}

		return err
	}
}

// HTTPMiddleware 返回一个用于遥测的 Gin 中间件。
// 它自动为传入的 HTTP 请求创建 span、记录指标并传播追踪上下文。
func HTTPMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		spanCtx, span := tracer.Start(ctx, c.FullPath(),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPMethodKey.String(c.Request.Method),
				semconv.HTTPURLKey.String(c.Request.URL.String()),
				semconv.NetHostNameKey.String(c.Request.Host),
			))
		defer span.End()

		c.Request = c.Request.WithContext(spanCtx)

		startTime := time.Now()
		c.Next()
		duration := time.Since(startTime)
		statusCode := c.Writer.Status()

		attrs := attribute.NewSet(
			semconv.HTTPMethodKey.String(c.Request.Method),
			semconv.HTTPRouteKey.String(c.FullPath()),
			semconv.HTTPStatusCodeKey.Int(statusCode),
		)
		if httpServerRequests != nil {
			httpServerRequests.Add(spanCtx, 1, metric.WithAttributeSet(attrs))
		}
		if httpServerDuration != nil {
			httpServerDuration.Record(spanCtx, duration.Seconds(), metric.WithAttributeSet(attrs))
		}

		sCode, sMsg := httpStatusCodeToSpanStatus(statusCode)
		span.SetStatus(sCode, sMsg)
		if len(c.Errors) > 0 {
			span.RecordError(c.Errors.Last().Err)
		}

		if len(c.Errors) > 0 || statusCode >= 500 {
			slog.Error("HTTP request failed", "method", c.Request.Method, "path", c.FullPath(), "status", statusCode, "duration", duration)
		} else if duration > 500*time.Millisecond {
			slog.Warn("HTTP request slow", "method", c.Request.Method, "path", c.FullPath(), "duration", duration)
		}
	}
}

// statusCodeToSpanStatus 将 gRPC 状态码转换为 OpenTelemetry span 状态码和消息。
func statusCodeToSpanStatus(code codes.Code) (otelcodes.Code, string) {
	if code >= codes.OK && code < codes.Canceled {
		return otelcodes.Ok, ""
	}
	return otelcodes.Error, code.String()
}

// httpStatusCodeToSpanStatus 将 HTTP 状态码转换为 OpenTelemetry span 状态码和消息。
func httpStatusCodeToSpanStatus(code int) (otelcodes.Code, string) {
	if code >= 200 && code < 400 {
		return otelcodes.Ok, ""
	}
	return otelcodes.Error, ""
}

// grpcMetadataCarrier 为 gRPC 元数据实现 propagation.TextMapCarrier 接口。
type grpcMetadataCarrier struct {
	MD metadata.MD
}

// Get 返回给定键的值。
func (c *grpcMetadataCarrier) Get(key string) string {
	vals := c.MD.Get(key)
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// Set 设置给定键的值。
func (c *grpcMetadataCarrier) Set(key, value string) {
	c.MD.Set(key, value)
}

// Keys 返回元数据中的所有键。
func (c *grpcMetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c.MD))
	for k := range c.MD {
		keys = append(keys, k)
	}
	return keys
}
