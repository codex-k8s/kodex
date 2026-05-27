// Package value contains immutable value objects used by provider-hub.
package value

import (
	"time"

	"github.com/google/uuid"

	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
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
	CommandID              uuid.UUID
	IdempotencyKey         string
	ExpectedVersion        *int64
	Actor                  Actor
	Reason                 string
	RequestID              string
	RequestContext         RequestContext
	OccurredAt             time.Time
	OperationPolicyContext ProviderOperationPolicyContext
	ApprovalGateRef        ApprovalGateReference
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

// ProviderEventPayload is generated from AsyncAPI and used by provider events.
type ProviderEventPayload = providerevents.Payload

// ProviderWebhookFactKind identifies the neutral artifact kind extracted from a provider webhook.
type ProviderWebhookFactKind string

const (
	ProviderWebhookFactKindWorkItem ProviderWebhookFactKind = "work_item"
	ProviderWebhookFactKindComment  ProviderWebhookFactKind = "comment"
)

// ProviderWebhookFacts contains provider-neutral data extracted by a provider adapter.
type ProviderWebhookFacts struct {
	FactKind             ProviderWebhookFactKind
	ProviderWorkItemID   string
	ProviderCommentID    string
	Kind                 string
	Number               int64
	RepositoryFullName   string
	RepositoryProviderID string
	OccurredAt           time.Time
	WorkItem             *ProviderWorkItemSnapshot
	Comment              *ProviderCommentSnapshot
	MergeSignal          *ProviderRepositoryMergeSignalSnapshot
}

// ProviderWorkItemSnapshot is a provider-neutral snapshot extracted from webhook or sync payload.
type ProviderWorkItemSnapshot struct {
	ProjectID          string
	RepositoryID       string
	ProviderSlug       string
	ProviderWorkItemID string
	RepositoryFullName string
	Kind               string
	Number             int64
	URL                string
	Title              string
	State              string
	Body               string
	Labels             []string
	Assignees          []string
	Milestone          string
	ProviderUpdatedAt  time.Time
}

// ProviderCommentSnapshot is a provider-neutral comment-like snapshot.
type ProviderCommentSnapshot struct {
	ProviderSlug       string
	ProviderCommentID  string
	ProviderWorkItemID string
	Kind               string
	ReviewState        string
	AuthorLogin        string
	Body               string
	ProviderCreatedAt  time.Time
	ProviderUpdatedAt  time.Time
}

// ProviderRepositoryMergeSignalSnapshot contains safe provider refs from a merged PR webhook.
type ProviderRepositoryMergeSignalSnapshot struct {
	PullRequestProviderID string
	PullRequestURL        string
	BaseBranch            string
	HeadBranch            string
	MergeCommitSHA        string
	SourceRef             string
	MergedAt              time.Time
}
