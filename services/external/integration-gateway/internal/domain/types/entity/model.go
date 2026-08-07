package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
)

type Definition struct {
	ID        string
	Version   uint64
	Digest    string
	Source    []byte
	Tools     []Tool
	CreatedAt time.Time
}

type Tool struct {
	Name              string
	Version           uint64
	Description       string
	Capability        string
	Risk              enum.RiskLevel
	Permission        string
	ApprovalPolicy    enum.ApprovalPolicy
	Idempotency       enum.IdempotencyMode
	InputSchema       json.RawMessage
	OutputSchema      json.RawMessage
	RedactionPointers []string
	DirectDelivery    *DirectDelivery
	HTTP              HTTPAdapter
}

type DirectDelivery struct {
	Reference        string
	CLINames         []string
	EnvironmentNames []string
}

type HTTPAdapter struct {
	Method            string
	Path              string
	Timeout           time.Duration
	IdempotencyHeader string
	CredentialHeaders map[string]string
}

type Connection struct {
	ID                    string
	TenantID              string
	ProjectID             string
	IntegrationID         string
	IntegrationVersion    uint64
	IntegrationDigest     string
	DefinitionID          string
	DefinitionVersion     uint64
	EndpointRef           string
	EndpointURL           string
	CredentialBindingRefs []CredentialBinding
	Revision              uint64
	Generation            uint64
	Status                enum.ConnectionStatus
	ValidationCode        enum.ValidationCode
	ValidatedAt           *time.Time
	ExpiresAt             *time.Time
}

type CredentialBinding struct {
	ID               string
	Version          uint64
	Revision         uint64
	ProjectionDigest string
	Purpose          string
	SecretRef        string
	PrincipalRef     string
	ExpiresAt        *time.Time
}

