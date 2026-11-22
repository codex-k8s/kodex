package controlplane

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sshrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

// sshHealthOwnerBinding preserves the original gateway lease and full snapshot digest; the broker never substitutes its own claimant instance.
// sshHealthOwnerBinding сохраняет original gateway lease и full snapshot digest; broker не подставляет свой claimant instance.
func sshHealthOwnerBinding(b managedssh.Binding) *cp.ManagedSSHHealthBinding {
	return &cp.ManagedSSHHealthBinding{WorkloadInstance: b.WorkloadInstance, ConnectionTestRef: b.ConnectionTestRef, LeaseRef: b.LeaseRef, Fence: b.Fence, Generation: b.Generation, InputDigest: b.InputDigest}
}

// BeginSSHIdentityHealthProbe obtains private owner-selected material once over the registered protected route, rejecting every unknown or partial acknowledgement before a key read.
// BeginSSHIdentityHealthProbe получает private owner-selected material однократно через registered protected route и отвергает unknown либо partial acknowledgement до key read.
func (owner *Owner) BeginSSHIdentityHealthProbe(ctx context.Context, binding managedssh.Binding) (managedssh.HealthAdmission, error) {
	var result managedssh.HealthAdmission
	if owner == nil || owner.client == nil || owner.client.SSHOperations == nil || binding.Validate() != nil || binding.ConnectionTestRef == "" {
		return result, sshrepo.ErrIdentityUnavailable
	}
	response, err := owner.client.SSHOperations.BeginSSHIdentityHealthProbe(ctx, &cp.BeginSSHIdentityHealthProbeRequest{Binding: sshHealthOwnerBinding(binding)})
	if err != nil || ctx.Err() != nil || !knownSSHOwnerProto(response, response.GetIdentity(), response.GetIdentity().GetPreparation(), response.GetExpiresAt()) || response.ExpiresAt.CheckValid() != nil {
		return result, sshrepo.ErrIdentityUnavailable
	}
	x := response.Identity
	prep := x.Preparation
	result = managedssh.HealthAdmission{AttemptRef: response.AttemptRef, SnapshotDigest: response.SnapshotDigest, Mode: managedssh.TransportMode(response.TransportMode), ExpiresAt: response.ExpiresAt.AsTime(), Identity: managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: prep.IdentityRevisionRef, TargetRef: prep.TargetRef, Revision: prep.Revision, Namespace: prep.Namespace, ContentSHA256: prep.ContentSha256, AuthenticationSHA256: x.AuthenticationSha256, SecretName: x.SecretName, SecretUID: x.SecretUid, SecretResourceVersion: x.SecretResourceVersion}}
	if !managedssh.OwnerReference(result.AttemptRef) || !managedssh.Digest(result.SnapshotDigest) || !result.Identity.Valid() || (result.Mode != managedssh.ManagedTransport && result.Mode != managedssh.ExecuteTransport) || !result.ExpiresAt.After(time.Now()) || result.ExpiresAt.After(time.Now().Add(30*time.Second)) {
		return managedssh.HealthAdmission{}, sshrepo.ErrIdentityUnavailable
	}
	return result, nil
}

// CompleteSSHIdentityHealthProbe submits one exact joined observation or unavailable fact; it never retries a lost owner acknowledgement or reports credential rejection from an ambiguous SSH error.
// CompleteSSHIdentityHealthProbe передаёт exact joined observation либо unavailable fact; lost owner acknowledgement не повторяется, ambiguous SSH error не объявляется credential rejection.
func (owner *Owner) CompleteSSHIdentityHealthProbe(ctx context.Context, binding managedssh.Binding, admission managedssh.HealthAdmission, observation managedssh.HealthObservation) (managedssh.HealthReceipt, error) {
	if owner == nil || owner.client == nil || owner.client.SSHOperations == nil || binding.Validate() != nil || binding.ConnectionTestRef == "" || !managedssh.OwnerReference(admission.AttemptRef) || !managedssh.Digest(admission.SnapshotDigest) {
		return managedssh.HealthReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	request := &cp.CompleteSSHIdentityHealthProbeRequest{Binding: sshHealthOwnerBinding(binding), AttemptRef: admission.AttemptRef, SnapshotDigest: admission.SnapshotDigest, Outcome: cp.ManagedSSHHealthOutcome_MANAGED_SSH_HEALTH_OUTCOME_UNAVAILABLE}
	if observation.ObservedSubjectDigest != "" || !observation.QualifiedUntil.IsZero() {
		if !managedssh.Digest(observation.ObservedSubjectDigest) || observation.QualifiedUntil.IsZero() || observation.QualifiedUntil.Nanosecond() != 0 {
			return managedssh.HealthReceipt{}, sshrepo.ErrIdentityRejected
		}
		request.Outcome = cp.ManagedSSHHealthOutcome_MANAGED_SSH_HEALTH_OUTCOME_AUTHENTICATED
		request.ObservedSubjectDigest = observation.ObservedSubjectDigest
		request.QualifiedUntil = timestamppb.New(observation.QualifiedUntil)
		if request.QualifiedUntil.CheckValid() != nil {
			return managedssh.HealthReceipt{}, sshrepo.ErrIdentityRejected
		}
	}
	response, err := owner.client.SSHOperations.CompleteSSHIdentityHealthProbe(ctx, request)
	if err != nil || ctx.Err() != nil || !knownSSHOwnerProto(response) || response.TestRef != binding.ConnectionTestRef || (response.State != "SUCCEEDED" && response.State != "FAILED" && response.State != "CANCELLED") || response.Applied == (response.State == "CANCELLED") || response.State == "SUCCEEDED" && observation.ObservedSubjectDigest == "" {
		return managedssh.HealthReceipt{}, sshrepo.ErrIdentityUnavailable
	}
	return managedssh.HealthReceipt{TestRef: response.TestRef, State: response.State, Applied: response.Applied}, nil
}
