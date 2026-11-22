package grpc

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sshExecutionInvalid never reflects private command bytes, identity locators, fences or upstream error details.
// sshExecutionInvalid не отражает private command bytes, identity locators, fences либо upstream error details.
const sshExecutionInvalid = "SSH execution contract is invalid"

// sshExecutionBinding accepts only the exact original invocation lease, rejecting unknown wire fields and health substitution before owner I/O.
// sshExecutionBinding принимает лишь exact original invocation lease, отвергая unknown wire fields и health substitution до owner I/O.
func sshExecutionBinding(input *cp.ManagedSSHExecutionBinding) (managedssh.Binding, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return managedssh.Binding{}, false
	}
	binding := managedssh.Binding{WorkloadInstance: input.WorkloadInstance, InvocationRef: input.InvocationRef, LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation, InputDigest: input.InputDigest}
	return binding, binding.Validate() == nil && binding.InvocationRef != ""
}

// sshExecutionOutcome maps only registered wire outcomes; default and future enum numbers cannot become failure or success.
// sshExecutionOutcome преобразует лишь registered wire outcomes; default и future enum numbers не становятся failure либо success.
func sshExecutionOutcome(input cp.ManagedSSHExecutionOutcome) (managedssh.ExecutionOutcome, bool) {
	switch input {
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_SUCCEEDED:
		return managedssh.ExecutionSucceeded, true
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_FAILED:
		return managedssh.ExecutionFailed, true
	case cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_UNKNOWN:
		return managedssh.ExecutionUnknown, true
	}
	return "", false
}

