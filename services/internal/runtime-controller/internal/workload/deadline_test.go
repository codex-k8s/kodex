package workload

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
	"time"
)

func TestRuntimeExecutionDeadlineProtoRoundtripAndRejectsInvalidPins(t *testing.T) {
	start := time.Date(2026, 10, 8, 0, 0, 0, 123000, time.UTC)
	value := &cp.RuntimeExecutionDeadline{Policy: runtimecontract.WorkflowExecutionDeadlinePolicy, EffectiveDeadlineAt: timestamppb.New(start.Add(24 * time.Hour)), Clocks: []*cp.RuntimeExecutionClock{{RunRef: "run_fixture01", WorkflowVersionRef: "wfv_fixture01", WorkflowVersionDigest: strings.Repeat("a", 64), TimeoutSeconds: 86400, StartedAt: timestamppb.New(start), DeadlineAt: timestamppb.New(start.Add(24 * time.Hour))}}}
	got, err := RuntimeExecutionDeadlineFromProto(value)
	if err != nil || !got.Clocks[0].StartedAt.Equal(start) || !got.EffectiveDeadlineAt.Equal(value.EffectiveDeadlineAt.AsTime()) {
		t.Fatalf("deadline pins lost: %v", err)
	}
	for _, name := range []string{"missing_time", "wrong_digest", "wrong_effective", "nil_clock", "step_limit", "unknown_clock", "unknown_policy"} {
		t.Run(name, func(t *testing.T) {
			invalid := proto.Clone(value).(*cp.RuntimeExecutionDeadline)
			switch name {
			case "unknown_clock":
				invalid.Clocks[0].ProtoReflect().SetUnknown([]byte{0x50, 0x01})
			case "unknown_policy":
				invalid.ProtoReflect().SetUnknown([]byte{0x50, 0x01})
			case "missing_time":
				invalid.Clocks[0].StartedAt = nil
			case "wrong_digest":
				invalid.Clocks[0].WorkflowVersionDigest = "bad"
			case "wrong_effective":
				invalid.EffectiveDeadlineAt = timestamppb.New(start)
			case "nil_clock":
				invalid.Clocks[0] = nil
			case "step_limit":
				invalid.Clocks[0].StepKey = "step"
				invalid.Clocks[0].TimeoutSeconds = 86401
			}
			if _, err := RuntimeExecutionDeadlineFromProto(invalid); err == nil {
				t.Fatal("invalid deadline admitted")
			}
		})
	}
	compiled, _, _ := loadRunnerInputSchema(t)
	manager := newTestManager(t, fake.NewSimpleClientset())
	execution := testExecution(false)
	execution.Revision.ExecutionDeadline = value
	sealTestTurnExecution(execution)
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		t.Fatal(err)
	}
	validateRunnerInputSchema(t, compiled, input)
	raw, err := json.Marshal(input.ExecutionDeadline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimecontract.DecodeRuntimeExecutionDeadline(json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	execution.Revision.ExecutionDeadline.Clocks[0].WorkflowVersionDigest = strings.Repeat("b", 64)
	if _, _, err := manager.BuildTurnInput(execution); err == nil {
		t.Fatal("deadline source pin changed without revision digest")
	}
}
