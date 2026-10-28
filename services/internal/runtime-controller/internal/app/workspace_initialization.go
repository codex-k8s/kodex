package app

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
)

// initializationAcknowledgedMessage marks owner-acknowledged diagnostic delivery before cleanup.
// initializationAcknowledgedMessage обозначает подтверждённую owner доставку до cleanup.
const initializationAcknowledgedMessage = "runtime initialization failure after owner acknowledgment"

// completeFailureWithInitialization changes no authority, lease duration, callback grace or retry.
// The existing completion stores only a closed localized summary; private pins remain log metadata.
// completeFailureWithInitialization не меняет authority, lease duration, callback grace либо retry.
// Completion сохраняет closed localized summary; private pins остаются metadata лога.
func (runtime *runtime) completeFailureWithInitialization(base context.Context, input runtimecontract.RunnerInput, observation *workload.InitializationObservation) {
	if observation == nil || !observation.Valid() {
		runtime.completeFailure(base, input, "RUNTIME_WORKLOAD_EXITED", "WORKSPACE_INIT_EXITED_NONZERO")
		return
	}
	runtime.deliverFailureCompletion(base, input, runtimecontract.InitializationFailureCode, "WORKSPACE_INIT_EXITED_NONZERO", observation)
}

// logInitializationAcknowledgment records normalized enums and the exact trusted input tuple only.
// It does not infer a historical Pod UID, emit metric labels, copy record bytes or claim live cause.
// logInitializationAcknowledgment пишет нормализованные enums и точный trusted input tuple.
// Historical Pod UID, metric labels, raw record bytes и недоказанная live cause не создаются.
func (runtime *runtime) logInitializationAcknowledgment(ctx context.Context, input runtimecontract.RunnerInput, observation *workload.InitializationObservation) {
	if runtime.logger == nil || observation == nil || !observation.Valid() {
		return
	}
	normalized := runtimecontract.NewInitializationObservation(input, observation.Stage, observation.Reason)
	recordStatus := "UNAVAILABLE"
	if observation.RecordValidated {
		recordStatus = "OBSERVED"
	}
	runtime.logger.WarnContext(ctx, initializationAcknowledgedMessage, "stage", normalized.Stage, "reason", normalized.Reason, "record_status", recordStatus,
		"pod_uid", observation.PodUID, "run_ref", input.RunRef, "node_ref", input.NodeRef, "session_ref", input.SessionRef, "turn_ref", input.TurnRef, "attempt", input.Attempt,
		"runtime_revision_digest", input.RuntimeRevisionDigest, "input_digest", input.InputDigest, "execution_binding_digest", input.ExecutionBindingDigest)
}
