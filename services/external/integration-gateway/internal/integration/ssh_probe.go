package integration

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"time"
)

// SSHProbeReceipt retains the original owner completion only; no generic material, target endpoint or command crosses the gateway.
// SSHProbeReceipt сохраняет лишь original owner completion; generic material, target endpoint либо command не пересекают gateway.
type SSHProbeReceipt struct {
	TestRef, State                    string
	Attempted, Applied, Authenticated bool
}

// ProbeSSH carries the opaque full-snapshot digest unchanged over one bounded broker RPC after exact package/delivery/lease checks; it never uses the generic EmailExecution mapper.
// ProbeSSH передаёт opaque full-snapshot digest без изменения через bounded broker RPC после exact package/delivery/lease checks; generic EmailExecution mapper не используется.
func (adapter *Adapter) ProbeSSH(ctx context.Context, claim *cp.IntegrationConnectionTestClaim, instance string, broker sb.ManagedSSHServiceClient) (SSHProbeReceipt, error) {
	var result SSHProbeReceipt
	reject := &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	if ctx == nil || claim == nil || len(claim.ProtoReflect().GetUnknown()) != 0 || claim.CredentialRevision != nil || claim.CredentialMaterializationRef != "" || claim.CredentialDeliveryKind != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY || claim.BrokerIdentityPurpose != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET || !managedssh.OwnerReference(claim.BrokerIdentityRef) || claim.BrokerIdentityRevision < 1 || claim.BrokerIdentityRevision > 9007199254740991 || !managedssh.OwnerReference(claim.ConnectionRef) {
		return result, reject
	}
	lease := claim.Lease
	if lease == nil || len(lease.ProtoReflect().GetUnknown()) != 0 || lease.ExpiresAt == nil || len(lease.ExpiresAt.ProtoReflect().GetUnknown()) != 0 || lease.ExpiresAt.CheckValid() != nil || !lease.ExpiresAt.AsTime().After(time.Now()) {
		return result, reject
	}
	binding := managedssh.Binding{WorkloadInstance: instance, ConnectionTestRef: claim.TestRef, LeaseRef: lease.Ref, Fence: lease.Fence, Generation: lease.Generation, InputDigest: claim.InputDigest}
	if binding.Validate() != nil {
		return result, reject
	}
	if claim.PublicConfiguration == nil || len(claim.PublicConfiguration.ProtoReflect().GetUnknown()) != 0 {
		return result, reject
	}
	request := Request{DefinitionPackage: append([]byte(nil), claim.DefinitionPackage...), DefinitionKey: claim.DefinitionKey, DefinitionVersion: claim.DefinitionVersion, DefinitionDigest: claim.DefinitionDigest, ConnectionRef: claim.ConnectionRef, Configuration: claim.PublicConfiguration.AsMap()}
	definition, err := adapter.validateExecutableDefinition(request)
	if err != nil {
		return result, err
	}
	delivery, known := definition.ConnectionCredentialDelivery()
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	configuration, err := normalizeStringMap(request.Configuration)
	validDefinition := definition.Metadata.Key == integrationpackage.SSHManagedDefinitionKey && definition.Spec.HealthCheck.Operation == "ssh.target.inspect" || definition.Metadata.Key == integrationpackage.SSHExecuteDefinitionKey && definition.Spec.HealthCheck.Operation == "ssh.identity.inspect"
	if err != nil || definition.ValidateConfiguration(configuration) != nil || !known || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != "SSH_TARGET" || definition.Spec.Adapter != "SSH_TARGET" || !validDefinition || !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" || definition.Spec.HealthCheck.TimeoutSeconds < 1 {
		return result, reject
	}
	if broker == nil {
		return result, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	deadline := time.Now().Add(time.Duration(min(definition.Spec.HealthCheck.TimeoutSeconds, 20)) * time.Second)
	if lease.ExpiresAt.AsTime().Before(deadline) {
		deadline = lease.ExpiresAt.AsTime()
	}
	operation, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if operation.Err() != nil {
		return result, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	result.TestRef, result.Attempted = claim.TestRef, true
	response, err := broker.ProbeSSHIdentity(operation, &sb.ProbeSSHIdentityRequest{Binding: &sb.ManagedSSHHealthBinding{WorkloadInstance: binding.WorkloadInstance, ConnectionTestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}})
	if err != nil || operation.Err() != nil {
		return result, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.TestRef != claim.TestRef || (response.State != "SUCCEEDED" && response.State != "FAILED" && response.State != "CANCELLED") || response.Applied == (response.State == "CANCELLED") {
		return result, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	result.State, result.Applied, result.Authenticated = response.State, response.Applied, response.State == "SUCCEEDED"
	return result, nil
}
