package integration

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// SSHInvocation retains an owned immutable copy of the exact claimed metadata and original lease; it contains no key, target endpoint, replacement credential or new authority.
// SSHInvocation сохраняет owned immutable copy exact claimed metadata и original lease без key, target endpoint, replacement credential либо новой authority.
type SSHInvocation struct {
	binding managedssh.Binding
	expires time.Time
}

// SSHInvocationResult reports only the exact purpose-owner receipt and whether an RPC was attempted; RUNNING or uncertain delivery never becomes success.
// SSHInvocationResult сообщает лишь exact purpose-owner receipt и факт RPC attempt; RUNNING либо uncertain delivery не становится success.
type SSHInvocationResult struct {
	Receipt   managedssh.ExecutionReceipt
	Attempted bool
}

// PrepareSSHInvocation validates the fresh exact SSH package, custody representation, grant/input/scope pins and lease before removing the generic mapper's unrelated EmailExecution metadata.
// PrepareSSHInvocation проверяет fresh exact SSH package, custody representation, grant/input/scope pins и lease до удаления unrelated EmailExecution metadata generic mapper.
func (adapter *Adapter) PrepareSSHInvocation(claim *cp.IntegrationInvocationClaim, instance string) (*SSHInvocation, error) {
	reject := &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	if adapter == nil || claim == nil || proto.Size(claim) > 512<<10 {
		return nil, reject
	}
	codec := grpcserver.StrictProtoCodec()
	wire, err := codec.Marshal(claim)
	if err != nil {
		return nil, reject
	}
	defer clear(wire)
	owned := &cp.IntegrationInvocationClaim{}
	if codec.Unmarshal(wire, owned) != nil || len(owned.ProtoReflect().GetUnknown()) != 0 {
		return nil, reject
	}
	if owned.CredentialDeliveryKind != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY || owned.BrokerIdentityPurpose != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET ||
		!managedssh.OwnerReference(owned.BrokerIdentityRef) || owned.BrokerIdentityRevision < 1 || owned.BrokerIdentityRevision > 9007199254740991 || owned.CredentialAdmissionRequired || owned.CredentialRevision != nil || owned.CredentialMaterializationRef != "" ||
		owned.ResourcePermissionProfile != cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_SSH_SCOPED_RESOURCE_V1 || owned.WorkMode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE || !managedssh.OwnerReference(owned.ConnectionRef) ||
		owned.Lease == nil || owned.Lease.ExpiresAt == nil || owned.Lease.ExpiresAt.CheckValid() != nil || !owned.Lease.ExpiresAt.AsTime().After(time.Now()) || owned.Lease.Generation != 1 {
		return nil, reject
	}
	binding := managedssh.Binding{WorkloadInstance: instance, InvocationRef: owned.InvocationRef, LeaseRef: owned.Lease.Ref, Fence: owned.Lease.Fence, Generation: owned.Lease.Generation, InputDigest: owned.InputDigest}
	if binding.Validate() != nil {
		return nil, reject
	}
	request := RequestFromInvocation(owned)
	definition, err := adapter.validateExecutableDefinition(request)
	if err != nil {
		return nil, err
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid || delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != managedssh.IdentityPurpose || definition.Spec.Adapter != "SSH_TARGET" ||
		(definition.Metadata.Key != integrationpackage.SSHManagedDefinitionKey && definition.Metadata.Key != integrationpackage.SSHExecuteDefinitionKey) {
		return nil, reject
	}
	request.EmailExecution = nil
	_, _, canonical, _, err := adapter.validateInvocationDefinition(request, definition)
	if err != nil {
		return nil, err
	}
	defer clear(canonical)
	input, err := managedssh.DecodeOperationInput(request.Operation, canonical)
	if err != nil {
		return nil, reject
	}
	defer input.Clear()
	return &SSHInvocation{binding: binding, expires: owned.Lease.ExpiresAt.AsTime()}, nil
}

// Binding returns only original opaque claim coordinates for local lifecycle accounting; it cannot be used to replace the owner lease.
// Binding возвращает лишь original opaque claim coordinates для local lifecycle accounting; заменить owner lease нельзя.
func (invocation *SSHInvocation) Binding() managedssh.Binding {
	if invocation == nil {
		return managedssh.Binding{}
	}
	return invocation.binding
}

// ExpiresAt returns the original finite outer lease; no local consumer or retry can extend it.
// ExpiresAt возвращает original finite outer lease; local consumer и retry не продлевают её.
func (invocation *SSHInvocation) ExpiresAt() time.Time {
	if invocation == nil {
		return time.Time{}
	}
	return invocation.expires
}

// sshInvocationWireBinding supplies exactly the preserved owner claim to the broker's closed unary methods.
// sshInvocationWireBinding передаёт broker closed unary methods ровно сохранённый owner claim.
func sshInvocationWireBinding(binding managedssh.Binding) *sb.ManagedSSHExecutionBinding {
	return &sb.ManagedSSHExecutionBinding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}
}

