package platform

import "github.com/codex-k8s/kodex/libs/go/integrationpackage"

// integrationInvocationActorPermission separates assigned Kubernetes/SSH use from Connection editing.
// It accepts only the already validated immutable package; legacy adapters preserve their existing
// management policy. Current grant, root, identity and RuntimeRevision eligibility stay mandatory.
// integrationInvocationActorPermission разделяет assigned Kubernetes/SSH use и редактирование Connection.
// Принимает только validated immutable package; legacy adapters сохраняют прежнюю management policy.
// Current grant/root/identity/RuntimeRevision eligibility остаются обязательными.
func integrationInvocationActorPermission(definition integrationpackage.Package) string {
	delivery, known := definition.ConnectionCredentialDelivery()
	if known && definition.Metadata.Key == integrationpackage.KubernetesDefinitionKey && definition.Spec.Adapter == "KUBERNETES" && delivery.Kind == integrationpackage.CredentialDeliveryBrokerIdentity && delivery.BrokerPurpose == "KUBERNETES_TARGET" {
		return "integration.view"
	}
	if known && (definition.Metadata.Key == integrationpackage.SSHManagedDefinitionKey || definition.Metadata.Key == integrationpackage.SSHExecuteDefinitionKey) && definition.Spec.Adapter == "SSH_TARGET" && delivery.Kind == integrationpackage.CredentialDeliveryBrokerIdentity && delivery.BrokerPurpose == "SSH_TARGET" {
		return "integration.view"
	}
	return "integration.manage"
}
