package websockettransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"testing"
)

// TestModelMeasurementRealtimeProjection preserves explicit zero TTFT, missing counters and closed source validation on the shared HTTP/WebSocket path.
// TestModelMeasurementRealtimeProjection сохраняет явный нулевой TTFT, отсутствие counters и закрытую проверку source в общем HTTP/WebSocket пути.
func TestModelMeasurementRealtimeProjection(t *testing.T) {
	value := &cp.ModelMeasurement{Ref: "request_fixture01", Source: "HTTP_REQUEST", Phase: "COMPACTION", Outcome: "COMPLETED", Completeness: "COMPLETE", DurationMs: new(int64(10)), TtftMs: new(int64(0))}
	event := &cp.RunEvent{Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, ModelMeasurement: value, Execution: &cp.RunEventExecution{RunRef: "run_fixture01", NodeRef: "nod_fixture01", SessionRef: "ses_fixture01", TurnRef: "trn_fixture01", TurnNumber: 1, Attempt: 1}}
	got, err := projectRunEvent(event, func(s string) string { return s })
	if err != nil || got.ModelMeasurement == nil || got.ModelMeasurement.TtftMs == nil || *got.ModelMeasurement.TtftMs != 0 || got.ModelMeasurement.Usage != nil {
		t.Fatal("measurement projection lost presence", err)
	}
	value.TtftMs = nil
	value.Completeness = "PARTIAL"
	got, err = projectRunEvent(event, func(s string) string { return s })
	if err != nil || got.ModelMeasurement.TtftMs != nil {
		t.Fatal("unknown TTFT invented", err)
	}
	value.Source = "ARBITRARY"
	if _, err := projectRunEvent(event, func(s string) string { return s }); err == nil {
		t.Fatal("unknown measurement source accepted")
	}
}
