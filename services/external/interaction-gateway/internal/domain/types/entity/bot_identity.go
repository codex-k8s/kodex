package entity

import (
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
)

// AgentMattermostBotIdentity хранит raw provider IDs только внутри trusted
// gateway boundary. IdentityRef и Selector — случайные непрозрачные refs.
type AgentMattermostBotIdentity struct {
	IdentityRef             string
	ProviderObjectRef       string
	Selector                string
	AgentRef                string
	AgentStableKey          string
	ProviderBotID           string
	ProviderUserID          string
	ProviderTeamID          string
	ProviderTokenID         string
	CredentialBindingID     string
	CredentialSecretRef     string
	CredentialSecretVersion uint64
	CredentialSHA256        string
	Username                string
	DisplayName             string
	Status                  enum.AgentBotIdentityStatus
	ProviderVersion         uint64
	ProviderGeneration      uint64
	ProviderSnapshotSHA256  string
	ProviderCausalitySHA256 string
	ObservedAt              time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type AgentMattermostBotBinding struct {
	AgentRef      string
	AgentVersion  uint64
	Identity      AgentMattermostBotIdentity
	ReceiptSHA256 string
	UpdatedAt     time.Time
}

type AgentMattermostBotCreateIntent struct {
	AgentRef             string
	ExpectedAgentVersion uint64
	Username             string
	DisplayName          string
	ProviderCorrelation  string
	RequestSHA256        string
}

type AgentMattermostBotOperation struct {
	ID                    string
	Principal             TeamPrincipal
	Action                string
	IdempotencyKey        string
	AgentRef              string
	ExpectedAgentVersion  uint64
	PredecessorGeneration uint64
	IdentityRef           string
	Selector              string
	Intent                AgentMattermostBotCreateIntent
	RequestSHA256         string
	State                 enum.AgentBotIdentityOperationState
	Identity              AgentMattermostBotIdentity
	Result                AgentMattermostBotBinding
	ReceiptID             string
	ReceiptRevision       uint64
	ReceiptSHA256         string
	CommandIntentSHA256   string
	Fence                 uint64
	LeaseToken            string
	FailureCode           string
	RetryNotBefore        time.Time
	RecoveryDeadline      time.Time
	EffectStartedAt       time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}
