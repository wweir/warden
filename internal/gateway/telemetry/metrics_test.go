package telemetry_test

import (
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
)

func TestHistogramQuantile(t *testing.T) {
	buckets := []*dto.Bucket{
		{UpperBound: ptrFloat64(10), CumulativeCount: ptrUint64(10)},
		{UpperBound: ptrFloat64(20), CumulativeCount: ptrUint64(30)},
		{UpperBound: ptrFloat64(50), CumulativeCount: ptrUint64(100)},
	}

	got := telemetrypkg.HistogramQuantile(0.95, buckets)
	want := 47.857142857142854
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("histogramQuantile(0.95) = %v, want %v", got, want)
	}
}

func TestHistogramQuantile_InfFallback(t *testing.T) {
	buckets := []*dto.Bucket{
		{UpperBound: ptrFloat64(10), CumulativeCount: ptrUint64(50)},
		{UpperBound: ptrFloat64(math.Inf(1)), CumulativeCount: ptrUint64(100)},
	}

	got := telemetrypkg.HistogramQuantile(0.99, buckets)
	if got != 10 {
		t.Fatalf("histogramQuantile(0.99) with +Inf bucket = %v, want 10", got)
	}
}

func TestHistogramQuantile_Empty(t *testing.T) {
	if got := telemetrypkg.HistogramQuantile(0.95, nil); got != 0 {
		t.Fatalf("histogramQuantile(0.95, nil) = %v, want 0", got)
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrUint64(v uint64) *uint64 {
	return &v
}
