package mcptransport

import (
	"context"
	"strings"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	providerToolDescriptions = map[string]string{
		ToolProviderProjectionGet:                        "Прочитать безопасную проекцию Issue или PR/MR через provider-hub.",
		ToolProviderProjectionFind:                       "Найти безопасную проекцию Issue или PR/MR по provider-native ссылке через provider-hub.",
		ToolProviderProjectionsList:                      "Получить список безопасных проекций Issue и PR/MR через provider-hub.",
		ToolProviderCommentsList:                         "Получить безопасные сводки комментариев, упоминаний и review-сигналов через provider-hub.",
		ToolProviderRelationshipsList:                    "Получить provider-native связи между work items через provider-hub.",
		ToolProviderArtifactSignalRegister:               "Зарегистрировать сигнал об артефакте provider-native объекта через provider-hub.",
		ToolProviderIssueCreate:                          "Создать provider-native Issue через provider-hub.",
		ToolProviderIssueUpdate:                          "Обновить разрешённые поля provider-native Issue через provider-hub.",
		ToolProviderCommentCreate:                        "Создать provider-native комментарий через provider-hub.",
		ToolProviderCommentUpdate:                        "Обновить platform-owned provider-native комментарий через provider-hub.",
		ToolProviderPullRequestCreate:                    "Создать provider-native PR/MR через provider-hub.",
		ToolProviderPullRequestUpdate:                    "Обновить разрешённые поля provider-native PR/MR через provider-hub.",
		ToolProviderReviewSignalCreate:                   "Создать review-сигнал, approval или changes-request через provider-hub.",
		ToolProviderRelationshipUpdate:                   "Сохранить или обновить provider-native связь через provider-hub.",
		ToolProviderRepositoryCreate:                     "Создать provider-native репозиторий через provider-hub.",
		ToolProviderRepositoryBootstrapPullRequestCreate: "Создать или обновить bootstrap branch и PR/MR через provider-hub.",
		ToolProviderRepositoryAdoptionPullRequestCreate:  "Создать или обновить adoption branch и PR/MR через provider-hub.",
	}
	providerWorkItemKinds = map[string]providersv1.WorkItemKind{
		"issue":         providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE,
		"pull_request":  providersv1.WorkItemKind_WORK_ITEM_KIND_PULL_REQUEST,
		"merge_request": providersv1.WorkItemKind_WORK_ITEM_KIND_MERGE_REQUEST,
	}
	providerWorkItemKindNames = map[providersv1.WorkItemKind]string{
		providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE:         "issue",
		providersv1.WorkItemKind_WORK_ITEM_KIND_PULL_REQUEST:  "pull_request",
		providersv1.WorkItemKind_WORK_ITEM_KIND_MERGE_REQUEST: "merge_request",
	}
	providerDriftStatuses = map[string]providersv1.WorkItemDriftStatus{
		"fresh":     providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FRESH,
		"suspected": providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_SUSPECTED,
		"stale":     providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_STALE,
		"failed":    providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FAILED,
	}
	providerDriftStatusNames = map[providersv1.WorkItemDriftStatus]string{
		providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FRESH:     "fresh",
		providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_SUSPECTED: "suspected",
		providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_STALE:     "stale",
		providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FAILED:    "failed",
	}
	providerWatermarkStatusNames = map[providersv1.WorkItemWatermarkStatus]string{
		providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_MISSING: "missing",
		providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_VALID:   "valid",
		providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_INVALID: "invalid",
		providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_STALE:   "stale",
	}
	providerCommentKinds = map[string]providersv1.CommentKind{
		"comment": providersv1.CommentKind_COMMENT_KIND_COMMENT,
		"review":  providersv1.CommentKind_COMMENT_KIND_REVIEW,
		"mention": providersv1.CommentKind_COMMENT_KIND_MENTION,
		"system":  providersv1.CommentKind_COMMENT_KIND_SYSTEM,
	}
	providerCommentKindNames = map[providersv1.CommentKind]string{
		providersv1.CommentKind_COMMENT_KIND_COMMENT: "comment",
		providersv1.CommentKind_COMMENT_KIND_REVIEW:  "review",
		providersv1.CommentKind_COMMENT_KIND_MENTION: "mention",
		providersv1.CommentKind_COMMENT_KIND_SYSTEM:  "system",
	}
	providerReviewStateNames = map[providersv1.ReviewState]string{
		providersv1.ReviewState_REVIEW_STATE_APPROVED:          "approved",
		providersv1.ReviewState_REVIEW_STATE_CHANGES_REQUESTED: "changes_requested",
		providersv1.ReviewState_REVIEW_STATE_COMMENTED:         "commented",
		providersv1.ReviewState_REVIEW_STATE_DISMISSED:         "dismissed",
		providersv1.ReviewState_REVIEW_STATE_PENDING:           "pending",
	}
	providerRelationshipSources = map[string]providersv1.RelationshipSource{
		"provider":       providersv1.RelationshipSource_RELATIONSHIP_SOURCE_PROVIDER,
		"watermark":      providersv1.RelationshipSource_RELATIONSHIP_SOURCE_WATERMARK,
		"comment":        providersv1.RelationshipSource_RELATIONSHIP_SOURCE_COMMENT,
		"manual":         providersv1.RelationshipSource_RELATIONSHIP_SOURCE_MANUAL,
		"reconciliation": providersv1.RelationshipSource_RELATIONSHIP_SOURCE_RECONCILIATION,
	}
	providerRelationshipSourceNames = map[providersv1.RelationshipSource]string{
		providersv1.RelationshipSource_RELATIONSHIP_SOURCE_PROVIDER:       "provider",
		providersv1.RelationshipSource_RELATIONSHIP_SOURCE_WATERMARK:      "watermark",
		providersv1.RelationshipSource_RELATIONSHIP_SOURCE_COMMENT:        "comment",
		providersv1.RelationshipSource_RELATIONSHIP_SOURCE_MANUAL:         "manual",
		providersv1.RelationshipSource_RELATIONSHIP_SOURCE_RECONCILIATION: "reconciliation",
	}
	providerRelationshipConfidence = map[string]providersv1.RelationshipConfidence{
		"confirmed": providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_CONFIRMED,
		"inferred":  providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_INFERRED,
		"suspected": providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_SUSPECTED,
	}
	providerRelationshipConfidenceNames = map[providersv1.RelationshipConfidence]string{
		providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_CONFIRMED: "confirmed",
		providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_INFERRED:  "inferred",
		providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_SUSPECTED: "suspected",
	}
	providerOperationTypes = map[string]providersv1.ProviderOperationType{
		"create_issue":                  providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE,
		"update_issue":                  providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_ISSUE,
		"create_comment":                providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_COMMENT,
		"update_comment":                providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_COMMENT,
		"create_pull_request":           providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_PULL_REQUEST,
		"create_review_signal":          providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REVIEW_SIGNAL,
		"update_relationship":           providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_RELATIONSHIP,
		"update_pull_request":           providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_PULL_REQUEST,
		"create_bootstrap_pull_request": providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_BOOTSTRAP_PULL_REQUEST,
		"create_repository":             providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REPOSITORY,
		"create_adoption_pull_request":  providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ADOPTION_PULL_REQUEST,
	}
	providerOperationTypeNames = map[providersv1.ProviderOperationType]string{
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE:                  "create_issue",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_ISSUE:                  "update_issue",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_COMMENT:                "create_comment",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_COMMENT:                "update_comment",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_PULL_REQUEST:           "create_pull_request",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REVIEW_SIGNAL:          "create_review_signal",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_RELATIONSHIP:           "update_relationship",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_PULL_REQUEST:           "update_pull_request",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_BOOTSTRAP_PULL_REQUEST: "create_bootstrap_pull_request",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REPOSITORY:             "create_repository",
		providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ADOPTION_PULL_REQUEST:  "create_adoption_pull_request",
	}
	providerOperationStatusNames = map[providersv1.ProviderOperationStatus]string{
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED:        "succeeded",
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_FAILED:           "failed",
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_RETRYABLE_FAILED: "retryable_failed",
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_DENIED:           "denied",
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_IN_PROGRESS:      "in_progress",
	}
	providerRiskLevels = map[string]providersv1.ProviderOperationRiskLevel{
		"low":      providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_LOW,
		"medium":   providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_MEDIUM,
		"high":     providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_HIGH,
		"critical": providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_CRITICAL,
	}
	providerRepositoryOwnerKinds = map[string]providersv1.RepositoryOwnerKind{
		"organization":       providersv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_ORGANIZATION,
		"authenticated_user": providersv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_AUTHENTICATED_USER,
	}
	providerRepositoryVisibilities = map[string]providersv1.RepositoryVisibility{
		"public":   providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PUBLIC,
		"private":  providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_PRIVATE,
		"internal": providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_INTERNAL,
	}
	providerReviewSignalKinds = map[string]providersv1.ReviewSignalKind{
		"comment":           providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_COMMENT,
		"approval":          providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_APPROVAL,
		"changes_requested": providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_CHANGES_REQUESTED,
	}
)

