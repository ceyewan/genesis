package db

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestWithTracer(t *testing.T) {
	t.Parallel()

	tp := noop.NewTracerProvider()
	opt := WithTracer(tp)

	o := &options{}
	opt(o)

	require.Equal(t, tp, o.tracer)
}
