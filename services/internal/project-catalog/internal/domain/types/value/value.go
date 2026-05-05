// Package value contains immutable value objects used by project-catalog.
package value

import (
	"time"

	"github.com/google/uuid"
)

// Actor identifies the principal that initiated a command or read.
type Actor struct {
	Type string
	ID   string
}

// RequestContext stores safe request metadata for audit and diagnostics.
type RequestContext struct {
	Source       string
	TraceID      string
	SessionID    string
	ClientIPHash string
}

// CommandMeta carries idempotency, concurrency and audit metadata.
type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	Actor           Actor
	Reason          string
	RequestID       string
	RequestContext  RequestContext
	OccurredAt      time.Time
}

// QueryMeta carries actor and safe request metadata for reads.
type QueryMeta struct {
	Actor          Actor
	RequestID      string
	RequestContext RequestContext
}

// PageRequest limits list responses.
type PageRequest struct {
	PageSize  int32
	PageToken string
}

// PageResult returns list continuation state.
type PageResult struct {
	NextPageToken string
}

// ProjectEventPayload is the common typed payload envelope for project events.
type ProjectEventPayload struct {
	ProjectID         string `json:"project_id,omitempty"`
	OrganizationID    string `json:"organization_id,omitempty"`
	Slug              string `json:"slug,omitempty"`
	Status            string `json:"status,omitempty"`
	RepositoryID      string `json:"repository_id,omitempty"`
	Provider          string `json:"provider,omitempty"`
	ProviderOwner     string `json:"provider_owner,omitempty"`
	ProviderName      string `json:"provider_name,omitempty"`
	IconObjectURI     string `json:"icon_object_uri,omitempty"`
	PolicyID          string `json:"policy_id,omitempty"`
	PolicyVersion     int64  `json:"policy_version,omitempty"`
	SourceCommitSHA   string `json:"source_commit_sha,omitempty"`
	SourceBlobSHA     string `json:"source_blob_sha,omitempty"`
	ContentHash       string `json:"content_hash,omitempty"`
	OverrideID        string `json:"override_id,omitempty"`
	TargetType        string `json:"target_type,omitempty"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	SourceID          string `json:"source_id,omitempty"`
	ScopeType         string `json:"scope_type,omitempty"`
	AccessMode        string `json:"access_mode,omitempty"`
	BranchRulesID     string `json:"branch_rules_id,omitempty"`
	ReleasePolicyID   string `json:"release_policy_id,omitempty"`
	ReleaseLineID     string `json:"release_line_id,omitempty"`
	PlacementPolicyID string `json:"placement_policy_id,omitempty"`
	Version           int64  `json:"version,omitempty"`
}