// ProviderToolsHandler routes provider MCP tools to provider-hub.
type ProviderToolsHandler struct {
	client ProviderHubClient
}

// NewProviderToolsHandler creates the provider tool boundary.
func NewProviderToolsHandler(client ProviderHubClient) *ProviderToolsHandler {
	return &ProviderToolsHandler{client: client}
}

func (handler *ProviderToolsHandler) GetProjection(ctx context.Context, _ *mcpsdk.CallToolRequest, input GetProviderProjectionInput) (*mcpsdk.CallToolResult, ProviderProjectionOutput, error) {
	return routeOwnerTool(ctx, input, getProviderProjectionRequest, handler.client.GetWorkItemProjection, providerProjectionOutput, ToolProviderProjectionGet)
}

func (handler *ProviderToolsHandler) FindProjection(ctx context.Context, _ *mcpsdk.CallToolRequest, input FindProviderProjectionInput) (*mcpsdk.CallToolResult, ProviderProjectionOutput, error) {
	return routeOwnerTool(ctx, input, findProviderProjectionRequest, handler.client.FindWorkItemByProviderRef, providerProjectionOutput, ToolProviderProjectionFind)
}

func (handler *ProviderToolsHandler) ListProjections(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListProviderProjectionsInput) (*mcpsdk.CallToolResult, ProviderProjectionListOutput, error) {
	return routeOwnerTool(ctx, input, listProviderProjectionsRequest, handler.client.ListWorkItemProjections, providerProjectionListOutput, ToolProviderProjectionsList)
}

func (handler *ProviderToolsHandler) ListComments(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListProviderCommentsInput) (*mcpsdk.CallToolResult, ProviderCommentListOutput, error) {
	return routeOwnerTool(ctx, input, listProviderCommentsRequest, handler.client.ListComments, providerCommentListOutput, ToolProviderCommentsList)
}

func (handler *ProviderToolsHandler) ListRelationships(ctx context.Context, _ *mcpsdk.CallToolRequest, input ListProviderRelationshipsInput) (*mcpsdk.CallToolResult, ProviderRelationshipListOutput, error) {
	return routeOwnerTool(ctx, input, listProviderRelationshipsRequest, handler.client.ListRelationships, providerRelationshipListOutput, ToolProviderRelationshipsList)
}

func (handler *ProviderToolsHandler) RegisterArtifactSignal(ctx context.Context, _ *mcpsdk.CallToolRequest, input RegisterProviderArtifactSignalInput) (*mcpsdk.CallToolResult, ProviderArtifactSignalOutput, error) {
	return routeOwnerTool(ctx, input, registerProviderArtifactSignalRequest, handler.client.RegisterProviderArtifactSignal, providerArtifactSignalOutput, ToolProviderArtifactSignalRegister)
}

func (handler *ProviderToolsHandler) CreateIssue(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateProviderIssueInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createProviderIssueRequest, handler.client.CreateIssue, providerOperationOutput, ToolProviderIssueCreate)
}

func (handler *ProviderToolsHandler) UpdateIssue(ctx context.Context, _ *mcpsdk.CallToolRequest, input UpdateProviderIssueInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, updateProviderIssueRequest, handler.client.UpdateIssue, providerOperationOutput, ToolProviderIssueUpdate)
}

func (handler *ProviderToolsHandler) CreateComment(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateProviderCommentInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createProviderCommentRequest, handler.client.CreateComment, providerOperationOutput, ToolProviderCommentCreate)
}

func (handler *ProviderToolsHandler) UpdateComment(ctx context.Context, _ *mcpsdk.CallToolRequest, input UpdateProviderCommentInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, updateProviderCommentRequest, handler.client.UpdateComment, providerOperationOutput, ToolProviderCommentUpdate)
}

func (handler *ProviderToolsHandler) CreatePullRequest(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateProviderPullRequestInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createProviderPullRequestRequest, handler.client.CreatePullRequest, providerOperationOutput, ToolProviderPullRequestCreate)
}

func (handler *ProviderToolsHandler) UpdatePullRequest(ctx context.Context, _ *mcpsdk.CallToolRequest, input UpdateProviderPullRequestInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, updateProviderPullRequestRequest, handler.client.UpdatePullRequest, providerOperationOutput, ToolProviderPullRequestUpdate)
}

func (handler *ProviderToolsHandler) CreateReviewSignal(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateProviderReviewSignalInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createProviderReviewSignalRequest, handler.client.CreateReviewSignal, providerOperationOutput, ToolProviderReviewSignalCreate)
}

