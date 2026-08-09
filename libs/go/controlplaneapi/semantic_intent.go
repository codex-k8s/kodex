package controlplaneapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

type ProviderCredentialMaterialization struct {
	CredentialBindingID  string    `json:"credential_binding_id"`
	BindingVersion       uint64    `json:"binding_version"`
	CredentialGeneration uint64    `json:"credential_generation"`
	Provider             string    `json:"provider"`
	ProviderObjectRef    string    `json:"provider_object_ref"`
	SecretRef            string    `json:"secret_ref"`
	SecretVersion        uint64    `json:"secret_version"`
	SecretContentSHA256  string    `json:"secret_content_sha256"`
	MaskedAccount        string    `json:"masked_account"`
	MaskedLabel          string    `json:"masked_label"`
	Capabilities         []string  `json:"capabilities"`
	ObservedUsage        uint64    `json:"observed_usage"`
	ObservedLimit        uint64    `json:"observed_limit"`
	ObservationRevision  uint64    `json:"observation_revision"`
	ObservedAt           time.Time `json:"observed_at"`
	WindowSeconds        uint64    `json:"window_seconds"`
	ResetsAt             time.Time `json:"resets_at"`
	ObservationExpiresAt time.Time `json:"observation_expires_at"`
	ObservationSHA256    string    `json:"observation_sha256"`
}

func ProviderCredentialMaterializationSHA256(value ProviderCredentialMaterialization) (string, error) {
	if uuid.Validate(value.CredentialBindingID) != nil || value.BindingVersion == 0 || value.CredentialGeneration == 0 || value.Provider == "" ||
		value.ProviderObjectRef == "" || value.SecretRef == "" || value.SecretVersion == 0 ||
		len(value.SecretContentSHA256) != 64 || value.MaskedAccount == "" || value.MaskedLabel == "" ||
		len(value.Capabilities) == 0 || value.ObservedLimit == 0 || value.ObservedUsage > value.ObservedLimit ||
		value.ObservationRevision == 0 || value.ObservedAt.IsZero() || value.WindowSeconds == 0 ||
		value.ResetsAt.IsZero() || !value.ObservationExpiresAt.After(value.ObservedAt) || !semanticDigest(value.SecretContentSHA256) || !semanticDigest(value.ObservationSHA256) {
		return "", errors.New("provider credential materialization is invalid")
	}
	value.Capabilities = append([]string(nil), value.Capabilities...)
	sort.Strings(value.Capabilities)
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("encode provider credential materialization")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func semanticDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

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

// ProviderPoolIntentSHA256 связывает provider observation receipt с exact
// owner command и immutable eligibility/capacity snapshot. Transport proof и
// provider_receipt не входят в canonical bytes.
func ProviderPoolIntentSHA256(
	authority VerifiedCommandAuthority,
	request *controlplanev1.ManageProviderPoolRequest,
) (string, error) {
	if request == nil || request.GetSpec() == nil ||
		authority.FullMethod != controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName {
		return "", errors.New("provider pool intent is invalid")
	}
	action := strings.ToLower(strings.TrimPrefix(request.GetAction().String(), "PROVIDER_POOL_ACTION_"))
	if action != "create" && action != "update" && action != "archive" && action != "delete" {
		return "", errors.New("provider pool action is invalid")
	}
	spec := proto.Clone(request.GetSpec()).(*controlplanev1.ProviderPoolSpec)
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(spec)
	if err != nil {
		return "", errors.New("encode provider pool intent")
	}
	return hashSemanticBusinessIntent(semanticBusinessIntent{
		ContractVersion: 1, Authority: authority, TargetKind: "provider_pool",
		TargetResource: request.GetProviderPoolId(), TargetStableKey: spec.GetStableKey(),
		Action: action, ExpectedVersion: request.GetExpectedVersion(), Name: request.GetName(),
		TypedIntentType: "controlplane.v1.ProviderPoolSpec", TypedIntent: encoded,
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
			return intent, nil, errors.New("git role definition intent is invalid")
		}
	case *controlplanev1.ReconcileGitAgentRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "agent", value.GetAgentId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.AgentSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		intent.ReferenceKeys = []string{value.GetRoleDefinitionStableKey(), value.GetInstructionSetStableKey(), value.GetProviderPoolStableKey()}
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName {
			return intent, nil, errors.New("git agent intent is invalid")
		}
	case *controlplanev1.ReconcileGitInstructionSetRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "instruction_set", value.GetInstructionSetId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.InstructionSetSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitInstructionSet_FullMethodName {
			return intent, nil, errors.New("git instruction set intent is invalid")
		}
	case *controlplanev1.ReconcileGitProviderPoolRequest:
		intent.TargetKind, intent.TargetResource, intent.ExpectedVersion, intent.Name = "provider_pool", value.GetProviderPoolId(), value.GetExpectedVersion(), value.GetName()
		intent.TypedIntentType, intent.TargetStableKey, spec = "controlplane.v1.ProviderPoolSpec", value.GetSpec().GetStableKey(), value.GetSpec()
		if authority.FullMethod != controlplanev1.ControlPlaneService_ReconcileGitProviderPool_FullMethodName {
			return intent, nil, errors.New("git provider pool intent is invalid")
		}
	default:
		return intent, nil, errors.New("git reconciliation request type is unsupported")
	}
	if spec == nil || intent.TargetStableKey == "" {
		return intent, nil, errors.New("git reconciliation typed intent is absent")
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
