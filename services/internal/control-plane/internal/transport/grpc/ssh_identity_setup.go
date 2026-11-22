package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sshSetupResultUnavailable is shared by invalid owner-result projections and never includes private material coordinates or upstream errors.
// sshSetupResultUnavailable объединяет ошибки проекции owner result и не включает private material coordinates либо upstream errors.
const sshSetupResultUnavailable = "SSH identity setup result unavailable"

// sshSetupRequestInvalid conceals malformed completion details without exposing private hashes, locators or fences.
// sshSetupRequestInvalid скрывает подробности malformed completion без private hashes, locators либо fences.
const sshSetupRequestInvalid = "SSH identity setup request is invalid"

// BeginSSHIdentitySetup forwards only content preconditions and explicit OCC to the authenticated owner; private bytes go directly to the broker afterwards.
// BeginSSHIdentitySetup передаёт authenticated owner лишь content preconditions и explicit OCC; private bytes далее идут прямо broker.
func (server *Server) BeginSSHIdentitySetup(ctx context.Context, request *cp.BeginSSHIdentitySetupRequest) (*cp.BeginSSHIdentitySetupResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHIdentitySetupService_BeginSSHIdentitySetup_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.BeginSSHIdentitySetup(ctx, p, sshSetupMutation(request.GetMutation()), command.SSHIdentitySetupBegin{
		ConnectionRef: request.GetConnectionRef(), ExpectedConnectionVersion: request.GetExpectedConnectionVersion(), ContentSHA256: request.GetContentSha256(), ContentBytes: request.GetContentBytes()})
	if err != nil {
		return nil, transportError(err)
	}
	view, err := castSSHSetupView(result.View)
	if err != nil {
		return nil, err
	}
	if result.ReconcileOnly {
		if len(result.Permit) != 0 || result.Endpoint.Origin != "" || result.Endpoint.TLSServerName != "" {
			return nil, status.Error(codes.Unavailable, sshSetupResultUnavailable)
		}
	} else if len(result.Permit) != 32 || !validSSHSetupUploadEndpoint(result.Endpoint) {
		return nil, status.Error(codes.Unavailable, sshSetupResultUnavailable)
	}
	return &cp.BeginSSHIdentitySetupResponse{Setup: view, UploadPermit: append([]byte(nil), result.Permit...), UploadOrigin: result.Endpoint.Origin, UploadTlsServerName: result.Endpoint.TLSServerName, ReconcileOnly: result.ReconcileOnly}, nil
}

// GetSSHIdentitySetup preserves the oneof selector and returns safe current metadata without reissuing an upload permit.
// GetSSHIdentitySetup сохраняет oneof selector и возвращает safe current metadata без повторной выдачи upload permit.
func (server *Server) GetSSHIdentitySetup(ctx context.Context, request *cp.GetSSHIdentitySetupRequest) (*cp.GetSSHIdentitySetupResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHIdentitySetupService_GetSSHIdentitySetup_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetSSHIdentitySetup(ctx, p, command.SSHIdentitySetupRead{ConnectionRef: request.GetConnectionRef(), SetupRef: request.GetSetupRef(), BeginIdempotencyKey: request.GetBeginIdempotencyKey()})
	if err != nil {
		return nil, transportError(err)
	}
	view, err := castSSHSetupView(result)
	if err != nil {
		return nil, err
	}
	return &cp.GetSSHIdentitySetupResponse{Setup: view}, nil
}

// CancelSSHIdentitySetup addresses one owner's setup, preserving late material evidence and never translating cancellation into remote absence.
// CancelSSHIdentitySetup адресует setup одного owner, сохраняя late material evidence и не превращая cancellation в remote absence.
func (server *Server) CancelSSHIdentitySetup(ctx context.Context, request *cp.CancelSSHIdentitySetupRequest) (*cp.CancelSSHIdentitySetupResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHIdentitySetupService_CancelSSHIdentitySetup_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.CancelSSHIdentitySetup(ctx, p, sshSetupMutation(request.GetMutation()), command.SSHIdentitySetupCancel{ConnectionRef: request.GetConnectionRef(), SetupRef: request.GetSetupRef()})
	if err != nil {
		return nil, transportError(err)
	}
	view, err := castSSHSetupView(result)
	if err != nil {
		return nil, err
	}
	return &cp.CancelSSHIdentitySetupResponse{Setup: view}, nil
}

