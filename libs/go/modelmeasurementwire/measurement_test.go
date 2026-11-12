package modelmeasurementwire

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"reflect"
	"testing"
)

// TestRoundTripPreservesUnknownAndIndependentCopies prevents omitted timing from becoming zero and caller mutation from rewriting receipts.
// TestRoundTripPreservesUnknownAndIndependentCopies исключает превращение отсутствующего timing в ноль и изменение квитанций через caller alias.
func TestRoundTripPreservesUnknownAndIndependentCopies(t *testing.T) {
	for _, value := range []runtimecontract.ModelMeasurement{
		{Ref: "request_fixture01", Source: "HTTP_REQUEST", Phase: "NORMAL", Outcome: "COMPLETED", Completeness: "COMPLETE", DurationMS: new(int64(100)), TTFTMS: new(int64(0))},
		{Ref: "step_fixture01", Source: "NATIVE_STEP", Phase: "COMPACTION", Outcome: "COMPLETED", Completeness: "PARTIAL", Usage: &runtimecontract.TokenUsage{Completeness: runtimecontract.UsagePartial, InputTokens: 20, OutputTokens: 5, TotalTokens: 25, CacheWriteInputTokens: 10}},
	} {
		wire := ToProto(&value)
		result := FromProto(wire)
		if !reflect.DeepEqual(&value, result) || result.Validate() != nil {
			t.Fatal("measurement changed across protobuf")
		}
		if wire.DurationMs != nil {
			*wire.DurationMs = 999
			if *result.DurationMS != 100 || *value.DurationMS != 100 {
				t.Fatal("timing alias retained")
			}
		}
		if wire.Usage != nil {
			wire.Usage.InputTokens = 999
			if result.Usage.InputTokens != 20 || value.Usage.InputTokens != 20 {
				t.Fatal("usage alias retained")
			}
		}
	}
	if ToProto(nil) != nil || FromProto(nil) != nil {
		t.Fatal("missing measurement invented")
	}
}
