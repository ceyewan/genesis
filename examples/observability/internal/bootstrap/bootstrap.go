package bootstrap

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/trace"
	"github.com/ceyewan/genesis/xerrors"
)

// Observability 持有应用创建的可观测性资源。
//
// 调用方只向业务组件注入 Logger 和 Meter；底层资源统一由 Shutdown 关闭。
type Observability struct {
	Logger clog.Logger
	Meter  metrics.Meter

	traceShutdown func(context.Context) error
	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
}

// Init 初始化一组身份一致的日志、指标和链路追踪资源。
func Init(serviceName string, metricsPort int) (*Observability, error) {
	version := getenv("SERVICE_VERSION", "dev")
	environment := getenv("DEPLOYMENT_ENVIRONMENT", "demo")
	instanceID := os.Getenv("SERVICE_INSTANCE_ID")
	if instanceID == "" {
		instanceID, _ = os.Hostname()
	}

	loggerCfg := clog.NewProdDefaultConfig("")
	loggerCfg.ServiceName = serviceName
	loggerCfg.Version = version
	loggerCfg.InstanceID = instanceID
	loggerCfg.Environment = environment
	logger, err := clog.New(
		loggerCfg,
		clog.WithNamespace(serviceName),
		clog.WithTraceContext(),
	)
	if err != nil {
		return nil, xerrors.Wrap(err, "init logger")
	}

	telemetryErrors := make(chan error, 32)
	traceCfg := trace.DefaultConfig(serviceName)
	traceCfg.Version = version
	traceCfg.InstanceID = instanceID
	traceCfg.Environment = environment
	traceCfg.Endpoint = getenv("OTLP_ENDPOINT", "localhost:4317")
	otlpInsecure, err := strconv.ParseBool(getenv("OTLP_INSECURE", "true"))
	if err != nil {
		_ = logger.Close()
		return nil, xerrors.Wrap(err, "parse OTLP_INSECURE")
	}
	traceCfg.Insecure = otlpInsecure
	traceCfg.ExportErrors = telemetryErrors
	traceShutdown, err := trace.Init(traceCfg)
	if err != nil {
		_ = logger.Close()
		return nil, xerrors.Wrap(err, "init trace")
	}

	metricsCfg := metrics.NewProdDefaultConfig(serviceName, version)
	metricsCfg.InstanceID = instanceID
	metricsCfg.Environment = environment
	metricsCfg.ListenAddress = getenv("METRICS_LISTEN_ADDRESS", "0.0.0.0")
	metricsCfg.Port = metricsPort
	metricsCfg.EnableRuntime = true
	metricsCfg.ServerErrors = telemetryErrors
	meter, err := metrics.New(metricsCfg, metrics.WithLogger(logger))
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = traceShutdown(cleanupCtx)
		_ = logger.Close()
		return nil, xerrors.Wrap(err, "init metrics")
	}

	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		for {
			select {
			case <-monitorCtx.Done():
				return
			case telemetryErr := <-telemetryErrors:
				if telemetryErr != nil {
					logger.Error("Telemetry background error", clog.Error(telemetryErr))
				}
			}
		}
	}()

	return &Observability{
		Logger:        logger,
		Meter:         meter,
		traceShutdown: traceShutdown,
		monitorCancel: monitorCancel,
		monitorDone:   monitorDone,
	}, nil
}

// Shutdown 按 metrics、trace、logger 的顺序释放资源。
func (o *Observability) Shutdown(ctx context.Context) error {
	if o == nil {
		return nil
	}

	var meterErr error
	if o.Meter != nil {
		meterErr = o.Meter.Shutdown(ctx)
	}
	var traceErr error
	if o.traceShutdown != nil {
		traceErr = o.traceShutdown(ctx)
	}
	if o.monitorCancel != nil {
		o.monitorCancel()
	}
	if o.monitorDone != nil {
		<-o.monitorDone
	}
	var loggerErr error
	if o.Logger != nil {
		loggerErr = o.Logger.Close()
	}
	return xerrors.Combine(meterErr, traceErr, loggerErr)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
