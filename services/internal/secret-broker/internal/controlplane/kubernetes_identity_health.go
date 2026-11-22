package controlplane

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// kubernetesHealthBindingToProto retains the gateway's original claim; the broker cannot replace its instance.
// kubernetesHealthBindingToProto сохраняет original gateway claim; broker не заменяет его instance.
func kubernetesHealthBindingToProto(binding managedkubernetes.Binding) *cp.ManagedKubernetesOwnerBinding {
	return &cp.ManagedKubernetesOwnerBinding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef,
		ConnectionTestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence,
		Generation: binding.Generation, InputDigest: binding.InputDigest}
}

// BeginKubernetesIdentityHealth calls the protected one-shot owner admission without retries.
// No raw identity material or caller-selected target enters the control plane request.
// BeginKubernetesIdentityHealth вызывает protected one-shot owner admission без retries.
// Raw identity material и caller-selected target не попадают в control plane request.
func (owner *Owner) BeginKubernetesIdentityHealth(ctx context.Context, binding managedkubernetes.Binding) (targetrepo.IdentityHealthAdmission, error) {
	var result targetrepo.IdentityHealthAdmission
	if owner.client.KubernetesOperations == nil || binding.Validate() != nil || binding.ConnectionTestRef == "" {
		return result, targetrepo.ErrHealthUnavailable
	}
	response, err := owner.client.KubernetesOperations.BeginManagedKubernetesHealthProbe(ctx, &cp.BeginManagedKubernetesHealthProbeRequest{Binding: kubernetesHealthBindingToProto(binding)})
	if err != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetIdentityLocator() == nil ||
		response.GetTarget() == nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil {
		return result, targetrepo.ErrHealthUnavailable
	}
	target := response.GetTarget()
	if len(target.ProtoReflect().GetUnknown()) != 0 || target.GetRevision() < 1 {
		return result, targetrepo.ErrHealthUnavailable
	}
	result = targetrepo.IdentityHealthAdmission{AttemptRef: response.GetAttemptRef(), SnapshotDigest: response.GetSnapshotDigest(),
		Identity: kubernetesSetupLocator(response.GetIdentityLocator()), ExpiresAt: response.GetExpiresAt().AsTime(),
		Target: value.KubernetesTarget{ID: target.GetTargetRef(), Revision: uint64(target.GetRevision()), ClusterUID: target.GetClusterUid(),
			ServerURL: target.GetServerOrigin(), TLSServerName: target.GetTlsServerName(), CertificateAuthorityHash: target.GetCertificateAuthoritySha256(),
			Context: target.GetEmbeddedContext(), AddressPins: append([]string(nil), target.GetAddressIps()...), KubeconfigSourceOrigins: append([]string(nil), target.GetKubeconfigSourceOrigins()...)}}
	return result, nil
}

// CompleteKubernetesIdentityHealth sends only the finite original fact after local work joins.
// An uncertain RPC never authorizes another probe; the immutable owner receipt supplies replay.
// CompleteKubernetesIdentityHealth отправляет лишь finite original fact после local work join.
// Uncertain RPC не разрешает другой probe; immutable owner receipt обеспечивает replay.
func (owner *Owner) CompleteKubernetesIdentityHealth(ctx context.Context, input targetrepo.IdentityHealthCompletion) (targetrepo.IdentityHealthReceipt, error) {
	var result targetrepo.IdentityHealthReceipt
	if owner.client.KubernetesOperations == nil || input.Binding.Validate() != nil || input.Binding.ConnectionTestRef == "" {
		return result, targetrepo.ErrHealthUnavailable
	}
	var outcome cp.ManagedKubernetesHealthOutcome
	switch input.Outcome {
	case "AUTHENTICATED":
		outcome = cp.ManagedKubernetesHealthOutcome_MANAGED_KUBERNETES_HEALTH_OUTCOME_AUTHENTICATED
	case "AUTH_REJECTED":
		outcome = cp.ManagedKubernetesHealthOutcome_MANAGED_KUBERNETES_HEALTH_OUTCOME_AUTHENTICATION_REJECTED
	case "UNAVAILABLE":
		outcome = cp.ManagedKubernetesHealthOutcome_MANAGED_KUBERNETES_HEALTH_OUTCOME_UNAVAILABLE
	default:
		return result, targetrepo.ErrHealthUnavailable
	}
	response, err := owner.client.KubernetesOperations.CompleteManagedKubernetesHealthProbe(ctx, &cp.CompleteManagedKubernetesHealthProbeRequest{
		Binding: kubernetesHealthBindingToProto(input.Binding), AttemptRef: input.Admission.AttemptRef,
		SnapshotDigest: input.Admission.SnapshotDigest, Outcome: outcome, ObservedSubjectDigest: input.SubjectDigest})
	if err != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, targetrepo.ErrHealthUnavailable
	}
	if response.GetTestRef() != input.Binding.ConnectionTestRef || (response.GetState() != "SUCCEEDED" && response.GetState() != "FAILED" && response.GetState() != "CANCELLED") {
		return result, targetrepo.ErrHealthUnavailable
	}
	return targetrepo.IdentityHealthReceipt{TestRef: response.GetTestRef(), State: response.GetState(), Applied: response.GetApplied()}, nil
}
