package connector

import (
	"context"

	"github.com/ceyewan/genesis/metrics"
)

const (
	MetricHealthChecks = "connector.health_checks.total"
	MetricReconnects   = "connector.reconnects.total"
)

type connectorTelemetry struct {
	driver       string
	name         string
	healthChecks metrics.Counter
	reconnects   metrics.Counter
}

func newConnectorTelemetry(meter metrics.Meter, driver, name string) *connectorTelemetry {
	healthChecks, err := meter.Counter(MetricHealthChecks, "Total number of connector health checks")
	if err != nil {
		healthChecks, _ = metrics.Discard().Counter(MetricHealthChecks, "")
	}
	reconnects, err := meter.Counter(MetricReconnects, "Total number of connector reconnects")
	if err != nil {
		reconnects, _ = metrics.Discard().Counter(MetricReconnects, "")
	}
	return &connectorTelemetry{driver: driver, name: name, healthChecks: healthChecks, reconnects: reconnects}
}

func (m *connectorTelemetry) observeHealth(ctx context.Context, err error) error {
	status := "success"
	if err != nil {
		status = "error"
	}
	m.healthChecks.Inc(ctx,
		metrics.L("driver", m.driver),
		metrics.L("name", m.name),
		metrics.L("status", status),
	)
	return err
}

func (m *connectorTelemetry) observeReconnect() {
	m.reconnects.Inc(context.Background(), metrics.L("driver", m.driver), metrics.L("name", m.name))
}
