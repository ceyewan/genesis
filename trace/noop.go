package trace

import (
	"context"

	"github.com/ceyewan/genesis/xerrors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Discard 创建一个不导出数据的 TracerProvider，仅用于本地生成 TraceID。
//
// Discard 仍然采用全局模式：它会安装全局 TracerProvider 和全局传播器。
// 因此它不是“局部无副作用”的 helper，而是“安装一个不导出的全局 provider”。
// 返回的 shutdown 在关闭该 provider 后，会在必要时把全局 tracing 状态重置为
// 安全默认值。
func Discard(serviceName string) (func(context.Context) error, error) {
	ctx := context.Background()

	resOpts := []resource.Option{}
	if serviceName != "" {
		resOpts = append(resOpts, resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		))
	}

	res, err := resource.New(ctx, resOpts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "create resource")
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))),
	)

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
