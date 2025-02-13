package telemetry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware wraps an http.Handler with OpenTelemetry tracing
func TracingMiddleware(next http.Handler, opts ...otelhttp.Option) http.Handler {
	// Default options
	defaultOpts := []otelhttp.Option{
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			var request struct {
				Method string `json:"method"`
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return r.Method + " " + r.URL.Path
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err := json.Unmarshal(body, &request); err != nil {
				return r.Method + " " + r.URL.Path
			}
			return request.Method
		}),
		otelhttp.WithFilter(func(r *http.Request) bool {
			// Don't trace health check endpoints
			return r.URL.Path != "/health"
		}),
		otelhttp.WithSpanOptions(trace.WithAttributes(
			attribute.String("server.type", "http"),
		)),
	}

	// Combine default options with custom options
	allOpts := append(defaultOpts, opts...)

	// Use the otelhttp handler with combined options
	return otelhttp.NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			next.ServeHTTP(w, r.WithContext(ctx))
		}),
		"http_server",
		allOpts...,
	)
}
