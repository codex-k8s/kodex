package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"strings"
)

// sshHealthRequestInvalid conceals malformed private binding fields and peer diagnostics.
// sshHealthRequestInvalid скрывает malformed private binding fields и peer diagnostics.
const sshHealthRequestInvalid = "SSH identity health request is invalid"

// sshHealthBinding decodes the exact private test oneof; invocation and unknown fields cannot enter health authority.
// sshHealthBinding разбирает exact private test oneof; invocation и unknown fields не входят в health authority.
func sshHealthBinding(input *cp.ManagedSSHHealthBinding) (managedssh.Binding, bool) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return managedssh.Binding{}, false
	}
	b := managedssh.Binding{WorkloadInstance: input.WorkloadInstance, ConnectionTestRef: input.ConnectionTestRef, LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation, InputDigest: input.InputDigest}
	return b, b.Validate() == nil
}

// BeginSSHIdentityHealthProbe forwards only the protected original claim and returns immutable private material coordinates after owner admission.
// BeginSSHIdentityHealthProbe передаёт лишь protected original claim и возвращает immutable private material coordinates после owner admission.
func (server *Server) BeginSSHIdentityHealthProbe(ctx context.Context, request *cp.BeginSSHIdentityHealthProbeRequest) (*cp.BeginSSHIdentityHealthProbeResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHOwnerService_BeginSSHIdentityHealthProbe_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshHealthRequestInvalid)
	}
	binding, valid := sshHealthBinding(request.Binding)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, sshHealthRequestInvalid)
	}
	result, err := server.service.BeginSSHIdentityHealthProbe(ctx, p, binding)
	if err != nil {
		return nil, transportError(err)
	}
	if !managedssh.OwnerReference(result.AttemptRef) || !managedssh.Digest(result.SnapshotDigest) || !result.Identity.Valid() || (result.Mode != managedssh.ManagedTransport && result.Mode != managedssh.ExecuteTransport) || result.ExpiresAt.IsZero() {
		return nil, status.Error(codes.Unavailable, sshHealthRequestInvalid)
	}
	x := result.Identity
	identity := &cp.ManagedSSHIdentityLocator{Preparation: &cp.ManagedSSHIdentityPreparation{IdentityRevisionRef: x.IdentityRevisionRef, TargetRef: x.TargetRef, Revision: x.Revision, Namespace: x.Namespace, ContentSha256: x.ContentSHA256}, AuthenticationSha256: x.AuthenticationSHA256, SecretName: x.SecretName, SecretUid: x.SecretUID, SecretResourceVersion: x.SecretResourceVersion}
	expires := timestamppb.New(result.ExpiresAt)
	if expires.CheckValid() != nil {
		return nil, status.Error(codes.Unavailable, sshHealthRequestInvalid)
	}
	return &cp.BeginSSHIdentityHealthProbeResponse{AttemptRef: result.AttemptRef, SnapshotDigest: result.SnapshotDigest, Identity: identity, TransportMode: string(result.Mode), ExpiresAt: expires}, nil
}

// CompleteSSHIdentityHealthProbe preserves an exact outcome and timestamp; unknown, mixed and partial observations close before the owner transition.
// CompleteSSHIdentityHealthProbe сохраняет exact outcome и timestamp; unknown, mixed и partial observations закрываются до owner transition.
func (server *Server) CompleteSSHIdentityHealthProbe(ctx context.Context, request *cp.CompleteSSHIdentityHealthProbeRequest) (*cp.CompleteSSHIdentityHealthProbeResponse, error) {
	p, err := principal(ctx, cp.ManagedSSHOwnerService_CompleteSSHIdentityHealthProbe_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, sshHealthRequestInvalid)
	}
	binding, valid := sshHealthBinding(request.Binding)
	if !valid {
		return nil, status.Error(codes.InvalidArgument, sshHealthRequestInvalid)
	}
	outcome := strings.TrimPrefix(request.Outcome.String(), "MANAGED_SSH_HEALTH_OUTCOME_")
	input := command.SSHIdentityHealthComplete{Binding: binding, AttemptRef: request.AttemptRef, SnapshotDigest: request.SnapshotDigest, Outcome: entity.PersonalIntegrationCredentialTestOutcome(outcome), ObservedSubjectDigest: request.ObservedSubjectDigest}
	if request.QualifiedUntil != nil {
		if request.QualifiedUntil.CheckValid() != nil || len(request.QualifiedUntil.ProtoReflect().GetUnknown()) != 0 {
			return nil, status.Error(codes.InvalidArgument, sshHealthRequestInvalid)
		}
		input.QualifiedUntil = request.QualifiedUntil.AsTime()
	}
	result, err := server.service.CompleteSSHIdentityHealthProbe(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	if result.TestRef != binding.ConnectionTestRef || (result.State != "SUCCEEDED" && result.State != "FAILED" && result.State != "CANCELLED") || result.Applied == (result.State == "CANCELLED") {
		return nil, status.Error(codes.Unavailable, sshHealthRequestInvalid)
	}
	return &cp.CompleteSSHIdentityHealthProbeResponse{TestRef: result.TestRef, State: result.State, Applied: result.Applied}, nil
}
