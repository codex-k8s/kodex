package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func castRuntimeExecutionDeadline(value any) (*cp.RuntimeExecutionDeadline, bool) {
	if value == nil {
		return nil, true
	}
	deadline, err := runtimecontract.DecodeRuntimeExecutionDeadline(value)
	if err != nil {
		return nil, false
	}
	result := &cp.RuntimeExecutionDeadline{Policy: deadline.Policy, EffectiveDeadlineAt: timestamppb.New(deadline.EffectiveDeadlineAt)}
	for _, clock := range deadline.Clocks {
		result.Clocks = append(result.Clocks, &cp.RuntimeExecutionClock{RunRef: clock.RunRef, WorkflowVersionRef: clock.WorkflowVersionRef,
			WorkflowVersionDigest: clock.WorkflowVersionDigest, StepKey: clock.StepKey, TimeoutSeconds: clock.TimeoutSeconds,
			StartedAt: timestamppb.New(clock.StartedAt), DeadlineAt: timestamppb.New(clock.DeadlineAt)})
	}
	return result, true
}
