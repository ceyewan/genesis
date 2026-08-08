package trace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type failingSpanExporter struct{ err error }

func (e failingSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return e.err
}
func (e failingSpanExporter) Shutdown(context.Context) error { return nil }

type blockingSpanProcessor struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingSpanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (*blockingSpanProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (*blockingSpanProcessor) ForceFlush(context.Context) error                { return nil }
func (p *blockingSpanProcessor) Shutdown(context.Context) error {
	close(p.started)
	<-p.release
	return nil
}

func TestShutdownHonorsEachCallersContext(t *testing.T) {
	processor := &blockingSpanProcessor{started: make(chan struct{}), release: make(chan struct{})}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	shutdown := newTracerShutdown(provider)
	firstDone := make(chan error, 1)
	go func() { firstDone <- shutdown(context.Background()) }()
	<-processor.started

	callerCtx, cancel := context.WithCancel(context.Background())
	cancel()
	secondDone := make(chan error, 1)
	go func() { secondDone <- shutdown(callerCtx) }()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second shutdown error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second shutdown ignored its context")
	}

	close(processor.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestReportingExporterExposesFailureWithoutBlocking(t *testing.T) {
	want := errors.New("collector unavailable")
	errorsCh := make(chan error, 1)
	exporter := &reportingExporter{SpanExporter: failingSpanExporter{err: want}, errors: errorsCh}
	requireStart := time.Now()
	err := exporter.ExportSpans(context.Background(), nil)
	if !errors.Is(err, want) {
		t.Fatalf("ExportSpans error = %v", err)
	}
	if time.Since(requireStart) > time.Second {
		t.Fatal("error reporting blocked export")
	}
	if got := <-errorsCh; !errors.Is(got, want) {
		t.Fatalf("reported error = %v", got)
	}

	// A full channel drops a notification rather than blocking exporter workers.
	errorsCh <- want
	if err := exporter.ExportSpans(context.Background(), nil); !errors.Is(err, want) {
		t.Fatal(err)
	}
}

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

func TestInitCopiesConfigAndShutdownIsConcurrentIdempotent(t *testing.T) {
	cfg := DefaultConfig("svc")
	cfg.Endpoint = "127.0.0.1:1"
	cfg.ExporterTimeout = 10 * time.Millisecond
	shutdown, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExporterTimeout != 10*time.Millisecond {
		t.Fatal("Init mutated caller config")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			errCh <- shutdown(ctx)
		})
	}
	wg.Wait()
	close(errCh)
	var first error
	for err := range errCh {
		if first == nil {
			first = err
		}
		if (first == nil) != (err == nil) || (first != nil && err.Error() != first.Error()) {
			t.Fatalf("shutdown results differ: %v vs %v", first, err)
		}
	}
}

func TestUnavailableExporterDoesNotBlockSpanAndReportsFailure(t *testing.T) {
	exportErrors := make(chan error, 1)
	cfg := DefaultConfig("unavailable-exporter-test")
	cfg.Endpoint = "127.0.0.1:1"
	cfg.Batcher = "simple"
	cfg.ExporterTimeout = 250 * time.Millisecond
	cfg.ExportErrors = exportErrors
	shutdown, err := Init(cfg)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	_, span := otel.Tracer("test").Start(context.Background(), "operation")
	span.End()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("span creation blocked on unavailable exporter for %v", elapsed)
	}

	select {
	case exportErr := <-exportErrors:
		if exportErr == nil {
			t.Fatal("reported export error is nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unavailable exporter did not report failure")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = shutdown(ctx)
}
