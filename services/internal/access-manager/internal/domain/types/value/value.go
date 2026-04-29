// Package value contains immutable value objects used by access-manager.
package value

import (
	"time"

	"github.com/google/uuid"
)

// Actor identifies the principal that initiated a command.
type Actor struct {
	Type string
	ID   string
}

// CommandMeta carries idempotency, concurrency and audit metadata.
type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           Actor
	Reason          string
	RequestID       string
	OccurredAt      time.Time
}

// SubjectRef references a subject without importing its aggregate model.
type SubjectRef struct {
	Type string
	ID   string
}

// ResourceRef references a protected resource in access checks.
type ResourceRef struct {
	Type string
	ID   string
}

// ScopeRef references the scope where a policy is evaluated.
type ScopeRef struct {
	Type string
	ID   string
}

// RuleExplanation describes one rule that affected an access decision.
type RuleExplanation struct {
	RuleID     uuid.UUID
	Effect     string
	Subject    SubjectRef
	ActionKey  string
	Scope      ScopeRef
	Priority   int
	ReasonCode string
}

// DecisionExplanation is the machine-readable explanation for an access decision.
type DecisionExplanation struct {
	Decision      string
	ReasonCode    string
	PolicyVersion int64
	MatchedRules  []RuleExplanation
}

// AccessEventPayload is the common typed payload envelope for access events.
type AccessEventPayload struct {
	OrganizationID           string `json:"organization_id,omitempty"`
	Kind                     string `json:"kind,omitempty"`
	Status                   string `json:"status,omitempty"`
	OldStatus                string `json:"old_status,omitempty"`
	NewStatus                string `json:"new_status,omitempty"`
	Version                  int64  `json:"version,omitempty"`
	GroupID                  string `json:"group_id,omitempty"`
	ScopeType                string `json:"scope_type,omitempty"`
	ScopeID                  string `json:"scope_id,omitempty"`
	MembershipID             string `json:"membership_id,omitempty"`
	SubjectType              string `json:"subject_type,omitempty"`
	SubjectID                string `json:"subject_id,omitempty"`
	TargetType               string `json:"target_type,omitempty"`
	TargetID                 string `json:"target_id,omitempty"`
	AllowlistEntryID         string `json:"allowlist_entry_id,omitempty"`
	MatchType                string `json:"match_type,omitempty"`
	UserID                   string `json:"user_id,omitempty"`
	IdentityID               string `json:"identity_id,omitempty"`
	IdentityProvider         string `json:"identity_provider,omitempty"`
	ExternalProviderID       string `json:"external_provider_id,omitempty"`
	Slug                     string `json:"slug,omitempty"`
	ExternalAccountID        string `json:"external_account_id,omitempty"`
	AccountType              string `json:"account_type,omitempty"`
	SecretBindingRefID       string `json:"secret_binding_ref_id,omitempty"`
	StoreType                string `json:"store_type,omitempty"`
	ExternalAccountBindingID string `json:"external_account_binding_id,omitempty"`
	UsageScopeType           string `json:"usage_scope_type,omitempty"`
	UsageScopeID             string `json:"usage_scope_id,omitempty"`
	AccessActionID           string `json:"access_action_id,omitempty"`
	ActionKey                string `json:"action_key,omitempty"`
	AccessRuleID             string `json:"access_rule_id,omitempty"`
	Effect                   string `json:"effect,omitempty"`
	AccessDecisionAuditID    string `json:"access_decision_audit_id,omitempty"`
	Decision                 string `json:"decision,omitempty"`
	ReasonCode               string `json:"reason_code,omitempty"`
}
