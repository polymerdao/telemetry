package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type filterSampler struct {
	baseSampler sdktrace.Sampler
}

var DropSpanAttribute = attribute.Bool("drop", true)

func (f *filterSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	// Drop specific spans by name
	if p.Name == "google.devtools.cloudtrace.v2.TraceService/BatchWriteSpans" {
		return sdktrace.SamplingResult{Decision: sdktrace.Drop}
	}
	return f.baseSampler.ShouldSample(p)
}

func (f *filterSampler) Description() string {
	return fmt.Sprintf("FilterSampler{%s}", f.baseSampler.Description())
}

// NewDropSpanProcessor returns a custom span processor that drops spans with the DropSpanAttribute set.
func NewDropSpanProcessor(next sdktrace.SpanProcessor) sdktrace.SpanProcessor {
	return &dropSpanProcessor{
		processor: next,
	}
}

type dropSpanProcessor struct {
	processor sdktrace.SpanProcessor
}

func (d *dropSpanProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	d.processor.OnStart(ctx, s)
}

func (d *dropSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// Check for the drop attribute in the finished span.
	for _, attr := range s.Attributes() {
		if attr.Key == DropSpanAttribute.Key && attr.Value.AsBool() {
			// Skip exporting this span.
			return
		}
	}
	// Otherwise, pass the span to the next processor.
	d.processor.OnEnd(s)
}

func (d *dropSpanProcessor) Shutdown(ctx context.Context) error {
	return d.processor.Shutdown(ctx)
}

func (d *dropSpanProcessor) ForceFlush(ctx context.Context) error {
	return d.processor.ForceFlush(ctx)
}

// getExporterFromEnv returns the exporter type from environment variable OTEL_TRACES_EXPORTER.
// Defaults to "gcp" if not set.
func getExporterFromEnv() string {
	if exporter := os.Getenv("OTEL_TRACES_EXPORTER"); exporter != "" {
		return exporter
	}
	return "gcp" // default
}

// createGCPExporter creates a Google Cloud Platform trace exporter.
func createGCPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	return texporter.New()
}

// createAWSExporter creates an OTLP exporter configured for AWS X-Ray.
// AWS X-Ray integration works by sending OTLP traces to AWS Distro for OpenTelemetry Collector
// which then forwards them to X-Ray.
func createAWSExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	// For AWS X-Ray, use OTLP endpoint - typically AWS ADOT Collector
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// Default AWS ADOT Collector HTTP endpoint for X-Ray
		endpoint = "http://localhost:4318"
	}
	
	return otlptracehttp.New(
		ctx,
		otlptracehttp.WithEndpoint(endpoint),
	)
}

// createOTLPExporter creates an OTLP HTTP trace exporter.
func createOTLPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4318"
	}
	
	return otlptracehttp.New(
		ctx,
		otlptracehttp.WithEndpoint(endpoint),
	)
}

// createConsoleExporter creates a console/stdout trace exporter for development.
func createConsoleExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
}

// createNoOpExporter creates a no-op exporter that discards all spans.
func createNoOpExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	// Return a no-op exporter that does nothing
	return &noOpExporter{}, nil
}

// noOpExporter is a no-op span exporter.
type noOpExporter struct{}

func (e *noOpExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *noOpExporter) Shutdown(ctx context.Context) error {
	return nil
}

// InitTracer initializes the OpenTelemetry tracer with configurable exporter and a drop span processor.
func InitTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	var shutdownFuncs []func(context.Context) error

	// Create a cleanup function that combines all shutdown functions
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// Configure Context Propagation to use the default W3C traceparent format
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create resource with service information and auto-detected metadata
	res, err := GetResource(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Get exporter type from environment variable
	exporterType := getExporterFromEnv()
	
	// Create the appropriate exporter based on configuration
	var exporter sdktrace.SpanExporter
	switch exporterType {
	case "gcp":
		exporter, err = createGCPExporter(ctx)
	case "aws", "xray":
		exporter, err = createAWSExporter(ctx)
	case "otlp":
		exporter, err = createOTLPExporter(ctx)
	case "console", "stdout":
		exporter, err = createConsoleExporter(ctx)
	case "none", "noop":
		exporter, err = createNoOpExporter(ctx)
	default:
		slog.Warn("unknown exporter type, defaulting to GCP", "type", exporterType)
		exporter, err = createGCPExporter(ctx)
	}
	
	if err != nil {
		err = errors.Join(err, shutdown(ctx))
		return shutdown, fmt.Errorf("failed to create trace exporter (%s): %w", exporterType, err)
	}

	shutdownFuncs = append(shutdownFuncs, exporter.Shutdown)

	// Create a BatchSpanProcessor and wrap it with the dropSpanProcessor.
	batchProcessor := sdktrace.NewBatchSpanProcessor(exporter)
	dropProcessor := NewDropSpanProcessor(batchProcessor)

	// Create TracerProvider with the drop span processor.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(dropProcessor),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(&filterSampler{
			baseSampler: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0)),
		}),
	)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

	// Set the global TracerProvider
	otel.SetTracerProvider(tp)

	return shutdown, nil
}

// GetResource returns the configured resource with all detected attributes
func GetResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
}

// GetParentContext creates a new context with OpenTelemetry trace context from a traceID
func GetParentContext(ctx context.Context, traceID string) context.Context {
	// Create a SpanContext for the original trace
	originalTraceID, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		slog.Warn("invalid trace ID format. will return original context", "traceID", traceID, "error", err)
		return ctx
	}

	return trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    originalTraceID,
		SpanID:     trace.SpanID{},
		TraceFlags: trace.FlagsSampled, // Ensure the trace is sampled
		Remote:     true,               // Indicates this is a remote context
	}))
}

type tracer struct {
	name   string
	tracer trace.Tracer
}

type Tracer interface {
	Span(ctx context.Context, opts ...trace.SpanStartOption) (context.Context, trace.Span)
}

func NewTracer(name string) Tracer {
	return &tracer{
		name:   name,
		tracer: otel.Tracer(name),
	}
}

// Span creates a new span with the caller function name appended to the tracer name.
// For example, if the tracer name is "myapp" and the caller function is "DoWork",
// the span name will be "myapp.DoWork".
func (t *tracer) Span(ctx context.Context, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	caller := "<unknown>"
	if pc, _, _, ok := runtime.Caller(1); ok {
		fn := runtime.FuncForPC(pc).Name()
		if lastDot := strings.LastIndex(fn, "."); lastDot != -1 {
			caller = fn[lastDot+1:]
		} else {
			caller = fn // no dot found, use the whole string
		}
	}
	spanName := fmt.Sprintf("%s.%s", t.name, caller)
	return t.tracer.Start(ctx, spanName, opts...)
}

func NewNoopTracer() Tracer {
	return &tracer{
		name:   "noop",
		tracer: noop.NewTracerProvider().Tracer("noop"),
	}
}