// ResolveSSHIdentityUpload serves the protected broker method only; the response contains pending private coordinates, never key bytes or another material write grant.
// ResolveSSHIdentityUpload обслуживает лишь protected broker method; response содержит pending private coordinates без key bytes либо другого material write grant.
func (server *Server) ResolveSSHIdentityUpload(ctx context.Context, request *cp.ResolveSSHIdentityUploadRequest) (*cp.ResolveSSHIdentityUploadResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHIdentitySetupService_ResolveSSHIdentityUpload_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ResolveSSHIdentityUpload(ctx, p, command.SSHIdentityUploadResolve{SetupRef: request.GetSetupRef(), PermitSHA256: request.GetPermitSha256()})
	if err != nil {
		return nil, transportError(err)
	}
	mode, ok := castSSHUploadMode(result.Mode)
	expires := timestamppb.New(result.ExpiresAt)
	if !ok || !result.Preparation.Valid() || !managedssh.Digest(result.Digest) || expires.CheckValid() != nil || result.ContentBytes == 0 || result.ContentBytes > 16384 ||
		(result.Mode == enum.SSHIdentityUploadWrite && result.AuthenticationSHA256 != "") || (result.Mode == enum.SSHIdentityUploadObserve && !managedssh.Digest(result.AuthenticationSHA256)) {
		return nil, status.Error(codes.Unavailable, sshSetupResultUnavailable)
	}
	return &cp.ResolveSSHIdentityUploadResponse{SetupRef: result.SetupRef, SnapshotDigest: result.Digest, Preparation: castSSHPreparation(result.Preparation), ContentBytes: result.ContentBytes,
		AuthenticationSha256: result.AuthenticationSHA256, ExpiresAt: expires, Mode: mode}, nil
}

// ClaimSSHIdentityUpload forwards broker-derived metadata to the one owner ledger; no request field selects policy, actor, namespace, revision or WRITE versus OBSERVE.
// ClaimSSHIdentityUpload передаёт broker-derived metadata единому owner ledger; request не выбирает policy, actor, namespace, revision либо WRITE/OBSERVE.
func (server *Server) ClaimSSHIdentityUpload(ctx context.Context, request *cp.ClaimSSHIdentityUploadRequest) (*cp.ClaimSSHIdentityUploadResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHIdentitySetupService_ClaimSSHIdentityUpload_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ClaimSSHIdentityUpload(ctx, p, command.SSHIdentityUploadClaim{SetupRef: request.GetSetupRef(), PermitSHA256: request.GetPermitSha256(), SnapshotDigest: request.GetSnapshotDigest(),
		WorkloadInstance: request.GetWorkloadInstance(), Key: managedssh.IdentityKeyMetadata{ContentSHA256: request.GetContentSha256(), AuthenticationSHA256: request.GetAuthenticationSha256()}})
	if err != nil {
		return nil, transportError(err)
	}
	mode, ok := castSSHUploadMode(result.Mode)
	expires := timestamppb.New(result.ExpiresAt)
	if !ok || !result.Provision.Valid() || expires.CheckValid() != nil || result.Generation < 1 || result.Generation > 8 || !managedssh.Digest(result.SnapshotDigest) ||
		!managedssh.OwnerReference(result.AttemptRef) || !managedssh.OwnerReference(result.WorkloadInstance) || !managedssh.OwnerReference(result.Fence) {
		return nil, status.Error(codes.Unavailable, sshSetupResultUnavailable)
	}
	preparation := managedssh.IdentityPreparation{IdentityRevisionRef: result.Provision.IdentityRevisionRef, TargetRef: result.Provision.TargetRef, Revision: result.Provision.Revision, Namespace: result.Provision.Namespace, ContentSHA256: result.Provision.ContentSHA256}
	return &cp.ClaimSSHIdentityUploadResponse{SetupRef: result.SetupRef, SnapshotDigest: result.SnapshotDigest, AttemptRef: result.AttemptRef, WorkloadInstance: result.WorkloadInstance, Generation: result.Generation,
		Fence: result.Fence, ExpiresAt: expires, Mode: mode, Preparation: castSSHPreparation(preparation), AuthenticationSha256: result.Provision.AuthenticationSHA256}, nil
}

