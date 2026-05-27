// Package value contains governance-manager value objects.
package value

// ExternalRef points to another owner domain without copying its state.
type ExternalRef struct {
	Type string
	Ref  string
}

// Actor identifies a user, service, agent or external account.
type Actor struct {
	Type string
	ID   string
}

// RequestContext stores safe request metadata for audit and access checks.
type RequestContext struct {
	Source       string
	TraceID      string
	SessionID    string
	ClientIPHash string
}

// LocalizedText carries text for one locale.
type LocalizedText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

// ProjectContextRef carries project-catalog refs used by governance.
type ProjectContextRef struct {
	ProjectRef       string `json:"project_ref,omitempty"`
	RepositoryRef    string `json:"repository_ref,omitempty"`
	ServiceRef       string `json:"service_ref,omitempty"`
	BranchRulesRef   string `json:"branch_rules_ref,omitempty"`
	ReleasePolicyRef string `json:"release_policy_ref,omitempty"`
	ReleaseLineRef   string `json:"release_line_ref,omitempty"`
}

// InteractionDeliveryRef points to interaction-hub delivery and callback facts.
type InteractionDeliveryRef struct {
	RequestRef  string `json:"request_ref,omitempty"`
	DeliveryRef string `json:"delivery_ref,omitempty"`
	CallbackRef string `json:"callback_ref,omitempty"`
	DecisionRef string `json:"decision_ref,omitempty"`
}

// EvidenceRef points to bounded evidence without embedding provider payloads, secrets or full logs.
type EvidenceRef struct {
	Kind           string `json:"kind"`
	Ref            string `json:"ref"`
	Summary        string `json:"summary"`
	Digest         string `json:"digest,omitempty"`
	RetentionClass string `json:"retention_class,omitempty"`
}

// ReleaseIntegrationRef links release governance to a bounded fact owned by another domain.
type ReleaseIntegrationRef struct {
	Domain     string `json:"domain"`
	Kind       string `json:"kind"`
	Ref        string `json:"ref"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Digest     string `json:"digest,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
	Version    string `json:"version,omitempty"`
}

// RiskEvaluationSummary carries bounded classifier inputs without raw payloads.
type RiskEvaluationSummary struct {
	ChangedFilesSummaryRef string                 `json:"changed_files_summary_ref,omitempty"`
	Summary                string                 `json:"summary,omitempty"`
	Factors                []RiskEvaluationFactor `json:"factors,omitempty"`
}

// RiskEvaluationFactor is one safe input fact for the risk classifier.
type RiskEvaluationFactor struct {
	SourceType string   `json:"source_type"`
	Ref        string   `json:"ref"`
	Summary    string   `json:"summary"`
	Tags       []string `json:"tags,omitempty"`
}
