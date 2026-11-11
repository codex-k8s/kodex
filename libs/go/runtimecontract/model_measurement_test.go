package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestModelMeasurementKeepsTransportAndNativeAccountingSeparate prevents retries, missing timing and unknown usage from becoming fabricated complete measurements.
// TestModelMeasurementKeepsTransportAndNativeAccountingSeparate исключает превращение retries, отсутствующего timing и неизвестного usage в выдуманные полные измерения.
func TestModelMeasurementKeepsTransportAndNativeAccountingSeparate(t *testing.T) {
	request := ModelMeasurement{Ref: "request_synthetic001", Source: "HTTP_REQUEST", Phase: "COMPACTION", Outcome: "COMPLETED", Completeness: "COMPLETE", DurationMS: new(int64(123)), TTFTMS: new(int64(40))}
	step := ModelMeasurement{Ref: "step_synthetic001", Source: "NATIVE_STEP", Phase: "NORMAL", Outcome: "COMPLETED", Completeness: "PARTIAL", Usage: &TokenUsage{Completeness: UsagePartial, TotalTokens: 120, InputTokens: 100, OutputTokens: 20, CachedInputTokens: 70, CacheWriteInputTokens: 10}}
	for _, measurement := range []ModelMeasurement{request, step, {Ref: "request_synthetic002", Source: "HTTP_REQUEST", Phase: "AUXILIARY", Outcome: "CANCELLED", Completeness: "UNKNOWN"}} {
		raw, _ := json.Marshal(measurement)
		if _, err := DecodeModelMeasurement(raw); err != nil {
			t.Fatal(err)
		}
	}
	for name, mutate := range map[string]func(*ModelMeasurement){
		"missing TTFT":            func(m *ModelMeasurement) { m.TTFTMS = nil },
		"negative duration":       func(m *ModelMeasurement) { m.DurationMS = new(int64(-1)) },
		"TTFT after completion":   func(m *ModelMeasurement) { m.TTFTMS = new(int64(124)) },
		"usage on transport":      func(m *ModelMeasurement) { m.Usage = step.Usage },
		"invented source":         func(m *ModelMeasurement) { m.Source = "PROVIDER_CACHE_PROOF" },
		"payload phase":           func(m *ModelMeasurement) { m.Phase = "arbitrary" },
		"foreign receipt kind":    func(m *ModelMeasurement) { m.Ref = step.Ref },
		"unknown measured timing": func(m *ModelMeasurement) { m.Completeness = "UNKNOWN" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := request
			mutate(&bad)
			if bad.Validate() == nil {
				t.Fatal("invalid measurement accepted")
			}
		})
	}
	raw, _ := json.Marshal(step)
	for _, bad := range []string{string(raw) + string(raw), strings.Replace(string(raw), `"phase":"NORMAL"`, `"phase":"NORMAL","phase":"COMPACTION"`, 1), strings.Replace(string(raw), `"source":`, `"tenant":"foreign","source":`, 1)} {
		if _, err := DecodeModelMeasurement([]byte(bad)); err == nil {
			t.Fatal("ambiguous measurement accepted")
		}
	}
	unknownStep := step
	unknownUsage := *step.Usage
	unknownUsage.Completeness = UsageUnknown
	unknownStep.Usage = &unknownUsage
	if unknownStep.Validate() == nil {
		t.Fatal("unknown counters promoted to partial observations")
	}
	missingTiming := request
	missingTiming.Completeness = "PARTIAL"
	missingTiming.TTFTMS, missingTiming.DurationMS = nil, nil
	if missingTiming.Validate() == nil {
		t.Fatal("absent timings promoted to partial observations")
	}
	step.Completeness = "COMPLETE"
	if step.Validate() == nil {
		t.Fatal("native partial counters advertised complete")
	}
}
