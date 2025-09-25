package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestSetupLogging(t *testing.T) {
	var buf bytes.Buffer
	SetupLoggingWithWriter("info", "json", &buf)

	// Log a test message
	slog.Info("test message")

	// Parse JSON output
	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))

	for key, value := range logEntry {
		t.Logf("%s: %v", key, value)
	}

	// Verify log format
	require.Equal(t, "test message", logEntry["message"].(string))
	require.Equal(t, "INFO", logEntry["severity"].(string))
	// Check for timestamp field
	_, ok := logEntry["timestamp"].(string)
	require.True(t, ok, "Expected timestamp field")

	pc, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	require.Equal(t, file, logEntry["source"].(map[string]any)["file"])
	require.Equal(t, runtime.FuncForPC(pc).Name(), logEntry["source"].(map[string]any)["function"])
	require.NotEmpty(t, logEntry["source"].(map[string]any)["line"])
}

func TestHandlerWithSpanContext(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	SetupLoggingWithWriter("info", "json", &buf)

	// Create a context with trace
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	// Log with trace context
	slog.InfoContext(ctx, "test message")

	// Parse JSON output
	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))

	for key, value := range logEntry {
		t.Logf("%s: %v", key, value)
	}

	require.Equal(t, "01000000000000000000000000000000", logEntry["logging.googleapis.com/trace"].(string))
	require.Equal(t, "0200000000000000", logEntry["logging.googleapis.com/spanId"].(string))
	require.True(t, logEntry["logging.googleapis.com/trace_sampled"].(bool))
}
