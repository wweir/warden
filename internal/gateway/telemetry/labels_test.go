package telemetry

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func labelPair(name, value string) *dto.LabelPair {
	return &dto.LabelPair{Name: &name, Value: &value}
}

func TestParseMetricLabels(t *testing.T) {
	got := parseMetricLabels([]*dto.LabelPair{
		labelPair("route", "/r"),
		labelPair("protocol", "chat"),
		labelPair("provider_model", "gpt-4o-mini"),
		labelPair("route_model", "gpt-4o"),
		labelPair("matched_pattern", "/r/*"),
		labelPair("provider", "prov-a"),
		labelPair("endpoint", "chat/completions"),
		labelPair("type", "prompt"),
		labelPair("status", "success"),
		labelPair("completeness", "exact"),
		labelPair("source", "upstream"),
		labelPair("unknown_label", "ignored"),
	})

	// route_model appears after provider_model, so Model must take route_model.
	want := metricLabels{
		Route:          "/r",
		Protocol:       "chat",
		RouteModel:     "gpt-4o",
		MatchedPattern: "/r/*",
		Provider:       "prov-a",
		ProviderModel:  "gpt-4o-mini",
		Model:          "gpt-4o",
		Endpoint:       "chat/completions",
		Type:           "prompt",
		Status:         "success",
		Completeness:   "exact",
		Source:         "upstream",
	}
	if got != want {
		t.Fatalf("parseMetricLabels = %+v, want %+v", got, want)
	}
}

func TestParseMetricLabelsEmpty(t *testing.T) {
	if got := parseMetricLabels(nil); got != (metricLabels{}) {
		t.Fatalf("parseMetricLabels(nil) = %+v, want zero value", got)
	}
}
