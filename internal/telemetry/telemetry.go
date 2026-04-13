// Package telemetry bootstraps OpenTelemetry for the Workshop backend.
//
// Configuration is entirely via environment variables. When
// WORKSHOP_OTEL_ENABLED is not "true", Init returns a no-op shutdown and
// all tracing / metrics code becomes zero-cost wrappers.
//
// Key env vars (see docs/specs/observability.md for the full table):
//
//	WORKSHOP_OTEL_ENABLED          master switch (default false)
//	OTEL_EXPORTER_OTLP_ENDPOINT   e.g. https://collector.internal:4318
//	OTEL_SERVICE_NAME             default "workshop-backend"
//	OTEL_SERVICE_VERSION          set via -ldflags at build time
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Enabled returns true if the user opted in to telemetry.
func Enabled() bool {
	return strings.EqualFold(os.Getenv("WORKSHOP_OTEL_ENABLED"), "true")
}

// Init configures the global TracerProvider and MeterProvider. Returns a
// shutdown function that flushes pending spans/metrics. When telemetry is
// disabled, Init returns a no-op shutdown and does nothing.
func Init(ctx context.Context, logger *slog.Logger) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }

	if !Enabled() {
		logger.Info("telemetry disabled (set WORKSHOP_OTEL_ENABLED=true to enable)")
		return noop, nil
	}

	// Build the resource that identifies this service instance.
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "workshop-backend"
	}
	serviceVersion := os.Getenv("OTEL_SERVICE_VERSION")
	if serviceVersion == "" {
		serviceVersion = "dev"
	}
	// Use NewSchemaless so the service attributes merge cleanly with
	// resource.Default() regardless of the OTel SDK's own schema version.
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return noop, err
	}

	// --- Traces ---

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, err
	}
	tp := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter,
			trace.WithBatchTimeout(5*time.Second),
		),
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// --- Metrics ---

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		// Don't fail the whole init on metric export failure — traces
		// alone are valuable. Log and keep going.
		logger.Warn("metric exporter init failed, metrics disabled", "err", err)
	}
	var mp *metric.MeterProvider
	if metricExporter != nil {
		mp = metric.NewMeterProvider(
			metric.WithReader(metric.NewPeriodicReader(metricExporter,
				metric.WithInterval(15*time.Second),
			)),
			metric.WithResource(res),
		)
		otel.SetMeterProvider(mp)
	}

	// --- Propagation ---

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// --- Logs ---

	logShutdown, logErr := InitLogging(ctx)
	if logErr != nil {
		logger.Warn("log exporter init failed, OTel logs disabled", "err", logErr)
	}

	// Re-register metric instruments against the real meter provider.
	InitMetrics()

	logger.Info("telemetry enabled",
		"service", serviceName,
		"version", serviceVersion,
		"endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	)

	// Shutdown flushes pending data. Called from main before exit.
	shutdown = func(ctx context.Context) error {
		var firstErr error
		if err := tp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		if mp != nil {
			if err := mp.Shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if logShutdown != nil {
			if err := logShutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	return shutdown, nil
}

// Tracer returns a named tracer for the given component. When telemetry is
// disabled the global TracerProvider returns a no-op tracer automatically.
func Tracer(name string) oteltrace.Tracer {
	return otel.Tracer("workshop/" + name)
}

// Attrs is a shorthand for trace.WithAttributes so callers don't need to
// import the SDK trace package alongside the API attribute package.
func Attrs(attrs ...attribute.KeyValue) oteltrace.SpanStartOption {
	return oteltrace.WithAttributes(attrs...)
}

// DiscardLogger returns a slog.Logger that discards all output. Useful for
// MCP subprocesses that pipe stdout/stderr to the MCP protocol.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ScrubBody returns the body truncated to maxLen with a suffix if it was
// longer, or the full body if it fits. Use for span attributes that may
// contain user-generated content (prompts, channel messages).
func ScrubBody(body string, maxLen int) string {
	if !ScrubEnabled() || len(body) <= maxLen {
		return body
	}
	return body[:maxLen] + "...(truncated)"
}

// ScrubEnabled returns true if body scrubbing is active. Defaults to true.
func ScrubEnabled() bool {
	v := os.Getenv("WORKSHOP_OTEL_SCRUB_BODIES")
	if v == "" {
		return true
	}
	return strings.EqualFold(v, "true")
}
