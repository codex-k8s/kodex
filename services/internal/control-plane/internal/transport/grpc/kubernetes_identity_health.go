package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// BeginManagedKubernetesHealthProbe returns private metadata only after the test's sole admission.
// Unlike operation Resolve, first health intentionally has no pre-existing subject digest.
// BeginManagedKubernetesHealthProbe возвращает private metadata только после единственного допуска test.
// В отличие от operation Resolve, первый health намеренно не имеет прежнего subject digest.
func (server *Server) BeginManagedKubernetesHealthProbe(ctx context.Context, request *cp.BeginManagedKubernetesHealthProbeRequest) (*cp.BeginManagedKubernetesHealthProbeResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_BeginManagedKubernetesHealthProbe_FullMethodName, func(principal value.Principal) (*cp.BeginManagedKubernetesHealthProbeResponse, error) {
		result, err := server.service.BeginManagedKubernetesHealthProbe(ctx, principal, managedKubernetesBindingFromProto(request.GetBinding()))
		if err != nil {
			return nil, err
		}
		expires := managedKubernetesTimestamp(result.ExpiresAt)
		identity := managedKubernetesIdentityToProto(result.Identity, result.Identity.IdentityRef, result.Target.Ref)
		if !managedkubernetes.ValidLocator(result.AttemptRef) || !managedkubernetes.ValidDigest(result.SnapshotDigest) ||
			expires == nil || identity == nil || result.Target.Ref != result.Identity.TargetRef || result.Target.Revision < 1 {
			return nil, errs.ErrUnavailable
		}
		return &cp.BeginManagedKubernetesHealthProbeResponse{AttemptRef: result.AttemptRef, SnapshotDigest: result.SnapshotDigest,
			IdentityLocator: identity, ExpiresAt: expires, Target: &cp.ManagedKubernetesTargetSnapshot{
				TargetRef: result.Target.Ref, Revision: result.Target.Revision, ServerOrigin: result.Target.ServerOrigin,
				TlsServerName: result.Target.TLSServerName, CertificateAuthoritySha256: result.Target.CertificateAuthoritySHA256,
				AddressIps: append([]string(nil), result.Target.AddressIPs...), EmbeddedContext: result.Target.EmbeddedContext,
				ClusterUid: result.Target.ClusterUID, KubeconfigSourceOrigins: append([]string(nil), result.Target.KubeconfigSourceOrigins...)}}, nil
	})
}

// CompleteManagedKubernetesHealthProbe accepts only the closed private authentication fact variant.
// It exposes no subject digest or locator in the response and never turns a failure into success.
// CompleteManagedKubernetesHealthProbe принимает только закрытый private authentication fact variant.
// Не раскрывает subject digest либо locator в response и не превращает failure в success.
func (server *Server) CompleteManagedKubernetesHealthProbe(ctx context.Context, request *cp.CompleteManagedKubernetesHealthProbeRequest) (*cp.CompleteManagedKubernetesHealthProbeResponse, error) {
	if !managedKubernetesWireValid(request) {
		return nil, transportError(errs.ErrInvalid)
	}
	var outcome entity.PersonalIntegrationCredentialTestOutcome
	switch request.GetOutcome() {
	case cp.ManagedKubernetesHealthOutcome_MANAGED_KUBERNETES_HEALTH_OUTCOME_AUTHENTICATED:
		outcome = entity.PersonalIntegrationCredentialAuthenticated
	case cp.ManagedKubernetesHealthOutcome_MANAGED_KUBERNETES_HEALTH_OUTCOME_AUTHENTICATION_REJECTED:
		outcome = entity.PersonalIntegrationCredentialAuthenticationRejected
	case cp.ManagedKubernetesHealthOutcome_MANAGED_KUBERNETES_HEALTH_OUTCOME_UNAVAILABLE:
		outcome = entity.PersonalIntegrationCredentialVerificationUnavailable
	default:
		return nil, transportError(errs.ErrInvalid)
	}
	return queryResponse(ctx, cp.ManagedKubernetesOwnerService_CompleteManagedKubernetesHealthProbe_FullMethodName, func(principal value.Principal) (*cp.CompleteManagedKubernetesHealthProbeResponse, error) {
		result, err := server.service.CompleteManagedKubernetesHealthProbe(ctx, principal, managedKubernetesBindingFromProto(request.GetBinding()), command.KubernetesIdentityHealthCompletion{
			AttemptRef: request.GetAttemptRef(), SnapshotDigest: request.GetSnapshotDigest(), Outcome: outcome, ObservedSubjectDigest: request.GetObservedSubjectDigest()})
		if err != nil {
			return nil, err
		}
		if !managedkubernetes.ValidLocator(result.TestRef) || (result.State != "SUCCEEDED" && result.State != "FAILED" && result.State != "CANCELLED") {
			return nil, errs.ErrUnavailable
		}
		return &cp.CompleteManagedKubernetesHealthProbeResponse{TestRef: result.TestRef, State: result.State, Applied: result.Applied}, nil
	})
}
