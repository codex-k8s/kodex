package controlplane

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// sshExecutionOwnerBinding forwards the original gateway claim without substituting the broker's own workload instance or minting a fence.
// sshExecutionOwnerBinding передаёт original gateway claim без подстановки broker workload instance либо создания fence.
func sshExecutionOwnerBinding(b managedssh.Binding) *cp.ManagedSSHExecutionBinding {
	return &cp.ManagedSSHExecutionBinding{WorkloadInstance: b.WorkloadInstance, InvocationRef: b.InvocationRef, LeaseRef: b.LeaseRef, Fence: b.Fence, Generation: b.Generation, InputDigest: b.InputDigest}
}

// sshExecutionOwnerError separates known owner refusal from an unresolved transport or infrastructure failure, without retaining private remote diagnostics.
// sshExecutionOwnerError отделяет known owner refusal от unresolved transport либо infrastructure failure без сохранения private remote diagnostics.
func sshExecutionOwnerError(err error) error {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.InvalidArgument, codes.NotFound, codes.FailedPrecondition, codes.AlreadyExists:
		return sshrepo.ErrIdentityRejected
	}
	return sshrepo.ErrIdentityUnavailable
}

// BeginSSHExecution obtains one private admission through the exact protected owner method; malformed successful replies never become retryable transport uncertainty.
// BeginSSHExecution получает одну private admission через exact protected owner method; malformed successful replies не становятся retryable transport uncertainty.
func (owner *Owner) BeginSSHExecution(ctx context.Context, b managedssh.Binding) (managedssh.ExecutionAdmission, error) {
	var result managedssh.ExecutionAdmission
	if owner == nil || owner.client == nil || owner.client.SSHOperations == nil || b.Validate() != nil || b.InvocationRef == "" {
		return result, sshrepo.ErrIdentityUnavailable
	}
	response, err := owner.client.SSHOperations.BeginSSHExecution(ctx, &cp.BeginSSHExecutionRequest{Binding: sshExecutionOwnerBinding(b)}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil {
		return result, sshExecutionOwnerError(err)
	}
	if ctx.Err() != nil {
		return result, sshrepo.ErrIdentityUnavailable
	}
	if !knownSSHOwnerProto(response, response.GetIdentity(), response.GetIdentity().GetPreparation(), response.GetExpiresAt()) || response.ExpiresAt.CheckValid() != nil {
		return result, sshrepo.ErrIdentityRejected
	}
	x, prep := response.Identity, response.Identity.Preparation
	result = managedssh.ExecutionAdmission{AttemptRef: response.AttemptRef, InvocationRef: response.InvocationRef, SnapshotDigest: response.SnapshotDigest, Operation: response.Operation,
		InputDigest: response.InputDigest, SubjectDigest: response.SubjectDigest, Mode: managedssh.TransportMode(response.TransportMode), CanonicalInput: append([]byte(nil), response.CanonicalInput...), ExpiresAt: response.ExpiresAt.AsTime(),
		Identity: managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: prep.IdentityRevisionRef, TargetRef: prep.TargetRef, Revision: prep.Revision, Namespace: prep.Namespace, ContentSHA256: prep.ContentSha256,
			AuthenticationSHA256: x.AuthenticationSha256, SecretName: x.SecretName, SecretUID: x.SecretUid, SecretResourceVersion: x.SecretResourceVersion}}
	if response.ReceiptSource != nil {
		if !knownSSHOwnerProto(response.ReceiptSource) {
			clear(result.CanonicalInput)
			return managedssh.ExecutionAdmission{}, sshrepo.ErrIdentityRejected
		}
		result.ReceiptSource = &managedssh.ManagedReceiptSource{InvocationRef: response.ReceiptSource.InvocationRef, Operation: response.ReceiptSource.Operation, CanonicalInput: append([]byte(nil), response.ReceiptSource.CanonicalInput...)}
	}
	if result.Validate(time.Now()) != nil || result.InvocationRef != b.InvocationRef || result.InputDigest != b.InputDigest {
		clear(result.CanonicalInput)
		if result.ReceiptSource != nil {
			clear(result.ReceiptSource.CanonicalInput)
		}
		return managedssh.ExecutionAdmission{}, sshrepo.ErrIdentityRejected
	}
	return result, nil
}

