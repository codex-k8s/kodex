package value

import (
	"time"
)

// ProviderOperationRiskLevel is the caller-computed risk classification for one provider command.
type ProviderOperationRiskLevel string

const (
	ProviderOperationRiskLevelLow      ProviderOperationRiskLevel = "low"
	ProviderOperationRiskLevelMedium   ProviderOperationRiskLevel = "medium"
	ProviderOperationRiskLevelHigh     ProviderOperationRiskLevel = "high"
	ProviderOperationRiskLevelCritical ProviderOperationRiskLevel = "critical"
)

// ProviderOperationPolicyContext carries the safe risk-policy snapshot accepted for one provider command.
type ProviderOperationPolicyContext struct {
	ProjectID         string                     `json:"project_id,omitempty"`
	RepositoryID      string                     `json:"repository_id,omitempty"`
	Stage             string                     `json:"stage,omitempty"`
	RoleID            string                     `json:"role_id,omitempty"`
	RoleKey           string                     `json:"role_key,omitempty"`
	OperationType     string                     `json:"operation_type,omitempty"`
	TargetRef         string                     `json:"target_ref,omitempty"`
	ChangedFields     []string                   `json:"changed_fields,omitempty"`
	RiskTags          []string                   `json:"risk_tags,omitempty"`
	RiskLevel         ProviderOperationRiskLevel `json:"risk_level,omitempty"`
	ApprovalRequired  bool                       `json:"approval_required,omitempty"`
	PolicyVersion     string                     `json:"policy_version,omitempty"`
	PolicySnapshotRef string                     `json:"policy_snapshot_ref,omitempty"`
}

// ApprovalGateReference points to an approval or gate decision owned by another service.
type ApprovalGateReference struct {
	ApprovalID       string     `json:"approval_id,omitempty"`
	GateType         string     `json:"gate_type,omitempty"`
	Decision         string     `json:"decision,omitempty"`
	DecidedByActorID string     `json:"decided_by_actor_id,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
	EvidenceRef      string     `json:"evidence_ref,omitempty"`
	PolicyVersion    string     `json:"policy_version,omitempty"`
}

// StringListPatch distinguishes between "do not change" and "replace with empty list".
type StringListPatch struct {
	Values []string
}
