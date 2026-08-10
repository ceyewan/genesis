// Package trace 提供 Genesis 的 OpenTelemetry 链路追踪初始化与传播辅助能力。
//
// 这个组件当前采用“全局模式”工作：Init 和 InstallLocalProvider 都会安装全局
// TracerProvider 与 TextMapPropagator。这样做便于 Gin、gRPC、数据库插件和
// MQ helper 共享同一套全局 tracing 状态；代价是重复初始化会覆盖之前安装的
// 全局 provider。
//
// 因此推荐的使用方式是：应用启动时只初始化一次 trace，并在退出时调用返回的
// shutdown 函数。对于只需要本地生成 TraceID 的场景，也应明确知道 InstallLocalProvider
// 仍然会修改全局 tracing 状态。
package trace

import (
	"context"
	"maps"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/ceyewan/genesis/xerrors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Inject 将当前 Context 的 Trace 信息注入到 carrier 中。
// 用于 MQ 等场景，将链路追踪信息传递给下游。nil carrier 是安全的 no-op。
func Inject(ctx context.Context, carrier map[string]string) {
	if carrier == nil {
		return
	}
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
	if cfg == nil {
		return nil, validateConfig(nil)
	}
	config := *cfg
	config.Headers = maps.Clone(cfg.Headers)
	config.setDefaults()
	if err := validateConfig(&config); err != nil {
		return nil, err
	}
	cfg = &config

	ctx := context.Background()

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.ExporterTimeout),
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "create otlp exporter")
	}
	spanExporter := sdktrace.SpanExporter(exporter)
	if cfg.ExportErrors != nil {
		spanExporter = &reportingExporter{SpanExporter: exporter, errors: cfg.ExportErrors}
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Version),
			attribute.String("service.instance.id", cfg.InstanceID),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, xerrors.Wrap(err, "create resource")
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Sampler))),
	}

	if cfg.Batcher == BatcherImmediate {
		tpOpts = append(tpOpts, sdktrace.WithBatcher(spanExporter,
			sdktrace.WithMaxExportBatchSize(1),
			sdktrace.WithBatchTimeout(time.Millisecond),
		))
	} else {
		tpOpts = append(tpOpts, sdktrace.WithBatcher(spanExporter))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return newTracerShutdown(tp), nil
}

func newTracerShutdown(tp *sdktrace.TracerProvider) func(context.Context) error {
	var shutdownOnce sync.Once
	shutdownDone := make(chan struct{})
	var shutdownErr error
	return func(ctx context.Context) error {
		if ctx == nil {
			return xerrors.New("trace shutdown context is nil")
		}
		shutdownOnce.Do(func() {
			go func() {
				defer close(shutdownDone)
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				shutdownErr = tp.Shutdown(shutdownCtx)
				if otel.GetTracerProvider() == tp {
					otel.SetTracerProvider(tracenoop.NewTracerProvider())
					otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
						propagation.TraceContext{},
						propagation.Baggage{},
					))
				}
			}()
		})
		select {
		case <-shutdownDone:
			return shutdownErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type reportingExporter struct {
	sdktrace.SpanExporter
	errors chan<- error
}

func (e *reportingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	if err != nil {
		select {
		case e.errors <- err:
		default:
		}
	}
	return err
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return xerrors.Wrap(ErrInvalidConfig, "config is required")
	}
	if strings.TrimSpace(cfg.ServiceName) == "" {
		return xerrors.Wrap(ErrInvalidConfig, "service_name is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return xerrors.Wrap(ErrInvalidConfig, "endpoint is required")
	}
	if math.IsNaN(cfg.Sampler) || cfg.Sampler < 0 || cfg.Sampler > 1 {
		return xerrors.Wrap(ErrInvalidConfig, "sampler must be between 0 and 1")
	}
	if cfg.ExporterTimeout < 0 {
		return xerrors.Wrap(ErrInvalidConfig, "exporter_timeout must not be negative")
	}
	if cfg.Batcher != "" && cfg.Batcher != BatcherBatch && cfg.Batcher != BatcherImmediate {
		return xerrors.Wrap(ErrInvalidConfig, "batcher must be \"batch\" or \"immediate\"")
	}
	return nil
}
