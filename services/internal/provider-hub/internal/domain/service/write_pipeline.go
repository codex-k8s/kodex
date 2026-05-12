package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const (
	writeFailureAccessDenied        = "access_denied"
	writeFailureApprovalRequired    = "approval_required"
	writeFailureExecutorUnavailable = "write_executor_unavailable"
	writeFailureProviderAuthFailed  = "provider_auth_failed"
	writeFailureProviderNotFound    = "provider_not_found"
	writeFailureProviderRateLimited = "provider_rate_limited"
	writeFailureProviderTransient   = "provider_transient_error"
	writeFailureProviderPermanent   = "provider_permanent_error"
	writeFailureProviderUnsupported = "provider_unsupported"
)

type providerWritePlan struct {
	operationType           enum.ProviderOperationType
	actionKey               string
	providerSlug            enum.ProviderSlug
	externalAccountID       uuid.UUID
	targetRef               string
	scopeType               string
	scopeID                 string
	resultTarget            *ProviderTarget
	meta                    value.CommandMeta
	executorRequest         providerclient.WriteRequest
	validateExpectedVersion func(context.Context) error
}

type providerWriteFailure struct {
	status       enum.ProviderOperationStatus
	errorCode    string
	errorMessage string
}

// CreateIssue records a typed provider issue creation command in the shared write pipeline.
func (s *Service) CreateIssue(ctx context.Context, input CreateIssueInput) (ProviderOperationResult, error) {
	if !validCommandIdentity(input.Meta) ||
		input.ProjectID == uuid.Nil ||
		input.RepositoryID == uuid.Nil ||
		!validProviderSlug(input.ProviderSlug) ||
		strings.TrimSpace(input.Title) == "" ||
		input.ExternalAccountID == uuid.Nil ||
		hasBlankStrings(input.Labels) ||
		hasBlankStrings(input.AssigneeProviderLogins) {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	watermarkJSON, err := optionalCanonicalJSONObject(input.WatermarkJSON)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	repositoryID := input.RepositoryID.String()
	targetRef := repositoryTargetRef(input.ProviderSlug, repositoryID)
	changedFields := requiredChangedFields(
		"title",
		"body",
		"labels",
		"assignee_provider_logins",
		optionalChangedField("milestone", input.Milestone != nil),
		optionalChangedField("work_item_type", input.WorkItemType != nil),
		optionalChangedField("watermark_json", len(watermarkJSON) > 0),
	)
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationCreateIssue,
		actionKey:         accesscatalog.ActionProviderIssueWrite,
		providerSlug:      input.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         "repository",
		scopeID:           repositoryID,
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.ProviderSlug,
			CreateIssue: &providerclient.CreateIssueCommand{
				ProjectID:              input.ProjectID.String(),
				RepositoryID:           repositoryID,
				Title:                  strings.TrimSpace(input.Title),
				Body:                   strings.TrimSpace(input.Body),
				Labels:                 append([]string(nil), trimStrings(input.Labels)...),
				AssigneeProviderLogins: append([]string(nil), trimStrings(input.AssigneeProviderLogins)...),
				Milestone:              optionalStringValue(input.Milestone),
				WorkItemType:           optionalStringValue(input.WorkItemType),
				WatermarkJSON:          watermarkJSON,
			},
		},
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// UpdateIssue records a typed provider issue update command in the shared write pipeline.
func (s *Service) UpdateIssue(ctx context.Context, input UpdateIssueInput) (ProviderOperationResult, error) {
	targetRef, err := providerTargetRef(input.Target)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if !validCommandIdentity(input.Meta) || input.ExternalAccountID == uuid.Nil {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	labelsPatch, err := normalizeStringListPatch(input.Labels)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	assigneesPatch, err := normalizeStringListPatch(input.AssigneeProviderLogins)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	watermarkJSON, err := optionalCanonicalJSONObjectPtr(input.WatermarkJSON)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	changedFields := requiredChangedFields(
		optionalChangedField("title", input.Title != nil),
		optionalChangedField("body", input.Body != nil),
		optionalChangedField("labels", labelsPatch != nil),
		optionalChangedField("assignee_provider_logins", assigneesPatch != nil),
		optionalChangedField("milestone", input.Milestone != nil),
		optionalChangedField("state", input.State != nil),
		optionalChangedField("work_item_type", input.WorkItemType != nil),
		optionalChangedField("watermark_json", watermarkJSON != nil),
	)
	if len(changedFields) == 0 {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationUpdateIssue,
		actionKey:         accesscatalog.ActionProviderIssueWrite,
		providerSlug:      input.Target.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           providerScopeIDFromTarget(input.Target),
		resultTarget:      targetCopy(input.Target),
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.Target.ProviderSlug,
			UpdateIssue: &providerclient.UpdateIssueCommand{
				Target:                  toClientTarget(input.Target),
				Title:                   optionalTextPtr(input.Title),
				Body:                    optionalTextPtr(input.Body),
				Labels:                  labelsPatch,
				AssigneeProviderLogins:  assigneesPatch,
				Milestone:               optionalTextPtr(input.Milestone),
				State:                   optionalTextPtr(input.State),
				WorkItemType:            optionalTextPtr(input.WorkItemType),
				WatermarkJSON:           watermarkJSON,
				ExpectedProviderVersion: strings.TrimSpace(input.ExpectedProviderVersion),
			},
		},
		validateExpectedVersion: s.expectedWorkItemVersionCheck(input.Target, input.Meta.ExpectedVersion),
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// CreateComment records a typed provider comment creation command in the shared write pipeline.
func (s *Service) CreateComment(ctx context.Context, input CreateCommentInput) (ProviderOperationResult, error) {
	targetRef, err := providerTargetRef(input.Target)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if !validCommandIdentity(input.Meta) || input.ExternalAccountID == uuid.Nil || strings.TrimSpace(input.Body) == "" {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationCreateComment,
		actionKey:         accesscatalog.ActionProviderCommentWrite,
		providerSlug:      input.Target.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           providerScopeIDFromTarget(input.Target),
		resultTarget:      targetCopy(input.Target),
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.Target.ProviderSlug,
			CreateComment: &providerclient.CreateCommentCommand{
				Target: toClientTarget(input.Target),
				Body:   strings.TrimSpace(input.Body),
			},
		},
	}, []string{"body"})
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// UpdateComment records a typed provider comment update command in the shared write pipeline.
func (s *Service) UpdateComment(ctx context.Context, input UpdateCommentInput) (ProviderOperationResult, error) {
	baseTargetRef, err := providerTargetRef(input.Target)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	commentID := strings.TrimSpace(input.ProviderCommentID)
	if !validCommandIdentity(input.Meta) || input.ExternalAccountID == uuid.Nil || commentID == "" || strings.TrimSpace(input.Body) == "" {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	targetRef := baseTargetRef + "#comment:" + commentID
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationUpdateComment,
		actionKey:         accesscatalog.ActionProviderCommentWrite,
		providerSlug:      input.Target.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           providerScopeIDFromTarget(input.Target),
		resultTarget:      targetCopy(input.Target),
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.Target.ProviderSlug,
			UpdateComment: &providerclient.UpdateCommentCommand{
				Target:                  toClientTarget(input.Target),
				ProviderCommentID:       commentID,
				Body:                    strings.TrimSpace(input.Body),
				ExpectedProviderVersion: strings.TrimSpace(input.ExpectedProviderVersion),
			},
		},
		validateExpectedVersion: s.expectedCommentVersionCheck(input.Target, commentID, input.Meta.ExpectedVersion),
	}, []string{"body"})
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// CreatePullRequest records a typed PR/MR creation command in the shared write pipeline.
func (s *Service) CreatePullRequest(ctx context.Context, input CreatePullRequestInput) (ProviderOperationResult, error) {
	if !validCommandIdentity(input.Meta) ||
		input.ProjectID == uuid.Nil ||
		input.RepositoryID == uuid.Nil ||
		!validProviderSlug(input.ProviderSlug) ||
		input.ExternalAccountID == uuid.Nil ||
		strings.TrimSpace(input.Title) == "" ||
		strings.TrimSpace(input.HeadBranch) == "" ||
		strings.TrimSpace(input.BaseBranch) == "" ||
		hasBlankStrings(input.Labels) {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	watermarkJSON, err := optionalCanonicalJSONObject(input.WatermarkJSON)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	repositoryID := input.RepositoryID.String()
	targetRef := repositoryTargetRef(input.ProviderSlug, repositoryID) + "#pull_request:new"
	changedFields := requiredChangedFields(
		"title",
		"body",
		"head_branch",
		"base_branch",
		"draft",
		"labels",
		optionalChangedField("linked_issue_ref", input.LinkedIssueRef != nil),
		optionalChangedField("watermark_json", len(watermarkJSON) > 0),
	)
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationCreatePullRequest,
		actionKey:         accesscatalog.ActionProviderPullRequestWrite,
		providerSlug:      input.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           repositoryID,
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.ProviderSlug,
			CreatePullRequest: &providerclient.CreatePullRequestCommand{
				ProjectID:      input.ProjectID.String(),
				RepositoryID:   repositoryID,
				Title:          strings.TrimSpace(input.Title),
				Body:           strings.TrimSpace(input.Body),
				HeadBranch:     strings.TrimSpace(input.HeadBranch),
				BaseBranch:     strings.TrimSpace(input.BaseBranch),
				Draft:          input.Draft,
				Labels:         append([]string(nil), trimStrings(input.Labels)...),
				LinkedIssueRef: optionalStringValue(input.LinkedIssueRef),
				WatermarkJSON:  watermarkJSON,
			},
		},
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// CreateReviewSignal records a typed provider review signal in the shared write pipeline.
func (s *Service) CreateReviewSignal(ctx context.Context, input CreateReviewSignalInput) (ProviderOperationResult, error) {
	targetRef, err := providerTargetRef(input.Target)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if !validCommandIdentity(input.Meta) ||
		input.ExternalAccountID == uuid.Nil ||
		!validReviewSignalKind(input.Kind) ||
		!validInlineComments(input.InlineComments) {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	changedFields := requiredChangedFields(
		"kind",
		optionalChangedField("body", strings.TrimSpace(input.Body) != ""),
		optionalChangedField("inline_comments", len(input.InlineComments) > 0),
	)
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationCreateReviewSignal,
		actionKey:         accesscatalog.ActionProviderReviewSignalWrite,
		providerSlug:      input.Target.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           providerScopeIDFromTarget(input.Target),
		resultTarget:      targetCopy(input.Target),
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.Target.ProviderSlug,
			CreateReviewSignal: &providerclient.CreateReviewSignalCommand{
				Target:         toClientTarget(input.Target),
				Kind:           toClientReviewSignalKind(input.Kind),
				Body:           strings.TrimSpace(input.Body),
				InlineComments: toClientInlineComments(input.InlineComments),
			},
		},
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// UpdateRelationship records one provider relationship update in the shared write pipeline.
func (s *Service) UpdateRelationship(ctx context.Context, input UpdateRelationshipInput) (ProviderOperationResult, error) {
	sourceRef, err := providerTargetRef(input.Source)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	targetProviderRef := optionalStringValue(input.TargetProviderRef)
	targetRef := sourceRef + "#relationship:" + strings.TrimSpace(input.RelationshipType)
	var resultTarget *ProviderTarget
	if input.Target != nil {
		targetProjectionRef, targetErr := providerTargetRef(*input.Target)
		if targetErr != nil {
			return ProviderOperationResult{}, targetErr
		}
		targetRef += ":" + targetProjectionRef
		resultTarget = targetCopy(*input.Target)
	}
	if targetProviderRef != "" {
		targetRef += ":provider_ref:" + targetProviderRef
	}
	if !validCommandIdentity(input.Meta) ||
		input.ExternalAccountID == uuid.Nil ||
		strings.TrimSpace(input.RelationshipType) == "" ||
		!validRelationshipSources([]enum.RelationshipSource{input.SourceKind}) ||
		!validRelationshipConfidenceLevels([]enum.RelationshipConfidence{input.Confidence}) ||
		(input.Target == nil && targetProviderRef == "") {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	changedFields := requiredChangedFields(
		"source",
		optionalChangedField("target", input.Target != nil),
		optionalChangedField("target_provider_ref", targetProviderRef != ""),
		"relationship_type",
		"source_kind",
		"confidence",
	)
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationUpdateRelationship,
		actionKey:         accesscatalog.ActionProviderRelationshipWrite,
		providerSlug:      input.Source.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           providerScopeIDFromTarget(input.Source),
		resultTarget:      resultTarget,
		meta:              input.Meta,
		validateExpectedVersion: s.expectedRelationshipVersionCheck(
			input,
			targetProviderRef,
			input.Meta.ExpectedVersion,
		),
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: input.Source.ProviderSlug,
			UpdateRelationship: &providerclient.UpdateRelationshipCommand{
				Source:            toClientTarget(input.Source),
				Target:            optionalClientTarget(input.Target),
				TargetProviderRef: targetProviderRef,
				RelationshipType:  strings.TrimSpace(input.RelationshipType),
				SourceKind:        input.SourceKind,
				Confidence:        input.Confidence,
			},
		},
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

func (s *Service) buildWritePlan(plan providerWritePlan, changedFields []string) (providerWritePlan, error) {
	policyContext, err := canonicalPolicyContext(plan.meta.OperationPolicyContext, plan.operationType, plan.targetRef, changedFields)
	if err != nil {
		return providerWritePlan{}, err
	}
	plan.meta.OperationPolicyContext = policyContext
	approvalRef, err := canonicalApprovalGateRef(plan.meta.ApprovalGateRef)
	if err != nil {
		return providerWritePlan{}, err
	}
	plan.meta.ApprovalGateRef = approvalRef
	return plan, nil
}

func (s *Service) executeProviderWrite(ctx context.Context, plan providerWritePlan) (ProviderOperationResult, error) {
	if plan.validateExpectedVersion != nil {
		if err := plan.validateExpectedVersion(ctx); err != nil {
			return ProviderOperationResult{}, err
		}
	}
	now := s.clock.Now().UTC()
	usage, failure, err := s.resolveWriteAccount(ctx, plan)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if failure != nil {
		return s.finalizeProviderWrite(ctx, plan, providerclient.WriteResult{}, now, *failure)
	}
	if plan.meta.OperationPolicyContext.ApprovalRequired && plan.meta.ApprovalGateRef.ApprovalID == "" {
		return s.finalizeProviderWrite(ctx, plan, providerclient.WriteResult{}, now, providerWriteFailure{
			status:       enum.ProviderOperationStatusDenied,
			errorCode:    writeFailureApprovalRequired,
			errorMessage: "approval gate reference is required",
		})
	}
	executor := s.providerWriteExecutors[plan.providerSlug]
	if executor == nil {
		return s.finalizeProviderWrite(ctx, plan, providerclient.WriteResult{}, now, providerWriteFailure{
			status:       enum.ProviderOperationStatusFailed,
			errorCode:    writeFailureExecutorUnavailable,
			errorMessage: "provider write executor is unavailable",
		})
	}
	result, execErr := executor.Execute(ctx, writeRequestWithCredential(plan.executorRequest, providerclient.AccountCredential{
		ExternalAccountID: plan.externalAccountID,
		ProviderSlug:      plan.providerSlug,
	}))
	if execErr != nil {
		return s.finalizeProviderWrite(ctx, plan, providerclient.WriteResult{}, now, mapProviderWriteError(execErr))
	}
	_ = usage
	return s.finalizeProviderWrite(ctx, plan, result, now, providerWriteFailure{status: enum.ProviderOperationStatusSucceeded})
}

func (s *Service) resolveWriteAccount(ctx context.Context, plan providerWritePlan) (ExternalAccountUsageResult, *providerWriteFailure, error) {
	if s.accountUsage == nil {
		failure := providerWriteFailure{
			status:       enum.ProviderOperationStatusRetryableFailed,
			errorCode:    writeFailureExecutorUnavailable,
			errorMessage: "account usage resolver is unavailable",
		}
		return ExternalAccountUsageResult{}, &failure, nil
	}
	usage, err := s.accountUsage.ResolveExternalAccountUsage(ctx, ExternalAccountUsageInput{
		ExternalAccountID: plan.externalAccountID,
		ActionKey:         plan.actionKey,
		ScopeType:         plan.scopeType,
		ScopeID:           plan.scopeID,
	})
	if err == nil {
		if enum.ProviderSlug(strings.TrimSpace(string(usage.ProviderSlug))) != plan.providerSlug || !slices.Contains(usage.AllowedActionKeys, plan.actionKey) {
			failure := providerWriteFailure{
				status:       enum.ProviderOperationStatusDenied,
				errorCode:    writeFailureAccessDenied,
				errorMessage: "external account is not allowed for requested provider action",
			}
			return ExternalAccountUsageResult{}, &failure, nil
		}
		return usage, nil, nil
	}
	failure := providerWriteFailure{
		status:       enum.ProviderOperationStatusRetryableFailed,
		errorCode:    writeFailureProviderTransient,
		errorMessage: "external account usage check failed",
	}
	switch {
	case errors.Is(err, errs.ErrForbidden):
		failure.status = enum.ProviderOperationStatusDenied
		failure.errorCode = writeFailureAccessDenied
		failure.errorMessage = "external account usage denied"
	case errors.Is(err, errs.ErrDependencyUnavailable):
		failure.status = enum.ProviderOperationStatusRetryableFailed
		failure.errorCode = writeFailureProviderTransient
		failure.errorMessage = "external account usage dependency unavailable"
	default:
		return ExternalAccountUsageResult{}, nil, err
	}
	return ExternalAccountUsageResult{}, &failure, nil
}

func (s *Service) finalizeProviderWrite(ctx context.Context, plan providerWritePlan, executorResult providerclient.WriteResult, startedAt time.Time, failure providerWriteFailure) (ProviderOperationResult, error) {
	finishedAt := s.clock.Now().UTC()
	operation := entity.ProviderOperation{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: startedAt,
			UpdatedAt: finishedAt,
		},
		CommandID:              providerCommandKey(plan.meta),
		ActorID:                actorUUIDPtr(plan.meta.Actor),
		ExternalAccountID:      plan.externalAccountID,
		ProviderSlug:           plan.providerSlug,
		OperationType:          plan.operationType,
		TargetRef:              plan.targetRef,
		Status:                 failure.status,
		ResultRef:              strings.TrimSpace(executorResult.ResultRef),
		ErrorCode:              failure.errorCode,
		ErrorMessage:           failure.errorMessage,
		OperationPolicyContext: plan.meta.OperationPolicyContext,
		ApprovalGateRef:        plan.meta.ApprovalGateRef,
		ProviderVersion:        strings.TrimSpace(executorResult.ProviderVersion),
		StartedAt:              startedAt,
		FinishedAt:             &finishedAt,
	}
	outboxEvent, err := s.providerOperationOutbox(operation)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	storedOperation, err := s.repository.ApplyProviderOperation(ctx, providerrepo.ProviderOperationCompletion{
		Operation:    operation,
		OutboxEvents: []entity.OutboxEvent{outboxEvent},
	})
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return ProviderOperationResult{
		ProviderOperation: &storedOperation,
		Result: ProviderOperationCommandResult{
			Target:                 plan.resultTarget,
			ResultRef:              strings.TrimSpace(executorResult.ResultRef),
			ProviderObjectID:       strings.TrimSpace(executorResult.ProviderObjectID),
			ProviderVersion:        strings.TrimSpace(executorResult.ProviderVersion),
			ReconciliationEnqueued: executorResult.ReconciliationEnqueued,
			EmittedEventTypes:      []string{providerOperationEventType(storedOperation.Status)},
		},
	}, nil
}

func (s *Service) providerOperationOutbox(operation entity.ProviderOperation) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:        string(operation.ProviderSlug),
		ExternalAccountID:   operation.ExternalAccountID.String(),
		ProviderOperationID: operation.ID.String(),
		OperationType:       string(operation.OperationType),
		ResultRef:           operation.ResultRef,
		Status:              string(operation.Status),
		ErrorCode:           operation.ErrorCode,
		Version:             operation.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(
		s.ids.New(),
		providerOperationEventType(operation.Status),
		providerAggregateProviderOperation,
		operation.ID,
		payload,
		operation.StartedAt,
	), nil
}

func providerOperationEventType(status enum.ProviderOperationStatus) string {
	if status == enum.ProviderOperationStatusSucceeded {
		return providerEventOperationCompleted
	}
	return providerEventOperationFailed
}

func providerCommandKey(meta value.CommandMeta) string {
	if meta.CommandID != uuid.Nil {
		return meta.CommandID.String()
	}
	actorType := strings.TrimSpace(meta.Actor.Type)
	if actorType == "" {
		actorType = "unknown"
	}
	actorID := strings.TrimSpace(meta.Actor.ID)
	if actorID == "" {
		actorID = "unknown"
	}
	return "idempotency:" + actorType + ":" + actorID + ":" + strings.TrimSpace(meta.IdempotencyKey)
}

func actorUUIDPtr(actor value.Actor) *uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(actor.ID))
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

func canonicalPolicyContext(policy value.ProviderOperationPolicyContext, operationType enum.ProviderOperationType, targetRef string, changedFields []string) (value.ProviderOperationPolicyContext, error) {
	if !validOperationTypes([]enum.ProviderOperationType{operationType}) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	if len(changedFields) == 0 {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	policy.ProjectID = strings.TrimSpace(policy.ProjectID)
	policy.RepositoryID = strings.TrimSpace(policy.RepositoryID)
	policy.Stage = strings.TrimSpace(policy.Stage)
	policy.RoleID = strings.TrimSpace(policy.RoleID)
	policy.RoleKey = strings.TrimSpace(policy.RoleKey)
	policy.PolicyVersion = strings.TrimSpace(policy.PolicyVersion)
	policy.PolicySnapshotRef = strings.TrimSpace(policy.PolicySnapshotRef)
	policy.ChangedFields = trimStrings(policy.ChangedFields)
	policy.RiskTags = trimStrings(policy.RiskTags)
	policy.OperationType = string(operationType)
	policy.TargetRef = targetRef
	if policy.ProjectID != "" && !validOptionalUUIDText(policy.ProjectID) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	if policy.RepositoryID != "" && !validOptionalUUIDText(policy.RepositoryID) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	if policy.RoleID != "" && !validOptionalUUIDText(policy.RoleID) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	if hasBlankStrings(policy.ChangedFields) || hasBlankStrings(policy.RiskTags) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	sort.Strings(policy.ChangedFields)
	sort.Strings(changedFields)
	if !slices.Equal(policy.ChangedFields, changedFields) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	if !validRiskLevel(policy.RiskLevel) {
		return value.ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	return policy, nil
}

func canonicalApprovalGateRef(reference value.ApprovalGateReference) (value.ApprovalGateReference, error) {
	reference.ApprovalID = strings.TrimSpace(reference.ApprovalID)
	reference.GateType = strings.TrimSpace(reference.GateType)
	reference.Decision = strings.TrimSpace(reference.Decision)
	reference.DecidedByActorID = strings.TrimSpace(reference.DecidedByActorID)
	reference.EvidenceRef = strings.TrimSpace(reference.EvidenceRef)
	reference.PolicyVersion = strings.TrimSpace(reference.PolicyVersion)
	if reference.ApprovalID == "" && reference.GateType == "" && reference.Decision == "" && reference.DecidedByActorID == "" && reference.DecidedAt == nil && reference.EvidenceRef == "" && reference.PolicyVersion == "" {
		return value.ApprovalGateReference{}, nil
	}
	if reference.ApprovalID == "" || reference.GateType == "" || reference.Decision == "" {
		return value.ApprovalGateReference{}, errs.ErrInvalidArgument
	}
	if reference.DecidedByActorID != "" && !validOptionalUUIDText(reference.DecidedByActorID) {
		return value.ApprovalGateReference{}, errs.ErrInvalidArgument
	}
	return reference, nil
}

func providerTargetRef(target ProviderTarget) (string, error) {
	if !validProviderSlug(target.ProviderSlug) {
		return "", errs.ErrInvalidArgument
	}
	if providerObjectID := strings.TrimSpace(target.ProviderObjectID); providerObjectID != "" {
		return string(target.ProviderSlug) + ":object:" + providerObjectID, nil
	}
	if repositoryFullName := strings.TrimSpace(target.RepositoryFullName); repositoryFullName != "" && validWorkItemKind(target.WorkItemKind) && target.Number > 0 {
		return fmt.Sprintf("%s:repo:%s:%s:%d", target.ProviderSlug, repositoryFullName, target.WorkItemKind, target.Number), nil
	}
	if providerRepositoryID := strings.TrimSpace(target.ProviderRepositoryID); providerRepositoryID != "" && validWorkItemKind(target.WorkItemKind) && target.Number > 0 {
		return fmt.Sprintf("%s:provider_repo:%s:%s:%d", target.ProviderSlug, providerRepositoryID, target.WorkItemKind, target.Number), nil
	}
	if webURL := strings.TrimSpace(target.WebURL); webURL != "" {
		return string(target.ProviderSlug) + ":url:" + webURL, nil
	}
	return "", errs.ErrInvalidArgument
}

func repositoryTargetRef(providerSlug enum.ProviderSlug, repositoryID string) string {
	return string(providerSlug) + ":repository:" + strings.TrimSpace(repositoryID)
}

func providerScopeIDFromTarget(target ProviderTarget) string {
	if repositoryFullName := strings.TrimSpace(target.RepositoryFullName); repositoryFullName != "" {
		return repositoryFullName
	}
	if providerRepositoryID := strings.TrimSpace(target.ProviderRepositoryID); providerRepositoryID != "" {
		return providerRepositoryID
	}
	if webURL := strings.TrimSpace(target.WebURL); webURL != "" {
		return webURL
	}
	return strings.TrimSpace(target.ProviderObjectID)
}

const providerUsageScopeRepository = "repository"

func requiredChangedFields(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func optionalChangedField(name string, present bool) string {
	if !present {
		return ""
	}
	return name
}

func normalizeStringListPatch(patch *value.StringListPatch) (*value.StringListPatch, error) {
	if patch == nil {
		return nil, nil
	}
	values := trimStrings(patch.Values)
	if hasBlankStrings(values) {
		return nil, errs.ErrInvalidArgument
	}
	return &value.StringListPatch{Values: append([]string(nil), values...)}, nil
}

func optionalCanonicalJSONObject(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return canonicalJSONObject(raw)
}

func optionalCanonicalJSONObjectPtr(raw *[]byte) (*[]byte, error) {
	if raw == nil {
		return nil, nil
	}
	value, err := optionalCanonicalJSONObject(*raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalTextPtr(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	return &text
}

func targetCopy(target ProviderTarget) *ProviderTarget {
	copyTarget := target
	copyTarget.RepositoryFullName = strings.TrimSpace(copyTarget.RepositoryFullName)
	copyTarget.ProviderRepositoryID = strings.TrimSpace(copyTarget.ProviderRepositoryID)
	copyTarget.ProviderObjectID = strings.TrimSpace(copyTarget.ProviderObjectID)
	copyTarget.WebURL = strings.TrimSpace(copyTarget.WebURL)
	return &copyTarget
}

func optionalClientTarget(target *ProviderTarget) *providerclient.Target {
	if target == nil {
		return nil
	}
	clientTarget := toClientTarget(*target)
	return &clientTarget
}

func toClientTarget(target ProviderTarget) providerclient.Target {
	return providerclient.Target{
		ProviderSlug:         target.ProviderSlug,
		RepositoryFullName:   strings.TrimSpace(target.RepositoryFullName),
		ProviderRepositoryID: strings.TrimSpace(target.ProviderRepositoryID),
		WorkItemKind:         target.WorkItemKind,
		Number:               target.Number,
		ProviderObjectID:     strings.TrimSpace(target.ProviderObjectID),
		WebURL:               strings.TrimSpace(target.WebURL),
	}
}

func toClientReviewSignalKind(kind enum.ReviewSignalKind) providerclient.ReviewSignalKind {
	switch kind {
	case enum.ReviewSignalKindComment:
		return providerclient.ReviewSignalKindComment
	case enum.ReviewSignalKindApproval:
		return providerclient.ReviewSignalKindApproval
	default:
		return providerclient.ReviewSignalKindChangesRequested
	}
}

func toClientInlineComments(comments []ProviderInlineComment) []providerclient.ReviewInlineComment {
	if len(comments) == 0 {
		return nil
	}
	result := make([]providerclient.ReviewInlineComment, 0, len(comments))
	for _, comment := range comments {
		result = append(result, providerclient.ReviewInlineComment{
			Path:                       strings.TrimSpace(comment.Path),
			Body:                       strings.TrimSpace(comment.Body),
			Line:                       comment.Line,
			StartLine:                  comment.StartLine,
			Side:                       strings.TrimSpace(comment.Side),
			StartSide:                  strings.TrimSpace(comment.StartSide),
			InReplyToProviderCommentID: strings.TrimSpace(comment.InReplyToProviderCommentID),
		})
	}
	return result
}

func writeRequestWithCredential(request providerclient.WriteRequest, credential providerclient.AccountCredential) providerclient.WriteRequest {
	request.Credential = credential
	return request
}

func mapProviderWriteError(err error) providerWriteFailure {
	var providerErr *providerclient.Error
	if !errors.As(err, &providerErr) {
		return providerWriteFailure{
			status:       enum.ProviderOperationStatusRetryableFailed,
			errorCode:    writeFailureProviderTransient,
			errorMessage: "provider write failed",
		}
	}
	switch providerErr.Kind {
	case providerclient.ErrorKindAuthFailed:
		return providerWriteFailure{status: enum.ProviderOperationStatusFailed, errorCode: writeFailureProviderAuthFailed, errorMessage: providerclient.ErrAuthFailed.Error()}
	case providerclient.ErrorKindNotFound:
		return providerWriteFailure{status: enum.ProviderOperationStatusFailed, errorCode: writeFailureProviderNotFound, errorMessage: providerclient.ErrNotFound.Error()}
	case providerclient.ErrorKindRateLimited:
		return providerWriteFailure{status: enum.ProviderOperationStatusRetryableFailed, errorCode: writeFailureProviderRateLimited, errorMessage: providerclient.ErrRateLimited.Error()}
	case providerclient.ErrorKindTransient:
		return providerWriteFailure{status: enum.ProviderOperationStatusRetryableFailed, errorCode: writeFailureProviderTransient, errorMessage: providerclient.ErrTransient.Error()}
	case providerclient.ErrorKindUnsupported:
		return providerWriteFailure{status: enum.ProviderOperationStatusFailed, errorCode: writeFailureProviderUnsupported, errorMessage: providerclient.ErrUnsupported.Error()}
	default:
		return providerWriteFailure{status: enum.ProviderOperationStatusFailed, errorCode: writeFailureProviderPermanent, errorMessage: providerclient.ErrPermanent.Error()}
	}
}

func validRiskLevel(level value.ProviderOperationRiskLevel) bool {
	switch level {
	case value.ProviderOperationRiskLevelLow,
		value.ProviderOperationRiskLevelMedium,
		value.ProviderOperationRiskLevelHigh,
		value.ProviderOperationRiskLevelCritical:
		return true
	default:
		return false
	}
}

func validOptionalUUIDText(text string) bool {
	id, err := uuid.Parse(strings.TrimSpace(text))
	return err == nil && id != uuid.Nil
}

func validReviewSignalKind(kind enum.ReviewSignalKind) bool {
	switch kind {
	case enum.ReviewSignalKindComment,
		enum.ReviewSignalKindApproval,
		enum.ReviewSignalKindChangesRequested:
		return true
	default:
		return false
	}
}

func validInlineComments(comments []ProviderInlineComment) bool {
	for _, comment := range comments {
		if strings.TrimSpace(comment.Path) == "" || strings.TrimSpace(comment.Body) == "" {
			return false
		}
	}
	return true
}

func (s *Service) expectedWorkItemVersionCheck(target ProviderTarget, expectedVersion *int64) func(context.Context) error {
	if expectedVersion == nil {
		return nil
	}
	return func(ctx context.Context) error {
		projection, err := s.repository.GetWorkItemProjection(ctx, workItemLookupFromTarget(target))
		if err != nil {
			return err
		}
		if projection.Version != *expectedVersion {
			return errs.ErrConflict
		}
		return nil
	}
}

func (s *Service) expectedCommentVersionCheck(target ProviderTarget, providerCommentID string, expectedVersion *int64) func(context.Context) error {
	if expectedVersion == nil {
		return nil
	}
	return func(ctx context.Context) error {
		projection, err := s.repository.GetWorkItemProjection(ctx, workItemLookupFromTarget(target))
		if err != nil {
			return err
		}
		comment, err := s.repository.GetCommentProjectionByProviderID(ctx, projection.ID, providerCommentID)
		if err != nil {
			return err
		}
		if comment.Version != *expectedVersion {
			return errs.ErrConflict
		}
		return nil
	}
}

func (s *Service) expectedRelationshipVersionCheck(input UpdateRelationshipInput, targetProviderRef string, expectedVersion *int64) func(context.Context) error {
	if expectedVersion == nil {
		return nil
	}
	return func(ctx context.Context) error {
		source, err := s.repository.GetWorkItemProjection(ctx, workItemLookupFromTarget(input.Source))
		if err != nil {
			return err
		}
		var targetID *uuid.UUID
		if input.Target != nil {
			target, targetErr := s.repository.GetWorkItemProjection(ctx, workItemLookupFromTarget(*input.Target))
			if targetErr != nil {
				return targetErr
			}
			targetID = &target.ID
		}
		relationships, _, err := s.repository.ListRelationships(ctx, query.RelationshipFilter{
			WorkItemProjectionID: &source.ID,
			RelationshipTypes:    []string{strings.TrimSpace(input.RelationshipType)},
			Page:                 value.PageRequest{PageSize: maxExpectedRelationshipCandidates},
		})
		if err != nil {
			return err
		}
		for _, relationship := range relationships {
			if !sameRelationshipIdentity(relationship, source.ID, targetID, targetProviderRef) {
				continue
			}
			if relationship.Version != *expectedVersion {
				return errs.ErrConflict
			}
			return nil
		}
		return errs.ErrConflict
	}
}

func workItemLookupFromTarget(target ProviderTarget) query.ProviderTargetLookup {
	return query.ProviderTargetLookup{
		ProviderSlug:       target.ProviderSlug,
		RepositoryFullName: strings.TrimSpace(target.RepositoryFullName),
		Kind:               target.WorkItemKind,
		Number:             target.Number,
		ProviderObjectID:   strings.TrimSpace(target.ProviderObjectID),
		WebURL:             strings.TrimSpace(target.WebURL),
	}
}

const maxExpectedRelationshipCandidates int32 = 100

func sameRelationshipIdentity(relationship entity.ProviderRelationship, sourceID uuid.UUID, targetID *uuid.UUID, targetProviderRef string) bool {
	if relationship.SourceWorkItemID != sourceID || relationship.TargetProviderRef != targetProviderRef {
		return false
	}
	if targetID == nil {
		return relationship.TargetWorkItemID == nil
	}
	if relationship.TargetWorkItemID == nil {
		return false
	}
	return *relationship.TargetWorkItemID == *targetID
}
