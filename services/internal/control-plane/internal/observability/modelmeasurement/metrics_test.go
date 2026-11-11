package modelmeasurement

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/prometheus/client_golang/prometheus"
	"strings"
	"testing"
)

// TestMetricsKeepUnknownTimingAbsentAndLabelsBounded verifies actual observations without introducing identifier or arbitrary phase labels.
// TestMetricsKeepUnknownTimingAbsentAndLabelsBounded проверяет реальные observations без identifier или произвольных phase labels.
func TestMetricsKeepUnknownTimingAbsentAndLabelsBounded(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := New(func(values ...prometheus.Collector) error {
		for _, value := range values {
			if err := registry.Register(value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	value := runtimecontract.ModelMeasurement{Ref: "request_synthetic01", Source: "HTTP_REQUEST", Phase: "COMPACTION", Outcome: "FAILED", Completeness: "PARTIAL", DurationMS: new(int64(100))}
	metrics.Observe(value)
	value.Phase = "untrusted-phase"
	metrics.Observe(value)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	observations := 0
	for _, family := range families {
		if family.GetName() == "kodex_control_plane_model_ttft_seconds" {
			t.Fatal("unknown TTFT became zero")
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if strings.Contains(label.GetValue(), "synthetic") || label.GetValue() == "untrusted-phase" {
					t.Fatal("unbounded metric label")
				}
			}
			if family.GetName() == "kodex_control_plane_model_measurements_total" {
				observations += int(metric.GetCounter().GetValue())
			}
		}
	}
	if observations != 1 {
		t.Fatal("invalid measurement counted")
	}
}
