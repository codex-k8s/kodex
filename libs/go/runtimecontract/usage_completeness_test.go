package runtimecontract

import (
	"encoding/json"
	"testing"
)

// TestUsageCompletenessWire distinguishes missing legacy quality, explicit numeric zero and partial
// subtotals while rejecting malformed/unknown wire values without discarding existing counters.
// TestUsageCompletenessWire отличает отсутствующее legacy-качество, явный числовой ноль и частичные
// подытоги, отвергая неверные wire-значения без потери существующих счётчиков.
func TestUsageCompletenessWire(t *testing.T) {
	for _, fixture := range []struct {
		name, raw string
		quality   UsageCompleteness
		tokens    int64
	}{
		{"legacy positive", `{"total_tokens":100,"input_tokens":100}`, UsageUnknown, 100},
		{"unknown zero", `{"completeness":"UNKNOWN"}`, UsageUnknown, 0},
		{"complete zero", `{"completeness":"COMPLETE"}`, UsageComplete, 0},
		{"partial subtotal", `{"completeness":"PARTIAL","total_tokens":100,"input_tokens":100}`, UsagePartial, 100},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var usage TokenUsage
			if json.Unmarshal([]byte(fixture.raw), &usage) != nil || usage.Validate() != nil || usage.Completeness != fixture.quality || usage.TotalTokens != fixture.tokens {
				t.Fatal("usage quality or numeric subtotal changed")
			}
			wire, err := json.Marshal(usage)
			var copied TokenUsage
			if err != nil || json.Unmarshal(wire, &copied) != nil || copied != usage {
				t.Fatal("usage round trip changed measurement")
			}
		})
	}
	for _, value := range []string{`null`, `0`, `{}`, `""`, `"complete"`, `"INVALID"`, `"COMPLETE" {}`} {
		var quality UsageCompleteness
		if json.Unmarshal([]byte(value), &quality) == nil {
			t.Fatal("invalid usage completeness accepted")
		}
	}
	if (TokenUsage{Completeness: UsageCompleteness(255)}).Validate() == nil {
		t.Fatal("unknown in-memory quality accepted")
	}
}
