package controlplane

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ResolveKubernetesIdentityUpload uses the protected setup route and supplies no caller identity selector.
// Only the permit digest reaches CP; raw material and permit never enter this adapter.
// ResolveKubernetesIdentityUpload использует protected setup route без caller identity selector.
// CP получает лишь permit digest; raw material и permit не входят в adapter.
func (owner *Owner) ResolveKubernetesIdentityUpload(ctx context.Context, ref, permitDigest string) (value.KubernetesIdentityUploadSnapshot, error) {
	var result value.KubernetesIdentityUploadSnapshot
	if owner.client.KubernetesIdentities == nil {
		return result, targetrepo.ErrSetupUnavailable
	}
	reply, err := owner.client.KubernetesIdentities.ResolveManagedKubernetesIdentityUpload(ctx, &cp.ResolveManagedKubernetesIdentityUploadRequest{SetupRef: ref, PermitSha256: permitDigest})
	if err != nil || reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || reply.GetSnapshot() == nil {
		return result, targetrepo.ErrSetupUnavailable
	}
	response := reply.GetSnapshot()
	if len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, targetrepo.ErrSetupUnavailable
	}
	result.SetupRef, result.Digest, result.ContentBytes, result.ReconcileOnly = response.GetSetupRef(), response.GetSnapshotDigest(), response.GetContentBytes(), response.GetReconcileOnly()
	if response.GetCompleted() != nil {
		view, valid := kubernetesSetupOwnerView(response.GetCompleted())
		if !valid || response.GetProvision() != nil || response.GetKnownIdentity() != nil || response.GetTarget() != nil {
			return result, targetrepo.ErrSetupUnavailable
		}
		result.Completed = &view
		return result, nil
	}
	if response.GetProvision() == nil || response.GetTarget() == nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil {
		return result, targetrepo.ErrSetupUnavailable
	}
	result.ExpiresAt = response.GetExpiresAt().AsTime()
	result.Provision = kubernetesSetupProvision(response.GetProvision())
	target := response.GetTarget()
	if target.GetRevision() < 1 || len(target.ProtoReflect().GetUnknown()) != 0 {
		return result, targetrepo.ErrSetupUnavailable
	}
	result.Target = value.KubernetesTarget{ID: target.GetTargetRef(), Revision: uint64(target.GetRevision()), ClusterUID: target.GetClusterUid(), ServerURL: target.GetServerOrigin(), TLSServerName: target.GetTlsServerName(),
		CertificateAuthorityHash: target.GetCertificateAuthoritySha256(), Context: target.GetEmbeddedContext(), AddressPins: append([]string(nil), target.GetAddressIps()...), KubeconfigSourceOrigins: append([]string(nil), target.GetKubeconfigSourceOrigins()...)}
	if response.GetKnownIdentity() != nil {
		identity := kubernetesSetupLocator(response.GetKnownIdentity())
		result.KnownIdentity = &identity
	}
	return result, nil
}

// ClaimKubernetesIdentityUpload requests only the fixed local claimant under exact immutable pins.
// The owner decides WRITE versus OBSERVE; the adapter cannot select or retry a write grant.
// ClaimKubernetesIdentityUpload запрашивает лишь fixed local claimant под exact immutable pins.
// Owner выбирает WRITE либо OBSERVE; adapter не выбирает и не повторяет write grant.
func (owner *Owner) ClaimKubernetesIdentityUpload(ctx context.Context, input targetrepo.IdentityUploadClaim) (value.KubernetesIdentityUploadAdmission, error) {
	var result value.KubernetesIdentityUploadAdmission
	if owner.client.KubernetesIdentities == nil || input.WorkloadInstance != owner.claimantID {
		return result, targetrepo.ErrSetupRejected
	}
	reply, err := owner.client.KubernetesIdentities.ClaimManagedKubernetesIdentityUpload(ctx, &cp.ClaimManagedKubernetesIdentityUploadRequest{SetupRef: input.SetupRef, PermitSha256: input.PermitSHA256,
		SnapshotDigest: input.SnapshotDigest, ContentSha256: input.ContentSHA256, WorkloadInstance: owner.claimantID})
	if err != nil || reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || reply.GetAdmission() == nil {
		return result, targetrepo.ErrSetupUnavailable
	}
	response := reply.GetAdmission()
	if response.GetProvision() == nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, targetrepo.ErrSetupUnavailable
	}
	result = value.KubernetesIdentityUploadAdmission{SetupRef: response.GetSetupRef(), AttemptRef: response.GetAttemptRef(), Generation: response.GetGeneration(), Fence: response.GetFence(),
		ExpiresAt: response.GetExpiresAt().AsTime(), ReconcileOnly: response.GetReconcileOnly(), Provision: kubernetesSetupProvision(response.GetProvision())}
	if response.GetKnownIdentity() != nil {
		identity := kubernetesSetupLocator(response.GetKnownIdentity())
		result.KnownIdentity = &identity
	}
	return result, nil
}

