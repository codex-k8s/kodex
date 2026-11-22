package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// ExecuteKubernetes dispatches exactly one owner binding through the typed broker RPC. It neither
// reads kubeconfig nor completes an invocation: the purpose consumer owns renew, fresh READ credit,
// stream exchange/cancel/join and terminal admission. An uncertain mutation never gets replayed.
// ExecuteKubernetes передаёт ровно один owner binding через typed broker RPC. Не читает kubeconfig
// и не завершает invocation: purpose consumer владеет renew, fresh READ credit, stream exchange/
// cancel/join и terminal admission. Неопределённая mutation не получает повтор.
func (adapter *Adapter) ExecuteKubernetes(ctx context.Context, request Request, binding *sb.ManagedKubernetesInvocationBinding, broker sb.ManagedKubernetesServiceClient) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	capability, err := adapter.validateKubernetesRequest(request, binding)
	if err != nil {
		return nil, err
	}
	if broker == nil {
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	timeout := time.Duration(capability.Execution.TimeoutSeconds) * time.Second
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := broker.ExecuteManagedKubernetesOperation(operationContext, &sb.ExecuteManagedKubernetesOperationRequest{
		Binding: proto.Clone(binding).(*sb.ManagedKubernetesInvocationBinding),
	})
	if err != nil {
		if binding.ConnectionTestRef == "" && response == nil && operationContext.Err() == nil {
			if request.Operation == "kubernetes.port-forward" {
				if rejected := kubernetesPreflightFromError(err); rejected != nil {
					return nil, rejected
				}
			}
			if pending := kubernetesPendingFromError(err); pending != nil {
				return nil, pending
			}
		}
		return nil, kubernetesBrokerFailure(capability.Risk, "transport")
	}
	if operationContext.Err() != nil || !validKubernetesResponse(response, binding, capability) {
		return nil, kubernetesBrokerFailure(capability.Risk, "response_validation")
	}
	return response, nil
}

// validateKubernetesRequest checks pinned package/input/grant metadata without accepting payload
// authority. The private binding comes only from the original owner claim, not Request.Input.
// validateKubernetesRequest сверяет pinned package/input/grant metadata без payload authority.
// Private binding поступает только из original owner claim, не Request.Input.
func (adapter *Adapter) validateKubernetesRequest(request Request, binding *sb.ManagedKubernetesInvocationBinding) (integrationpackage.Capability, error) {
	reject := &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	if binding == nil || binding.ConnectionTestRef != "" || len(binding.ProtoReflect().GetUnknown()) != 0 || request.Credential != nil || request.EmailExecution != nil {
		return integrationpackage.Capability{}, reject
	}
	ownerBinding := managedkubernetes.Binding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef,
		ConnectionTestRef: binding.ConnectionTestRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence,
		Generation: binding.Generation, InputDigest: binding.InputDigest}
	if ownerBinding.Validate() != nil || binding.InputDigest != request.InputDigest ||
		request.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionKubernetesScopedV1 {
		return integrationpackage.Capability{}, reject
	}
	definition, err := adapter.validateExecutableDefinition(request)
	if err != nil {
		return integrationpackage.Capability{}, err
	}
	delivery, valid := definition.ConnectionCredentialDelivery()
	if !valid || definition.Metadata.Key != integrationpackage.KubernetesDefinitionKey || definition.Spec.Adapter != string(integrationpackage.AdapterKubernetes) ||
		delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != "KUBERNETES_TARGET" {
		return integrationpackage.Capability{}, reject
	}
	capability, exists := definition.Capability(request.CapabilityKey)
	if !exists || capability.Operation != request.Operation || capability.Risk != request.Risk ||
		!capability.AllowsApprovalPolicy(request.ApprovalPolicy) || request.ResourceKind != "KUBERNETES_TARGET" ||
		!managedkubernetes.ValidLocator(request.EffectKey) || !managedkubernetes.ValidLocator(request.ConnectionRef) {
		return integrationpackage.Capability{}, reject
	}
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return integrationpackage.Capability{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	if !managedkubernetes.ValidLocator(request.GrantRef) || request.GrantVersion < 1 {
		return integrationpackage.Capability{}, &SafeError{Code: "INTEGRATION_GRANT_INVALID"}
	}
	expectedScope, err := capability.KubernetesResourceScope(configuration, request.ResourceScope["namespace"])
	encodedScope, scopeErr := json.Marshal(expectedScope)
	scopeDigest := sha256.Sum256(encodedScope)
	if err != nil || scopeErr != nil || !equalStringMap(expectedScope, request.ResourceScope) || hex.EncodeToString(scopeDigest[:]) != request.ResourceScopeDigest {
		return integrationpackage.Capability{}, reject
	}
	encodedInput, err := json.Marshal(request.Input)
	if err != nil {
		return integrationpackage.Capability{}, reject
	}
	input, canonical, err := managedkubernetes.DecodeOperationInput(request.Operation, encodedInput)
	inputDigest := sha256.Sum256(canonical)
	if err != nil || hex.EncodeToString(inputDigest[:]) != request.InputDigest || input.ValidateGrantScope(request.Operation, request.ResourceScope["namespace"]) != nil ||
		definition.ValidateInvocationApprovalPolicy(capability, request.ApprovalPolicy, canonical) != nil {
		return integrationpackage.Capability{}, reject
	}
	return capability, nil
}

