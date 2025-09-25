package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestFilterSampler_ShouldSample(t *testing.T) {
	tests := []struct {
		name       string
		params     sdktrace.SamplingParameters
		wantDrop   bool
		baseSample sdktrace.SamplingResult
	}{
		{
			name: "should drop span with drop attribute",
			params: sdktrace.SamplingParameters{
				Name:       "test-span",
				Attributes: []attribute.KeyValue{DropSpanAttribute},
			},
			wantDrop: true,
		},
		{
			name: "should drop BatchWriteSpans",
			params: sdktrace.SamplingParameters{
				Name: "google.devtools.cloudtrace.v2.TraceService/BatchWriteSpans",
			},
			wantDrop: true,
		},
		{
			name: "should not drop regular span",
			params: sdktrace.SamplingParameters{
				Name: "test-span",
			},
			wantDrop:   false,
			baseSample: sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample},
		},
		{
			name: "should not drop span with drop=false",
			params: sdktrace.SamplingParameters{
				Name: "test-span",
				Attributes: []attribute.KeyValue{
					attribute.Bool("drop", false),
				},
			},
			wantDrop:   false,
			baseSample: sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampler := &filterSampler{
				baseSampler: &testSampler{result: tt.baseSample},
			}

			got := sampler.ShouldSample(tt.params)

			if tt.wantDrop && got.Decision != sdktrace.Drop {
				t.Errorf("filterSampler.ShouldSample() = %v, want Drop", got.Decision)
			} else if !tt.wantDrop && got.Decision != tt.baseSample.Decision {
				t.Errorf("filterSampler.ShouldSample() = %v, want %v", got.Decision, tt.baseSample.Decision)
			}
		})
	}
}

// testSampler is a mock sampler for testing
type testSampler struct {
	result sdktrace.SamplingResult
}

func (s *testSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	return s.result
}

func (s *testSampler) Description() string {
	return "test sampler"
}

func TestLoggerWithNewTracer(t *testing.T) {
	var buf bytes.Buffer
	SetupLoggingWithWriter("info", "json", &buf)

	ctx := GetParentContext(context.Background(), "01000000000000000000000000000000")

	x := otel.Tracer("foo")
	ctx, span := x.Start(ctx, "bar")
	defer span.End()

	// tr := NewTracer("test-tracer")
	// ctx, span := tr.Span(ctx)
	// defer span.End()

	t.Logf("TraceID: %s, SpanID: %s", span.SpanContext().TraceID(), span.SpanContext().SpanID())
	// Log with trace context
	slog.InfoContext(ctx, "test message")

	var logEntry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))

	for key, value := range logEntry {
		t.Logf("%s: %v", key, value)
	}

	// Check that trace fields are present
	require.Contains(t, logEntry, "logging.googleapis.com/trace")
	require.Contains(t, logEntry, "logging.googleapis.com/spanId")
	require.Contains(t, logEntry, "logging.googleapis.com/trace_sampled")
}
