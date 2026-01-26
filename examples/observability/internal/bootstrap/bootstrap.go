package bootstrap

import (
	"context"
	"os"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
)

type Observability struct {
	Logger clog.Logger
	Meter  metrics.Meter
}

type Shutdown func(context.Context) error

func Init(ctx context.Context, serviceName string, metricsPort int) (Observability, []Shutdown, error) {
	otlpEndpoint := os.Getenv("OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4317"
	}

	traceShutdown, err := trace.Init(&trace.Config{
		ServiceName: serviceName,
		Endpoint:    otlpEndpoint,
		Sampler:     1.0,
		Insecure:    true,
	})
	if err != nil {
		return Observability{}, nil, xerrors.Wrap(err, "init trace")
	}

	logger, err := clog.New(
		&clog.Config{Level: "info", Format: "json"},
		clog.WithNamespace(serviceName),
		clog.WithTraceContext(),
	)
	if err != nil {
		_ = traceShutdown(ctx)
		return Observability{}, nil, xerrors.Wrap(err, "init logger")
	}

	metricsCfg := metrics.NewDevDefaultConfig(serviceName)
	metricsCfg.Port = metricsPort
	metricsCfg.EnableRuntime = true
	meter, err := metrics.New(metricsCfg)
	if err != nil {
		_ = traceShutdown(ctx)
		return Observability{}, nil, xerrors.Wrap(err, "init metrics")
	}

	shutdowns := []Shutdown{
		func(ctx context.Context) error { return meter.Shutdown(ctx) },
		func(ctx context.Context) error { return traceShutdown(ctx) },
	}

	return Observability{Logger: logger, Meter: meter}, shutdowns, nil
}