// kubernetesBrokerFailure hides raw gRPC/upstream errors and conservatively preserves mutation UNKNOWN.
// kubernetesBrokerFailure скрывает raw gRPC/upstream errors и консервативно сохраняет mutation UNKNOWN.
func kubernetesBrokerFailure(risk, stage string) error {
	if risk != "READ" {
		return &UnknownOutcomeError{stage: stage}
	}
	if stage == "transport" {
		return &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
}

// validKubernetesResponse distinguishes bounded completed projections from uncompleted streams.
// No response creates a grant, success receipt or current owner eligibility at this boundary.
// validKubernetesResponse различает bounded completed projections и незавершённые streams.
// Ответ не создаёт grant, success receipt либо current owner eligibility на этой boundary.
func validKubernetesResponse(response *sb.ExecuteManagedKubernetesOperationResponse, binding *sb.ManagedKubernetesInvocationBinding, capability integrationpackage.Capability) bool {
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return false
	}
	if completed := response.GetCompleted(); completed != nil {
		if len(completed.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(completed.EffectRef) {
			return false
		}
		outcome := strings.TrimPrefix(completed.RemoteOutcome.String(), "MANAGED_KUBERNETES_REMOTE_OUTCOME_")
		if binding.ConnectionTestRef != "" {
			return validKubernetesHealthResult(completed, binding.ConnectionTestRef)
		}
		if managedkubernetes.ValidateEffectResult(completed.ResultJson, completed.ResultDigest, outcome, completed.ReasonCode) != nil {
			return false
		}
		now := time.Now()
		return completed.EffectGeneration > 0 && completed.InvocationGeneration == binding.Generation &&
			completed.AuthorizedUntil != nil && completed.AuthorizedUntil.CheckValid() == nil &&
			len(completed.AuthorizedUntil.ProtoReflect().GetUnknown()) == 0 &&
			completed.AuthorizedUntil.AsTime().After(now) && !completed.AuthorizedUntil.AsTime().After(now.Add(30*time.Second))
	}
	stream := response.GetStream()
	operation, _ := integrationpackage.KubernetesOperationByKey(capability.Operation)
	if stream == nil || operation.Execution != integrationpackage.KubernetesStream || binding.ConnectionTestRef != "" ||
		len(stream.ProtoReflect().GetUnknown()) != 0 || !managedkubernetes.ValidLocator(stream.StreamRef) || stream.InvocationRef != binding.InvocationRef ||
		stream.Generation != binding.Generation || stream.ExpiresAt == nil || stream.ExpiresAt.CheckValid() != nil ||
		len(stream.ExpiresAt.ProtoReflect().GetUnknown()) != 0 ||
		!stream.ExpiresAt.AsTime().After(time.Now()) || stream.ExpiresAt.AsTime().After(time.Now().Add(600*time.Second)) ||
		stream.MaximumFrameBytes == 0 || stream.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes ||
		stream.MaximumTotalBytes == 0 || stream.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes || len(stream.OutputChannels) == 0 {
		return false
	}
	for _, channels := range [][]sb.ManagedKubernetesChannel{stream.InputChannels, stream.OutputChannels} {
		seen := map[sb.ManagedKubernetesChannel]bool{}
		for _, channel := range channels {
			if seen[channel] || !managedkubernetes.ValidChannel(strings.TrimPrefix(channel.String(), "MANAGED_KUBERNETES_CHANNEL_")) {
				return false
			}
			seen[channel] = true
		}
	}
	if capability.Risk == "READ" {
		for _, channel := range stream.InputChannels {
			if channel != sb.ManagedKubernetesChannel_MANAGED_KUBERNETES_CHANNEL_CONTROL {
				return false
			}
		}
	}
	return true
}

// validKubernetesHealthResult checks the separate owner-completed health receipt without inventing resource-effect authority.
// The shared validator checks only object bytes/digest here; the exact health outcome and projection are validated separately.
// validKubernetesHealthResult проверяет отдельный owner-completed health receipt без resource-effect authority.
// Shared validator проверяет здесь только object bytes/digest; exact health outcome и projection проверяются отдельно.
func validKubernetesHealthResult(result *sb.ManagedKubernetesSafeResult, testRef string) bool {
	if result.EffectRef != testRef || result.EffectGeneration != 0 || result.InvocationGeneration != 0 || result.AuthorizedUntil != nil ||
		managedkubernetes.ValidateEffectResult(result.ResultJson, result.ResultDigest, "SUCCEEDED", "") != nil {
		return false
	}
	var projection struct {
		Authenticated            *bool  `json:"authenticated"`
		ResourcePermissionChecks string `json:"resourcePermissionChecks"`
	}
	if internalrpcauth.DecodeStrictJSON(result.ResultJson, &projection) != nil || projection.Authenticated == nil || projection.ResourcePermissionChecks != "PER_OPERATION_REQUIRED" {
		return false
	}
	if *projection.Authenticated {
		return result.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED && result.ReasonCode == ""
	}
	return result.RemoteOutcome == sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED && result.ReasonCode == "HEALTH_CHECK_FAILED"
}