// CompleteKubernetesIdentityUpload sends one typed fact without automatic completion retries.
// Exact attempt fence, generation and workload remain the immutable original owner admission.
// CompleteKubernetesIdentityUpload передаёт один typed fact без automatic completion retries.
// Exact attempt fence, generation и workload остаются исходным immutable owner admission.
func (owner *Owner) CompleteKubernetesIdentityUpload(ctx context.Context, input targetrepo.IdentityUploadCompletion) (value.KubernetesIdentitySetupView, error) {
	var result value.KubernetesIdentitySetupView
	count := 0
	if input.Unknown {
		count++
	}
	if input.Identity != nil {
		count++
	}
	if input.Rejected != nil {
		count++
	}
	if owner.client.KubernetesIdentities == nil || input.Instance != owner.claimantID || count != 1 {
		return result, targetrepo.ErrSetupRejected
	}
	grant := input.Admission
	request := &cp.CompleteManagedKubernetesIdentityUploadRequest{SetupRef: grant.SetupRef, AttemptRef: grant.AttemptRef, Generation: grant.Generation, Fence: grant.Fence, WorkloadInstance: owner.claimantID}
	switch {
	case input.Identity != nil:
		pin := input.Identity
		if pin.Validate(grant.Provision.Namespace) != nil || pin.IdentityRef != grant.Provision.IdentityRef || pin.Revision != grant.Provision.Revision || pin.TargetRef != grant.Provision.TargetRef || pin.ContentSHA256 != grant.Provision.ContentSHA256 {
			return result, targetrepo.ErrSetupRejected
		}
		request.Outcome = &cp.CompleteManagedKubernetesIdentityUploadRequest_IdentityLocator{IdentityLocator: &cp.ManagedKubernetesIdentityLocator{IdentityRef: pin.IdentityRef, TargetRef: pin.TargetRef, Revision: pin.Revision,
			Namespace: pin.Namespace, SecretName: pin.Name, SecretUid: pin.UID, SecretResourceVersion: pin.ResourceVersion, ContentSha256: pin.ContentSHA256}}
	case input.Rejected != nil:
		if !input.Rejected.Valid() {
			return result, targetrepo.ErrSetupRejected
		}
		request.Outcome = &cp.CompleteManagedKubernetesIdentityUploadRequest_RejectedAcknowledgement{RejectedAcknowledgement: &cp.ManagedKubernetesIdentityAcknowledgementRejection{
			ResponseSha256: input.Rejected.ResponseSHA256, Truncated: input.Rejected.Truncated, ObservedUid: input.Rejected.ObservedUID}}
	default:
		request.Outcome = &cp.CompleteManagedKubernetesIdentityUploadRequest_Unknown{Unknown: &cp.ManagedKubernetesIdentityUploadUnknown{}}
	}
	reply, err := owner.client.KubernetesIdentities.CompleteManagedKubernetesIdentityUpload(ctx, request)
	if err != nil || reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || reply.GetSetup() == nil {
		return result, targetrepo.ErrSetupUnavailable
	}
	response := reply.GetSetup()
	result, valid := kubernetesSetupOwnerView(response)
	if !valid {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupUnavailable
	}
	return result, nil
}

// kubernetesSetupOwnerView maps only finite safe owner fields without private witness or descriptors.
// It preserves historical expiry and refuses unknown policy/state/reason rather than inferring readiness.
// kubernetesSetupOwnerView отображает лишь finite safe owner fields без private witness либо descriptors.
// Сохраняет historical expiry и отклоняет unknown policy/state/reason без inference readiness.
func kubernetesSetupOwnerView(input *cp.ManagedKubernetesIdentitySetupView) (value.KubernetesIdentitySetupView, bool) {
	var result value.KubernetesIdentitySetupView
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.GetExpiresAt() == nil || input.GetExpiresAt().CheckValid() != nil {
		return result, false
	}
	result = value.KubernetesIdentitySetupView{SetupRef: input.GetSetupRef(), ConnectionRef: input.GetConnectionRef(), ConnectionVersion: input.GetConnectionVersion(),
		BindingVersion: input.GetBindingVersion(), BindingGeneration: input.GetBindingGeneration(), IdentityRef: input.GetIdentityRef(), IdentityRevision: input.GetIdentityRevision(), TargetRef: input.GetTargetRef(),
		CanonicalContext: input.GetCanonicalContext(), State: strings.TrimPrefix(input.GetState().String(), "MANAGED_KUBERNETES_IDENTITY_SETUP_STATE_"), Policy: strings.TrimPrefix(input.GetCredentialPolicy().String(), "INTEGRATION_CREDENTIAL_POLICY_"),
		ReasonCode: input.GetReasonCode(), Version: input.GetVersion(), ExpiresAt: input.GetExpiresAt().AsTime()}
	return result, result.Valid()
}

// kubernetesSetupProvision copies only exact owner materialization pins without supplying defaults.
// kubernetesSetupProvision копирует лишь exact owner materialization pins без defaults.
func kubernetesSetupProvision(input *cp.ManagedKubernetesIdentityProvision) value.KubernetesIdentityProvision {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return value.KubernetesIdentityProvision{}
	}
	return value.KubernetesIdentityProvision{IdentityRef: input.GetIdentityRef(), TargetRef: input.GetTargetRef(), Namespace: input.GetNamespace(), Revision: input.GetRevision(), ContentSHA256: input.GetContentSha256()}
}

// kubernetesSetupLocator copies complete private purpose pins without generic Secret fallback.
// kubernetesSetupLocator копирует полные private purpose pins без generic Secret fallback.
func kubernetesSetupLocator(input *cp.ManagedKubernetesIdentityLocator) value.KubernetesIdentityLocator {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 {
		return value.KubernetesIdentityLocator{}
	}
	return value.KubernetesIdentityLocator{IdentityRef: input.GetIdentityRef(), TargetRef: input.GetTargetRef(), Revision: input.GetRevision(), Namespace: input.GetNamespace(), Name: input.GetSecretName(), UID: input.GetSecretUid(), ResourceVersion: input.GetSecretResourceVersion(), ContentSHA256: input.GetContentSha256()}
}
