package app

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// bindNativeCompletion accepts only sealed broker capture; arbitrary exported Result fields cannot persist state.
// Failed executions may carry no checkpoint, but a successful native turn must not silently discard its history.
// bindNativeCompletion принимает лишь sealed broker capture; произвольные Result fields не сохраняют state.
// Failed execution может не иметь checkpoint, но успешный native turn не должен терять history.
func bindNativeCompletion(input model.Input, result codex.Result, payload *runtimecontract.RunnerCompletionRequest) error {
	if result.HasVerifiedNativeCheckpoint(input) {
		checkpoint := *result.NativeSession
		payload.NativeSession = &checkpoint
		return nil
	}
	if result.NativeSession != nil || input.Provider == "opencode" && payload.Success {
		return runtimecontract.ErrNativeSession
	}
	return nil
}