// castSSHExecutionReceipt exposes a closed receipt state and metadata without treating a discarded observation or unreported intent as applied completion.
// castSSHExecutionReceipt выдаёт closed receipt state и metadata, не принимая discarded observation либо unreported intent за applied completion.
func castSSHExecutionReceipt(input entity.SSHExecutionReceipt) (*cp.ManagedSSHExecutionReceipt, error) {
	if input.Validate(time.Now(), true) != nil {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	if !managedssh.OwnerReference(input.AttemptRef) || !managedssh.Digest(input.SnapshotDigest) {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	out := &cp.ManagedSSHExecutionReceipt{AttemptRef: input.AttemptRef, SnapshotDigest: input.SnapshotDigest, State: input.State, Recorded: input.Recorded, Applied: input.Applied, OwnerState: input.OwnerState, ResultDigest: input.Observation.ResultDigest}
	if !input.Recorded {
		if input.State != "UNREPORTED" || input.Applied || input.Observation.Outcome != "" || input.Observation.ResultDigest != "" || len(input.Observation.Result) != 0 {
			return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
		}
		if !input.AuthorizedUntil.IsZero() {
			if !input.AuthorizedUntil.After(time.Now()) || input.AuthorizedUntil.After(time.Now().Add(30*time.Second)) {
				return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
			}
			out.AuthorizedUntil = timestamppb.New(input.AuthorizedUntil)
			if out.AuthorizedUntil.CheckValid() != nil {
				return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
			}
		}
		return out, nil
	}
	if !input.AuthorizedUntil.IsZero() {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	switch input.Observation.Outcome {
	case managedssh.ExecutionSucceeded:
		out.Outcome = cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_SUCCEEDED
	case managedssh.ExecutionFailed:
		out.Outcome = cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_FAILED
	case managedssh.ExecutionUnknown:
		out.Outcome = cp.ManagedSSHExecutionOutcome_MANAGED_SSH_EXECUTION_OUTCOME_UNKNOWN
	default:
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	expected := string(input.Observation.Outcome)
	if input.Observation.Outcome == managedssh.ExecutionUnknown {
		expected = "UNKNOWN_OUTCOME"
	}
	if input.Applied && input.State != expected || !input.Applied && input.State != "DISCARDED" ||
		input.Observation.Outcome == managedssh.ExecutionSucceeded && !managedssh.Digest(input.Observation.ResultDigest) ||
		input.Observation.Outcome == managedssh.ExecutionUnknown && (input.Observation.ResultDigest != "" || len(input.Observation.Result) != 0) ||
		!input.Applied && len(input.Observation.Result) != 0 {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	if len(input.Observation.Result) != 0 && input.Observation.Validate() != nil {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	return out, nil
}

// BeginSSHExecution returns one committed private admission; transport metadata cannot select actor, target, identity or a second dispatch attempt.
// BeginSSHExecution возвращает одну committed private admission; transport metadata не выбирает actor, target, identity либо second dispatch attempt.
func (server *Server) BeginSSHExecution(ctx context.Context, request *cp.BeginSSHExecutionRequest) (*cp.BeginSSHExecutionResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHOwnerService_BeginSSHExecution_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	binding, valid := sshExecutionBinding(request.Binding)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	result, err := server.service.BeginSSHExecution(ctx, p, binding)
	if err != nil {
		return nil, transportError(err)
	}
	if result.Validate(time.Now()) != nil || result.InvocationRef != binding.InvocationRef || result.InputDigest != binding.InputDigest || result.Recover {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	identity := result.Identity
	out := &cp.BeginSSHExecutionResponse{AttemptRef: result.AttemptRef, InvocationRef: result.InvocationRef, SnapshotDigest: result.SnapshotDigest,
		Operation: result.Operation, InputDigest: result.InputDigest, SubjectDigest: result.SubjectDigest, TransportMode: string(result.Mode), CanonicalInput: result.CanonicalInput, ExpiresAt: timestamppb.New(result.ExpiresAt),
		Identity: &cp.ManagedSSHIdentityLocator{Preparation: &cp.ManagedSSHIdentityPreparation{IdentityRevisionRef: identity.IdentityRevisionRef, TargetRef: identity.TargetRef, Revision: identity.Revision, Namespace: identity.Namespace, ContentSha256: identity.ContentSHA256},
			AuthenticationSha256: identity.AuthenticationSHA256, SecretName: identity.SecretName, SecretUid: identity.SecretUID, SecretResourceVersion: identity.SecretResourceVersion}}
	if result.ReceiptSource != nil {
		out.ReceiptSource = &cp.ManagedSSHReceiptSource{InvocationRef: result.ReceiptSource.InvocationRef, Operation: result.ReceiptSource.Operation, CanonicalInput: result.ReceiptSource.CanonicalInput}
	}
	if out.ExpiresAt.CheckValid() != nil {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	return out, nil
}

// CompleteSSHExecution forwards one complete bounded observation and returns output only when the exact owner's publication transaction applied it.
// CompleteSSHExecution передаёт одно complete bounded observation и выдаёт output лишь при applied exact owner publication transaction.
func (server *Server) CompleteSSHExecution(ctx context.Context, request *cp.CompleteSSHExecutionRequest) (*cp.CompleteSSHExecutionResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHOwnerService_CompleteSSHExecution_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	binding, valid := sshExecutionBinding(request.Binding)
	outcome, known := sshExecutionOutcome(request.Outcome)
	if !valid || !known {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	observation := managedssh.ExecutionObservation{Outcome: outcome, Result: request.ResultJson, ResultDigest: request.ResultDigest}
	if observation.Validate() != nil || !managedssh.OwnerReference(request.AttemptRef) || !managedssh.Digest(request.SnapshotDigest) {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	result, err := server.service.CompleteSSHExecution(ctx, p, command.SSHExecutionComplete{Binding: binding, AttemptRef: request.AttemptRef, SnapshotDigest: request.SnapshotDigest, Observation: observation})
	if err != nil {
		return nil, transportError(err)
	}
	receipt, err := castSSHExecutionReceipt(result)
	if err != nil {
		return nil, err
	}
	if result.AttemptRef != request.AttemptRef || result.SnapshotDigest != request.SnapshotDigest || !result.Recorded || result.Observation.Outcome != observation.Outcome || result.Observation.ResultDigest != observation.ResultDigest {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	return &cp.CompleteSSHExecutionResponse{Receipt: receipt, ResultJson: result.Observation.Result}, nil
}

// GetSSHExecution returns original metadata only; neither private output nor a new dispatch grant is reconstructed from historical intent.
// GetSSHExecution возвращает лишь original metadata; private output и новый dispatch grant не восстанавливаются из historical intent.
func (server *Server) GetSSHExecution(ctx context.Context, request *cp.GetSSHExecutionRequest) (*cp.GetSSHExecutionResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHOwnerService_GetSSHExecution_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	binding, valid := sshExecutionBinding(request.Binding)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, sshExecutionInvalid)
	}
	result, err := server.service.GetSSHExecution(ctx, p, binding)
	if err != nil {
		return nil, transportError(err)
	}
	if len(result.Observation.Result) != 0 {
		return nil, status.Error(codes.Unavailable, sshExecutionInvalid)
	}
	receipt, err := castSSHExecutionReceipt(result)
	if err != nil {
		return nil, err
	}
	return &cp.GetSSHExecutionResponse{Receipt: receipt}, nil
}
