package telemetry

import (
	"context"

	"github.com/ceyewan/genesis/internal/telemetry"
	implmetrics "github.com/ceyewan/genesis/internal/telemetry/metrics"
	impltrace "github.com/ceyewan/genesis/internal/telemetry/trace"
	"github.com/ceyewan/genesis/pkg/telemetry/types"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
)

// Config 是遥测系统的配置。
type Config = types.Config

// Telemetry 是遥测系统的主入口。
// 它提供对 Meter 和 Tracer 的访问，并返回用于自动遥测收集的中间件/拦截器。
type Telemetry interface {
	// Meter 返回用于创建指标的仪表。
	Meter() types.Meter

	// Tracer 返回用于创建 span 的追踪器。
	Tracer() types.Tracer

	// GRPCServerInterceptor 返回用于遥测的 gRPC 一元服务器拦截器。
	GRPCServerInterceptor() grpc.UnaryServerInterceptor

	// GRPCClientInterceptor 返回用于遥测的 gRPC 一元客户端拦截器。
	GRPCClientInterceptor() grpc.UnaryClientInterceptor

	// HTTPMiddleware 返回用于遥测的 Gin 中间件。
	HTTPMiddleware() gin.HandlerFunc

	// Shutdown 优雅地关闭遥测系统。
	Shutdown(ctx context.Context) error
}

// telemetryImpl 是 Telemetry 接口的具体实现。
type telemetryImpl struct {
	provider *telemetry.Provider
	meter    types.Meter
	tracer   types.Tracer
}

// New 创建一个新的 Telemetry 实例。
// 它初始化底层的 OpenTelemetry 提供程序并返回一个 Telemetry 接口。
// 调用者负责调用 Shutdown 来清理资源。
func New(cfg *Config) (Telemetry, error) {
	internalCfg := &telemetry.Config{
		ServiceName:          cfg.ServiceName,
		ExporterType:         cfg.ExporterType,
		ExporterEndpoint:     cfg.ExporterEndpoint,
		PrometheusListenAddr: cfg.PrometheusListenAddr,
		SamplerType:          cfg.SamplerType,
		SamplerRatio:         cfg.SamplerRatio,
	}

	p, err := telemetry.NewFactory(internalCfg)
	if err != nil {
		return nil, err
	}

	// 创建包装器
	// 注意：默认情况下，我们使用服务名称作为仪表范围名称
	otelMeter := p.MeterProvider.Meter(cfg.ServiceName)
	meter := implmetrics.NewMeter(otelMeter)

	otelTracer := p.TracerProvider.Tracer(cfg.ServiceName)
	tracer := impltrace.NewTracer(otelTracer)

	return &telemetryImpl{
		provider: p,
		meter:    meter,
		tracer:   tracer,
	}, nil
}

// Meter 返回用于创建指标的 Meter 实例。
func (t *telemetryImpl) Meter() types.Meter {
	return t.meter
}

// Tracer 返回用于创建 span 的 Tracer 实例。
func (t *telemetryImpl) Tracer() types.Tracer {
	return t.tracer
}

// GRPCServerInterceptor 返回用于自动遥测收集的 gRPC 一元服务器拦截器。
func (t *telemetryImpl) GRPCServerInterceptor() grpc.UnaryServerInterceptor {
	return telemetry.GRPCServerInterceptor()
}

// GRPCClientInterceptor 返回用于自动遥测收集的 gRPC 一元客户端拦截器。
func (t *telemetryImpl) GRPCClientInterceptor() grpc.UnaryClientInterceptor {
	return telemetry.GRPCClientInterceptor()
}

// HTTPMiddleware 返回用于自动遥测收集的 Gin 中间件。
func (t *telemetryImpl) HTTPMiddleware() gin.HandlerFunc {
	return telemetry.HTTPMiddleware()
}

// Shutdown 优雅地关闭遥测系统，释放相关资源。
func (t *telemetryImpl) Shutdown(ctx context.Context) error {
	return t.provider.ShutdownFunc(ctx)
}