// CompleteSSHIdentityUpload accepts a closed explicit outcome union under the authenticated broker route; the owner retains late evidence and controls attachment or quarantine atomically.
// CompleteSSHIdentityUpload принимает closed explicit outcome union в authenticated broker route; owner атомарно сохраняет late evidence и управляет attachment либо quarantine.
func (server *Server) CompleteSSHIdentityUpload(ctx context.Context, request *cp.CompleteSSHIdentityUploadRequest) (*cp.CompleteSSHIdentityUploadResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHIdentitySetupService_CompleteSSHIdentityUpload_FullMethodName)
	if err != nil {
		return nil, err
	}
	input, err := castSSHUploadCompletion(request)
	if err != nil {
		return nil, err
	}
	result, err := server.service.CompleteSSHIdentityUpload(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	view, err := castSSHSetupView(result)
	if err != nil {
		return nil, err
	}
	return &cp.CompleteSSHIdentityUploadResponse{Setup: view}, nil
}

// sshSetupMutation preserves exact idempotency bytes and explicit version presence; trimming must not turn a rejected key into another durable receipt coordinate.
// sshSetupMutation сохраняет exact idempotency bytes и explicit version presence; trimming не превращает rejected key в другую durable receipt coordinate.
func sshSetupMutation(input *cp.MutationContext) value.Mutation {
	if input == nil {
		return value.Mutation{}
	}
	return value.Mutation{ExpectedVersion: input.ExpectedVersion, IdempotencyKey: input.GetIdempotencyKey()}
}

// validSSHSetupUploadEndpoint admits a canonical HTTPS origin whose exact TLS host matches the owner-selected destination; URL credentials, query, fragment and alternate paths are forbidden.
// validSSHSetupUploadEndpoint допускает canonical HTTPS origin с exact TLS host выбранного owner destination; credentials, query, fragment и alternate paths запрещены.
func validSSHSetupUploadEndpoint(endpoint entity.SSHIdentityUploadEndpoint) bool {
	return endpoint.Valid()
}

// castSSHSetupView projects only safe known metadata; malformed stored states and incomplete owner coordinates fail closed instead of becoming an apparent usable credential.
// castSSHSetupView проецирует лишь safe known metadata; malformed stored states и incomplete owner coordinates закрыто отклоняются вместо видимости usable credential.
func castSSHSetupView(input entity.SSHIdentitySetupView) (*cp.ManagedSSHIdentitySetupView, error) {
	state, known := cp.ManagedSSHIdentitySetupState_value["MANAGED_SSH_IDENTITY_SETUP_STATE_"+string(input.State)]
	policy, policyKnown := cp.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(input.Policy)]
	expires := timestamppb.New(input.ExpiresAt)
	if !known || state == 0 || !policyKnown || policy == 0 || input.ExpiresAt.IsZero() || !managedssh.OwnerReference(input.Ref) || !managedssh.OwnerReference(input.ConnectionRef) || !managedssh.OwnerReference(input.IdentityRef) ||
		!managedssh.OwnerReference(input.TargetRef) || input.Version < 1 || input.ConnectionVersion < 1 || input.BindingVersion < 0 || input.BindingGeneration < 0 || input.IdentityRevision < 1 || expires.CheckValid() != nil {
		return nil, status.Error(codes.Unavailable, sshSetupResultUnavailable)
	}
	return &cp.ManagedSSHIdentitySetupView{Ref: input.Ref, ConnectionRef: input.ConnectionRef, IdentityRef: input.IdentityRef, TargetRef: input.TargetRef, ReasonCode: input.ReasonCode,
		Version: input.Version, ConnectionVersion: input.ConnectionVersion, BindingVersion: input.BindingVersion, BindingGeneration: input.BindingGeneration, IdentityRevision: input.IdentityRevision,
		Policy: cp.IntegrationCredentialPolicy(policy), State: cp.ManagedSSHIdentitySetupState(state), ExpiresAt: expires}, nil
}

