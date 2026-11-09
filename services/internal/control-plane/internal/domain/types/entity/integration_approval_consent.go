package entity

import "time"

// IntegrationApprovalConsent exposes only a self-owned human consent and its immutable object identity, never a credential, execution grant or caller-selected subject.
// IntegrationApprovalConsent показывает только собственное human consent и immutable object identity без credential, execution grant либо caller-selected subject.
type IntegrationApprovalConsent struct {
	Ref             string     `json:"ref"`
	Version         int64      `json:"version"`
	ConnectionRef   string     `json:"connectionRef"`
	ConnectionName  string     `json:"connectionName"`
	CapabilityKey   string     `json:"capabilityKey"`
	AgentRef        string     `json:"agentRef"`
	RootRunRef      string     `json:"rootRunRef"`
	Lifetime        string     `json:"lifetime"`
	State           string     `json:"state"`
	ScopePaths      []string   `json:"scopePaths"`
	ScopeValuesJSON string     `json:"scopeValuesJSON"`
	MaxEffects      int64      `json:"maxEffects"`
	ReservedEffects int64      `json:"reservedEffects"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	RevokedAt       *time.Time `json:"revokedAt,omitempty"`
}
