package controlplane

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ResolveSSHIdentityUpload uses the existing protected connection for fresh purpose-specific owner state; neither private key nor raw upload permit crosses this RPC.
// ResolveSSHIdentityUpload использует existing protected connection для fresh purpose-specific owner state; private key и raw upload permit не пересекают этот RPC.
func (owner *Owner) ResolveSSHIdentityUpload(ctx context.Context, setupRef, permitSHA256 string) (value.SSHIdentityUploadSnapshot, error) {
	if owner == nil || owner.client == nil || owner.client.SSHIdentities == nil || !managedssh.OwnerReference(setupRef) || !managedssh.Digest(permitSHA256) {
		return value.SSHIdentityUploadSnapshot{}, sshrepo.ErrIdentityUnavailable
	}
	response, err := owner.client.SSHIdentities.ResolveSSHIdentityUpload(ctx, &cp.ResolveSSHIdentityUploadRequest{SetupRef: setupRef, PermitSha256: permitSHA256})
	if err != nil || !knownSSHOwnerProto(response, response.GetPreparation(), response.GetExpiresAt()) || response.GetExpiresAt().CheckValid() != nil {
		return value.SSHIdentityUploadSnapshot{}, sshrepo.ErrIdentityUnavailable
	}
	mode, ok := sshOwnerUploadMode(response.GetMode())
	if !ok {
		return value.SSHIdentityUploadSnapshot{}, sshrepo.ErrIdentityUnavailable
	}
	result := value.SSHIdentityUploadSnapshot{SetupRef: response.GetSetupRef(), Digest: response.GetSnapshotDigest(), Mode: mode, Preparation: sshOwnerPreparation(response.GetPreparation()),
		ContentBytes: response.GetContentBytes(), AuthenticationSHA256: response.GetAuthenticationSha256(), ExpiresAt: response.GetExpiresAt().AsTime()}
	if result.SetupRef != setupRef || !result.Valid(time.Now()) {
		return value.SSHIdentityUploadSnapshot{}, sshrepo.ErrIdentityUnavailable
	}
	return result, nil
}

// ClaimSSHIdentityUpload forwards only the original snapshot and broker-derived key metadata, without retrying a lost owner acknowledgement or requesting a caller-selected mode.
// ClaimSSHIdentityUpload передаёт лишь original snapshot и broker-derived key metadata без retry потерянного owner acknowledgement либо caller-selected mode.
func (owner *Owner) ClaimSSHIdentityUpload(ctx context.Context, input sshrepo.IdentityUploadClaim) (value.SSHIdentityUploadAdmission, error) {
	if owner == nil || owner.client == nil || owner.client.SSHIdentities == nil || !managedssh.OwnerReference(input.SetupRef) || !managedssh.OwnerReference(input.WorkloadInstance) ||
		!managedssh.Digest(input.PermitSHA256) || !managedssh.Digest(input.SnapshotDigest) || !managedssh.Digest(input.Key.ContentSHA256) || !managedssh.Digest(input.Key.AuthenticationSHA256) {
		return value.SSHIdentityUploadAdmission{}, sshrepo.ErrIdentityUnavailable
	}
	response, err := owner.client.SSHIdentities.ClaimSSHIdentityUpload(ctx, &cp.ClaimSSHIdentityUploadRequest{SetupRef: input.SetupRef, PermitSha256: input.PermitSHA256, SnapshotDigest: input.SnapshotDigest,
		WorkloadInstance: input.WorkloadInstance, ContentSha256: input.Key.ContentSHA256, AuthenticationSha256: input.Key.AuthenticationSHA256})
	if err != nil || !knownSSHOwnerProto(response, response.GetPreparation(), response.GetExpiresAt()) || response.GetExpiresAt().CheckValid() != nil {
		return value.SSHIdentityUploadAdmission{}, sshrepo.ErrIdentityUnavailable
	}
	mode, ok := sshOwnerUploadMode(response.GetMode())
	preparation := sshOwnerPreparation(response.GetPreparation())
	provision, valid := preparation.Provision(input.Key)
	if !ok || !valid || response.GetAuthenticationSha256() != input.Key.AuthenticationSHA256 || response.GetSetupRef() != input.SetupRef || response.GetSnapshotDigest() != input.SnapshotDigest || response.GetWorkloadInstance() != input.WorkloadInstance ||
		!managedssh.OwnerReference(response.GetAttemptRef()) || !managedssh.OwnerReference(response.GetFence()) || response.GetGeneration() < 1 || response.GetGeneration() > 8 || !response.GetExpiresAt().AsTime().After(time.Now()) || response.GetExpiresAt().AsTime().After(time.Now().Add(30*time.Second)) {
		return value.SSHIdentityUploadAdmission{}, sshrepo.ErrIdentityUnavailable
	}
	return value.SSHIdentityUploadAdmission{SetupRef: response.GetSetupRef(), SnapshotDigest: response.GetSnapshotDigest(), AttemptRef: response.GetAttemptRef(), WorkloadInstance: response.GetWorkloadInstance(),
		Fence: response.GetFence(), Generation: response.GetGeneration(), Mode: mode, Provision: provision, ExpiresAt: response.GetExpiresAt().AsTime()}, nil
}