func (handler *ProviderToolsHandler) UpdateRelationship(ctx context.Context, _ *mcpsdk.CallToolRequest, input UpdateProviderRelationshipInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, updateProviderRelationshipRequest, handler.client.UpdateRelationship, providerOperationOutput, ToolProviderRelationshipUpdate)
}

func (handler *ProviderToolsHandler) CreateRepository(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateProviderRepositoryInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createProviderRepositoryRequest, handler.client.CreateRepository, providerOperationOutput, ToolProviderRepositoryCreate)
}

func (handler *ProviderToolsHandler) CreateBootstrapPullRequest(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateBootstrapPullRequestInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createBootstrapPullRequestRequest, handler.client.CreateBootstrapPullRequest, providerOperationOutput, ToolProviderRepositoryBootstrapPullRequestCreate)
}

func (handler *ProviderToolsHandler) CreateAdoptionPullRequest(ctx context.Context, _ *mcpsdk.CallToolRequest, input CreateAdoptionPullRequestInput) (*mcpsdk.CallToolResult, ProviderOperationOutput, error) {
	return routeOwnerTool(ctx, input, createAdoptionPullRequestRequest, handler.client.CreateAdoptionPullRequest, providerOperationOutput, ToolProviderRepositoryAdoptionPullRequestCreate)
}

func getProviderProjectionRequest(input GetProviderProjectionInput) (*providersv1.GetWorkItemProjectionRequest, error) {
	meta, err := providerQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.WorkItemProjectionID) == "" {
		return nil, invalidInput("work_item_projection_id is required")
	}
	return &providersv1.GetWorkItemProjectionRequest{
		WorkItemProjectionId: strings.TrimSpace(input.WorkItemProjectionID),
		Meta:                 meta,
	}, nil
}

func findProviderProjectionRequest(input FindProviderProjectionInput) (*providersv1.FindWorkItemByProviderRefRequest, error) {
	meta, err := providerQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := providerTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	return &providersv1.FindWorkItemByProviderRefRequest{Target: target, Meta: meta}, nil
}

func listProviderProjectionsRequest(input ListProviderProjectionsInput) (*providersv1.ListWorkItemProjectionsRequest, error) {
	meta, err := providerQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	kinds, err := providerWorkItemKindList(input.Kinds, "kinds")
	if err != nil {
		return nil, err
	}
	driftStatuses, err := providerDriftStatusList(input.DriftStatuses, "drift_statuses")
	if err != nil {
		return nil, err
	}
	return &providersv1.ListWorkItemProjectionsRequest{
		ProjectId:          optionalString(input.ProjectID),
		RepositoryId:       optionalString(input.RepositoryID),
		ProviderSlug:       optionalString(input.ProviderSlug),
		RepositoryFullName: optionalString(input.RepositoryFullName),
		Kinds:              kinds,
		States:             input.States,
		Labels:             input.Labels,
		WorkItemTypes:      input.WorkItemTypes,
		DriftStatuses:      driftStatuses,
		UpdatedSince:       optionalString(input.UpdatedSince),
		Page:               providerPageRequest(input.Page),
		Meta:               meta,
	}, nil
}

func listProviderCommentsRequest(input ListProviderCommentsInput) (*providersv1.ListCommentsRequest, error) {
	meta, err := providerQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	kinds, err := providerCommentKindList(input.Kinds, "kinds")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.WorkItemProjectionID) == "" {
		return nil, invalidInput("work_item_projection_id is required")
	}
	return &providersv1.ListCommentsRequest{
		WorkItemProjectionId: strings.TrimSpace(input.WorkItemProjectionID),
		Kinds:                kinds,
		Page:                 providerPageRequest(input.Page),
		Meta:                 meta,
	}, nil
}

func listProviderRelationshipsRequest(input ListProviderRelationshipsInput) (*providersv1.ListRelationshipsRequest, error) {
	meta, err := providerQueryMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	sources, err := providerRelationshipSourceList(input.Sources, "sources")
	if err != nil {
		return nil, err
	}
	confidence, err := providerRelationshipConfidenceList(input.ConfidenceLevels, "confidence_levels")
	if err != nil {
		return nil, err
	}
	return &providersv1.ListRelationshipsRequest{
		WorkItemProjectionId: optionalString(input.WorkItemProjectionID),
		RelationshipTypes:    input.RelationshipTypes,
		Sources:              sources,
		ConfidenceLevels:     confidence,
		Page:                 providerPageRequest(input.Page),
		Meta:                 meta,
	}, nil
}

func registerProviderArtifactSignalRequest(input RegisterProviderArtifactSignalInput) (*providersv1.RegisterProviderArtifactSignalRequest, error) {
	meta, err := providerCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	target, err := providerTarget(input.Target, true, "target")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Source) == "" {
		return nil, invalidInput("source is required")
	}
	if strings.TrimSpace(input.ObservedAt) == "" {
		return nil, invalidInput("observed_at is required")
	}
	externalAccountID, err := requiredTrimmed(input.ExternalAccountID, "external_account_id")
	if err != nil {
		return nil, err
	}
	return &providersv1.RegisterProviderArtifactSignalRequest{
		SignalId:          optionalString(input.SignalID),
		Target:            target,
		Source:            strings.TrimSpace(input.Source),
		ObservedAt:        strings.TrimSpace(input.ObservedAt),
		PayloadJson:       optionalString(input.PayloadJSON),
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}, nil
}

func createProviderIssueRequest(input CreateProviderIssueInput) (*providersv1.CreateIssueRequest, error) {
	meta, err := providerCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	repositoryTarget, err := providerTarget(input.RepositoryTarget, true, "repository_target")
	if err != nil {
		return nil, err
	}
	projectID, repositoryID, providerSlug, title, externalAccountID, err := providerCreateBase(input.ProjectID, input.RepositoryID, input.ProviderSlug, input.Title, input.ExternalAccountID)
	if err != nil {
		return nil, err
	}
	return &providersv1.CreateIssueRequest{
		ProjectId:              projectID,
		RepositoryId:           repositoryID,
		ProviderSlug:           providerSlug,
		Title:                  title,
		Body:                   input.Body,
		Labels:                 input.Labels,
		AssigneeProviderLogins: input.AssigneeProviderLogins,
		Milestone:              optionalString(input.Milestone),
		WorkItemType:           optionalString(input.WorkItemType),
		WatermarkJson:          optionalString(input.WatermarkJSON),
		Meta:                   meta,
		ExternalAccountId:      externalAccountID,
		RepositoryTarget:       repositoryTarget,
	}, nil
}

