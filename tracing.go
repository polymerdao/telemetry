package telemetry

import (
	"context"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/trace"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
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

// InitTracer initializes the OpenTelemetry tracer with google exporter and a drop span processor.
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

	// Configure Trace Export to send spans as OTLP
	exporter, err := texporter.New()
	if err != nil {
		err = errors.Join(err, shutdown(ctx))
		return shutdown, fmt.Errorf("failed to create trace exporter: %w", err)
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

// GetTelemetryParentContext creates a new context with OpenTelemetry trace context from a traceID
func GetTelemetryParentContext(ctx context.Context, traceID string) context.Context {
	// Create a SpanContext for the original trace
	originalTraceID, _ := trace.TraceIDFromHex(traceID)
	parentSpanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    originalTraceID,
		SpanID:     trace.SpanID{},
		TraceFlags: trace.FlagsSampled, // Ensure the trace is sampled
		Remote:     true,               // Indicates this is a remote context
	})
	return trace.ContextWithRemoteSpanContext(ctx, parentSpanContext)
}
