package observability

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Telemetry struct {
	tracer *sdktrace.TracerProvider
	meter  *sdkmetric.MeterProvider
}

// Newは標準OTEL環境変数からExporterを選び、指定がない開発環境では外部送信しない。
func New(ctx context.Context, serviceName string) (*Telemetry, error) {
	res, err := resource.New(ctx, resource.WithTelemetrySDK(), resource.WithAttributes(attribute.String("service.name", serviceName)), resource.WithFromEnv())
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}
	traceOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if configured("OTEL_TRACES_EXPORTER", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT") {
		exporter, err := autoexport.NewSpanExporter(ctx)
		if err != nil {
			return nil, fmt.Errorf("create trace exporter: %w", err)
		}
		if !autoexport.IsNoneSpanExporter(exporter) {
			traceOptions = append(traceOptions, sdktrace.WithBatcher(exporter))
		}
	}
	tracer := sdktrace.NewTracerProvider(traceOptions...)
	metricOptions := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if configured("OTEL_METRICS_EXPORTER", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT") {
		reader, err := autoexport.NewMetricReader(ctx)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("create metric reader: %w", err), tracer.Shutdown(ctx))
		}
		if !autoexport.IsNoneMetricReader(reader) {
			metricOptions = append(metricOptions, sdkmetric.WithReader(reader))
		}
	}
	meter := sdkmetric.NewMeterProvider(metricOptions...)
	otel.SetTracerProvider(tracer)
	otel.SetMeterProvider(meter)
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())
	return &Telemetry{tracer: tracer, meter: meter}, nil
}

func configured(keys ...string) bool {
	for _, key := range keys {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return errors.Join(t.meter.Shutdown(ctx), t.tracer.Shutdown(ctx))
}
