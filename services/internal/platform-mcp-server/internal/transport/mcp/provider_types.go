package mcptransport

import (
	"context"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
)

const (
	ToolProviderProjectionGet                        = "provider.projection.get"
	ToolProviderProjectionFind                       = "provider.projection.find"
	ToolProviderProjectionsList                      = "provider.projections.list"
	ToolProviderCommentsList                         = "provider.comments.list"
	ToolProviderRelationshipsList                    = "provider.relationships.list"
	ToolProviderArtifactSignalRegister               = "provider.artifact_signal.register"
	ToolProviderIssueCreate                          = "provider.issue.create"
	ToolProviderIssueUpdate                          = "provider.issue.update"
	ToolProviderCommentCreate                        = "provider.comment.create"
	ToolProviderCommentUpdate                        = "provider.comment.update"
	ToolProviderPullRequestCreate                    = "provider.pull_request.create"
	ToolProviderPullRequestUpdate                    = "provider.pull_request.update"
	ToolProviderReviewSignalCreate                   = "provider.review_signal.create"
	ToolProviderRelationshipUpdate                   = "provider.relationship.update"
	ToolProviderRepositoryCreate                     = "provider.repository.create"
	ToolProviderRepositoryBootstrapPullRequestCreate = "provider.repository.bootstrap_pull_request.create"
	ToolProviderRepositoryAdoptionPullRequestCreate  = "provider.repository.adoption_pull_request.create"
)

// ProviderHubClient is the owner route used by provider MCP tools.
type ProviderHubClient interface {
	GetWorkItemProjection(context.Context, *providersv1.GetWorkItemProjectionRequest) (*providersv1.WorkItemProjectionResponse, error)
	FindWorkItemByProviderRef(context.Context, *providersv1.FindWorkItemByProviderRefRequest) (*providersv1.WorkItemProjectionResponse, error)
	ListWorkItemProjections(context.Context, *providersv1.ListWorkItemProjectionsRequest) (*providersv1.ListWorkItemProjectionsResponse, error)
	ListComments(context.Context, *providersv1.ListCommentsRequest) (*providersv1.ListCommentsResponse, error)
	ListRelationships(context.Context, *providersv1.ListRelationshipsRequest) (*providersv1.ListRelationshipsResponse, error)
	RegisterProviderArtifactSignal(context.Context, *providersv1.RegisterProviderArtifactSignalRequest) (*providersv1.ProviderArtifactSignalResponse, error)
	CreateIssue(context.Context, *providersv1.CreateIssueRequest) (*providersv1.ProviderOperationResponse, error)
	UpdateIssue(context.Context, *providersv1.UpdateIssueRequest) (*providersv1.ProviderOperationResponse, error)
	CreateComment(context.Context, *providersv1.CreateCommentRequest) (*providersv1.ProviderOperationResponse, error)
	UpdateComment(context.Context, *providersv1.UpdateCommentRequest) (*providersv1.ProviderOperationResponse, error)
	CreatePullRequest(context.Context, *providersv1.CreatePullRequestRequest) (*providersv1.ProviderOperationResponse, error)
	UpdatePullRequest(context.Context, *providersv1.UpdatePullRequestRequest) (*providersv1.ProviderOperationResponse, error)
	CreateReviewSignal(context.Context, *providersv1.CreateReviewSignalRequest) (*providersv1.ProviderOperationResponse, error)
	UpdateRelationship(context.Context, *providersv1.UpdateRelationshipRequest) (*providersv1.ProviderOperationResponse, error)
	CreateRepository(context.Context, *providersv1.CreateRepositoryRequest) (*providersv1.ProviderOperationResponse, error)
	CreateBootstrapPullRequest(context.Context, *providersv1.CreateBootstrapPullRequestRequest) (*providersv1.ProviderOperationResponse, error)
	CreateAdoptionPullRequest(context.Context, *providersv1.CreateAdoptionPullRequestRequest) (*providersv1.ProviderOperationResponse, error)
}