// sshExecutionReceipt decodes closed owner metadata and optional authorized output; it never manufactures an admission from a historical intent.
// sshExecutionReceipt разбирает closed owner metadata и optional authorized output; admission из historical intent не создаётся.
func sshExecutionReceipt(input *cp.ManagedSSHExecutionReceipt, output []byte) (managedssh.ExecutionReceipt, error) {
	if !knownSSHOwnerProto(input) {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	result := managedssh.ExecutionReceipt{AttemptRef: input.AttemptRef, SnapshotDigest: input.SnapshotDigest, State: input.State, Recorded: input.Recorded, Applied: input.Applied, OwnerState: input.OwnerState,
		Observation: managedssh.ExecutionObservation{Result: append([]byte(nil), output...), ResultDigest: input.ResultDigest}}
	switch input.Outcome {
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_UNSPECIFIED:
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_SUCCEEDED:
		result.Observation.Outcome = managedssh.ExecutionSucceeded
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_FAILED:
		result.Observation.Outcome = managedssh.ExecutionFailed
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_UNKNOWN:
		result.Observation.Outcome = managedssh.ExecutionUnknown
	default:
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	if input.AuthorizedUntil != nil {
		if !knownSSHOwnerProto(input.AuthorizedUntil) || input.AuthorizedUntil.CheckValid() != nil {
			return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
		}
		result.AuthorizedUntil = input.AuthorizedUntil.AsTime()
	}
	if result.Validate(time.Now(), len(output) != 0) != nil {
		clear(result.Observation.Result)
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	return result, nil
}

// CompleteSSHExecution sends one joined observation with the original intent and distinguishes damaged successful acknowledgements from lost transport responses.
// CompleteSSHExecution отправляет одно joined observation с original intent и отличает damaged successful acknowledgement от lost transport response.
func (owner *Owner) CompleteSSHExecution(ctx context.Context, b managedssh.Binding, admission managedssh.ExecutionAdmission, observation managedssh.ExecutionObservation) (managedssh.ExecutionReceipt, error) {
	if owner == nil || owner.client == nil || owner.client.SSHOperations == nil || b.Validate() != nil || b.InvocationRef == "" || observation.Validate() != nil || !managedssh.OwnerReference(admission.AttemptRef) || !managedssh.Digest(admission.SnapshotDigest) {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	request := &cp.CompleteSSHExecutionRequest{Binding: sshExecutionOwnerBinding(b), AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest, ResultJson: observation.Result, ResultDigest: observation.ResultDigest}
	switch observation.Outcome {
	case managedssh.ExecutionSucceeded:
		request.Outcome = cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_SUCCEEDED
	case managedssh.ExecutionFailed:
		request.Outcome = cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_FAILED
	case managedssh.ExecutionUnknown:
		request.Outcome = cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_UNKNOWN
	}
	response, err := owner.client.SSHOperations.CompleteSSHExecution(ctx, request, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil {
		return managedssh.ExecutionReceipt{}, sshExecutionOwnerError(err)
	}
	if ctx.Err() != nil {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	if !knownSSHOwnerProto(response, response.GetReceipt()) {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	result, err := sshExecutionReceipt(response.Receipt, response.ResultJson)
	if err != nil {
		return managedssh.ExecutionReceipt{}, err
	}
	if !result.Recorded || result.AttemptRef != admission.AttemptRef || result.SnapshotDigest != admission.SnapshotDigest || result.Observation.Outcome != observation.Outcome || result.Observation.ResultDigest != observation.ResultDigest {
		clear(result.Observation.Result)
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	return result, nil
}

// GetSSHExecution reads original metadata once; the typed response cannot carry private output or turn an unreported intent into a new Begin.
// GetSSHExecution читает original metadata один раз; typed response не содержит private output и не превращает unreported intent в новый Begin.
func (owner *Owner) GetSSHExecution(ctx context.Context, b managedssh.Binding) (managedssh.ExecutionReceipt, error) {
	if owner == nil || owner.client == nil || owner.client.SSHOperations == nil || b.Validate() != nil || b.InvocationRef == "" {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	response, err := owner.client.SSHOperations.GetSSHExecution(ctx, &cp.GetSSHExecutionRequest{Binding: sshExecutionOwnerBinding(b)}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil {
		return managedssh.ExecutionReceipt{}, sshExecutionOwnerError(err)
	}
	if ctx.Err() != nil {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	if !knownSSHOwnerProto(response, response.GetReceipt()) {
		return managedssh.ExecutionReceipt{}, sshrepo.ErrIdentityRejected
	}
	return sshExecutionReceipt(response.Receipt, nil)
}