func updateProviderIssueRequest(input UpdateProviderIssueInput) (*providersv1.UpdateIssueRequest, error) {
	meta, target, externalAccountID, err := updateWorkItemBase(input.Meta, input.Target, input.ExternalAccountID)
	if err != nil {
		return nil, err
	}
	return &providersv1.UpdateIssueRequest{
		Target:                  target,
		Title:                   optionalString(input.Title),
		Body:                    optionalString(input.Body),
		Labels:                  stringListPatch(input.Labels),
		AssigneeProviderLogins:  stringListPatch(input.AssigneeProviderLogins),
		Milestone:               optionalString(input.Milestone),
		State:                   optionalString(input.State),
		WorkItemType:            optionalString(input.WorkItemType),
		WatermarkJson:           optionalString(input.WatermarkJSON),
		ExpectedProviderVersion: optionalString(input.ExpectedProviderVersion),
		Meta:                    meta,
		ExternalAccountId:       externalAccountID,
	}, nil
}

func createProviderCommentRequest(input CreateProviderCommentInput) (*providersv1.CreateCommentRequest, error) {
	meta, target, externalAccountID, err := commandWithTarget(input.Meta, input.Target, input.ExternalAccountID, "target")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Body) == "" {
		return nil, invalidInput("body is required")
	}
	return &providersv1.CreateCommentRequest{
		Target:            target,
		Body:              input.Body,
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}, nil
}

func updateProviderCommentRequest(input UpdateProviderCommentInput) (*providersv1.UpdateCommentRequest, error) {
	meta, target, externalAccountID, err := commandWithTarget(input.Meta, input.Target, input.ExternalAccountID, "target")
	if err != nil {
		return nil, err
	}
	providerCommentID, err := requiredTrimmed(input.ProviderCommentID, "provider_comment_id")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Body) == "" {
		return nil, invalidInput("body is required")
	}
	return &providersv1.UpdateCommentRequest{
		Target:                  target,
		ProviderCommentId:       providerCommentID,
		Body:                    input.Body,
		ExpectedProviderVersion: optionalString(input.ExpectedProviderVersion),
		Meta:                    meta,
		ExternalAccountId:       externalAccountID,
	}, nil
}

func createProviderPullRequestRequest(input CreateProviderPullRequestInput) (*providersv1.CreatePullRequestRequest, error) {
	meta, err := providerCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	repositoryTarget, err := providerTarget(input.RepositoryTarget, true, "repository_target")
	if err != nil {
		return nil, err
	}
	projectID, repositoryID, providerSlug, title, externalAccountID, err := providerCreateBase(input.ProjectID, input.RepositoryID, input.ProviderSlug, input.Title, input.ExternalAccountID)
	if err != nil {
		return nil, err
	}
	headBranch, err := requiredTrimmed(input.HeadBranch, "head_branch")
	if err != nil {
		return nil, err
	}
	baseBranch, err := requiredTrimmed(input.BaseBranch, "base_branch")
	if err != nil {
		return nil, err
	}
	return &providersv1.CreatePullRequestRequest{
		ProjectId:         projectID,
		RepositoryId:      repositoryID,
		ProviderSlug:      providerSlug,
		Title:             title,
		Body:              input.Body,
		HeadBranch:        headBranch,
		BaseBranch:        baseBranch,
		Draft:             input.Draft,
		Labels:            input.Labels,
		LinkedIssueRef:    optionalString(input.LinkedIssueRef),
		WatermarkJson:     optionalString(input.WatermarkJSON),
		Meta:              meta,
		ExternalAccountId: externalAccountID,
		RepositoryTarget:  repositoryTarget,
	}, nil
}

func updateProviderPullRequestRequest(input UpdateProviderPullRequestInput) (*providersv1.UpdatePullRequestRequest, error) {
	meta, target, externalAccountID, err := updateWorkItemBase(input.Meta, input.Target, input.ExternalAccountID)
	if err != nil {
		return nil, err
	}
	return &providersv1.UpdatePullRequestRequest{
		Target:                  target,
		Title:                   optionalString(input.Title),
		Body:                    optionalString(input.Body),
		Labels:                  stringListPatch(input.Labels),
		AssigneeProviderLogins:  stringListPatch(input.AssigneeProviderLogins),
		Milestone:               optionalString(input.Milestone),
		State:                   optionalString(input.State),
		BaseBranch:              optionalString(input.BaseBranch),
		MaintainerCanModify:     input.MaintainerCanModify,
		WatermarkJson:           optionalString(input.WatermarkJSON),
		ExpectedProviderVersion: optionalString(input.ExpectedProviderVersion),
		Meta:                    meta,
		ExternalAccountId:       externalAccountID,
	}, nil
}

func createProviderReviewSignalRequest(input CreateProviderReviewSignalInput) (*providersv1.CreateReviewSignalRequest, error) {
	meta, target, externalAccountID, err := commandWithTarget(input.Meta, input.Target, input.ExternalAccountID, "target")
	if err != nil {
		return nil, err
	}
	kind, err := providerReviewSignalKind(input.Kind)
	if err != nil {
		return nil, err
	}
	return &providersv1.CreateReviewSignalRequest{
		Target:            target,
		Kind:              kind,
		Body:              input.Body,
		InlineComments:    reviewInlineComments(input.InlineComments),
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}, nil
}

func updateProviderRelationshipRequest(input UpdateProviderRelationshipInput) (*providersv1.UpdateRelationshipRequest, error) {
	meta, source, externalAccountID, err := commandWithTarget(input.Meta, input.Source, input.ExternalAccountID, "source")
	if err != nil {
		return nil, err
	}
	target, err := providerTarget(input.Target, false, "target")
	if err != nil {
		return nil, err
	}
	if target == nil && strings.TrimSpace(input.TargetProviderRef) == "" {
		return nil, invalidInput("target or target_provider_ref is required")
	}
	relationshipType, err := requiredTrimmed(input.RelationshipType, "relationship_type")
	if err != nil {
		return nil, err
	}
	sourceKind, err := providerRelationshipSourceRequired(input.SourceKind, "source_kind")
	if err != nil {
		return nil, err
	}
	confidence, err := providerRelationshipConfidenceRequired(input.Confidence, "confidence")
	if err != nil {
		return nil, err
	}
	return &providersv1.UpdateRelationshipRequest{
		Source:            source,
		Target:            target,
		TargetProviderRef: optionalString(input.TargetProviderRef),
		RelationshipType:  relationshipType,
		SourceKind:        sourceKind,
		Confidence:        confidence,
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}, nil
}

func createProviderRepositoryRequest(input CreateProviderRepositoryInput) (*providersv1.CreateRepositoryRequest, error) {
	meta, err := providerCommandMeta(input.Meta)
	if err != nil {
		return nil, err
	}
	projectID, repositoryID, providerSlug, _, externalAccountID, err := providerCreateBase(input.ProjectID, input.RepositoryID, input.ProviderSlug, "repository", input.ExternalAccountID)
	if err != nil {
		return nil, err
	}
	ownerKind, err := providerRepositoryOwnerKind(input.OwnerKind)
	if err != nil {
		return nil, err
	}
	visibility, err := providerRepositoryVisibility(input.Visibility)
	if err != nil {
		return nil, err
	}
	repositoryName, err := requiredTrimmed(input.RepositoryName, "repository_name")
	if err != nil {
		return nil, err
	}
	return &providersv1.CreateRepositoryRequest{
		ProjectId:         projectID,
		RepositoryId:      repositoryID,
		ProviderSlug:      providerSlug,
		OwnerKind:         ownerKind,
		ProviderOwner:     optionalString(input.ProviderOwner),
		RepositoryName:    repositoryName,
		Visibility:        visibility,
		Description:       optionalString(input.Description),
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}, nil
}