// ProviderCommandMetaInput carries safe command metadata for provider-hub tools.
type ProviderCommandMetaInput struct {
	CommandID              string                              `json:"command_id,omitempty" jsonschema:"unique command identifier"`
	IdempotencyKey         string                              `json:"idempotency_key,omitempty" jsonschema:"idempotency key scoped by operation and actor"`
	ExpectedVersion        *int64                              `json:"expected_version,omitempty" jsonschema:"expected aggregate version for optimistic concurrency"`
	Actor                  ProviderActorInput                  `json:"actor" jsonschema:"authenticated caller"`
	Reason                 string                              `json:"reason" jsonschema:"machine or operator reason for audit"`
	RequestID              string                              `json:"request_id" jsonschema:"request identifier for logs and audit"`
	RequestContext         ProviderRequestContextInput         `json:"request_context" jsonschema:"safe request context"`
	OperationPolicyContext ProviderOperationPolicyContextInput `json:"operation_policy_context" jsonschema:"safe risk policy context"`
	ApprovalGateRef        ProviderApprovalGateRefInput        `json:"approval_gate_ref,omitempty" jsonschema:"already approved gate reference"`
}

// ProviderQueryMetaInput carries safe read metadata for provider-hub tools.
type ProviderQueryMetaInput struct {
	Actor          ProviderActorInput          `json:"actor" jsonschema:"authenticated caller"`
	RequestID      string                      `json:"request_id" jsonschema:"request identifier for logs and audit"`
	RequestContext ProviderRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

// ProviderActorInput identifies a user, service, agent or external account.
type ProviderActorInput struct {
	Type string `json:"type" jsonschema:"actor type such as user, service, agent or external_account"`
	ID   string `json:"id" jsonschema:"actor identifier in its owner domain"`
}

// ProviderRequestContextInput carries safe metadata and never includes tokens or secrets.
type ProviderRequestContextInput struct {
	Source       string `json:"source" jsonschema:"caller surface, for example platform-mcp-server"`
	TraceID      string `json:"trace_id,omitempty" jsonschema:"platform trace identifier"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"user or agent session identifier"`
	ClientIPHash string `json:"client_ip_hash,omitempty" jsonschema:"hashed client address"`
}

// ProviderOperationPolicyContextInput carries safe policy context for provider writes.
type ProviderOperationPolicyContextInput struct {
	ProjectID         string   `json:"project_id,omitempty" jsonschema:"project identifier used by risk policy"`
	RepositoryID      string   `json:"repository_id,omitempty" jsonschema:"repository binding identifier used by risk policy"`
	Stage             string   `json:"stage,omitempty" jsonschema:"workflow or delivery stage"`
	RoleID            string   `json:"role_id,omitempty" jsonschema:"platform role identifier"`
	RoleKey           string   `json:"role_key,omitempty" jsonschema:"stable role key"`
	OperationType     string   `json:"operation_type" jsonschema:"provider operation type"`
	TargetRef         string   `json:"target_ref,omitempty" jsonschema:"safe provider target reference"`
	ChangedFields     []string `json:"changed_fields,omitempty" jsonschema:"typed field names changed by command"`
	RiskTags          []string `json:"risk_tags,omitempty" jsonschema:"policy-specific risk tags"`
	RiskLevel         string   `json:"risk_level,omitempty" jsonschema:"risk level: low, medium, high or critical"`
	ApprovalRequired  bool     `json:"approval_required,omitempty" jsonschema:"whether approval_gate_ref must be present"`
	PolicyVersion     string   `json:"policy_version,omitempty" jsonschema:"risk policy version"`
	PolicySnapshotRef string   `json:"policy_snapshot_ref,omitempty" jsonschema:"immutable policy snapshot or decision reference"`
}

// ProviderApprovalGateRefInput references an already approved gate.
type ProviderApprovalGateRefInput struct {
	ApprovalID       string `json:"approval_id,omitempty" jsonschema:"approval or gate identifier"`
	GateType         string `json:"gate_type,omitempty" jsonschema:"gate type"`
	Decision         string `json:"decision,omitempty" jsonschema:"approved decision value"`
	DecidedByActorID string `json:"decided_by_actor_id,omitempty" jsonschema:"approving platform actor identifier"`
	DecidedAt        string `json:"decided_at,omitempty" jsonschema:"RFC3339 decision timestamp"`
	EvidenceRef      string `json:"evidence_ref,omitempty" jsonschema:"safe approval evidence reference"`
	PolicyVersion    string `json:"policy_version,omitempty" jsonschema:"policy version used by approval service"`
}

// ProviderTargetInput identifies a provider-native object.
type ProviderTargetInput struct {
	ProviderSlug         string `json:"provider_slug" jsonschema:"provider identifier such as github or gitlab"`
	RepositoryFullName   string `json:"repository_full_name,omitempty" jsonschema:"owner/name or provider equivalent"`
	ProviderRepositoryID string `json:"provider_repository_id,omitempty" jsonschema:"provider-native repository id"`
	WorkItemKind         string `json:"work_item_kind,omitempty" jsonschema:"work item kind: issue, pull_request or merge_request"`
	Number               *int64 `json:"number,omitempty" jsonschema:"provider-native issue or PR/MR number"`
	ProviderObjectID     string `json:"provider_object_id,omitempty" jsonschema:"provider-native stable object id"`
	WebURL               string `json:"web_url,omitempty" jsonschema:"safe provider URL"`
}

// ProviderPageInput limits list responses.
type ProviderPageInput = AgentPageInput

// StringListPatchInput distinguishes absent list update from replacing with empty value.
type StringListPatchInput struct {
	Present bool     `json:"present,omitempty" jsonschema:"whether to apply the replacement"`
	Values  []string `json:"values,omitempty" jsonschema:"complete replacement list"`
}

type GetProviderProjectionInput struct {
	Meta                 ProviderQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	WorkItemProjectionID string                 `json:"work_item_projection_id" jsonschema:"work item projection identifier"`
}

type FindProviderProjectionInput struct {
	Meta   ProviderQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	Target ProviderTargetInput    `json:"target" jsonschema:"provider-native object target"`
}

type ListProviderProjectionsInput struct {
	Meta               ProviderQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	ProjectID          string                 `json:"project_id,omitempty" jsonschema:"project filter"`
	RepositoryID       string                 `json:"repository_id,omitempty" jsonschema:"repository binding filter"`
	ProviderSlug       string                 `json:"provider_slug,omitempty" jsonschema:"provider filter"`
	RepositoryFullName string                 `json:"repository_full_name,omitempty" jsonschema:"repository full name filter"`
	Kinds              []string               `json:"kinds,omitempty" jsonschema:"work item kind filters"`
	States             []string               `json:"states,omitempty" jsonschema:"provider-normalized state filters"`
	Labels             []string               `json:"labels,omitempty" jsonschema:"label filters"`
	WorkItemTypes      []string               `json:"work_item_types,omitempty" jsonschema:"platform work item type filters"`
	DriftStatuses      []string               `json:"drift_statuses,omitempty" jsonschema:"projection freshness filters"`
	UpdatedSince       string                 `json:"updated_since,omitempty" jsonschema:"RFC3339 lower provider update timestamp"`
	Page               ProviderPageInput      `json:"page,omitempty" jsonschema:"page request"`
}

type ListProviderCommentsInput struct {
	Meta                 ProviderQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	WorkItemProjectionID string                 `json:"work_item_projection_id" jsonschema:"work item projection identifier"`
	Kinds                []string               `json:"kinds,omitempty" jsonschema:"comment kind filters"`
	Page                 ProviderPageInput      `json:"page,omitempty" jsonschema:"page request"`
}

type ListProviderRelationshipsInput struct {
	Meta                 ProviderQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	WorkItemProjectionID string                 `json:"work_item_projection_id,omitempty" jsonschema:"work item projection identifier"`
	RelationshipTypes    []string               `json:"relationship_types,omitempty" jsonschema:"relationship type filters"`
	Sources              []string               `json:"sources,omitempty" jsonschema:"relationship source filters"`
	ConfidenceLevels     []string               `json:"confidence_levels,omitempty" jsonschema:"confidence filters"`
	Page                 ProviderPageInput      `json:"page,omitempty" jsonschema:"page request"`
}

type RegisterProviderArtifactSignalInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	SignalID          string                   `json:"signal_id,omitempty" jsonschema:"idempotent signal identifier"`
	Target            ProviderTargetInput      `json:"target" jsonschema:"provider-native target"`
	Source            string                   `json:"source" jsonschema:"signal source"`
	ObservedAt        string                   `json:"observed_at" jsonschema:"RFC3339 observation timestamp"`
	PayloadJSON       string                   `json:"payload_json,omitempty" jsonschema:"bounded typed signal payload"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type CreateProviderIssueInput struct {
	Meta                   ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	ProjectID              string                   `json:"project_id" jsonschema:"project identifier"`
	RepositoryID           string                   `json:"repository_id" jsonschema:"repository binding identifier"`
	ProviderSlug           string                   `json:"provider_slug" jsonschema:"provider identifier"`
	Title                  string                   `json:"title" jsonschema:"issue title"`
	Body                   string                   `json:"body" jsonschema:"issue body"`
	Labels                 []string                 `json:"labels,omitempty" jsonschema:"provider labels"`
	AssigneeProviderLogins []string                 `json:"assignee_provider_logins,omitempty" jsonschema:"provider assignees"`
	Milestone              string                   `json:"milestone,omitempty" jsonschema:"provider milestone"`
	WorkItemType           string                   `json:"work_item_type,omitempty" jsonschema:"platform work item type"`
	WatermarkJSON          string                   `json:"watermark_json,omitempty" jsonschema:"platform watermark payload"`
	ExternalAccountID      string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
	RepositoryTarget       ProviderTargetInput      `json:"repository_target" jsonschema:"provider repository target"`
}

type UpdateProviderIssueInput struct {
	Meta                    ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	Target                  ProviderTargetInput      `json:"target" jsonschema:"provider issue target"`
	Title                   string                   `json:"title,omitempty" jsonschema:"replacement title"`
	Body                    string                   `json:"body,omitempty" jsonschema:"replacement body"`
	Labels                  StringListPatchInput     `json:"labels,omitempty" jsonschema:"label replacement"`
	AssigneeProviderLogins  StringListPatchInput     `json:"assignee_provider_logins,omitempty" jsonschema:"assignee replacement"`
	Milestone               string                   `json:"milestone,omitempty" jsonschema:"provider milestone"`
	State                   string                   `json:"state,omitempty" jsonschema:"provider-normalized state"`
	WorkItemType            string                   `json:"work_item_type,omitempty" jsonschema:"platform work item type"`
	WatermarkJSON           string                   `json:"watermark_json,omitempty" jsonschema:"platform watermark payload"`
	ExpectedProviderVersion string                   `json:"expected_provider_version,omitempty" jsonschema:"expected provider version or update marker"`
	ExternalAccountID       string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type CreateProviderCommentInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	Target            ProviderTargetInput      `json:"target" jsonschema:"provider work item target"`
	Body              string                   `json:"body" jsonschema:"comment body"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type UpdateProviderCommentInput struct {
	Meta                    ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	Target                  ProviderTargetInput      `json:"target" jsonschema:"provider comment target"`
	ProviderCommentID       string                   `json:"provider_comment_id" jsonschema:"provider-native comment id"`
	Body                    string                   `json:"body" jsonschema:"replacement comment body"`
	ExpectedProviderVersion string                   `json:"expected_provider_version,omitempty" jsonschema:"expected provider version or update marker"`
	ExternalAccountID       string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type CreateProviderPullRequestInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	ProjectID         string                   `json:"project_id" jsonschema:"project identifier"`
	RepositoryID      string                   `json:"repository_id" jsonschema:"repository binding identifier"`
	ProviderSlug      string                   `json:"provider_slug" jsonschema:"provider identifier"`
	Title             string                   `json:"title" jsonschema:"PR/MR title"`
	Body              string                   `json:"body" jsonschema:"PR/MR body"`
	HeadBranch        string                   `json:"head_branch" jsonschema:"source branch"`
	BaseBranch        string                   `json:"base_branch" jsonschema:"target branch"`
	Draft             bool                     `json:"draft,omitempty" jsonschema:"provider draft flag"`
	Labels            []string                 `json:"labels,omitempty" jsonschema:"provider labels"`
	LinkedIssueRef    string                   `json:"linked_issue_ref,omitempty" jsonschema:"linked source issue ref"`
	WatermarkJSON     string                   `json:"watermark_json,omitempty" jsonschema:"platform watermark payload"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
	RepositoryTarget  ProviderTargetInput      `json:"repository_target" jsonschema:"provider repository target"`
}

type UpdateProviderPullRequestInput struct {
	Meta                    ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	Target                  ProviderTargetInput      `json:"target" jsonschema:"provider PR/MR target"`
	Title                   string                   `json:"title,omitempty" jsonschema:"replacement title"`
	Body                    string                   `json:"body,omitempty" jsonschema:"replacement body"`
	Labels                  StringListPatchInput     `json:"labels,omitempty" jsonschema:"label replacement"`
	AssigneeProviderLogins  StringListPatchInput     `json:"assignee_provider_logins,omitempty" jsonschema:"assignee replacement"`
	Milestone               string                   `json:"milestone,omitempty" jsonschema:"provider milestone"`
	State                   string                   `json:"state,omitempty" jsonschema:"provider-normalized state"`
	BaseBranch              string                   `json:"base_branch,omitempty" jsonschema:"replacement base branch"`
	MaintainerCanModify     *bool                    `json:"maintainer_can_modify,omitempty" jsonschema:"provider maintainer edit flag"`
	WatermarkJSON           string                   `json:"watermark_json,omitempty" jsonschema:"platform watermark payload"`
	ExpectedProviderVersion string                   `json:"expected_provider_version,omitempty" jsonschema:"expected provider version or update marker"`
	ExternalAccountID       string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type CreateProviderReviewSignalInput struct {
	Meta              ProviderCommandMetaInput   `json:"meta" jsonschema:"command metadata"`
	Target            ProviderTargetInput        `json:"target" jsonschema:"provider PR/MR target"`
	Kind              string                     `json:"kind" jsonschema:"review signal kind: comment, approval or changes_requested"`
	Body              string                     `json:"body" jsonschema:"review body"`
	InlineComments    []ReviewInlineCommentInput `json:"inline_comments,omitempty" jsonschema:"inline review comments"`
	ExternalAccountID string                     `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type ReviewInlineCommentInput struct {
	Path                       string `json:"path" jsonschema:"repository file path"`
	Body                       string `json:"body" jsonschema:"inline comment body"`
	Line                       *int64 `json:"line,omitempty" jsonschema:"end line in provider diff"`
	StartLine                  *int64 `json:"start_line,omitempty" jsonschema:"start line in provider diff"`
	Side                       string `json:"side,omitempty" jsonschema:"provider diff side"`
	StartSide                  string `json:"start_side,omitempty" jsonschema:"provider diff start side"`
	InReplyToProviderCommentID string `json:"in_reply_to_provider_comment_id,omitempty" jsonschema:"provider inline thread comment id"`
}

type UpdateProviderRelationshipInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	Source            ProviderTargetInput      `json:"source" jsonschema:"source work item target"`
	Target            ProviderTargetInput      `json:"target,omitempty" jsonschema:"target work item target"`
	TargetProviderRef string                   `json:"target_provider_ref,omitempty" jsonschema:"URL or provider ref for unresolved target"`
	RelationshipType  string                   `json:"relationship_type" jsonschema:"relationship type"`
	SourceKind        string                   `json:"source_kind" jsonschema:"relationship source"`
	Confidence        string                   `json:"confidence" jsonschema:"relationship confidence"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type CreateProviderRepositoryInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	ProjectID         string                   `json:"project_id" jsonschema:"project identifier"`
	RepositoryID      string                   `json:"repository_id" jsonschema:"repository binding identifier"`
	ProviderSlug      string                   `json:"provider_slug" jsonschema:"provider identifier"`
	OwnerKind         string                   `json:"owner_kind" jsonschema:"repository owner kind: organization or authenticated_user"`
	ProviderOwner     string                   `json:"provider_owner,omitempty" jsonschema:"provider organization login"`
	RepositoryName    string                   `json:"repository_name" jsonschema:"repository name without owner prefix"`
	Visibility        string                   `json:"visibility" jsonschema:"visibility: public, private or internal"`
	Description       string                   `json:"description,omitempty" jsonschema:"safe provider repository description"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type ProviderTextFileInput struct {
	Path       string `json:"path" jsonschema:"repository-relative file path"`
	Content    string `json:"content" jsonschema:"UTF-8 text content prepared outside provider-hub"`
	Executable bool   `json:"executable,omitempty" jsonschema:"whether file should be executable"`
}

type CreateBootstrapPullRequestInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	ProjectID         string                   `json:"project_id" jsonschema:"project identifier"`
	RepositoryID      string                   `json:"repository_id" jsonschema:"repository binding identifier"`
	ProviderSlug      string                   `json:"provider_slug" jsonschema:"provider identifier"`
	RepositoryTarget  ProviderTargetInput      `json:"repository_target" jsonschema:"existing provider repository target"`
	BaseBranch        string                   `json:"base_branch" jsonschema:"target branch"`
	BootstrapBranch   string                   `json:"bootstrap_branch" jsonschema:"bootstrap source branch"`
	CommitMessage     string                   `json:"commit_message" jsonschema:"safe commit message"`
	Title             string                   `json:"title" jsonschema:"PR/MR title"`
	Body              string                   `json:"body" jsonschema:"PR/MR body"`
	Draft             bool                     `json:"draft,omitempty" jsonschema:"provider draft flag"`
	Files             []ProviderTextFileInput  `json:"files" jsonschema:"prepared text files"`
	WatermarkJSON     string                   `json:"watermark_json,omitempty" jsonschema:"platform watermark payload"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type CreateAdoptionPullRequestInput struct {
	Meta              ProviderCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	ProjectID         string                   `json:"project_id" jsonschema:"project identifier"`
	RepositoryID      string                   `json:"repository_id" jsonschema:"repository binding identifier"`
	ProviderSlug      string                   `json:"provider_slug" jsonschema:"provider identifier"`
	RepositoryTarget  ProviderTargetInput      `json:"repository_target" jsonschema:"existing provider repository target"`
	BaseBranch        string                   `json:"base_branch" jsonschema:"target branch"`
	AdoptionBranch    string                   `json:"adoption_branch" jsonschema:"adoption source branch"`
	CommitMessage     string                   `json:"commit_message" jsonschema:"safe commit message"`
	Title             string                   `json:"title" jsonschema:"PR/MR title"`
	Body              string                   `json:"body" jsonschema:"PR/MR body"`
	Draft             bool                     `json:"draft,omitempty" jsonschema:"provider draft flag"`
	Files             []ProviderTextFileInput  `json:"files" jsonschema:"prepared text files"`
	WatermarkJSON     string                   `json:"watermark_json,omitempty" jsonschema:"platform watermark payload"`
	ExternalAccountID string                   `json:"external_account_id" jsonschema:"selected external account identifier"`
}

type ProviderProjectionOutput struct {
	Projection ProviderWorkItemSummary `json:"projection" jsonschema:"work item projection"`
}

type ProviderProjectionListOutput struct {
	Projections []ProviderWorkItemSummary `json:"projections" jsonschema:"work item projections"`
	Page        PageSummary               `json:"page" jsonschema:"page metadata"`
}

type ProviderCommentListOutput struct {
	Comments []ProviderCommentSummary `json:"comments" jsonschema:"comment projections"`
	Page     PageSummary              `json:"page" jsonschema:"page metadata"`
}

type ProviderRelationshipListOutput struct {
	Relationships []ProviderRelationshipSummary `json:"relationships" jsonschema:"provider relationships"`
	Page          PageSummary                   `json:"page" jsonschema:"page metadata"`
}

type ProviderArtifactSignalOutput struct {
	SignalID string                `json:"signal_id" jsonschema:"accepted signal identifier"`
	Status   string                `json:"status" jsonschema:"processing status"`
	Target   ProviderTargetSummary `json:"target" jsonschema:"provider target"`
}

type ProviderOperationOutput struct {
	Operation  ProviderOperationSummary     `json:"operation" jsonschema:"provider operation"`
	Projection ProviderWorkItemSummary      `json:"projection,omitempty" jsonschema:"resulting projection"`
	Comment    ProviderCommentSummary       `json:"comment,omitempty" jsonschema:"resulting comment"`
	Relation   ProviderRelationshipSummary  `json:"relationship,omitempty" jsonschema:"resulting relationship"`
	Result     ProviderCommandResultSummary `json:"result" jsonschema:"safe command result"`
}

type ProviderWorkItemSummary struct {
	ID                 string   `json:"id" jsonschema:"projection identifier"`
	ProviderSlug       string   `json:"provider_slug" jsonschema:"provider identifier"`
	ProviderWorkItemID string   `json:"provider_work_item_id" jsonschema:"provider-native work item id"`
	ProjectID          string   `json:"project_id,omitempty" jsonschema:"project identifier"`
	RepositoryID       string   `json:"repository_id,omitempty" jsonschema:"repository binding identifier"`
	RepositoryFullName string   `json:"repository_full_name" jsonschema:"repository full name"`
	Kind               string   `json:"kind" jsonschema:"work item kind"`
	Number             int64    `json:"number" jsonschema:"provider-native issue or PR/MR number"`
	WebURL             string   `json:"web_url" jsonschema:"safe provider URL"`
	Title              string   `json:"title" jsonschema:"current provider title"`
	State              string   `json:"state" jsonschema:"provider-normalized lifecycle state"`
	WorkItemType       string   `json:"work_item_type,omitempty" jsonschema:"platform work item type"`
	Labels             []string `json:"labels,omitempty" jsonschema:"normalized labels"`
	Milestone          string   `json:"milestone,omitempty" jsonschema:"normalized milestone"`
	WatermarkStatus    string   `json:"watermark_status" jsonschema:"watermark status"`
	BodyDigest         string   `json:"body_digest" jsonschema:"provider body digest"`
	ProviderUpdatedAt  string   `json:"provider_updated_at,omitempty" jsonschema:"provider update timestamp"`
	SyncedAt           string   `json:"synced_at" jsonschema:"last sync timestamp"`
	DriftStatus        string   `json:"drift_status" jsonschema:"projection freshness"`
	Version            int64    `json:"version" jsonschema:"projection version"`
}

type ProviderCommentSummary struct {
	ID                   string `json:"id" jsonschema:"comment projection identifier"`
	WorkItemProjectionID string `json:"work_item_projection_id" jsonschema:"work item projection identifier"`
	ProviderCommentID    string `json:"provider_comment_id" jsonschema:"provider-native comment id"`
	Kind                 string `json:"kind" jsonschema:"comment kind"`
	AuthorProviderLogin  string `json:"author_provider_login" jsonschema:"provider author login"`
	BodyDigest           string `json:"body_digest" jsonschema:"comment body digest"`
	Summary              string `json:"summary" jsonschema:"short safe excerpt"`
	ProviderCreatedAt    string `json:"provider_created_at,omitempty" jsonschema:"provider creation timestamp"`
	ProviderUpdatedAt    string `json:"provider_updated_at,omitempty" jsonschema:"provider update timestamp"`
	ReviewState          string `json:"review_state" jsonschema:"review state"`
}

type ProviderRelationshipSummary struct {
	ID                         string `json:"id" jsonschema:"relationship identifier"`
	SourceWorkItemProjectionID string `json:"source_work_item_projection_id" jsonschema:"source projection identifier"`
	TargetWorkItemProjectionID string `json:"target_work_item_projection_id,omitempty" jsonschema:"target projection identifier"`
	TargetProviderRef          string `json:"target_provider_ref,omitempty" jsonschema:"target provider ref"`
	RelationshipType           string `json:"relationship_type" jsonschema:"relationship type"`
	Source                     string `json:"source" jsonschema:"relationship source"`
	Confidence                 string `json:"confidence" jsonschema:"relationship confidence"`
	CreatedAt                  string `json:"created_at" jsonschema:"creation timestamp"`
	Version                    int64  `json:"version" jsonschema:"relationship version"`
}

type ProviderOperationSummary struct {
	ID                  string `json:"id" jsonschema:"operation identifier"`
	CommandID           string `json:"command_id" jsonschema:"idempotent command identifier"`
	ActorID             string `json:"actor_id,omitempty" jsonschema:"platform actor identifier"`
	ExternalAccountID   string `json:"external_account_id" jsonschema:"external account identifier"`
	ProviderSlug        string `json:"provider_slug" jsonschema:"provider identifier"`
	OperationType       string `json:"operation_type" jsonschema:"operation type"`
	TargetRef           string `json:"target_ref" jsonschema:"provider target reference"`
	Status              string `json:"status" jsonschema:"operation status"`
	ResultRef           string `json:"result_ref,omitempty" jsonschema:"safe provider result URL or id"`
	ErrorCode           string `json:"error_code,omitempty" jsonschema:"provider error code"`
	RateLimitSnapshotID string `json:"rate_limit_snapshot_id,omitempty" jsonschema:"rate limit snapshot identifier"`
	StartedAt           string `json:"started_at" jsonschema:"operation start timestamp"`
	FinishedAt          string `json:"finished_at,omitempty" jsonschema:"operation finish timestamp"`
	ProviderVersion     string `json:"provider_version,omitempty" jsonschema:"provider version marker"`
}

type ProviderCommandResultSummary struct {
	Target                 ProviderTargetSummary `json:"target" jsonschema:"provider target"`
	ResultRef              string                `json:"result_ref,omitempty" jsonschema:"provider URL or id safe for UI and audit"`
	ProviderObjectID       string                `json:"provider_object_id,omitempty" jsonschema:"provider-native object id"`
	ProviderVersion        string                `json:"provider_version,omitempty" jsonschema:"provider version marker"`
	ReconciliationEnqueued bool                  `json:"reconciliation_enqueued" jsonschema:"whether follow-up reconciliation was scheduled"`
	EmittedEventTypes      []string              `json:"emitted_event_types,omitempty" jsonschema:"domain event names emitted by provider-hub"`
	BaseBranch             string                `json:"base_branch,omitempty" jsonschema:"prepared default branch"`
}

type ProviderTargetSummary = ProviderTargetInput
