package callback

import (
	"context"
	"encoding/json"
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// observeWorkspace authenticates the exact ticket and asks the owner only with controller-observed storage IDs.
// It neither accepts caller UID authority nor performs filesystem writes or retries an uncertain BIND.
// observeWorkspace аутентифицирует exact ticket и передаёт owner только controller-observed storage IDs.
// Caller UID не является authority; здесь нет filesystem writes или retry неизвестного BIND.
func (server *Server) observeWorkspace(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok || input.WorkspaceStorage == nil {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerWorkspaceObservation
	if decode(request, &payload, runtimecontract.MaximumWorkspaceObservationBytes) != nil || payload.ValidateForInput(input) != nil {
		http.Error(writer, "invalid runtime workspace observation", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	defer cancel()
	// QUIESCENT requires the separate finalizer/positive writer-join path; no phase-only fallback is permitted.
	// QUIESCENT требует отдельный finalizer/positive writer-join path; phase-only fallback запрещён.
	if payload.Phase != runtimecontract.WorkspaceObserveBind {
		http.Error(writer, "runtime workspace quiescence requires controller observation", http.StatusForbidden)
		return
	}
	observation, err := server.manager.ObserveWorkspaceBind(ctx, input)
	if err != nil {
		http.Error(writer, "runtime workspace owned storage is unavailable", http.StatusServiceUnavailable)
		return
	}
	response, err := server.control.Runtime.ObserveWorkspace(ctx, &controlplanev1.ObserveWorkspaceRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, "workspace:"+payload.Phase)},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest,
		WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, WorkspaceGeneration: input.WorkspaceStorage.Generation,
		Phase:  controlplanev1.RuntimeWorkspaceObservationPhase_RUNTIME_WORKSPACE_OBSERVATION_PHASE_BIND,
		PodUid: observation.PodUID, PvcUid: observation.PVCUID,
	})
	if err != nil {
		writeControlError(writer, err)
		return
	}
	raw := response.GetWriterAcknowledgement()
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(raw)
	if err != nil || !ack.Matches(input) || ack.Writer.PodUID != observation.PodUID || ack.Writer.PVCUID != observation.PVCUID ||
		!response.GetCurrentTuple() || response.GetStorageState() != "WRITING" || response.GetWriterReleased() || response.GetCommittedPhase() != runtimecontract.WorkspaceObserveBind {
		http.Error(writer, "runtime workspace owner acknowledgement is invalid", http.StatusServiceUnavailable)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(json.RawMessage(raw))
}
