package grpc

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestKubernetesSetupRequirementsProjection checks exact Connection/version/policy binding and
// prevents a TOKEN or foreign target from acquiring KUBE-only pre-Begin metadata.
// TestKubernetesSetupRequirementsProjection проверяет exact Connection/version/policy binding и
// запрещает TOKEN либо foreign target получать KUBE-only pre-Begin metadata.
func TestKubernetesSetupRequirementsProjection(t *testing.T) {
	requirements := &entity.KubernetesIdentitySetupRequirements{TargetRef: "target_fixture01", TargetRevision: 3, CanonicalContext: "operator-context", ConnectionVersion: 7, Policy: entity.IntegrationCredentialInitiatorRequired}
	if result := kubernetesSetupRequirementsToProto(requirements, 7, requirements.Policy); result == nil || result.GetCanonicalContext() != requirements.CanonicalContext || result.GetTargetRevision() != 3 {
		t.Fatal("safe pre-Begin requirements lost target revision")
	}
	if kubernetesSetupRequirementsToProto(requirements, 8, requirements.Policy) != nil || kubernetesSetupRequirementsToProto(requirements, 7, entity.IntegrationCredentialProjectShared) != nil {
		t.Fatal("foreign requirements version/policy became usable metadata")
	}
	connection := entity.IntegrationConnection{Ref: "int_fixture01", Version: 7, CredentialPolicy: requirements.Policy, PublicConfiguration: map[string]any{"target_ref": requirements.TargetRef}, CredentialDelivery: integrationpackage.CredentialDelivery{Kind: integrationpackage.CredentialDeliveryToken}, KubernetesSetupRequirements: requirements}
	if castConnection(connection) != nil {
		t.Fatal("TOKEN source acquired KUBE export requirements")
	}
	connection.CredentialDelivery = integrationpackage.CredentialDelivery{Kind: integrationpackage.CredentialDeliveryBrokerIdentity, BrokerPurpose: string(entity.IntegrationBrokerIdentityKubernetes)}
	if result := castConnection(connection); result == nil || result.GetKubernetesSetupRequirements() == nil {
		t.Fatal("known KUBE binding lost safe requirements")
	}
	connection.PublicConfiguration["target_ref"] = "target_foreign01"
	if castConnection(connection) != nil {
		t.Fatal("foreign target context passed scoped connection projection")
	}
}
