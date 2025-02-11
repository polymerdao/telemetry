package telemetry

import (
	"context"
	"errors"
	"fmt"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type filterSampler struct {
	baseSampler sdktrace.Sampler
}

func (f *filterSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if p.Name == "google.devtools.cloudtrace.v2.TraceService/BatchWriteSpans" {
		return sdktrace.SamplingResult{Decision: sdktrace.Drop}
	}
	return f.baseSampler.ShouldSample(p)
}

func (f *filterSampler) Description() string {
	return fmt.Sprintf("FilterSampler{%s}", f.baseSampler.Description())
}

// InitTracer initializes the OpenTelemetry tracer with google exporter
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

	// Create TracerProvider with the exporter
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
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
