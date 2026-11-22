package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ResolveKubernetesOperation calls the protected owner read with the original claim and no retry.
// Unknown nested fields, mixed address classes, weakened approval and expired metadata are refused.
// Native credential loading still validates exact route/CA/address pins before any network access.
// ResolveKubernetesOperation вызывает protected owner read с original claim без retry.
// Unknown nested fields, mixed address classes, weakened approval и expired metadata отклоняются.
// Native credential loading проверяет exact route/CA/address pins до любого network access.
func (owner *Owner) ResolveKubernetesOperation(ctx context.Context, binding managedkubernetes.Binding) (value.KubernetesOperationSnapshot, error) {
	return owner.resolveKubernetesOperation(ctx, binding, "")
}

// ResolveKubernetesStream requires the exact current stream, not merely a still-live outer lease.
// The owner checks stream/effect/child/plan lineage and clamps expiry to the stream hard deadline.
// ResolveKubernetesStream требует exact current stream, а не только живой outer lease.
// Owner проверяет stream/effect/child/plan lineage и ограничивает expiry stream hard deadline.
func (owner *Owner) ResolveKubernetesStream(ctx context.Context, binding managedkubernetes.Binding, streamRef string) (value.KubernetesOperationSnapshot, error) {
	if len(streamRef) > 96 || !managedkubernetes.ValidLocator(streamRef) {
		return value.KubernetesOperationSnapshot{}, targetrepo.ErrOperationUnavailable
	}
	return owner.resolveKubernetesOperation(ctx, binding, streamRef)
}

