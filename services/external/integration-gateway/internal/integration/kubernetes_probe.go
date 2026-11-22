package integration

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
)

// KubernetesProbeReceipt records only a joined purpose-owner health result; authentication does not grant resource access.
// Attempted survives a lost or invalid acknowledgement, while Completed requires a validated owner receipt.
// KubernetesProbeReceipt хранит лишь joined purpose-owner health result; authentication не даёт resource access.
// Attempted сохраняется при lost/invalid acknowledgement; Completed требует validated owner receipt.
type KubernetesProbeReceipt struct {
	TestRef                             string
	Attempted, Completed, Authenticated bool
}

// ProbeKubernetes sends the original metadata-only health claim to the broker with its opaque owner snapshot digest.
// A health digest identifies the entire test snapshot, not an empty business operation input; no credentials or grant are synthesized.
// ProbeKubernetes передаёт original metadata-only health claim broker с opaque owner snapshot digest.
// Health digest связывает весь test snapshot, не пустой business operation input; credentials и grant не синтезируются.
func (adapter *Adapter) ProbeKubernetes(ctx context.Context, claim *cp.IntegrationConnectionTestClaim, instance string, broker sb.ManagedKubernetesServiceClient) (KubernetesProbeReceipt, error) {
	var receipt KubernetesProbeReceipt
	if claim == nil || len(claim.ProtoReflect().GetUnknown()) != 0 || claim.GetCredentialRevision() != nil || claim.GetCredentialMaterializationRef() != "" ||
		claim.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY ||
		claim.GetBrokerIdentityPurpose() != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET ||
		!managedkubernetes.ValidLocator(claim.GetBrokerIdentityRef()) || claim.GetBrokerIdentityRevision() < 1 ||
		!managedkubernetes.ValidLocator(claim.GetConnectionRef()) {
		return receipt, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	lease := claim.GetLease()
	if configuration := claim.GetPublicConfiguration(); configuration != nil && len(configuration.ProtoReflect().GetUnknown()) != 0 {
		return receipt, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if lease == nil || len(lease.ProtoReflect().GetUnknown()) != 0 || lease.ExpiresAt == nil || lease.ExpiresAt.CheckValid() != nil ||
		len(lease.ExpiresAt.ProtoReflect().GetUnknown()) != 0 || !lease.ExpiresAt.AsTime().After(time.Now()) {
		return receipt, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	binding := managedkubernetes.Binding{WorkloadInstance: instance, ConnectionTestRef: claim.TestRef, LeaseRef: lease.Ref, Fence: lease.Fence, Generation: lease.Generation, InputDigest: claim.InputDigest}
	if binding.Validate() != nil {
		return receipt, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	request := RequestFromTest(claim)
	definition, err := adapter.validateExecutableDefinition(request)
	if err != nil {
		return receipt, err
	}
	delivery, known := definition.ConnectionCredentialDelivery()
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil || !known || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity ||
		delivery.BrokerPurpose != "KUBERNETES_TARGET" || definition.Metadata.Key != integrationpackage.KubernetesDefinitionKey ||
		definition.Spec.Adapter != string(integrationpackage.AdapterKubernetes) || definition.Spec.HealthCheck.Operation != "kubernetes.identity" || definition.Spec.HealthCheck.TimeoutSeconds < 1 {
		return receipt, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	if broker == nil {
		return receipt, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	deadline := time.Now().Add(time.Duration(min(definition.Spec.HealthCheck.TimeoutSeconds, 20)) * time.Second)
	if lease.ExpiresAt.AsTime().Before(deadline) {
		deadline = lease.ExpiresAt.AsTime()
	}
	operation, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if operation.Err() != nil {
		return receipt, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	receipt.TestRef, receipt.Attempted = binding.ConnectionTestRef, true
	wire := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: binding.WorkloadInstance, ConnectionTestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}
	response, err := broker.ExecuteManagedKubernetesOperation(operation, &sb.ExecuteManagedKubernetesOperationRequest{Binding: wire})
	if err != nil || operation.Err() != nil {
		return receipt, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	if !validKubernetesResponse(response, wire, integrationpackage.Capability{}) {
		return receipt, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	receipt.Completed = true
	receipt.Authenticated = response.GetCompleted().RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED
	return receipt, nil
}
