package trace

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/xerrors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

// Init 初始化全局 TracerProvider
//
// 该函数会创建一个连接到指定 Endpoint (如 Tempo/Jaeger) 的 TracerProvider，
// 并将其设置为全局 Provider。同时也会设置 TextMapPropagator 用于跨进程 Context 传播。
//
// 返回值是一个 Shutdown 函数，调用者应在应用退出时调用它以刷新剩余数据。
func Init(cfg *Config) (func(context.Context) error, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	ctx := context.Background()

	// 1. 创建 OTLP gRPC Exporter
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(5 * time.Second),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create otlp exporter")
	}

	// 2. 创建 Resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create resource")
	}

	// 3. 配置 TracerProvider
	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Sampler))),
	}

	if cfg.Batcher == "simple" {
		tpOpts = append(tpOpts, sdktrace.WithSyncer(exporter))
	} else {
		// 默认使用 Batcher，更适合高吞吐场景
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exporter))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)

	// 4. 设置全局 Provider
	otel.SetTracerProvider(tp)

	// 5. 设置全局 Propagator
	// TraceContext: W3C 标准 (traceparent header)
	// Baggage: 用于在链路中透传自定义 KV
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return xerrors.Wrap(xerrors.ErrInvalidInput, "config is required")
	}
	if cfg.ServiceName == "" {
		return xerrors.Wrap(xerrors.ErrInvalidInput, "service_name is required")
	}
	if cfg.Endpoint == "" {
		return xerrors.Wrap(xerrors.ErrInvalidInput, "endpoint is required")
	}
	if cfg.Sampler < 0 || cfg.Sampler > 1 {
		return xerrors.Wrapf(xerrors.ErrInvalidInput, "sampler must be between 0 and 1, got %v", cfg.Sampler)
	}
	if cfg.Batcher != "" && cfg.Batcher != "batch" && cfg.Batcher != "simple" {
		return xerrors.Wrapf(xerrors.ErrInvalidInput, "batcher must be \"batch\" or \"simple\", got %q", cfg.Batcher)
	}
	return nil
}
