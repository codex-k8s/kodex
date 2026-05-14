package client

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// ReviewInlineComment is a typed inline review comment ready for provider execution.
type ReviewInlineComment struct {
	Path                       string
	Body                       string
	Line                       *int64
	StartLine                  *int64
	Side                       string
	StartSide                  string
	InReplyToProviderCommentID string
}

// WriteRequest carries one typed provider write command through the shared executor pipeline.
type WriteRequest struct {
	Credential                 AccountCredential
	CommandID                  string
	TargetRef                  string
	ProviderSlug               enum.ProviderSlug
	CreateIssue                *CreateIssueCommand
	UpdateIssue                *UpdateIssueCommand
	CreateComment              *CreateCommentCommand
	UpdateComment              *UpdateCommentCommand
	CreatePullRequest          *CreatePullRequestCommand
	UpdatePullRequest          *UpdatePullRequestCommand
	CreateBootstrapPullRequest *CreateBootstrapPullRequestCommand
	CreateReviewSignal         *CreateReviewSignalCommand
	UpdateRelationship         *UpdateRelationshipCommand
}

// CreateIssueCommand describes one provider-native create issue request.
type CreateIssueCommand struct {
	ProjectID              string
	RepositoryID           string
	RepositoryTarget       Target
	Title                  string
	Body                   string
	Labels                 []string
	AssigneeProviderLogins []string
	Milestone              string
	WorkItemType           string
	WatermarkJSON          []byte
}

// UpdateIssueCommand describes one provider-native issue update.
type UpdateIssueCommand struct {
	Target                  Target
	Title                   *string
	Body                    *string
	Labels                  *value.StringListPatch
	AssigneeProviderLogins  *value.StringListPatch
	Milestone               *string
	State                   *string
	WorkItemType            *string
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
}

// CreateCommentCommand describes one provider-native comment creation.
type CreateCommentCommand struct {
	Target Target
	Body   string
}

// UpdateCommentCommand describes one provider-native comment update.
type UpdateCommentCommand struct {
	Target                  Target
	ProviderCommentID       string
	Body                    string
	ExpectedProviderVersion string
}

// CreatePullRequestCommand describes one provider-native pull request creation.
type CreatePullRequestCommand struct {
	ProjectID        string
	RepositoryID     string
	RepositoryTarget Target
	Title            string
	Body             string
	HeadBranch       string
	BaseBranch       string
	Draft            bool
	Labels           []string
	LinkedIssueRef   string
	WatermarkJSON    []byte
}

// BootstrapFile describes one prepared repository file to write into a bootstrap branch.
type BootstrapFile struct {
	Path       string
	Content    string
	Executable bool
}

// CreateBootstrapPullRequestCommand writes prepared files to a bootstrap branch and opens or updates PR.
type CreateBootstrapPullRequestCommand struct {
	ProjectID        string
	RepositoryID     string
	RepositoryTarget Target
	BaseBranch       string
	BootstrapBranch  string
	CommitMessage    string
	Title            string
	Body             string
	Draft            bool
	Files            []BootstrapFile
	WatermarkJSON    []byte
}

// UpdatePullRequestCommand describes one provider-native pull request update.
type UpdatePullRequestCommand struct {
	Target                  Target
	Title                   *string
	Body                    *string
	Labels                  *value.StringListPatch
	AssigneeProviderLogins  *value.StringListPatch
	Milestone               *string
	State                   *string
	BaseBranch              *string
	MaintainerCanModify     *bool
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
}

// ReviewSignalKind classifies provider-native review actions.
type ReviewSignalKind string

const (
	ReviewSignalKindComment          ReviewSignalKind = "comment"
	ReviewSignalKindApproval         ReviewSignalKind = "approval"
	ReviewSignalKindChangesRequested ReviewSignalKind = "changes_requested"
)

// CreateReviewSignalCommand describes one provider-native review action.
type CreateReviewSignalCommand struct {
	Target         Target
	Kind           ReviewSignalKind
	Body           string
	InlineComments []ReviewInlineComment
}

// UpdateRelationshipCommand describes one relationship upsert in the mirrored provider graph.
type UpdateRelationshipCommand struct {
	Source            Target
	Target            *Target
	TargetProviderRef string
	RelationshipType  string
	SourceKind        enum.RelationshipSource
	Confidence        enum.RelationshipConfidence
}

// RelationshipResult describes one provider relationship projection to upsert after a command.
type RelationshipResult struct {
	Source            Target
	Target            *Target
	TargetProviderRef string
	RelationshipType  string
	SourceKind        enum.RelationshipSource
	Confidence        enum.RelationshipConfidence
}

// Target is a normalized provider-native object reference used by write commands.
type Target struct {
	ProviderSlug         enum.ProviderSlug
	RepositoryFullName   string
	ProviderRepositoryID string
	WorkItemKind         enum.WorkItemKind
	Number               int64
	ProviderObjectID     string
	WebURL               string
}

// WriteResult is the safe normalized outcome produced by a provider write executor.
type WriteResult struct {
	ResultRef              string
	ProviderObjectID       string
	ProviderVersion        string
	WorkItem               *value.ProviderWorkItemSnapshot
	Comment                *value.ProviderCommentSnapshot
	Relationship           *RelationshipResult
	WorkItemProjectionID   *uuid.UUID
	CommentProjectionID    *uuid.UUID
	RelationshipID         *uuid.UUID
	ReconciliationEnqueued bool
}

// WriteExecutor isolates provider-specific write execution behind the shared pipeline.
type WriteExecutor interface {
	ProviderSlug() enum.ProviderSlug
	Execute(context.Context, WriteRequest) (WriteResult, error)
}
