package telemetry

import (
	"testing"

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