// resolveKubernetesOperation shares strict snapshot parsing between base reads and exact stream reads.
// It preserves the optional stream selector and never retries a rejected read as a base operation.
// resolveKubernetesOperation объединяет strict snapshot parsing для base reads и exact stream reads.
// Сохраняет optional stream selector и не повторяет rejected read как base operation.
func (owner *Owner) resolveKubernetesOperation(ctx context.Context, binding managedkubernetes.Binding, streamRef string) (value.KubernetesOperationSnapshot, error) {
	var empty value.KubernetesOperationSnapshot
	if ctx == nil || ctx.Err() != nil || owner == nil || owner.client == nil || owner.client.KubernetesOperations == nil || binding.Validate() != nil || binding.InvocationRef == "" {
		return empty, targetrepo.ErrOperationUnavailable
	}
	response, err := owner.client.KubernetesOperations.ResolveManagedKubernetesOperation(ctx, &cp.ResolveManagedKubernetesOperationRequest{Binding: kubernetesHealthBindingToProto(binding), StreamRef: streamRef})
	if err != nil || ctx.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.GetCleanupReceipt() != nil || response.GetExpiresAt() == nil || response.GetExpiresAt().CheckValid() != nil || len(response.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	now := time.Now()
	registered, known := integrationpackage.KubernetesOperationByKey(response.GetOperation())
	input, canonical, inputErr := managedkubernetes.DecodeOperationInput(response.GetOperation(), response.GetNormalizedInputJson())
	if !known || inputErr != nil || !bytes.Equal(canonical, response.GetNormalizedInputJson()) || response.GetRisk() != string(registered.Risk) ||
		registered.Approval == integrationpackage.ApprovalHumanEachEffect && response.GetApprovalPolicy() != string(registered.Approval) ||
		managedkubernetes.ValidateInvocationApprovalPolicy(input, response.GetRisk(), response.GetApprovalPolicy()) != nil ||
		!managedkubernetes.ValidDigest(response.GetSnapshotDigest()) || !managedkubernetes.ValidDigest(response.GetSubjectDigest()) || response.GetIdentityGeneration() < 1 || !response.GetExpiresAt().AsTime().After(now) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	target, valid := kubernetesOperationTarget(response.GetTarget())
	identity := kubernetesSetupLocator(response.GetIdentityLocator())
	if !valid || identity.Validate(identity.Namespace) != nil || identity.IdentityRef != response.GetIdentityRef() || identity.TargetRef != target.ID {
		return empty, targetrepo.ErrOperationUnavailable
	}
	result := value.KubernetesOperationSnapshot{Digest: response.GetSnapshotDigest(), Operation: response.GetOperation(), Risk: response.GetRisk(), ApprovalPolicy: response.GetApprovalPolicy(), IdentityRef: response.GetIdentityRef(), IdentityGeneration: response.GetIdentityGeneration(), SubjectDigest: response.GetSubjectDigest(), Identity: identity, Target: target, Input: input, NormalizedInput: canonical, ExpiresAt: response.GetExpiresAt().AsTime(), ReconcileOnly: response.GetReconcileOnly(), PlanRef: response.GetPlanRef(), PlanRevision: response.GetPlanRevision(), PlanDigest: response.GetPlanDigest(), ProtectedNamespaces: slices.Clone(response.GetProtectedNamespaces())}
	if result.PlanRef == "" {
		if result.PlanRevision != 0 || result.PlanDigest != "" || result.ReconcileOnly || streamRef != "" {
			return empty, targetrepo.ErrOperationUnavailable
		}
	} else if !managedkubernetes.ValidLocator(result.PlanRef) || result.PlanRevision < 1 || !managedkubernetes.ValidDigest(result.PlanDigest) {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if len(result.ProtectedNamespaces) > 1024 {
		return empty, targetrepo.ErrOperationUnavailable
	}
	for i, namespace := range result.ProtectedNamespaces {
		if len(validation.IsDNS1123Label(namespace)) != 0 || slices.Contains(result.ProtectedNamespaces[:i], namespace) {
			return empty, targetrepo.ErrOperationUnavailable
		}
	}
	if response.GetResource() != nil {
		r := response.GetResource()
		if len(r.ProtoReflect().GetUnknown()) != 0 || response.GetDiscovery() != nil || registered.Execution == integrationpackage.KubernetesOffline {
			return empty, targetrepo.ErrOperationUnavailable
		}
		address := managedkubernetes.ResourceAddress{Group: r.GetGroup(), Version: r.GetVersion(), Resource: r.GetResource(), Namespace: r.GetNamespace(), Name: r.GetName(), Subresource: r.GetSubresource(), UID: r.GetUid(), ResourceVersion: r.GetResourceVersion(), Namespaced: r.GetNamespaced(), AllNamespaces: r.GetAllNamespaces()}
		allNamespaces := result.Operation == "kubernetes.list.all-namespaces" || result.Operation == "kubernetes.watch.all-namespaces"
		if address.Validate() != nil || address.AllNamespaces != allNamespaces || address.Group != input.Group || address.Version != input.Version || address.Resource != input.Resource || address.Namespace != input.Namespace || address.Name != input.Name || address.UID != input.UID || address.ResourceVersion != input.ResourceVersion || address.Subresource != input.Subresource {
			return empty, targetrepo.ErrOperationUnavailable
		}
		result.Resource = &address
	} else if response.GetDiscovery() != nil {
		d := response.GetDiscovery()
		if len(d.ProtoReflect().GetUnknown()) != 0 || registered.Execution != integrationpackage.KubernetesPlan {
			return empty, targetrepo.ErrOperationUnavailable
		}
		address := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryKind(strings.TrimPrefix(d.GetKind().String(), "MANAGED_KUBERNETES_DISCOVERY_KIND_")), Group: d.GetGroup(), Version: d.GetVersion(), ImmutableSchemaHash: d.GetImmutableSchemaHash()}
		if _, err := address.RelativeURL(); err != nil {
			return empty, targetrepo.ErrOperationUnavailable
		}
		result.Discovery = &address
	} else if registered.Execution != integrationpackage.KubernetesPlan && registered.Execution != integrationpackage.KubernetesOffline {
		return empty, targetrepo.ErrOperationUnavailable
	}
	if response.GetNodeTransport() != nil {
		node, valid := kubernetesOperationNode(response.GetNodeTransport(), target, now)
		if !valid {
			return empty, targetrepo.ErrOperationUnavailable
		}
		result.NodeTransport = &node
	}
	return result, nil
}

// kubernetesOperationTarget preserves the owner route and decodes the single pinned policy source.
// It copies mutable slices so a client buffer cannot alter an admitted snapshot after validation.
// kubernetesOperationTarget сохраняет owner route и декодирует единственный pinned policy source.
// Копирует mutable slices, исключая изменение admitted snapshot через client buffer.
func kubernetesOperationTarget(input *cp.ManagedKubernetesTargetSnapshot) (value.KubernetesTarget, bool) {
	var result value.KubernetesTarget
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.GetRevision() < 1 || !managedkubernetes.ValidIdentityReference(input.GetTargetRef()) || !managedkubernetes.ValidHTTPSOrigin(input.GetServerOrigin()) || !managedkubernetes.ValidDigest(input.GetCertificateAuthoritySha256()) || !managedkubernetes.ValidKubeconfigSourceOrigins(input.GetKubeconfigSourceOrigins()) || input.GetEmbeddedContext() == "" || len(input.GetEmbeddedContext()) > 256 || input.GetClusterUid() == "" || input.GetTlsServerName() == "" || len(input.GetAddressIps()) == 0 || len(input.GetAddressIps()) > 8 {
		return result, false
	}
	result = value.KubernetesTarget{ID: input.GetTargetRef(), Revision: uint64(input.GetRevision()), ClusterUID: input.GetClusterUid(), ServerURL: input.GetServerOrigin(), TLSServerName: input.GetTlsServerName(), CertificateAuthorityHash: input.GetCertificateAuthoritySha256(), Context: input.GetEmbeddedContext(), AddressPins: slices.Clone(input.GetAddressIps()), KubeconfigSourceOrigins: slices.Clone(input.GetKubeconfigSourceOrigins())}
	if len(input.GetResourcePolicyJson()) == 0 {
		return result, input.GetResourcePolicySha256() == ""
	}
	policy, digest, err := managedkubernetes.DecodeResourcePolicy(input.GetResourcePolicyJson())
	if err != nil || digest != input.GetResourcePolicySha256() {
		return value.KubernetesTarget{}, false
	}
	result.ResourcePolicy = &policy
	result.ResourcePolicySHA256 = digest
	return result, true
}

// kubernetesOperationNode copies a finite registered pin without inferring transport availability.
// The native adapter must still verify current CA certificates, addresses and the observed serving key.
// kubernetesOperationNode копирует finite registered pin без inference transport availability.
// Native adapter проверяет current CA certificates, addresses и observed serving key отдельно.
func kubernetesOperationNode(input *cp.ManagedKubernetesNodeTransportSnapshot, target value.KubernetesTarget, now time.Time) (value.KubernetesNodeTransport, bool) {
	var result value.KubernetesNodeTransport
	if len(input.ProtoReflect().GetUnknown()) != 0 || input.GetNotBefore() == nil || input.GetExpiresAt() == nil || input.GetNotBefore().CheckValid() != nil || input.GetExpiresAt().CheckValid() != nil || len(input.GetNotBefore().ProtoReflect().GetUnknown()) != 0 || len(input.GetExpiresAt().ProtoReflect().GetUnknown()) != 0 || input.GetGeneration() < 1 || input.GetTargetRevision() != int64(target.Revision) || input.GetTargetRef() != target.ID || !managedkubernetes.ValidLocator(input.GetPinRef()) || len(validation.IsDNS1123Subdomain(input.GetNodeName())) != 0 || input.GetNodeUid() == "" || !managedkubernetes.ValidHTTPSOrigin(input.GetServerOrigin()) || input.GetTlsServerName() == "" || !managedkubernetes.ValidDigest(input.GetServerPublicKeySha256()) || len(input.GetAddressIps()) == 0 || len(input.GetAddressIps()) > 8 || len(input.GetCertificateAuthorityPem()) == 0 || len(input.GetCertificateAuthorityPem()) > managedkubernetes.MaximumCredentialBytes {
		return result, false
	}
	before, after := input.GetNotBefore().AsTime(), input.GetExpiresAt().AsTime()
	protocol := managedkubernetes.PrivilegedProtocol(input.GetProtocol())
	digest := sha256.Sum256(input.GetCertificateAuthorityPem())
	if before.After(now) || !after.After(now) || after.Sub(before) > 24*time.Hour || protocol.WireProtocol() == "" || hex.EncodeToString(digest[:]) != input.GetCertificateAuthoritySha256() {
		return result, false
	}
	return value.KubernetesNodeTransport{Ref: input.GetPinRef(), TargetRef: input.GetTargetRef(), NodeName: input.GetNodeName(), NodeUID: input.GetNodeUid(), Generation: input.GetGeneration(), TargetRevision: input.GetTargetRevision(), ServerOrigin: input.GetServerOrigin(), TLSServerName: input.GetTlsServerName(), CertificateAuthoritySHA256: input.GetCertificateAuthoritySha256(), ServerPublicKeySHA256: input.GetServerPublicKeySha256(), CertificateAuthorityPEM: bytes.Clone(input.GetCertificateAuthorityPem()), AddressIPs: slices.Clone(input.GetAddressIps()), NotBefore: before, ExpiresAt: after, Protocol: protocol}, true
}
