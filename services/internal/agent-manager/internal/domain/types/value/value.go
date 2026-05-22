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

type GuidanceRef struct {
	PackageInstallationRef string `json:"package_installation_ref"`
	PackageVersionRef      string `json:"package_version_ref"`
	ManifestDigest         string `json:"manifest_digest"`
	SourceRef              string `json:"source_ref,omitempty"`
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
