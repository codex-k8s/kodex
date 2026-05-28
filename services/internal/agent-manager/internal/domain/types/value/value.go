// Package value contains agent-manager value objects.
package value

import "github.com/google/uuid"

type Actor struct {
	Type string
	ID   string
}

type LocalizedText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

type ScopeRef struct {
	Type string
	Ref  string
}

type ObjectRef struct {
	ObjectURI       string
	ObjectDigest    string
	ObjectSizeBytes *int64
}

type RuntimeContextRef struct {
	SlotRef      string `json:"slot_ref,omitempty"`
	JobRef       string `json:"job_ref,omitempty"`
	WorkspaceRef string `json:"workspace_ref,omitempty"`
	ContextRef   string `json:"context_ref,omitempty"`
}

type ProviderTargetRef struct {
	WorkItemRef     string `json:"work_item_ref,omitempty"`
	PullRequestRef  string `json:"pull_request_ref,omitempty"`
	CommentRef      string `json:"comment_ref,omitempty"`
	ReviewSignalRef string `json:"review_signal_ref,omitempty"`
}

type GovernanceContextRef struct {
	RiskAssessmentRef         string `json:"risk_assessment_ref,omitempty"`
	GateRequestRef            string `json:"gate_request_ref,omitempty"`
	GateDecisionRef           string `json:"gate_decision_ref,omitempty"`
	ReleaseDecisionPackageRef string `json:"release_decision_package_ref,omitempty"`
	ReleaseDecisionRef        string `json:"release_decision_ref,omitempty"`
	RiskProfileRef            string `json:"risk_profile_ref,omitempty"`
	GatePolicyRef             string `json:"gate_policy_ref,omitempty"`
	ReleasePolicyRef          string `json:"release_policy_ref,omitempty"`
}

type GuidanceRef struct {
	PackageInstallationRef string `json:"package_installation_ref"`
	PackageVersionRef      string `json:"package_version_ref"`
	ManifestDigest         string `json:"manifest_digest"`
	SourceRef              string `json:"source_ref,omitempty"`
	CapabilityRef          string `json:"capability_ref,omitempty"`
	CapabilityKind         string `json:"capability_kind,omitempty"`
	PackageRef             string `json:"package_ref,omitempty"`
	PackageSlug            string `json:"package_slug,omitempty"`
	PackageVersionLabel    string `json:"package_version_label,omitempty"`
	PolicySummaryJSON      string `json:"policy_summary_json,omitempty"`
}

type GuidanceSelectionHint struct {
	PackageInstallationRef string
	PackageSlug            string
}

type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           Actor
}

type QueryMeta struct {
	Actor Actor
	Page  PageRequest
}

type PageRequest struct {
	PageSize  int32
	PageToken string
}

type PageResult struct {
	NextPageToken string
}
