package trace

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func setupTracerForTest(t *testing.T) (oteltrace.Tracer, *tracetest.SpanRecorder) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp.Tracer("test"), recorder
}

func TestStartProducerSpan(t *testing.T) {
	tracer, recorder := setupTracerForTest(t)

	meta := MessagingMeta{
		System:      MessagingSystemNATS,
		Destination: "orders.created",
		Operation:   MessagingOperationPublish,
	}
	_, span, headers := StartProducerSpan(context.Background(), tracer, SpanNameMQPublish("orders.created"), meta)
	span.End()

	if headers["traceparent"] == "" {
		t.Fatalf("traceparent header should be injected")
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if spans[0].Name() != SpanNameMQPublish("orders.created") {
		t.Fatalf("span name = %q, want %q", spans[0].Name(), SpanNameMQPublish("orders.created"))
	}
}

func TestStartConsumerSpanFromHeadersUsesLink(t *testing.T) {
	tracer, recorder := setupTracerForTest(t)

	parentCtx, parentSpan := tracer.Start(context.Background(), "upstream")
	headers := map[string]string{}
	Inject(parentCtx, headers)
	parentSC := parentSpan.SpanContext()
	parentSpan.End()

	_, consumerSpan := StartConsumerSpanFromHeaders(
		context.Background(),
		tracer,
		SpanNameMQConsume("orders.created"),
		headers,
		MessagingMeta{
			System:        MessagingSystemNATS,
			Destination:   "orders.created",
			Operation:     MessagingOperationConsume,
			ConsumerGroup: "workers",
		},
	)
	consumerSpan.End()

	var consumer sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == SpanNameMQConsume("orders.created") {
			consumer = s
			break
		}
	}
	if consumer == nil {
		t.Fatalf("consumer span not found")
	}
	if consumer.Parent().IsValid() {
		t.Fatalf("consumer span should not use remote span as direct parent")
	}
	if len(consumer.Links()) != 1 {
		t.Fatalf("consumer links = %d, want 1", len(consumer.Links()))
	}
	if consumer.Links()[0].SpanContext.TraceID() != parentSC.TraceID() {
		t.Fatalf("linked trace id mismatch")
	}
}

func TestStartConsumerSpanFromHeadersUsesChildOf(t *testing.T) {
	tracer, recorder := setupTracerForTest(t)

	parentCtx, parentSpan := tracer.Start(context.Background(), "upstream")
	headers := map[string]string{}
	Inject(parentCtx, headers)
	parentSC := parentSpan.SpanContext()
	parentSpan.End()

	_, consumerSpan := StartConsumerSpanFromHeaders(
		context.Background(),
		tracer,
		SpanNameMQConsume("orders.created"),
		headers,
		MessagingMeta{
			System:        MessagingSystemNATS,
			Destination:   "orders.created",
			Operation:     MessagingOperationConsume,
			ConsumerGroup: "workers",
			TraceRelation: MessagingTraceRelationChildOf,
		},
	)
	consumerSpan.End()

	var consumer sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == SpanNameMQConsume("orders.created") {
			consumer = s
			break
		}
	}
	if consumer == nil {
		t.Fatalf("consumer span not found")
	}
	if !consumer.Parent().IsValid() {
		t.Fatalf("consumer span should use remote span as parent under child_of mode")
	}
	if consumer.Parent().TraceID() != parentSC.TraceID() {
		t.Fatalf("consumer parent trace id mismatch")
	}
	if consumer.Parent().SpanID() != parentSC.SpanID() {
		t.Fatalf("consumer parent span id mismatch")
	}
	if len(consumer.Links()) != 0 {
		t.Fatalf("consumer links = %d, want 0 in child_of mode", len(consumer.Links()))
	}
}

func TestMarkSpanError(t *testing.T) {
	tracer, recorder := setupTracerForTest(t)
	_, span := tracer.Start(context.Background(), "work")
	err := errors.New("boom")
	MarkSpanError(span, err)
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("status code = %v, want error", spans[0].Status().Code)
	}
}
