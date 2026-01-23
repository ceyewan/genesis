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

// Init 初始化全局 TracerProvider，返回 shutdown 函数。
func Init(cfg *Config) (func(context.Context) error, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	ctx := context.Background()

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(5 * time.Second),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "create otlp exporter")
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, xerrors.Wrap(err, "create resource")
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Sampler))),
	}

	if cfg.Batcher == "simple" {
		tpOpts = append(tpOpts, sdktrace.WithSyncer(exporter))
	} else {
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exporter))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return xerrors.New("config is required")
	}
	if cfg.ServiceName == "" {
		return xerrors.New("service_name is required")
	}
	if cfg.Endpoint == "" {
		return xerrors.New("endpoint is required")
	}
	if cfg.Sampler < 0 || cfg.Sampler > 1 {
		return xerrors.New("sampler must be between 0 and 1")
	}
	if cfg.Batcher != "" && cfg.Batcher != "batch" && cfg.Batcher != "simple" {
		return xerrors.New("batcher must be \"batch\" or \"simple\"")
	}
	return nil
}