func createBootstrapPullRequestRequest(input CreateBootstrapPullRequestInput) (*providersv1.CreateBootstrapPullRequestRequest, error) {
	meta, repositoryTarget, projectID, repositoryID, providerSlug, externalAccountID, err := preparedBranchBase(
		input.Meta,
		input.RepositoryTarget,
		input.ProjectID,
		input.RepositoryID,
		input.ProviderSlug,
		input.ExternalAccountID,
	)
	if err != nil {
		return nil, err
	}
	baseBranch, branch, commitMessage, title, err := preparedBranchRequired(input.BaseBranch, input.BootstrapBranch, input.CommitMessage, input.Title, "bootstrap_branch")
	if err != nil {
		return nil, err
	}
	return &providersv1.CreateBootstrapPullRequestRequest{
		ProjectId:         projectID,
		RepositoryId:      repositoryID,
		ProviderSlug:      providerSlug,
		RepositoryTarget:  repositoryTarget,
		BaseBranch:        baseBranch,
		BootstrapBranch:   branch,
		CommitMessage:     commitMessage,
		Title:             title,
		Body:              input.Body,
		Draft:             input.Draft,
		Files:             bootstrapFiles(input.Files),
		WatermarkJson:     optionalString(input.WatermarkJSON),
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}, nil
}

func createAdoptionPullRequestRequest(input CreateAdoptionPullRequestInput) (*providersv1.CreateAdoptionPullRequestRequest, error) {
	meta, repositoryTarget, projectID, repositoryID, providerSlug, externalAccountID, err := preparedBranchBase(
		input.Meta,
		input.RepositoryTarget,
		input.ProjectID,
		input.RepositoryID,
		input.ProviderSlug,
		input.ExternalAccountID,
	)
	if err != nil {
		return nil, err
	}
	baseBranch, branch, commitMessage, title, err := preparedBranchRequired(input.BaseBranch, input.AdoptionBranch, input.CommitMessage, input.Title, "adoption_branch")
	if err != nil {
		return nil, err
	}
	request := &providersv1.CreateAdoptionPullRequestRequest{
		ProjectId:         projectID,
		RepositoryId:      repositoryID,
		ProviderSlug:      providerSlug,
		RepositoryTarget:  repositoryTarget,
		BaseBranch:        baseBranch,
		AdoptionBranch:    branch,
		CommitMessage:     commitMessage,
		Title:             title,
		Meta:              meta,
		ExternalAccountId: externalAccountID,
	}
	request.Body = input.Body
	request.Draft = input.Draft
	request.Files = adoptionFiles(input.Files)
	request.WatermarkJson = optionalString(input.WatermarkJSON)
	return request, nil
}

func providerCommandMeta(input ProviderCommandMetaInput) (*providersv1.CommandMeta, error) {
	actor, err := providerActor(input.Actor)
	if err != nil {
		return nil, err
	}
	requestContext, err := providerRequestContext(input.RequestContext)
	if err != nil {
		return nil, err
	}
	policyContext, err := providerOperationPolicyContext(input.OperationPolicyContext)
	if err != nil {
		return nil, err
	}
	approvalGateRef, err := providerApprovalGateRef(input.ApprovalGateRef, policyContext.GetApprovalRequired())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.CommandID) == "" && strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, invalidInput("command_id or idempotency_key is required")
	}
	if strings.TrimSpace(input.RequestID) == "" {
		return nil, invalidInput("request_id is required")
	}
	return &providersv1.CommandMeta{
		CommandId:              optionalString(input.CommandID),
		IdempotencyKey:         optionalString(input.IdempotencyKey),
		ExpectedVersion:        input.ExpectedVersion,
		Actor:                  actor,
		Reason:                 strings.TrimSpace(input.Reason),
		RequestId:              strings.TrimSpace(input.RequestID),
		RequestContext:         requestContext,
		OperationPolicyContext: policyContext,
		ApprovalGateRef:        approvalGateRef,
	}, nil
}

func providerQueryMeta(input ProviderQueryMetaInput) (*providersv1.QueryMeta, error) {
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return nil, invalidInput("request_id is required")
	}
	actor, err := providerActor(input.Actor)
	if err != nil {
		return nil, err
	}
	requestContext, err := providerRequestContext(input.RequestContext)
	if err != nil {
		return nil, err
	}
	return &providersv1.QueryMeta{
		Actor:          actor,
		RequestId:      requestID,
		RequestContext: requestContext,
	}, nil
}

func providerActor(input ProviderActorInput) (*providersv1.Actor, error) {
	actorType, actorID, err := actorFields(input.Type, input.ID)
	if err != nil {
		return nil, err
	}
	return &providersv1.Actor{Type: actorType, Id: actorID}, nil
}

func providerRequestContext(input ProviderRequestContextInput) (*providersv1.RequestContext, error) {
	source, err := safeRequestSource(input.Source)
	if err != nil {
		return nil, err
	}
	contextValue := &providersv1.RequestContext{Source: source}
	contextValue.TraceId = optionalString(input.TraceID)
	contextValue.SessionId = optionalString(input.SessionID)
	contextValue.ClientIpHash = optionalString(input.ClientIPHash)
	return contextValue, nil
}

func providerOperationPolicyContext(input ProviderOperationPolicyContextInput) (*providersv1.ProviderOperationPolicyContext, error) {
	operationType, err := providerOperationType(input.OperationType)
	if err != nil {
		return nil, err
	}
	riskLevel, err := providerRiskLevel(input.RiskLevel)
	if err != nil {
		return nil, err
	}
	return &providersv1.ProviderOperationPolicyContext{
		ProjectId:         optionalString(input.ProjectID),
		RepositoryId:      optionalString(input.RepositoryID),
		Stage:             optionalString(input.Stage),
		RoleId:            optionalString(input.RoleID),
		RoleKey:           optionalString(input.RoleKey),
		OperationType:     operationType,
		TargetRef:         optionalString(input.TargetRef),
		ChangedFields:     input.ChangedFields,
		RiskTags:          input.RiskTags,
		RiskLevel:         riskLevel,
		ApprovalRequired:  input.ApprovalRequired,
		PolicyVersion:     optionalString(input.PolicyVersion),
		PolicySnapshotRef: optionalString(input.PolicySnapshotRef),
	}, nil
}

