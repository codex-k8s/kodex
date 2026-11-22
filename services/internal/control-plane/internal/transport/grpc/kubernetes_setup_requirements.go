package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// kubernetesSetupRequirementsToProto emits safe pre-Begin export metadata only when it matches
// the already-authorized addressed Connection snapshot. It never interprets this metadata as a grant.
// kubernetesSetupRequirementsToProto выдаёт safe pre-Begin export metadata только при совпадении
// с already-authorized Connection snapshot. Metadata никогда не интерпретируются как grant.
func kubernetesSetupRequirementsToProto(input *entity.KubernetesIdentitySetupRequirements, version int64, policy entity.IntegrationCredentialPolicy) *cp.KubernetesIdentitySetupRequirements {
	if input == nil || !input.Valid() || input.ConnectionVersion != version || input.Policy != policy {
		return nil
	}
	return &cp.KubernetesIdentitySetupRequirements{TargetRef: input.TargetRef, TargetRevision: input.TargetRevision, CanonicalContext: input.CanonicalContext, ConnectionVersion: input.ConnectionVersion, CredentialPolicy: cp.IntegrationCredentialPolicy(cp.IntegrationCredentialPolicy_value["INTEGRATION_CREDENTIAL_POLICY_"+string(input.Policy)])}
}
