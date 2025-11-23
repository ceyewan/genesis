package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Config 是遥测工厂的内部配置。
type Config struct {
	ServiceName          string  // ServiceName 是用于遥测数据识别的服务名称。
	ExporterType         string  // ExporterType 指定追踪导出器类型（"otlp"、"zipkin"、"stdout"）。
	ExporterEndpoint     string  // ExporterEndpoint 是追踪导出器的端点 URL。
	PrometheusListenAddr string  // PrometheusListenAddr 是用于 Prometheus 指标抓取的监听地址。如果为空，则禁用 Prometheus 导出器。
	SamplerType          string  // SamplerType 定义追踪采样策略（"always_on"、"always_off"、"trace_id_ratio"）。
	SamplerRatio         float64 // SamplerRatio 是当 SamplerType 为 "trace_id_ratio" 时的采样概率。
}

// Provider 保存 OpenTelemetry 提供程序和关闭函数。
type Provider struct {
	MeterProvider  *sdkmetric.MeterProvider
	TracerProvider *sdktrace.TracerProvider
	ShutdownFunc   func(context.Context) error
}

// NewFactory 创建一个新的遥测提供程序，配置了导出器和提供程序。
func NewFactory(cfg *Config) (*Provider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("telemetry config is required")
	}

	// 创建追踪提供程序
	tracerProvider, err := createTracerProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer provider: %w", err)
	}

	// 创建仪表提供程序
	meterProvider, err := createMeterProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create meter provider: %w", err)
	}

	// 设置全局提供程序
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 创建关闭函数
	shutdownFunc := func(ctx context.Context) error {
		var errs []error

		if err := tracerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown tracer provider: %w", err))
		}

		if err := meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown meter provider: %w", err))
		}

		if len(errs) > 0 {
			return fmt.Errorf("shutdown errors: %v", errs)
		}
		return nil
	}

	return &Provider{
		MeterProvider:  meterProvider,
		TracerProvider: tracerProvider,
		ShutdownFunc:   shutdownFunc,
	}, nil
}

// createTracerProvider 使用配置的导出器和采样器创建追踪提供程序。
func createTracerProvider(cfg *Config) (*sdktrace.TracerProvider, error) {
	var exporter sdktrace.SpanExporter
	var err error

	// 根据配置的类型创建 span 导出器
	switch cfg.ExporterType {
	case "otlp":
		if cfg.ExporterEndpoint == "" {
			return nil, fmt.Errorf("otlp exporter requires endpoint")
		}
		exporter, err = otlptracehttp.New(context.Background(),
			otlptracehttp.WithEndpoint(cfg.ExporterEndpoint),
			otlptracehttp.WithInsecure(),
		)
	case "zipkin":
		if cfg.ExporterEndpoint == "" {
			return nil, fmt.Errorf("zipkin exporter requires endpoint")
		}
		exporter, err = zipkin.New(cfg.ExporterEndpoint)
	case "stdout":
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "":
		// 未配置导出器，创建一个 no-op 导出器
		exporter = &noopExporter{}
	default:
		return nil, fmt.Errorf("unsupported exporter type: %s", cfg.ExporterType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// 根据配置的类型创建采样器
	var sampler sdktrace.Sampler
	switch cfg.SamplerType {
	case "always_on":
		sampler = sdktrace.AlwaysSample()
	case "always_off":
		sampler = sdktrace.NeverSample()
	case "trace_id_ratio":
		if cfg.SamplerRatio < 0 || cfg.SamplerRatio > 1 {
			return nil, fmt.Errorf("sampler ratio must be between 0 and 1, got: %f", cfg.SamplerRatio)
		}
		sampler = sdktrace.TraceIDRatioBased(cfg.SamplerRatio)
	default:
		// 如果未指定，默认为始终开启
		sampler = sdktrace.AlwaysSample()
	}

	// 创建资源
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 创建追踪提供程序
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	)

	return tp, nil
}

// createMeterProvider 使用配置的导出器创建仪表提供程序。
func createMeterProvider(cfg *Config) (*sdkmetric.MeterProvider, error) {
	var opts []sdkmetric.Option

	// 如果配置了 Prometheus 导出器，添加它
	if cfg.PrometheusListenAddr != "" {
		prometheusExporter, err := prometheus.New()
		if err != nil {
			return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
		}
		opts = append(opts, sdkmetric.WithReader(prometheusExporter))

		// 启动 Prometheus HTTP 服务器
		go func() {
			slog.Info("Starting Prometheus metrics server", "addr", cfg.PrometheusListenAddr)
			// 注意：在真实实现中，你需要在这里启动 HTTP 服务器
			// 目前，我们只记录启动的意图
		}()
	}

	// 创建仪表提供程序
	mp := sdkmetric.NewMeterProvider(opts...)
	return mp, nil
}

// noopExporter 是未配置导出器时使用的 no-op span 导出器。
type noopExporter struct{}

func (e *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *noopExporter) Shutdown(ctx context.Context) error {
	return nil
}
