package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// LogProvider holds the OTel log provider so Init can register its shutdown.
var logProvider *sdklog.LoggerProvider

// InitLogging sets up an OTel LoggerProvider that exports via OTLP/HTTP.
// Returns a shutdown function. When telemetry is disabled, this is never
// called and the existing slog.TextHandler is used as-is.
func InitLogging(ctx context.Context) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }

	exporter, err := otlploghttp.New(ctx)
	if err != nil {
		return noop, err
	}

	logProvider = sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	return func(ctx context.Context) error {
		return logProvider.Shutdown(ctx)
	}, nil
}

// NewTeeLogger builds a slog.Logger that writes to BOTH the given local
// handler (e.g. slog.TextHandler for stderr) AND the OTel log pipeline.
// When telemetry is disabled, call this with nil otelHandler and you get
// back a plain logger.
func NewTeeLogger(local slog.Handler) *slog.Logger {
	if logProvider == nil {
		return slog.New(local)
	}
	otelHandler := otelslog.NewHandler("workshop",
		otelslog.WithLoggerProvider(logProvider),
	)
	return slog.New(&teeHandler{local: local, otel: otelHandler})
}

// teeHandler fans out each slog record to two handlers. This keeps local
// dev output on stderr while also shipping structured logs to Loki via
// the OTel collector. Trace correlation (trace_id / span_id) is injected
// by the otelslog handler automatically when a span is active in ctx.
type teeHandler struct {
	local slog.Handler
	otel  slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.local.Enabled(ctx, level) || t.otel.Enabled(ctx, level)
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always write to local — ignore OTel errors so a dead collector
	// doesn't break log output.
	_ = t.otel.Handle(ctx, r)
	return t.local.Handle(ctx, r)
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		local: t.local.WithAttrs(attrs),
		otel:  t.otel.WithAttrs(attrs),
	}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		local: t.local.WithGroup(name),
		otel:  t.otel.WithGroup(name),
	}
}