func ConnectionBindingDigest(connection Connection) string {
	bindings := slices.Clone(connection.CredentialBindingRefs)
	sort.Slice(bindings, func(left, right int) bool {
		if bindings[left].ID == bindings[right].ID {
			return bindings[left].Version < bindings[right].Version
		}
		return bindings[left].ID < bindings[right].ID
	})
	canonical, err := json.Marshal(struct {
		ID                 string              `json:"id"`
		Revision           uint64              `json:"revision"`
		Generation         uint64              `json:"generation"`
		IntegrationID      string              `json:"integration_id"`
		IntegrationVersion uint64              `json:"integration_version"`
		IntegrationDigest  string              `json:"integration_digest"`
		DefinitionID       string              `json:"definition_id"`
		DefinitionVersion  uint64              `json:"definition_version"`
		EndpointRef        string              `json:"endpoint_ref"`
		Bindings           []CredentialBinding `json:"credential_bindings"`
	}{
		connection.ID, connection.Revision, connection.Generation, connection.IntegrationID,
		connection.IntegrationVersion, connection.IntegrationDigest, connection.DefinitionID,
		connection.DefinitionVersion, connection.EndpointRef, bindings,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

type Grant struct {
	ID                     string
	TenantID               string
	ProjectID              string
	ProcessID              string
	SessionID              string
	SessionVersion         uint64
	ThreadID               string
	TurnID                 string
	TurnVersion            uint64
	Attempt                uint32
	InputDigest            string
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	RuntimeRevisionDigest  string
	RuntimeManifestDigest  string
	RoleID                 string
	RoleVersion            uint64
	IntegrationID          string
	ConnectionID           string
	Capabilities           []string
	Permissions            []string
	Generation             uint64
	Status                 enum.GrantStatus
	ExpiresAt              time.Time
}

type TransportSession struct {
	ID                     string
	TenantID               string
	ProjectID              string
	ProcessID              string
	AgentSessionID         string
	AgentSessionVersion    uint64
	ThreadID               string
	TurnID                 string
	TurnVersion            uint64
	Attempt                uint32
	InputDigest            string
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	RuntimeRevisionDigest  string
	RuntimeManifestDigest  string
	RoleID                 string
	RoleVersion            uint64
	GrantGeneration        uint64
	TokenDigest            string
	Status                 enum.SessionStatus
	RequestCount           uint64
	ConcurrentRequests     uint32
	ExpiresAt              time.Time
	LastSeenAt             time.Time
}

type Invocation struct {
	ID                      string
	TenantID                string
	ProjectID               string
	TransportSessionID      string
	ProcessID               string
	AgentSessionID          string
	AgentSessionVersion     uint64
	ThreadID                string
	TurnID                  string
	TurnVersion             uint64
	Attempt                 uint32
	InputDigest             string
	RuntimeRevisionID       string
	RuntimeRevisionVersion  uint64
	RuntimeRevisionDigest   string
	RuntimeManifestDigest   string
	RoleID                  string
	RoleVersion             uint64
	DefinitionID            string
	DefinitionVersion       uint64
	DefinitionDigest        string
	ConnectionID            string
	ConnectionRevision      uint64
	ConnectionGeneration    uint64
	ConnectionBindingDigest string
	PinnedConnection        Connection
	PinnedTool              Tool
	GrantID                 string
	GrantGeneration         uint64
	Capability              string
	ToolName                string
	ToolVersion             uint64
	Risk                    enum.RiskLevel
	Permission              string
	SemanticKey             string
	CanonicalRequestHash    string
	EncryptedArguments      []byte
	Preview                 json.RawMessage
	Status                  enum.InvocationStatus
	CreatedAt               time.Time
	ExpiresAt               time.Time
	UpdatedAt               time.Time
}

type Approval struct {
	ID                 string
	InvocationID       string
	Version            uint64
	RequestHash        string
	Preview            json.RawMessage
	Status             enum.ApprovalStatus
	DecidedBy          string
	DecisionReasonCode string
	ExpiresAt          time.Time
	DecidedAt          *time.Time
}

type ExecutionAttempt struct {
	ID                     string
	InvocationID           string
	Number                 uint32
	Fence                  uint64
	ConnectionGeneration   uint64
	GrantGeneration        uint64
	ProviderIdempotencyKey string
	StartedAt              time.Time
	ProviderDispatchedAt   *time.Time
	FinishedAt             *time.Time
	Outcome                enum.InvocationStatus
}

type PinnedCredentialBinding struct {
	ID      string
	Version uint64
	Digest  string
}

// ContinuationEffect — зашифрованный durable command к авторитетному
// control-plane. Поля caller authority отсутствуют: exact tuple закреплён
// application grant и повторно разрешается владельцем домена.
type ContinuationEffect struct {
	InvocationID              string
	TenantID                  string
	ProjectID                 string
	ApprovalID                string
	RequestDigest             string
	IntegrationID             string
	IntegrationVersion        uint64
	IntegrationDigest         string
	CredentialBindings        []PinnedCredentialBinding
	EncryptedApplicationGrant []byte
	ApplicationGrantExpiresAt time.Time
	ContinuationID            string
	Version                   uint64
	Fence                     uint64
	ApprovalState             string
	ExecutionState            string
	ContinuationState         string
	Action                    enum.ContinuationAction
	DesiredAction             enum.ContinuationAction
	AvailableAt               time.Time
	LeaseID                   string
	LeaseFence                uint64
	LeaseExpiresAt            time.Time
	Attempts                  uint32
}

type Result struct {
	InvocationID     string
	AttemptID        string
	Status           enum.InvocationStatus
	EncryptedPayload []byte
	PayloadDigest    string
	ProviderReceipt  string
	DeliveryVersion  uint64
	DeliveryFence    uint64
	AcknowledgedAt   *time.Time
	CompletedAt      time.Time
}

type AuditEvent struct {
	ID           string
	TenantID     string
	ProjectID    string
	ActorID      string
	Action       string
	ResourceKind string
	ResourceID   string
	RequestHash  string
	Outcome      string
	ReasonCode   string
	OccurredAt   time.Time
}