func providerApprovalGateRef(input ProviderApprovalGateRefInput, required bool) (*providersv1.ApprovalGateReference, error) {
	if !required && strings.TrimSpace(input.ApprovalID) == "" && strings.TrimSpace(input.GateType) == "" && strings.TrimSpace(input.Decision) == "" {
		return nil, nil
	}
	approvalID, err := requiredTrimmed(input.ApprovalID, "approval_gate_ref.approval_id")
	if err != nil {
		return nil, err
	}
	gateType, err := requiredTrimmed(input.GateType, "approval_gate_ref.gate_type")
	if err != nil {
		return nil, err
	}
	decision, err := requiredTrimmed(input.Decision, "approval_gate_ref.decision")
	if err != nil {
		return nil, err
	}
	return &providersv1.ApprovalGateReference{
		ApprovalId:       approvalID,
		GateType:         gateType,
		Decision:         decision,
		DecidedByActorId: optionalString(input.DecidedByActorID),
		DecidedAt:        optionalString(input.DecidedAt),
		EvidenceRef:      optionalString(input.EvidenceRef),
		PolicyVersion:    optionalString(input.PolicyVersion),
	}, nil
}

func providerTarget(input ProviderTargetInput, required bool, field string) (*providersv1.ProviderTarget, error) {
	empty := strings.TrimSpace(input.ProviderSlug) == "" &&
		strings.TrimSpace(input.RepositoryFullName) == "" &&
		strings.TrimSpace(input.ProviderRepositoryID) == "" &&
		input.Number == nil &&
		strings.TrimSpace(input.ProviderObjectID) == "" &&
		strings.TrimSpace(input.WebURL) == "" &&
		strings.TrimSpace(input.WorkItemKind) == ""
	if empty {
		if required {
			return nil, invalidInput(field + " is required")
		}
		return nil, nil
	}
	providerSlug, err := requiredTrimmed(input.ProviderSlug, field+".provider_slug")
	if err != nil {
		return nil, err
	}
	kind, err := optionalProviderWorkItemKind(input.WorkItemKind, field+".work_item_kind")
	if err != nil {
		return nil, err
	}
	return &providersv1.ProviderTarget{
		ProviderSlug:         providerSlug,
		RepositoryFullName:   optionalString(input.RepositoryFullName),
		ProviderRepositoryId: optionalString(input.ProviderRepositoryID),
		WorkItemKind:         kind,
		Number:               input.Number,
		ProviderObjectId:     optionalString(input.ProviderObjectID),
		WebUrl:               optionalString(input.WebURL),
	}, nil
}

func providerPageRequest(input ProviderPageInput) *providersv1.PageRequest {
	return &providersv1.PageRequest{
		PageSize:  input.PageSize,
		PageToken: optionalString(input.PageToken),
	}
}

func stringListPatch(input StringListPatchInput) *providersv1.StringListPatch {
	if !input.Present {
		return nil
	}
	return &providersv1.StringListPatch{Values: input.Values}
}

func commandWithTarget(metaInput ProviderCommandMetaInput, targetInput ProviderTargetInput, externalAccount string, targetField string) (*providersv1.CommandMeta, *providersv1.ProviderTarget, string, error) {
	meta, err := providerCommandMeta(metaInput)
	if err != nil {
		return nil, nil, "", err
	}
	target, err := providerTarget(targetInput, true, targetField)
	if err != nil {
		return nil, nil, "", err
	}
	externalAccountID, err := requiredTrimmed(externalAccount, "external_account_id")
	if err != nil {
		return nil, nil, "", err
	}
	return meta, target, externalAccountID, nil
}

func updateWorkItemBase(metaInput ProviderCommandMetaInput, targetInput ProviderTargetInput, externalAccount string) (*providersv1.CommandMeta, *providersv1.ProviderTarget, string, error) {
	return commandWithTarget(metaInput, targetInput, externalAccount, "target")
}

func providerCreateBase(project string, repository string, provider string, title string, externalAccount string) (string, string, string, string, string, error) {
	projectID, err := requiredTrimmed(project, "project_id")
	if err != nil {
		return "", "", "", "", "", err
	}
	repositoryID, err := requiredTrimmed(repository, "repository_id")
	if err != nil {
		return "", "", "", "", "", err
	}
	providerSlug, err := requiredTrimmed(provider, "provider_slug")
	if err != nil {
		return "", "", "", "", "", err
	}
	trimmedTitle, err := requiredTrimmed(title, "title")
	if err != nil {
		return "", "", "", "", "", err
	}
	externalAccountID, err := requiredTrimmed(externalAccount, "external_account_id")
	if err != nil {
		return "", "", "", "", "", err
	}
	return projectID, repositoryID, providerSlug, trimmedTitle, externalAccountID, nil
}

func preparedBranchBase(metaInput ProviderCommandMetaInput, repositoryTargetInput ProviderTargetInput, project string, repository string, provider string, externalAccount string) (*providersv1.CommandMeta, *providersv1.ProviderTarget, string, string, string, string, error) {
	meta, err := providerCommandMeta(metaInput)
	if err != nil {
		return nil, nil, "", "", "", "", err
	}
	repositoryTarget, err := providerTarget(repositoryTargetInput, true, "repository_target")
	if err != nil {
		return nil, nil, "", "", "", "", err
	}
	projectID, repositoryID, providerSlug, _, externalAccountID, err := providerCreateBase(project, repository, provider, "pull request", externalAccount)
	if err != nil {
		return nil, nil, "", "", "", "", err
	}
	return meta, repositoryTarget, projectID, repositoryID, providerSlug, externalAccountID, nil
}

func preparedBranchRequired(baseBranch string, branch string, commitMessage string, title string, branchField string) (string, string, string, string, error) {
	trimmedBaseBranch, err := requiredTrimmed(baseBranch, "base_branch")
	if err != nil {
		return "", "", "", "", err
	}
	trimmedBranch, err := requiredTrimmed(branch, branchField)
	if err != nil {
		return "", "", "", "", err
	}
	trimmedCommitMessage, err := requiredTrimmed(commitMessage, "commit_message")
	if err != nil {
		return "", "", "", "", err
	}
	trimmedTitle, err := requiredTrimmed(title, "title")
	if err != nil {
		return "", "", "", "", err
	}
	return trimmedBaseBranch, trimmedBranch, trimmedCommitMessage, trimmedTitle, nil
}

func bootstrapFiles(inputs []ProviderTextFileInput) []*providersv1.BootstrapFile {
	return preparedTextFiles(inputs, bootstrapFile)
}

func adoptionFiles(inputs []ProviderTextFileInput) []*providersv1.AdoptionFile {
	return preparedTextFiles(inputs, adoptionFile)
}

func preparedTextFiles[T any](inputs []ProviderTextFileInput, cast func(ProviderTextFileInput) T) []T {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]T, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, cast(input))
	}
	return result
}

func bootstrapFile(input ProviderTextFileInput) *providersv1.BootstrapFile {
	return &providersv1.BootstrapFile{
		Path:       strings.TrimSpace(input.Path),
		Content:    input.Content,
		Executable: input.Executable,
	}
}