// castSSHPreparation carries private immutable material coordinates only on the protected broker response; purpose is fixed by the distinct SSH protocol.
// castSSHPreparation передаёт private immutable material coordinates только в protected broker response; purpose фиксирован отдельным SSH protocol.
func castSSHPreparation(input managedssh.IdentityPreparation) *cp.ManagedSSHIdentityPreparation {
	return &cp.ManagedSSHIdentityPreparation{IdentityRevisionRef: input.IdentityRevisionRef, TargetRef: input.TargetRef, Revision: input.Revision, Namespace: input.Namespace, ContentSha256: input.ContentSHA256}
}

// castSSHUploadMode refuses an absent or future grant mode rather than treating it as WRITE.
// castSSHUploadMode отклоняет absent либо future grant mode вместо преобразования в WRITE.
func castSSHUploadMode(input enum.SSHIdentityUploadMode) (cp.ManagedSSHIdentityUploadMode, bool) {
	switch input {
	case enum.SSHIdentityUploadWrite:
		return cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_WRITE, true
	case enum.SSHIdentityUploadObserve:
		return cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_OBSERVE, true
	default:
		return cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_UNSPECIFIED, false
	}
}

// castSSHUploadCompletion preserves explicit receipt presence; nil oneof bodies never become an UNKNOWN classification or a fabricated known locator.
// castSSHUploadCompletion сохраняет explicit receipt presence; nil oneof bodies не становятся UNKNOWN classification либо fabricated known locator.
func castSSHUploadCompletion(input *cp.CompleteSSHIdentityUploadRequest) (command.SSHIdentityUploadComplete, error) {
	result := command.SSHIdentityUploadComplete{SetupRef: input.GetSetupRef(), AttemptRef: input.GetAttemptRef(), WorkloadInstance: input.GetWorkloadInstance(), Fence: input.GetFence(), Generation: input.GetGeneration()}
	switch outcome := input.GetOutcome().(type) {
	case *cp.CompleteSSHIdentityUploadRequest_Identity:
		if outcome == nil || outcome.Identity == nil || outcome.Identity.Preparation == nil {
			return result, status.Error(codes.InvalidArgument, sshSetupRequestInvalid)
		}
		x, p := outcome.Identity, outcome.Identity.Preparation
		result.Identity = &managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: p.GetIdentityRevisionRef(), TargetRef: p.GetTargetRef(), Revision: p.GetRevision(), Namespace: p.GetNamespace(), ContentSHA256: p.GetContentSha256(),
			AuthenticationSHA256: x.GetAuthenticationSha256(), SecretName: x.GetSecretName(), SecretUID: x.GetSecretUid(), SecretResourceVersion: x.GetSecretResourceVersion()}
	case *cp.CompleteSSHIdentityUploadRequest_Unknown:
		if outcome == nil || outcome.Unknown == nil {
			return result, status.Error(codes.InvalidArgument, sshSetupRequestInvalid)
		}
		result.Unknown = true
	case *cp.CompleteSSHIdentityUploadRequest_RejectedAcknowledgement:
		if outcome == nil || outcome.RejectedAcknowledgement == nil {
			return result, status.Error(codes.InvalidArgument, sshSetupRequestInvalid)
		}
		x := outcome.RejectedAcknowledgement
		result.Rejected = &command.SSHIdentityRejectedAcknowledgement{ResponseSHA256: x.GetResponseSha256(), Truncated: x.GetTruncated(), ObservedUID: x.GetObservedUid()}
	default:
		return result, status.Error(codes.InvalidArgument, sshSetupRequestInvalid)
	}
	return result, nil
}
