package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestSetupLogging(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	SetupLogging()

	// Log a test message
	slog.Info("test message")

	// Reset stdout
	w.Close()
	os.Stdout = old

	// Read captured output
	var buf bytes.Buffer
	io.Copy(&buf, r)

	// Parse JSON output
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON log output: %v", err)
	}

	// Verify log format
	if msg, ok := logEntry["message"].(string); !ok || msg != "test message" {
		t.Errorf("Expected message 'test message', got %v", logEntry["message"])
	}
	if _, ok := logEntry["timestamp"].(string); !ok {
		t.Error("Expected timestamp field")
	}
	if _, ok := logEntry["severity"].(string); !ok {
		t.Error("Expected severity field")
	}
}

func TestSetupZapLogging(t *testing.T) {
	logger := SetupZapLogging()

	// Create an observer for testing
	core, logs := observer.New(zap.InfoLevel)
	logger = zap.New(core)

	// Log a test message
	logger.Info("test message")

	// Verify log entry
	if logs.Len() != 1 {
		t.Errorf("Expected 1 log entry, got %d", logs.Len())
	}

	entry := logs.All()[0]
	if entry.Message != "test message" {
		t.Errorf("Expected message 'test message', got %s", entry.Message)
	}
}

func TestWithTraceContext(t *testing.T) {
	// Create a logger with an observer
	core, logs := observer.New(zap.InfoLevel)
	logger := zap.New(core)

	// Create a context with trace
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	// Log with trace context
	WithTraceContext(ctx, logger).Info("test message")

	// Verify log entry
	if logs.Len() != 1 {
		t.Fatalf("Expected 1 log entry, got %d", logs.Len())
	}

	entry := logs.All()[0]
	fields := entry.ContextMap()

	// Check trace fields
	traceID, ok := fields["logging.googleapis.com/trace"].(string)
	if !ok || traceID != "01000000000000000000000000000000" {
		t.Errorf("Expected trace ID '01000000000000000000000000000000', got %v", traceID)
	}

	spanID, ok := fields["logging.googleapis.com/spanId"].(string)
	if !ok || spanID != "0200000000000000" {
		t.Errorf("Expected span ID '0200000000000000', got %v", spanID)
	}

	sampled, ok := fields["logging.googleapis.com/trace_sampled"].(bool)
	if !ok || !sampled {
		t.Errorf("Expected trace_sampled to be true")
	}
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
