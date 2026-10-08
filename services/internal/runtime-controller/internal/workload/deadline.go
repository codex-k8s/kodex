package workload

import (
	"errors"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func RuntimeExecutionDeadlineFromProto(value *cp.RuntimeExecutionDeadline) (*runtimecontract.RuntimeExecutionDeadline, error) {
	if value == nil {
		return nil, nil
	}
	if len(value.ProtoReflect().GetUnknown()) != 0 || value.EffectiveDeadlineAt == nil || value.EffectiveDeadlineAt.CheckValid() != nil {
		return nil, errors.New("runtime execution deadline is invalid")
	}
	result := &runtimecontract.RuntimeExecutionDeadline{Policy: value.Policy, EffectiveDeadlineAt: value.EffectiveDeadlineAt.AsTime()}
	for _, clock := range value.Clocks {
		if clock == nil || len(clock.ProtoReflect().GetUnknown()) != 0 || clock.StartedAt == nil || clock.DeadlineAt == nil || clock.StartedAt.CheckValid() != nil || clock.DeadlineAt.CheckValid() != nil {
			return nil, errors.New("runtime execution clock is invalid")
		}
		result.Clocks = append(result.Clocks, runtimecontract.RuntimeExecutionClock{RunRef: clock.RunRef, WorkflowVersionRef: clock.WorkflowVersionRef,
			WorkflowVersionDigest: clock.WorkflowVersionDigest, StepKey: clock.StepKey, TimeoutSeconds: clock.TimeoutSeconds,
			StartedAt: clock.StartedAt.AsTime(), DeadlineAt: clock.DeadlineAt.AsTime()})
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}