// CompleteSSHIdentityUpload reports exactly one joined storage outcome under its original fence; failure never retries the material write or transforms missing completion into observation credit.
// CompleteSSHIdentityUpload сообщает ровно один joined storage outcome под original fence; отказ не повторяет material write и не превращает missing completion в observation credit.
func (owner *Owner) CompleteSSHIdentityUpload(ctx context.Context, input sshrepo.IdentityUploadCompletion) error {
	if owner == nil || owner.client == nil || owner.client.SSHIdentities == nil || !input.Valid() || !managedssh.OwnerReference(input.Admission.SetupRef) || !managedssh.OwnerReference(input.Admission.AttemptRef) ||
		!managedssh.OwnerReference(input.Admission.WorkloadInstance) || !managedssh.OwnerReference(input.Admission.Fence) || input.Admission.Generation < 1 || input.Admission.Generation > 8 {
		return sshrepo.ErrIdentityUnavailable
	}
	request := &cp.CompleteSSHIdentityUploadRequest{SetupRef: input.Admission.SetupRef, AttemptRef: input.Admission.AttemptRef, WorkloadInstance: input.Admission.WorkloadInstance, Fence: input.Admission.Fence, Generation: input.Admission.Generation}
	switch {
	case input.Identity != nil:
		x := input.Identity
		request.Outcome = &cp.CompleteSSHIdentityUploadRequest_Identity{Identity: &cp.ManagedSSHIdentityLocator{Preparation: &cp.ManagedSSHIdentityPreparation{IdentityRevisionRef: x.IdentityRevisionRef, TargetRef: x.TargetRef, Revision: x.Revision, Namespace: x.Namespace, ContentSha256: x.ContentSHA256},
			AuthenticationSha256: x.AuthenticationSHA256, SecretName: x.SecretName, SecretUid: x.SecretUID, SecretResourceVersion: x.SecretResourceVersion}}
	case input.Unknown:
		request.Outcome = &cp.CompleteSSHIdentityUploadRequest_Unknown{Unknown: &cp.ManagedSSHIdentityUnknownOutcome{}}
	case input.Rejected != nil:
		request.Outcome = &cp.CompleteSSHIdentityUploadRequest_RejectedAcknowledgement{RejectedAcknowledgement: &cp.ManagedSSHIdentityRejectedAcknowledgement{
			ResponseSha256: input.Rejected.ResponseSHA256, Truncated: input.Rejected.Truncated, ObservedUid: input.Rejected.ObservedUID}}
	}
	response, err := owner.client.SSHIdentities.CompleteSSHIdentityUpload(ctx, request)
	if err != nil || !knownSSHOwnerProto(response, response.GetSetup(), response.GetSetup().GetExpiresAt()) || response.GetSetup().GetExpiresAt().CheckValid() != nil || response.GetSetup().GetRef() != input.Admission.SetupRef {
		return sshrepo.ErrIdentityUnavailable
	}
	state := response.GetSetup().GetState()
	if _, ok := cp.ManagedSSHIdentitySetupState_name[int32(state)]; !ok || state == cp.ManagedSSHIdentitySetupState_MANAGED_SSH_IDENTITY_SETUP_STATE_UNSPECIFIED {
		return sshrepo.ErrIdentityUnavailable
	}
	return nil
}

// knownSSHOwnerProto rejects absent or unknown-field owner messages before nested data is used; every message in these finite responses is supplied explicitly by its consumer.
// knownSSHOwnerProto отклоняет absent либо unknown-field owner messages до использования nested data; каждый message этих конечных responses явно передаётся consumer.
func knownSSHOwnerProto(messages ...proto.Message) bool {
	for _, message := range messages {
		if message == nil || !message.ProtoReflect().IsValid() || len(message.ProtoReflect().GetUnknown()) != 0 {
			return false
		}
	}
	return true
}

// sshOwnerPreparation copies private immutable coordinates without a generic TOKEN descriptor or caller-selected purpose.
// sshOwnerPreparation копирует private immutable coordinates без generic TOKEN descriptor либо caller-selected purpose.
func sshOwnerPreparation(input *cp.ManagedSSHIdentityPreparation) managedssh.IdentityPreparation {
	return managedssh.IdentityPreparation{IdentityRevisionRef: input.GetIdentityRevisionRef(), TargetRef: input.GetTargetRef(), Revision: input.GetRevision(), Namespace: input.GetNamespace(), ContentSHA256: input.GetContentSha256()}
}

// sshOwnerUploadMode refuses absent and future protocol branches instead of granting WRITE by default.
// sshOwnerUploadMode отклоняет absent и future protocol branches вместо выдачи WRITE по умолчанию.
func sshOwnerUploadMode(input cp.ManagedSSHIdentityUploadMode) (value.SSHIdentityUploadMode, bool) {
	switch input {
	case cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_WRITE:
		return value.SSHIdentityUploadWrite, true
	case cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_OBSERVE:
		return value.SSHIdentityUploadObserve, true
	default:
		return "", false
	}
}

// CheckSSHIdentitySetupReadiness probes the same protected Resolve RPC with an unmatchable permit digest; only its authoritative NotFound proves method admission and owner SQL availability, never material custody or SSH reachability.
// CheckSSHIdentitySetupReadiness проверяет тот же protected Resolve RPC с unmatchable permit digest; лишь authoritative NotFound доказывает method admission и owner SQL availability, не material custody либо SSH reachability.
func (owner *Owner) CheckSSHIdentitySetupReadiness(ctx context.Context) error {
	if owner == nil || owner.client == nil || owner.client.SSHIdentities == nil || ctx == nil || ctx.Err() != nil {
		return sshrepo.ErrIdentityUnavailable
	}
	_, err := owner.client.SSHIdentities.ResolveSSHIdentityUpload(ctx, &cp.ResolveSSHIdentityUploadRequest{SetupRef: "ssu_00000000000000000000000000000000", PermitSha256: "0000000000000000000000000000000000000000000000000000000000000000"})
	if ctx.Err() != nil || status.Code(err) != codes.NotFound {
		return sshrepo.ErrIdentityUnavailable
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return sshrepo.ErrIdentityUnavailable
	}
	return nil
}
