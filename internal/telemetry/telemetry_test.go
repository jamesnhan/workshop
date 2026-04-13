package telemetry

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEnabled_defaultFalse(t *testing.T) {
	t.Setenv("WORKSHOP_OTEL_ENABLED", "")
	assert.False(t, Enabled())
}

func TestEnabled_trueVariants(t *testing.T) {
	for _, v := range []string{"true", "True", "TRUE"} {
		t.Setenv("WORKSHOP_OTEL_ENABLED", v)
		assert.True(t, Enabled(), "should be enabled for %q", v)
	}
}

func TestEnabled_falseVariants(t *testing.T) {
	for _, v := range []string{"false", "0", "no", ""} {
		t.Setenv("WORKSHOP_OTEL_ENABLED", v)
		assert.False(t, Enabled(), "should be disabled for %q", v)
	}
}

func TestInit_noopWhenDisabled(t *testing.T) {
	t.Setenv("WORKSHOP_OTEL_ENABLED", "false")
	shutdown, err := Init(context.Background(), discardLogger())
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	// No-op shutdown should not error.
	assert.NoError(t, shutdown(context.Background()))
}

func TestInit_enabledWithNoEndpoint(t *testing.T) {
	// If enabled but the endpoint is unreachable, Init still succeeds
	// because the SDK defers exporter dial to send time. Shutdown may
	// error (trying to flush to a dead endpoint), so we just verify
	// Init returns cleanly and the shutdown func is non-nil.
	t.Setenv("WORKSHOP_OTEL_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:0")
	shutdown, err := Init(context.Background(), discardLogger())
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	// Shutdown with an already-cancelled context so the flush attempt
	// is short-circuited rather than hanging on a dead endpoint.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = shutdown(ctx)
}

// --- ScrubBody ---

func TestScrubBody_shortBodyUntouched(t *testing.T) {
	t.Setenv("WORKSHOP_OTEL_SCRUB_BODIES", "true")
	assert.Equal(t, "hello", ScrubBody("hello", 256))
}

func TestScrubBody_longBodyTruncated(t *testing.T) {
	t.Setenv("WORKSHOP_OTEL_SCRUB_BODIES", "true")
	long := string(make([]byte, 300))
	result := ScrubBody(long, 256)
	assert.Len(t, result, 256+len("...(truncated)"))
	assert.True(t, len(result) < len(long))
}

func TestScrubBody_disabledPassesThrough(t *testing.T) {
	t.Setenv("WORKSHOP_OTEL_SCRUB_BODIES", "false")
	long := string(make([]byte, 500))
	assert.Equal(t, long, ScrubBody(long, 256))
}

func TestScrubEnabled_defaultTrue(t *testing.T) {
	t.Setenv("WORKSHOP_OTEL_SCRUB_BODIES", "")
	assert.True(t, ScrubEnabled())
}
