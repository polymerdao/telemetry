package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestSetupLogging(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	SetupLogging("info", "json")

	// Log a test message
	slog.Info("test message")

	// Reset stdout
	require.NoError(t, w.Close())
	os.Stdout = old

	// Read captured output
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	require.NoError(t, err)

	// Parse JSON output
	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))

	// Verify log format
	require.Equal(t, "test message", logEntry["message"].(string))
	require.Equal(t, "INFO", logEntry["severity"].(string))
	// Check for timestamp field
	_, ok := logEntry["timestamp"].(string)
	require.True(t, ok, "Expected timestamp field")
}

func TestHandlerWithSpanContext(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	instrumentedHandler := handlerWithSpanContext(handler)

	// Create a context with trace
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	// Log with trace context
	logger := slog.New(instrumentedHandler)
	logger.InfoContext(ctx, "test message")

	// Parse JSON output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON log output: %v", err)
	}

	// Check trace fields
	traceID, ok := logEntry["logging.googleapis.com/trace"].(string)
	if !ok || traceID != "01000000000000000000000000000000" {
		t.Errorf("Expected trace ID '01000000000000000000000000000000', got %v", traceID)
	}

	spanID, ok := logEntry["logging.googleapis.com/spanId"].(string)
	if !ok || spanID != "0200000000000000" {
		t.Errorf("Expected span ID '0200000000000000', got %v", spanID)
	}

	sampled, ok := logEntry["logging.googleapis.com/trace_sampled"].(bool)
	if !ok || !sampled {
		t.Errorf("Expected trace_sampled to be true")
	}
}
