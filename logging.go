package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// otelSlogHandler wraps a slog.Handler to automatically add OpenTelemetry trace context
// This handler works with child loggers created using With()
type otelSlogHandler struct {
	handler slog.Handler
}

func newOtelSlogHandler(handler slog.Handler) *otelSlogHandler {
	return &otelSlogHandler{handler: handler}
}

func (h *otelSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *otelSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	// Get the SpanContext from the context and add trace attributes
	// following Cloud Logging structured log format described in:
	// https://cloud.google.com/logging/docs/structured-logging#special-payload-fields
	if s := trace.SpanContextFromContext(ctx); s.IsValid() {
		record.AddAttrs(
			slog.String("logging.googleapis.com/trace", s.TraceID().String()),
			slog.String("logging.googleapis.com/spanId", s.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled", s.TraceFlags().IsSampled()),
		)
	}
	return h.handler.Handle(ctx, record)
}

func (h *otelSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &otelSlogHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *otelSlogHandler) WithGroup(name string) slog.Handler {
	return &otelSlogHandler{handler: h.handler.WithGroup(name)}
}

func replacer(groups []string, a slog.Attr) slog.Attr {
	// Rename attribute keys to match Cloud Logging structured log format
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
		// Map slog.Level string values to Cloud Logging LogSeverity
		// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity
		if level := a.Value.Any().(slog.Level); level == slog.LevelWarn {
			a.Value = slog.StringValue("WARNING")
		}
	case slog.TimeKey:
		a.Key = "timestamp"
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

// [END opentelemetry_instrumentation_spancontext_logger]

func SetupLogging(level, format string) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		fmt.Fprintf(os.Stderr, "invalid log level %q, defaulting to info: %v\n", level, err)
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:       lvl,
		ReplaceAttr: replacer,
		AddSource:   true,
	}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	// Wrap with our OpenTelemetry-aware handler that works with child loggers
	otelHandler := newOtelSlogHandler(handler)

	// Set this handler as the global slog handler.
	slog.SetDefault(slog.New(otelHandler))
}