// sshInvocationReply checks every original claim pin, owner state and output digest before local use; a successful malformed ACK is permanently distinguished from transport uncertainty.
// sshInvocationReply проверяет каждый original claim pin, owner state и output digest до local use; successful malformed ACK отличим от transport uncertainty.
func (invocation *SSHInvocation) sshInvocationReply(input *sb.ManagedSSHExecutionResult, metadata bool) (managedssh.ExecutionReceipt, error) {
	reject := &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || input.Binding == nil || len(input.Binding.ProtoReflect().GetUnknown()) != 0 || !proto.Equal(input.Binding, sshInvocationWireBinding(invocation.binding)) {
		return managedssh.ExecutionReceipt{}, reject
	}
	receipt := managedssh.ExecutionReceipt{AttemptRef: input.AttemptRef, SnapshotDigest: input.SnapshotDigest, State: input.State, OwnerState: input.OwnerState, Recorded: input.Recorded, Applied: input.Applied,
		Observation: managedssh.ExecutionObservation{Outcome: managedssh.ExecutionOutcome(input.Outcome), Result: append([]byte(nil), input.ResultJson...), ResultDigest: input.ResultDigest}}
	if receipt.Validate(time.Now(), !metadata) != nil || !metadata && !receipt.Recorded {
		clear(receipt.Observation.Result)
		return managedssh.ExecutionReceipt{}, reject
	}
	return receipt, nil
}

// Execute sends the original claim exactly once and never invokes generic TOKEN execution or completion; the purpose owner already persists the outer result.
// Execute отправляет original claim ровно один раз без generic TOKEN execution либо completion; purpose owner сам сохраняет outer result.
func (invocation *SSHInvocation) Execute(ctx context.Context, broker sb.ManagedSSHServiceClient) (SSHInvocationResult, error) {
	var result SSHInvocationResult
	if invocation == nil || ctx == nil || ctx.Err() != nil || broker == nil || !invocation.expires.After(time.Now()) {
		return result, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	operation, cancel := context.WithDeadline(ctx, invocation.expires)
	defer cancel()
	result.Attempted = true
	response, err := broker.ExecuteSSHIdentity(operation, &sb.ExecuteSSHIdentityRequest{Binding: sshInvocationWireBinding(invocation.binding)}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil || operation.Err() != nil {
		return result, &SafeError{Code: "INTEGRATION_OUTCOME_UNKNOWN"}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	result.Receipt, err = invocation.sshInvocationReply(response.Result, false)
	return result, err
}

// Reconcile performs one metadata-only read after uncertain delivery; it never replays Execute, extends the lease or treats absence as proof of no effect.
// Reconcile выполняет одно metadata-only read после uncertain delivery; Execute не повторяется, lease не продлевается, отсутствие не считается no-effect proof.
func (invocation *SSHInvocation) Reconcile(ctx context.Context, broker sb.ManagedSSHServiceClient) (SSHInvocationResult, error) {
	var result SSHInvocationResult
	if invocation == nil || ctx == nil || ctx.Err() != nil || broker == nil {
		return result, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	response, err := broker.ReconcileSSHIdentity(ctx, &sb.ReconcileSSHIdentityRequest{Binding: sshInvocationWireBinding(invocation.binding)}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil || ctx.Err() != nil {
		return result, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return result, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	result.Receipt, err = invocation.sshInvocationReply(response.Result, true)
	return result, err
}
