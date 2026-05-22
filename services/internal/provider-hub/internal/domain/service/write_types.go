package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// ProviderTarget identifies a provider-native object for write commands.
type ProviderTarget struct {
	ProviderSlug         enum.ProviderSlug
	RepositoryFullName   string
	ProviderRepositoryID string
	WorkItemKind         enum.WorkItemKind
	Number               int64
	ProviderObjectID     string
	WebURL               string
}

// CreateIssueInput describes one typed issue creation command.
type CreateIssueInput struct {
	ProjectID              uuid.UUID
	RepositoryID           uuid.UUID
	ProviderSlug           enum.ProviderSlug
	RepositoryTarget       ProviderTarget
	Title                  string
	Body                   string
	Labels                 []string
	AssigneeProviderLogins []string
	Milestone              *string
	WorkItemType           *string
	WatermarkJSON          []byte
	Meta                   value.CommandMeta
	ExternalAccountID      uuid.UUID
}

// UpdateIssueInput describes one typed issue update command.
type UpdateIssueInput struct {
	Target                  ProviderTarget
	Title                   *string
	Body                    *string
	Labels                  *value.StringListPatch
	AssigneeProviderLogins  *value.StringListPatch
	Milestone               *string
	State                   *string
	WorkItemType            *string
	WatermarkJSON           *[]byte
	ExpectedProviderVersion string
	Meta                    value.CommandMeta
	ExternalAccountID       uuid.UUID
}

// CreateCommentInput describes one typed comment creation command.
type CreateCommentInput struct {
	Target            ProviderTarget
	Body              string
	Meta              value.CommandMeta
	ExternalAccountID uuid.UUID
}

// UpdateCommentInput describes one typed comment update command.
type UpdateCommentInput struct {
	Target                  ProviderTarget
	ProviderCommentID       string
	Body                    string
	ExpectedProviderVersion string
	Meta                    value.CommandMeta
	ExternalAccountID       uuid.UUID
}

// CreatePullRequestInput describes one typed PR/MR creation command.
type CreatePullRequestInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	ProviderSlug      enum.ProviderSlug
	RepositoryTarget  ProviderTarget
	Title             string
	Body              string
	HeadBranch        string
	BaseBranch        string
	Draft             bool
	Labels            []string
	LinkedIssueRef    *string
	WatermarkJSON     []byte
	Meta              value.CommandMeta
	ExternalAccountID uuid.UUID
}

// CreateRepositoryInput describes one provider-native repository creation command.
type CreateRepositoryInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	ProviderSlug      enum.ProviderSlug
	OwnerKind         enum.RepositoryOwnerKind
	ProviderOwner     *string
	RepositoryName    string
	Visibility        enum.RepositoryVisibility
	Description       *string
	Meta              value.CommandMeta
	ExternalAccountID uuid.UUID
}

// BootstrapFile describes one prepared text file for bootstrap branch creation.
type BootstrapFile struct {
	Path       string
	Content    string
	Executable bool
}

// CreateBootstrapPullRequestInput describes provider-side bootstrap PR creation for an existing empty repository.
type CreateBootstrapPullRequestInput struct {
	ProjectID         uuid.UUID
	RepositoryID      uuid.UUID
	ProviderSlug      enum.ProviderSlug
	RepositoryTarget  ProviderTarget
	BaseBranch        string
	BootstrapBranch   string
	CommitMessage     string
	Title             string
	Body              string
	Draft             bool
	Files             []BootstrapFile
	WatermarkJSON     []byte
	Meta              value.CommandMeta
	ExternalAccountID uuid.UUID
}

// UpdatePullRequestInput describes one typed PR/MR update command.
type UpdatePullRequestInput struct {
	Target                  ProviderTarget
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
	Meta                    value.CommandMeta
	ExternalAccountID       uuid.UUID
}

// CreateReviewSignalInput describes one typed review-signal command.
type CreateReviewSignalInput struct {
	Target            ProviderTarget
	Kind              enum.ReviewSignalKind
	Body              string
	InlineComments    []ProviderInlineComment
	Meta              value.CommandMeta
	ExternalAccountID uuid.UUID
}

// UpdateRelationshipInput describes one relationship upsert command.
type UpdateRelationshipInput struct {
	Source            ProviderTarget
	Target            *ProviderTarget
	TargetProviderRef *string
	RelationshipType  string
	SourceKind        enum.RelationshipSource
	Confidence        enum.RelationshipConfidence
	Meta              value.CommandMeta
	ExternalAccountID uuid.UUID
}

// ProviderOperationCommandResult is the safe command result returned to callers.
type ProviderOperationCommandResult struct {
	Target                 *ProviderTarget
	ResultRef              string
	ProviderObjectID       string
	ProviderVersion        string
	ReconciliationEnqueued bool
	EmittedEventTypes      []string
	BaseBranch             string
}

// ProviderOperationResult returns the final audited provider operation and optional projections.
type ProviderOperationResult struct {
	ProviderOperation  *entity.ProviderOperation
	WorkItemProjection *entity.ProviderWorkItemProjection
	CommentProjection  *entity.ProviderCommentProjection
	Relationship       *entity.ProviderRelationship
	Result             ProviderOperationCommandResult
}

// ProviderInlineComment is one typed inline review comment accepted from transport.
type ProviderInlineComment struct {
	Path                       string
	Body                       string
	Line                       *int64
	StartLine                  *int64
	Side                       string
	StartSide                  string
	InReplyToProviderCommentID string
}
