package telemetry

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	defaultServiceName = "finerag-backend"
	initTimeout        = 5 * time.Second
	metricInterval     = 30 * time.Second
)

func Init(ctx context.Context) (func(context.Context) error, error) {
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return func(context.Context) error { return nil }, nil
	}

	initCtx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()

	res, err := resource.New(initCtx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName()),
		),
	)
	if err != nil {
		return nil, err
	}

	traceExporter, err := otlptracehttp.New(initCtx)
	if err != nil {
		return nil, err
	}
	metricExporter, err := otlpmetrichttp.New(initCtx)
	if err != nil {
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(metricInterval))),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		var shutdownErr error
		shutdownErr = errors.Join(shutdownErr, meterProvider.Shutdown(shutdownCtx))
		shutdownErr = errors.Join(shutdownErr, tracerProvider.Shutdown(shutdownCtx))
		return shutdownErr
	}, nil
}

func WrapHandler(name string, next http.Handler) http.Handler {
	if next == nil {
		return nil
	}
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		return next
	}
	operationName := strings.TrimSpace(name)
	if operationName == "" {
		operationName = defaultServiceName
	}
	return otelhttp.NewHandler(next, operationName)
}

func serviceName() string {
	if value := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); value != "" {
		return value
	}
	return defaultServiceName
}