func adoptionFile(input ProviderTextFileInput) *providersv1.AdoptionFile {
	return &providersv1.AdoptionFile{
		Path:       strings.TrimSpace(input.Path),
		Content:    input.Content,
		Executable: input.Executable,
	}
}

func reviewInlineComments(inputs []ReviewInlineCommentInput) []*providersv1.ReviewInlineComment {
	if len(inputs) == 0 {
		return nil
	}
	result := make([]*providersv1.ReviewInlineComment, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, &providersv1.ReviewInlineComment{
			Path:                       strings.TrimSpace(input.Path),
			Body:                       input.Body,
			Line:                       input.Line,
			StartLine:                  input.StartLine,
			Side:                       optionalString(input.Side),
			StartSide:                  optionalString(input.StartSide),
			InReplyToProviderCommentId: optionalString(input.InReplyToProviderCommentID),
		})
	}
	return result
}

func providerProjectionOutput(response *providersv1.WorkItemProjectionResponse) ProviderProjectionOutput {
	return ProviderProjectionOutput{Projection: providerWorkItemSummary(response.GetWorkItemProjection())}
}

func providerProjectionListOutput(response *providersv1.ListWorkItemProjectionsResponse) ProviderProjectionListOutput {
	return ProviderProjectionListOutput{
		Projections: providerWorkItemSummaries(response.GetWorkItemProjections()),
		Page:        providerPageSummary(response.GetPage()),
	}
}

func providerCommentListOutput(response *providersv1.ListCommentsResponse) ProviderCommentListOutput {
	return ProviderCommentListOutput{
		Comments: providerCommentSummaries(response.GetComments()),
		Page:     providerPageSummary(response.GetPage()),
	}
}

func providerRelationshipListOutput(response *providersv1.ListRelationshipsResponse) ProviderRelationshipListOutput {
	return ProviderRelationshipListOutput{
		Relationships: providerRelationshipSummaries(response.GetRelationships()),
		Page:          providerPageSummary(response.GetPage()),
	}
}

func providerArtifactSignalOutput(response *providersv1.ProviderArtifactSignalResponse) ProviderArtifactSignalOutput {
	return ProviderArtifactSignalOutput{
		SignalID: response.GetSignalId(),
		Status:   response.GetStatus(),
		Target:   providerTargetSummaryV1(response.GetTarget()),
	}
}

func providerOperationOutput(response *providersv1.ProviderOperationResponse) ProviderOperationOutput {
	return ProviderOperationOutput{
		Operation:  providerOperationSummary(response.GetProviderOperation()),
		Projection: providerWorkItemSummary(response.GetWorkItemProjection()),
		Comment:    providerCommentSummary(response.GetCommentProjection()),
		Relation:   providerRelationshipSummary(response.GetRelationship()),
		Result:     providerCommandResultSummary(response.GetResult()),
	}
}

func providerWorkItemSummaries(items []*providersv1.WorkItemProjection) []ProviderWorkItemSummary {
	return summarizeItems(items, providerWorkItemSummary)
}

func providerWorkItemSummary(item *providersv1.WorkItemProjection) ProviderWorkItemSummary {
	if item == nil {
		return ProviderWorkItemSummary{}
	}
	return ProviderWorkItemSummary{
		ID:                 item.GetWorkItemProjectionId(),
		ProviderSlug:       item.GetProviderSlug(),
		ProviderWorkItemID: item.GetProviderWorkItemId(),
		ProjectID:          item.GetProjectId(),
		RepositoryID:       item.GetRepositoryId(),
		RepositoryFullName: item.GetRepositoryFullName(),
		Kind:               providerWorkItemKindName(item.GetKind()),
		Number:             item.GetNumber(),
		WebURL:             item.GetWebUrl(),
		Title:              item.GetTitle(),
		State:              item.GetState(),
		WorkItemType:       item.GetWorkItemType(),
		Labels:             item.GetLabels(),
		Milestone:          item.GetMilestone(),
		WatermarkStatus:    enumName(item.GetWatermarkStatus(), providerWatermarkStatusNames),
		BodyDigest:         item.GetBodyDigest(),
		ProviderUpdatedAt:  item.GetProviderUpdatedAt(),
		SyncedAt:           item.GetSyncedAt(),
		DriftStatus:        enumName(item.GetDriftStatus(), providerDriftStatusNames),
		Version:            item.GetVersion(),
	}
}

func providerCommentSummaries(items []*providersv1.CommentProjection) []ProviderCommentSummary {
	return summarizeItems(items, providerCommentSummary)
}

func providerCommentSummary(item *providersv1.CommentProjection) ProviderCommentSummary {
	if item == nil {
		return ProviderCommentSummary{}
	}
	return ProviderCommentSummary{
		ID:                   item.GetCommentProjectionId(),
		WorkItemProjectionID: item.GetWorkItemProjectionId(),
		ProviderCommentID:    item.GetProviderCommentId(),
		Kind:                 enumName(item.GetKind(), providerCommentKindNames),
		AuthorProviderLogin:  item.GetAuthorProviderLogin(),
		BodyDigest:           item.GetBodyDigest(),
		Summary:              item.GetSummary(),
		ProviderCreatedAt:    item.GetProviderCreatedAt(),
		ProviderUpdatedAt:    item.GetProviderUpdatedAt(),
		ReviewState:          enumName(item.GetReviewState(), providerReviewStateNames),
	}
}

func providerRelationshipSummaries(items []*providersv1.ProviderRelationship) []ProviderRelationshipSummary {
	return summarizeItems(items, providerRelationshipSummary)
}

func providerRelationshipSummary(item *providersv1.ProviderRelationship) ProviderRelationshipSummary {
	if item == nil {
		return ProviderRelationshipSummary{}
	}
	return ProviderRelationshipSummary{
		ID:                         item.GetRelationshipId(),
		SourceWorkItemProjectionID: item.GetSourceWorkItemProjectionId(),
		TargetWorkItemProjectionID: item.GetTargetWorkItemProjectionId(),
		TargetProviderRef:          item.GetTargetProviderRef(),
		RelationshipType:           item.GetRelationshipType(),
		Source:                     enumName(item.GetSource(), providerRelationshipSourceNames),
		Confidence:                 enumName(item.GetConfidence(), providerRelationshipConfidenceNames),
		CreatedAt:                  item.GetCreatedAt(),
		Version:                    item.GetVersion(),
	}
}

func providerOperationSummary(operation *providersv1.ProviderOperation) ProviderOperationSummary {
	if operation == nil {
		return ProviderOperationSummary{}
	}
	return ProviderOperationSummary{
		ID:                  operation.GetProviderOperationId(),
		CommandID:           operation.GetCommandId(),
		ActorID:             operation.GetActorId(),
		ExternalAccountID:   operation.GetExternalAccountId(),
		ProviderSlug:        operation.GetProviderSlug(),
		OperationType:       enumName(operation.GetOperationType(), providerOperationTypeNames),
		TargetRef:           operation.GetTargetRef(),
		Status:              enumName(operation.GetStatus(), providerOperationStatusNames),
		ResultRef:           operation.GetResultRef(),
		ErrorCode:           operation.GetErrorCode(),
		RateLimitSnapshotID: operation.GetRateLimitSnapshotId(),
		StartedAt:           operation.GetStartedAt(),
		FinishedAt:          operation.GetFinishedAt(),
		ProviderVersion:     operation.GetProviderVersion(),
	}
}

