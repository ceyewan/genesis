// Package trace 提供 Genesis 的 OpenTelemetry 链路追踪初始化与传播辅助能力。
//
// 这个组件当前采用“全局模式”工作：Init 和 Discard 都会安装全局
// TracerProvider 与 TextMapPropagator。这样做便于 Gin、gRPC、数据库插件和
// MQ helper 共享同一套全局 tracing 状态；代价是重复初始化会覆盖之前安装的
// 全局 provider。
//
// 因此推荐的使用方式是：应用启动时只初始化一次 trace，并在退出时调用返回的
// shutdown 函数。对于只需要本地生成 TraceID 的场景，也应明确知道 Discard
// 仍然会修改全局 tracing 状态。
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
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Inject 将当前 Context 的 Trace 信息注入到 carrier 中
// 用于 MQ 等场景，将链路追踪信息传递给下游
func Inject(ctx context.Context, carrier map[string]string) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(carrier))
}

// Extract 从 carrier 中提取 Trace 信息并返回新的 Context
// 用于 MQ 消费者等场景，还原上游的链路追踪信息
func Extract(ctx context.Context, carrier map[string]string) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
}

// Init 初始化全局 TracerProvider，返回 shutdown 函数。
//
// Init 当前采用全局模式：它会创建一个新的 TracerProvider，并安装为
// OpenTelemetry 全局 TracerProvider 和全局传播器。调用方通常应在应用启动时
// 调用一次，并负责在退出时执行返回的 shutdown 函数。
//
// 返回的 shutdown 会关闭底层 provider；若当前全局 TracerProvider 仍指向该
// 实例，还会将全局 tracing 状态重置为安全默认值。
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

	return func(ctx context.Context) error {
		err := tp.Shutdown(ctx)
		if otel.GetTracerProvider() == tp {
			otel.SetTracerProvider(tracenoop.NewTracerProvider())
			otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			))
		}
		return err
	}, nil
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
