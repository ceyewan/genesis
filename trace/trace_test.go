package trace

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestInitValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "nil config", cfg: nil},
		{name: "missing service name", cfg: &Config{Endpoint: "localhost:4317", Sampler: 1}},
		{name: "missing endpoint", cfg: &Config{ServiceName: "svc", Sampler: 1}},
		{name: "invalid sampler low", cfg: &Config{ServiceName: "svc", Endpoint: "localhost:4317", Sampler: -0.1}},
		{name: "invalid sampler high", cfg: &Config{ServiceName: "svc", Endpoint: "localhost:4317", Sampler: 1.1}},
		{name: "invalid batcher", cfg: &Config{ServiceName: "svc", Endpoint: "localhost:4317", Sampler: 1, Batcher: "weird"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Init(tt.cfg); err == nil {
				t.Fatalf("Init() error = nil, want validation error")
			}
		})
	}
}

func TestDiscardInstallsGlobalTracingState(t *testing.T) {
	beforeProvider := otel.GetTracerProvider()

	shutdown, err := Discard("test-service")
	if err != nil {
		t.Fatalf("Discard() error = %v", err)
	}

	afterProvider := otel.GetTracerProvider()
	if beforeProvider == afterProvider {
		t.Fatalf("global tracer provider was not replaced")
	}

	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "work")
	defer span.End()

	carrier := map[string]string{}
	Inject(ctx, carrier)
	if carrier["traceparent"] == "" {
		t.Fatalf("traceparent header should be injected")
	}

	extracted := Extract(context.Background(), carrier)
	if spanCtx := span.SpanContext(); !spanCtx.IsValid() {
		t.Fatalf("span context should be valid")
	}
	if remoteSC := oteltrace.SpanContextFromContext(extracted); !remoteSC.IsValid() {
		t.Fatalf("extracted span context should be valid")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}

	resetProvider := otel.GetTracerProvider()
	if resetProvider == afterProvider {
		t.Fatalf("global tracer provider was not reset after shutdown")
	}
}