func providerCommandResultSummary(result *providersv1.ProviderOperationCommandResult) ProviderCommandResultSummary {
	if result == nil {
		return ProviderCommandResultSummary{}
	}
	return ProviderCommandResultSummary{
		Target:                 providerTargetSummaryV1(result.GetTarget()),
		ResultRef:              result.GetResultRef(),
		ProviderObjectID:       result.GetProviderObjectId(),
		ProviderVersion:        result.GetProviderVersion(),
		ReconciliationEnqueued: result.GetReconciliationEnqueued(),
		EmittedEventTypes:      result.GetEmittedEventTypes(),
		BaseBranch:             result.GetBaseBranch(),
	}
}

func providerTargetSummaryV1(target *providersv1.ProviderTarget) ProviderTargetSummary {
	if target == nil {
		return ProviderTargetSummary{}
	}
	return ProviderTargetSummary{
		ProviderSlug:         target.GetProviderSlug(),
		RepositoryFullName:   target.GetRepositoryFullName(),
		ProviderRepositoryID: target.GetProviderRepositoryId(),
		WorkItemKind:         providerWorkItemKindName(target.GetWorkItemKind()),
		Number:               target.Number,
		ProviderObjectID:     target.GetProviderObjectId(),
		WebURL:               target.GetWebUrl(),
	}
}

func providerPageSummary(page *providersv1.PageResponse) PageSummary {
	if page == nil {
		return PageSummary{}
	}
	return PageSummary{NextPageToken: page.GetNextPageToken()}
}

func providerWorkItemKind(value string) (providersv1.WorkItemKind, error) {
	return requiredEnumValue(providerEnumKey(value), providerWorkItemKinds, providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED, "work_item_kind")
}

func optionalProviderWorkItemKind(value string, field string) (*providersv1.WorkItemKind, error) {
	key := providerEnumKey(value)
	if key == "" {
		return nil, nil
	}
	enumValue, err := requiredEnumValue(key, providerWorkItemKinds, providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED, field)
	if err != nil {
		return nil, err
	}
	return &enumValue, nil
}

func providerWorkItemKindList(values []string, field string) ([]providersv1.WorkItemKind, error) {
	return enumList(values, providerWorkItemKind, field)
}

func providerCommentKindList(values []string, field string) ([]providersv1.CommentKind, error) {
	return enumListFromMap(values, providerCommentKinds, providersv1.CommentKind_COMMENT_KIND_UNSPECIFIED, field)
}

func providerDriftStatusList(values []string, field string) ([]providersv1.WorkItemDriftStatus, error) {
	return enumListFromMap(values, providerDriftStatuses, providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_UNSPECIFIED, field)
}

func providerRelationshipSourceList(values []string, field string) ([]providersv1.RelationshipSource, error) {
	return enumListFromMap(values, providerRelationshipSources, providersv1.RelationshipSource_RELATIONSHIP_SOURCE_UNSPECIFIED, field)
}

func providerRelationshipSourceRequired(value string, field string) (providersv1.RelationshipSource, error) {
	return requiredEnumValue(providerEnumKey(value), providerRelationshipSources, providersv1.RelationshipSource_RELATIONSHIP_SOURCE_UNSPECIFIED, field)
}

func providerRelationshipConfidenceList(values []string, field string) ([]providersv1.RelationshipConfidence, error) {
	return enumListFromMap(values, providerRelationshipConfidence, providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_UNSPECIFIED, field)
}

func providerRelationshipConfidenceRequired(value string, field string) (providersv1.RelationshipConfidence, error) {
	return requiredEnumValue(providerEnumKey(value), providerRelationshipConfidence, providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_UNSPECIFIED, field)
}

func providerOperationType(value string) (providersv1.ProviderOperationType, error) {
	return requiredEnumValue(providerEnumKey(value), providerOperationTypes, providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED, "operation_policy_context.operation_type")
}

func providerRiskLevel(value string) (providersv1.ProviderOperationRiskLevel, error) {
	key := providerEnumKey(value)
	if key == "" {
		return providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_UNSPECIFIED, nil
	}
	return requiredEnumValue(key, providerRiskLevels, providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_UNSPECIFIED, "operation_policy_context.risk_level")
}

func providerRepositoryOwnerKind(value string) (providersv1.RepositoryOwnerKind, error) {
	return requiredEnumValue(providerEnumKey(value), providerRepositoryOwnerKinds, providersv1.RepositoryOwnerKind_REPOSITORY_OWNER_KIND_UNSPECIFIED, "owner_kind")
}

func providerRepositoryVisibility(value string) (providersv1.RepositoryVisibility, error) {
	return requiredEnumValue(providerEnumKey(value), providerRepositoryVisibilities, providersv1.RepositoryVisibility_REPOSITORY_VISIBILITY_UNSPECIFIED, "visibility")
}

func providerReviewSignalKind(value string) (providersv1.ReviewSignalKind, error) {
	return requiredEnumValue(providerEnumKey(value), providerReviewSignalKinds, providersv1.ReviewSignalKind_REVIEW_SIGNAL_KIND_UNSPECIFIED, "kind")
}

func providerWorkItemKindName(value providersv1.WorkItemKind) string {
	return enumName(value, providerWorkItemKindNames)
}

func enumList[T comparable](values []string, parse func(string) (T, error), field string) ([]T, error) {
	if len(values) == 0 {
		return nil, nil
	}
	result := make([]T, 0, len(values))
	for _, value := range values {
		parsed, err := parse(value)
		if err != nil {
			return nil, invalidInput(field + " contains invalid value")
		}
		result = append(result, parsed)
	}
	return result, nil
}

func enumListFromMap[T comparable](values []string, enumValues map[string]T, zero T, field string) ([]T, error) {
	return enumList(values, func(value string) (T, error) {
		return requiredEnumValue(providerEnumKey(value), enumValues, zero, field)
	}, field)
}

func providerEnumKey(value string) string {
	key := normalizedKey(value)
	prefixes := []string{
		"work_item_kind_",
		"work_item_drift_status_",
		"comment_kind_",
		"relationship_source_",
		"relationship_confidence_",
		"provider_operation_type_",
		"provider_operation_risk_level_",
		"repository_owner_kind_",
		"repository_visibility_",
		"review_signal_kind_",
	}
	for _, prefix := range prefixes {
		key = strings.TrimPrefix(key, prefix)
	}
	return key
}

func requiredTrimmed(value string, field string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", invalidInput(field + " is required")
	}
	return trimmed, nil
}
