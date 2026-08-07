package controlplaneapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

// VerifiedCommandAuthority содержит только стабильную authority, уже
// проверенную consumer-ом. Transport proof, JTI, policy/signer revision и
// digest самого receipt намеренно не входят в semantic business intent.
type VerifiedCommandAuthority struct {
	ActorID        string `json:"actor_id"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	WorkloadID     string `json:"workload_id"`
	FullMethod     string `json:"full_method"`
}

type semanticBusinessIntent struct {
	ContractVersion uint32                   `json:"contract_version"`
	Authority       VerifiedCommandAuthority `json:"authority"`
	TargetKind      string                   `json:"target_kind"`
	TargetResource  string                   `json:"target_resource_id,omitempty"`
	TargetStableKey string                   `json:"target_stable_key"`
	Action          string                   `json:"action"`
	ExpectedVersion uint64                   `json:"expected_version,omitempty"`
	Name            string                   `json:"name"`
	ReferenceKeys   []string                 `json:"reference_keys,omitempty"`
	TypedIntentType string                   `json:"typed_intent_type"`
	TypedIntent     []byte                   `json:"typed_intent"`
}

// ProviderConnectionReferenceIntentSHA256 связывает ProviderEffect receipt с
// точной typed-командой. Idempotency key и provider_receipt из request в hash
// не попадают, поэтому producer вычисляет его до подписи.
func ProviderConnectionReferenceIntentSHA256(
	authority VerifiedCommandAuthority,
	request *controlplanev1.ManageProviderConnectionReferenceRequest,
) (string, error) {
	if request == nil || request.GetSpec() == nil ||
		authority.FullMethod != controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName {
		return "", errors.New("provider connection reference intent is invalid")
	}
	action := strings.ToLower(strings.TrimPrefix(request.GetAction().String(), "PROVIDER_CONNECTION_REFERENCE_ACTION_"))
	if action != "register" && action != "refresh" && action != "archive" {
		return "", errors.New("provider connection reference action is invalid")
	}
	spec := proto.Clone(request.GetSpec()).(*controlplanev1.ProviderConnectionReferenceSpec)
	if spec.GetReceiptId() != "" || spec.GetReceiptVersion() != 0 || spec.GetReceiptSha256() != "" {
		return "", errors.New("provider connection reference receipt fields are server-owned")
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec)
	if err != nil {
		return "", errors.New("encode provider connection reference intent")
	}
	return hashSemanticBusinessIntent(semanticBusinessIntent{
		ContractVersion: 1,
		Authority:       authority,
		TargetKind:      "provider_connection_reference",
		TargetResource:  request.GetProviderConnectionReferenceId(),
		TargetStableKey: spec.GetStableKey(),
		Action:          action,
		ExpectedVersion: request.GetExpectedVersion(),
		Name:            request.GetName(),
		TypedIntentType: "controlplane.v1.ProviderConnectionReferenceSpec",
		TypedIntent:     encoded,
	})
}

// GitReconciliationIntentSHA256 связывает четыре закрытых ReconcileGit* с
// exact typed spec и reference keys. Receipt, подпись и idempotency key
// исключены; ownership source/revision/digest остаются внутри typed spec.
func GitReconciliationIntentSHA256(
	authority VerifiedCommandAuthority,
	request proto.Message,
) (string, error) {
	intent, spec, err := gitIntent(authority, request)
	if err != nil {
		return "", err
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec)
	if err != nil {
		return "", errors.New("encode Git reconciliation intent")
	}
	intent.TypedIntent = encoded
	return hashSemanticBusinessIntent(intent)
}

func gitIntent(authority VerifiedCommandAuthority, request proto.Message) (semanticBusinessIntent, proto.Message, error) {
	intent := semanticBusinessIntent{ContractVersion: 1, Authority: authority, Action: "reconcile_git"}
	var spec proto.Message
	switch value := request.(type) {
	case *controlplanev1.ReconcileGitRoleDefinitionRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "role_definition", value.GetRoleDefinitionId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.RoleDefinitionSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitRoleDefinition_FullMethodName {
			return intent, nil, errors.New("Git role definition intent is invalid")
		}
	case *controlplanev1.ReconcileGitAgentRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "agent", value.GetAgentId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.AgentSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		intent.ReferenceKeys = []string{value.GetRoleDefinitionStableKey(), value.GetInstructionSetStableKey(), value.GetProviderPoolStableKey()}
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName {
			return intent, nil, errors.New("Git agent intent is invalid")
		}
	case *controlplanev1.ReconcileGitInstructionSetRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "instruction_set", value.GetInstructionSetId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.InstructionSetSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitInstructionSet_FullMethodName {
			return intent, nil, errors.New("Git instruction set intent is invalid")
		}
	case *controlplanev1.ReconcileGitProviderPoolRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "provider_pool", value.GetProviderPoolId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.ProviderPoolSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitProviderPool_FullMethodName {
			return intent, nil, errors.New("Git provider pool intent is invalid")
		}
	default:
		return intent, nil, errors.New("Git reconciliation request type is unsupported")
	}
	if spec == nil || intent.TargetStableKey == "" {
		return intent, nil, errors.New("Git reconciliation typed intent is absent")
	}
	return intent, spec, nil
}

func hashSemanticBusinessIntent(intent semanticBusinessIntent) (string, error) {
	if uuid.Validate(intent.Authority.ActorID) != nil || uuid.Validate(intent.Authority.OrganizationID) != nil ||
		uuid.Validate(intent.Authority.ProjectID) != nil || intent.Authority.WorkloadID == "" ||
		!strings.HasPrefix(intent.Authority.FullMethod, "/controlplane.v1.ControlPlaneService/") ||
		intent.TargetKind == "" || intent.TargetStableKey == "" || intent.Action == "" || intent.Name == "" ||
		(intent.TargetResource != "" && uuid.Validate(intent.TargetResource) != nil) || len(intent.TypedIntent) == 0 {
		return "", errors.New("semantic business intent authority or target is invalid")
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return "", errors.New("encode semantic business intent")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
