package trace

import (
	"context"

	"github.com/ceyewan/genesis/xerrors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

// Discard 初始化一个不导出数据的 TracerProvider，但仍会生成 TraceID。
// 适用于仅需要日志关联、而不需要上报 Trace 的场景。
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
		return nil, xerrors.Wrap(err, "failed to create resource")
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

	return tp.Shutdown, nil
}
