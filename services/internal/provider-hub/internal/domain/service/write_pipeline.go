package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/libs/go/secretresolver"
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
	writeFailureSecretUnavailable   = "secret_unavailable"
	writeFailureProviderAuthFailed  = "provider_auth_failed"
	writeFailureProviderNotFound    = "provider_not_found"
	writeFailureProviderRateLimited = "provider_rate_limited"
	writeFailureProviderTransient   = "provider_transient_error"
	writeFailureProviderPermanent   = "provider_permanent_error"
	writeFailureProviderUnsupported = "provider_unsupported"

	maxBootstrapFiles             = 64
	maxBootstrapFileContentBytes  = 512 * 1024
	maxBootstrapTotalContentBytes = 4 * 1024 * 1024
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
	skipProviderCredential  bool
}

type providerWriteFailure struct {
	status       enum.ProviderOperationStatus
	errorCode    string
	errorMessage string
}

// CreateIssue records a typed provider issue creation command in the shared write pipeline.
func (s *Service) CreateIssue(ctx context.Context, input CreateIssueInput) (ProviderOperationResult, error) {
	repositoryTarget, err := repositoryTarget(input.ProviderSlug, input.RepositoryTarget)
	if err != nil {
		return ProviderOperationResult{}, err
	}
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
				RepositoryTarget:       toClientTarget(repositoryTarget),
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
	repositoryTarget, err := repositoryTarget(input.ProviderSlug, input.RepositoryTarget)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if !validCreateProjectRepositoryWriteInput(input.Meta, input.ProjectID, input.RepositoryID, input.ProviderSlug, input.ExternalAccountID) ||
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
				ProjectID:        input.ProjectID.String(),
				RepositoryID:     repositoryID,
				RepositoryTarget: toClientTarget(repositoryTarget),
				Title:            strings.TrimSpace(input.Title),
				Body:             strings.TrimSpace(input.Body),
				HeadBranch:       strings.TrimSpace(input.HeadBranch),
				BaseBranch:       strings.TrimSpace(input.BaseBranch),
				Draft:            input.Draft,
				Labels:           append([]string(nil), trimStrings(input.Labels)...),
				LinkedIssueRef:   optionalStringValue(input.LinkedIssueRef),
				WatermarkJSON:    watermarkJSON,
			},
		},
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// CreateBootstrapPullRequest records provider-side bootstrap branch/PR creation for an existing empty repository.
func (s *Service) CreateBootstrapPullRequest(ctx context.Context, input CreateBootstrapPullRequestInput) (ProviderOperationResult, error) {
	repositoryTarget, err := repositoryTarget(input.ProviderSlug, input.RepositoryTarget)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	files, err := normalizeBootstrapFiles(input.Files)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if !validCreateProjectRepositoryWriteInput(input.Meta, input.ProjectID, input.RepositoryID, input.ProviderSlug, input.ExternalAccountID) ||
		strings.TrimSpace(input.BaseBranch) == "" ||
		strings.TrimSpace(input.BootstrapBranch) == "" ||
		strings.TrimSpace(input.CommitMessage) == "" ||
		strings.TrimSpace(input.Title) == "" ||
		len(files) == 0 {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	watermarkJSON, err := optionalCanonicalJSONObject(input.WatermarkJSON)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	repositoryID := input.RepositoryID.String()
	targetRef := repositoryTargetRef(input.ProviderSlug, repositoryID) + "#bootstrap_pull_request:" + strings.TrimSpace(input.BootstrapBranch)
	changedFields := requiredChangedFields(
		"repository_target",
		"base_branch",
		"bootstrap_branch",
		"commit_message",
		"title",
		"body",
		"files",
		"draft",
		optionalChangedField("watermark_json", len(watermarkJSON) > 0),
	)
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationCreateBootstrapPullRequest,
		actionKey:         accesscatalog.ActionProviderRepositoryWrite,
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
			CreateBootstrapPullRequest: &providerclient.CreateBootstrapPullRequestCommand{
				ProjectID:        input.ProjectID.String(),
				RepositoryID:     repositoryID,
				RepositoryTarget: toClientTarget(repositoryTarget),
				BaseBranch:       strings.TrimSpace(input.BaseBranch),
				BootstrapBranch:  strings.TrimSpace(input.BootstrapBranch),
				CommitMessage:    strings.TrimSpace(input.CommitMessage),
				Title:            strings.TrimSpace(input.Title),
				Body:             strings.TrimSpace(input.Body),
				Draft:            input.Draft,
				Files:            toClientBootstrapFiles(files),
				WatermarkJSON:    watermarkJSON,
			},
		},
	}, changedFields)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return s.executeProviderWrite(ctx, plan)
}

// UpdatePullRequest records a typed provider PR/MR update command in the shared write pipeline.
func (s *Service) UpdatePullRequest(ctx context.Context, input UpdatePullRequestInput) (ProviderOperationResult, error) {
	target := input.Target
	if target.WorkItemKind == "" {
		target.WorkItemKind = enum.WorkItemKindPullRequest
	}
	if target.WorkItemKind != enum.WorkItemKindPullRequest {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	targetRef, err := providerTargetRef(target)
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
		optionalChangedField("base_branch", input.BaseBranch != nil),
		optionalChangedField("maintainer_can_modify", input.MaintainerCanModify != nil),
		optionalChangedField("watermark_json", watermarkJSON != nil),
	)
	if len(changedFields) == 0 {
		return ProviderOperationResult{}, errs.ErrInvalidArgument
	}
	plan, err := s.buildWritePlan(providerWritePlan{
		operationType:     enum.ProviderOperationUpdatePullRequest,
		actionKey:         accesscatalog.ActionProviderPullRequestWrite,
		providerSlug:      target.ProviderSlug,
		externalAccountID: input.ExternalAccountID,
		targetRef:         targetRef,
		scopeType:         providerUsageScopeRepository,
		scopeID:           providerScopeIDFromTarget(target),
		resultTarget:      targetCopy(target),
		meta:              input.Meta,
		executorRequest: providerclient.WriteRequest{
			CommandID:    providerCommandKey(input.Meta),
			TargetRef:    targetRef,
			ProviderSlug: target.ProviderSlug,
			UpdatePullRequest: &providerclient.UpdatePullRequestCommand{
				Target:                  toClientTarget(target),
				Title:                   optionalTextPtr(input.Title),
				Body:                    optionalTextPtr(input.Body),
				Labels:                  labelsPatch,
				AssigneeProviderLogins:  assigneesPatch,
				Milestone:               optionalTextPtr(input.Milestone),
				State:                   optionalTextPtr(input.State),
				BaseBranch:              optionalTextPtr(input.BaseBranch),
				MaintainerCanModify:     input.MaintainerCanModify,
				WatermarkJSON:           watermarkJSON,
				ExpectedProviderVersion: strings.TrimSpace(input.ExpectedProviderVersion),
			},
		},
		validateExpectedVersion: s.expectedWorkItemVersionCheck(target, input.Meta.ExpectedVersion),
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
		operationType:          enum.ProviderOperationUpdateRelationship,
		actionKey:              accesscatalog.ActionProviderRelationshipWrite,
		providerSlug:           input.Source.ProviderSlug,
		externalAccountID:      input.ExternalAccountID,
		targetRef:              targetRef,
		scopeType:              providerUsageScopeRepository,
		scopeID:                providerScopeIDFromTarget(input.Source),
		resultTarget:           resultTarget,
		meta:                   input.Meta,
		skipProviderCredential: true,
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
	now := s.clock.Now().UTC()
	replayed, replayedResult, err := s.replayProviderWrite(ctx, plan)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if replayed {
		return replayedResult, nil
	}
	if plan.validateExpectedVersion != nil {
		if err := plan.validateExpectedVersion(ctx); err != nil {
			return ProviderOperationResult{}, err
		}
	}
	startedOperation, err := s.startProviderWrite(ctx, plan, now)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	usage, failure, err := s.resolveWriteAccount(ctx, plan)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	if failure != nil {
		return s.finalizeProviderWrite(ctx, plan, startedOperation, providerclient.WriteResult{}, *failure)
	}
	if plan.meta.OperationPolicyContext.ApprovalRequired && plan.meta.ApprovalGateRef.ApprovalID == "" {
		return s.finalizeProviderWrite(ctx, plan, startedOperation, providerclient.WriteResult{}, providerWriteFailure{
			status:       enum.ProviderOperationStatusDenied,
			errorCode:    writeFailureApprovalRequired,
			errorMessage: "approval gate reference is required",
		})
	}
	executor := s.providerWriteExecutors[plan.providerSlug]
	if executor == nil {
		return s.finalizeProviderWrite(ctx, plan, startedOperation, providerclient.WriteResult{}, providerWriteFailure{
			status:       enum.ProviderOperationStatusFailed,
			errorCode:    writeFailureExecutorUnavailable,
			errorMessage: "provider write executor is unavailable",
		})
	}
	credential := providerclient.AccountCredential{
		ExternalAccountID: plan.externalAccountID,
		ProviderSlug:      plan.providerSlug,
	}
	if !plan.skipProviderCredential {
		secret, failure, resolveErr := s.resolveWriteSecret(ctx, usage)
		if resolveErr != nil {
			return ProviderOperationResult{}, resolveErr
		}
		if failure != nil {
			return s.finalizeProviderWrite(ctx, plan, startedOperation, providerclient.WriteResult{}, *failure)
		}
		defer secret.Clear()
		credential.Token = secret
	}
	result, execErr := executor.Execute(ctx, writeRequestWithCredential(plan.executorRequest, credential))
	if execErr != nil {
		return s.finalizeProviderWrite(ctx, plan, startedOperation, providerclient.WriteResult{}, mapProviderWriteError(execErr))
	}
	_ = usage
	return s.finalizeProviderWrite(ctx, plan, startedOperation, result, providerWriteFailure{status: enum.ProviderOperationStatusSucceeded})
}

func (s *Service) startProviderWrite(ctx context.Context, plan providerWritePlan, now time.Time) (entity.ProviderOperation, error) {
	operation := entity.ProviderOperation{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		CommandID:              providerCommandKey(plan.meta),
		ActorID:                actorUUIDPtr(plan.meta.Actor),
		ExternalAccountID:      plan.externalAccountID,
		ProviderSlug:           plan.providerSlug,
		OperationType:          plan.operationType,
		TargetRef:              plan.targetRef,
		Status:                 enum.ProviderOperationStatusInProgress,
		OperationPolicyContext: plan.meta.OperationPolicyContext,
		ApprovalGateRef:        plan.meta.ApprovalGateRef,
		StartedAt:              now,
	}
	stored, inserted, err := s.repository.RecordProviderOperation(ctx, operation)
	if err == nil {
		if inserted && stored.Status == enum.ProviderOperationStatusInProgress {
			return stored, nil
		}
		if sameProviderWriteReplay(stored, plan) {
			return entity.ProviderOperation{}, errs.ErrConflict
		}
		return entity.ProviderOperation{}, errs.ErrConflict
	}
	if !errors.Is(err, errs.ErrConflict) {
		return entity.ProviderOperation{}, err
	}
	existing, replayErr := s.repository.GetProviderOperationByCommand(ctx, plan.operationType, providerCommandKey(plan.meta))
	if replayErr != nil {
		return entity.ProviderOperation{}, err
	}
	if !sameProviderWriteReplay(existing, plan) {
		return entity.ProviderOperation{}, errs.ErrConflict
	}
	return entity.ProviderOperation{}, errs.ErrConflict
}

func (s *Service) replayProviderWrite(ctx context.Context, plan providerWritePlan) (bool, ProviderOperationResult, error) {
	commandKey := providerCommandKey(plan.meta)
	if strings.TrimSpace(commandKey) == "" {
		return false, ProviderOperationResult{}, nil
	}
	stored, err := s.repository.GetProviderOperationByCommand(ctx, plan.operationType, commandKey)
	if errors.Is(err, errs.ErrNotFound) {
		return false, ProviderOperationResult{}, nil
	}
	if err != nil {
		return false, ProviderOperationResult{}, err
	}
	if !sameProviderWriteReplay(stored, plan) {
		return false, ProviderOperationResult{}, errs.ErrConflict
	}
	if stored.Status == enum.ProviderOperationStatusInProgress {
		return false, ProviderOperationResult{}, errs.ErrConflict
	}
	return true, ProviderOperationResult{
		ProviderOperation: &stored,
		Result: ProviderOperationCommandResult{
			Target:            plan.resultTarget,
			ResultRef:         stored.ResultRef,
			ProviderVersion:   stored.ProviderVersion,
			EmittedEventTypes: []string{providerOperationEventType(stored.Status)},
		},
	}, nil
}

func sameProviderWriteReplay(stored entity.ProviderOperation, plan providerWritePlan) bool {
	return stored.CommandID == providerCommandKey(plan.meta) &&
		sameOptionalUUID(stored.ActorID, actorUUIDPtr(plan.meta.Actor)) &&
		stored.ExternalAccountID == plan.externalAccountID &&
		stored.ProviderSlug == plan.providerSlug &&
		stored.OperationType == plan.operationType &&
		stored.TargetRef == plan.targetRef &&
		sameCanonicalJSON(stored.OperationPolicyContext, plan.meta.OperationPolicyContext) &&
		sameCanonicalJSON(stored.ApprovalGateRef, plan.meta.ApprovalGateRef)
}

func sameCanonicalJSON(left any, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func sameOptionalUUID(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s *Service) resolveWriteSecret(ctx context.Context, usage ExternalAccountUsageResult) (secretresolver.SecretValue, *providerWriteFailure, error) {
	if s.secretResolver == nil {
		return secretresolver.SecretValue{}, &providerWriteFailure{
			status:       enum.ProviderOperationStatusRetryableFailed,
			errorCode:    writeFailureSecretUnavailable,
			errorMessage: "secret resolver is unavailable",
		}, nil
	}
	secret, err := s.secretResolver.Resolve(ctx, secretresolver.SecretRef{
		StoreType: usage.SecretStoreType,
		StoreRef:  usage.SecretStoreRef,
	})
	if err == nil {
		return secret, nil, nil
	}
	failure := providerWriteFailure{
		status:       enum.ProviderOperationStatusRetryableFailed,
		errorCode:    writeFailureSecretUnavailable,
		errorMessage: "provider credential is unavailable",
	}
	if errors.Is(mapSecretResolverError(err), errs.ErrPreconditionFailed) {
		failure.status = enum.ProviderOperationStatusFailed
	}
	return secretresolver.SecretValue{}, &failure, nil
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

func (s *Service) finalizeProviderWrite(ctx context.Context, plan providerWritePlan, startedOperation entity.ProviderOperation, executorResult providerclient.WriteResult, failure providerWriteFailure) (ProviderOperationResult, error) {
	finishedAt := s.clock.Now().UTC()
	operation := entity.ProviderOperation{
		Base: entity.Base{
			ID:        startedOperation.ID,
			Version:   startedOperation.Version,
			CreatedAt: startedOperation.CreatedAt,
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
		StartedAt:              startedOperation.StartedAt,
		FinishedAt:             &finishedAt,
	}
	outboxEvent, err := s.providerOperationOutbox(operation)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	projectionUpdate, providerEvents, projectionOutboxEvents, projectionResult, err := s.providerWriteProjection(ctx, plan, executorResult, finishedAt)
	if err != nil {
		return ProviderOperationResult{}, err
	}
	outboxEvents := append([]entity.OutboxEvent{outboxEvent}, projectionOutboxEvents...)
	storedOperation, err := s.repository.ApplyProviderOperation(ctx, providerrepo.ProviderOperationCompletion{
		Operation:        operation,
		ProjectionUpdate: projectionUpdate,
		ProviderEvents:   providerEvents,
		OutboxEvents:     outboxEvents,
	})
	if err != nil {
		return ProviderOperationResult{}, err
	}
	return ProviderOperationResult{
		ProviderOperation:  &storedOperation,
		WorkItemProjection: projectionResult.WorkItem,
		CommentProjection:  projectionResult.Comment,
		Relationship:       projectionResult.Relationship,
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

type providerWriteProjectionResult struct {
	WorkItem     *entity.ProviderWorkItemProjection
	Comment      *entity.ProviderCommentProjection
	Relationship *entity.ProviderRelationship
}

func (s *Service) providerWriteProjection(ctx context.Context, plan providerWritePlan, result providerclient.WriteResult, now time.Time) (providerrepo.ProjectionUpdate, []entity.ProviderEvent, []entity.OutboxEvent, providerWriteProjectionResult, error) {
	var update providerrepo.ProjectionUpdate
	var providerEvents []entity.ProviderEvent
	var outboxEvents []entity.OutboxEvent
	var commandResult providerWriteProjectionResult
	if result.WorkItem != nil {
		workItem, relationships, err := workItemProjectionFromSnapshot(*result.WorkItem, now)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, providerWriteProjectionResult{}, err
		}
		update.WorkItem = &workItem
		update.Relationships = append(update.Relationships, relationships...)
		commandResult.WorkItem = &workItem
		providerEvent, outboxEvent, err := s.providerWriteWorkItemEvents(plan, workItem)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, providerWriteProjectionResult{}, err
		}
		providerEvents = append(providerEvents, providerEvent)
		outboxEvents = append(outboxEvents, outboxEvent)
		if plan.operationType == enum.ProviderOperationCreateBootstrapPullRequest {
			bootstrapEvent, err := s.providerRepositoryBootstrapCompletedOutbox(plan, workItem)
			if err != nil {
				return providerrepo.ProjectionUpdate{}, nil, nil, providerWriteProjectionResult{}, err
			}
			outboxEvents = append(outboxEvents, bootstrapEvent)
		}
	}
	if result.Comment != nil {
		comment := commentProjectionFromSnapshot(*result.Comment, now)
		update.Comments = append(update.Comments, comment)
		commandResult.Comment = &comment
		providerEvent, outboxEvent, err := s.providerWriteCommentEvents(plan, comment, *result.Comment)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, providerWriteProjectionResult{}, err
		}
		providerEvents = append(providerEvents, providerEvent)
		outboxEvents = append(outboxEvents, outboxEvent)
	}
	if result.Relationship != nil {
		relationship, err := s.providerWriteRelationship(ctx, *result.Relationship, now)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, providerWriteProjectionResult{}, err
		}
		update.Relationships = append(update.Relationships, relationship)
		commandResult.Relationship = &relationship
		outboxEvent, err := s.providerWriteRelationshipOutbox(plan, relationship)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, providerWriteProjectionResult{}, err
		}
		outboxEvents = append(outboxEvents, outboxEvent)
	}
	return update, providerEvents, outboxEvents, commandResult, nil
}

func (s *Service) providerWriteWorkItemEvents(plan providerWritePlan, workItem entity.ProviderWorkItemProjection) (entity.ProviderEvent, entity.OutboxEvent, error) {
	providerEventID := s.ids.New()
	providerPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(plan.providerSlug),
		ProviderEventID:      providerEventID.String(),
		ExternalAccountID:    plan.externalAccountID.String(),
		RepositoryFullName:   workItem.RepositoryFullName,
		ProviderWorkItemID:   workItem.ProviderWorkItemID,
		WorkItemProjectionID: workItem.ID.String(),
		Kind:                 string(workItem.Kind),
		Number:               workItem.Number,
		WatermarkStatus:      string(workItem.WatermarkStatus),
		DriftStatus:          string(workItem.DriftStatus),
		OperationType:        string(plan.operationType),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	providerEvent := entity.ProviderEvent{
		ID:            providerEventID,
		EventType:     providerEventWorkItemSynced,
		AggregateType: providerAggregateWorkItem,
		AggregateID:   workItem.ProviderWorkItemID,
		PayloadJSON:   providerPayload,
		OccurredAt:    workItem.SyncedAt,
	}
	outboxPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(plan.providerSlug),
		ExternalAccountID:    plan.externalAccountID.String(),
		RepositoryFullName:   workItem.RepositoryFullName,
		ProviderWorkItemID:   workItem.ProviderWorkItemID,
		WorkItemProjectionID: workItem.ID.String(),
		Kind:                 string(workItem.Kind),
		Number:               workItem.Number,
		WatermarkStatus:      string(workItem.WatermarkStatus),
		DriftStatus:          string(workItem.DriftStatus),
		OperationType:        string(plan.operationType),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	return providerEvent, outboxEventRecord(s.ids.New(), providerEventWorkItemSynced, providerAggregateWorkItem, workItem.ID, outboxPayload, workItem.SyncedAt), nil
}

func (s *Service) providerWriteCommentEvents(plan providerWritePlan, comment entity.ProviderCommentProjection, snapshot value.ProviderCommentSnapshot) (entity.ProviderEvent, entity.OutboxEvent, error) {
	occurredAt := comment.UpdatedAt
	if comment.ProviderUpdatedAt != nil {
		occurredAt = *comment.ProviderUpdatedAt
	}
	providerEventID := s.ids.New()
	providerPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:        string(plan.providerSlug),
		ProviderEventID:     providerEventID.String(),
		ExternalAccountID:   plan.externalAccountID.String(),
		ProviderWorkItemID:  snapshot.ProviderWorkItemID,
		ProviderCommentID:   comment.ProviderCommentID,
		CommentProjectionID: comment.ID.String(),
		Kind:                string(comment.Kind),
		ReviewState:         string(comment.ReviewState),
		OperationType:       string(plan.operationType),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	providerEvent := entity.ProviderEvent{
		ID:            providerEventID,
		EventType:     providerEventCommentSynced,
		AggregateType: providerAggregateComment,
		AggregateID:   comment.ProviderCommentID,
		PayloadJSON:   providerPayload,
		OccurredAt:    occurredAt,
	}
	outboxPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:        string(plan.providerSlug),
		ExternalAccountID:   plan.externalAccountID.String(),
		ProviderWorkItemID:  snapshot.ProviderWorkItemID,
		ProviderCommentID:   comment.ProviderCommentID,
		CommentProjectionID: comment.ID.String(),
		Kind:                string(comment.Kind),
		ReviewState:         string(comment.ReviewState),
		OperationType:       string(plan.operationType),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	return providerEvent, outboxEventRecord(s.ids.New(), providerEventCommentSynced, providerAggregateComment, comment.ID, outboxPayload, occurredAt), nil
}

func (s *Service) providerWriteRelationship(ctx context.Context, result providerclient.RelationshipResult, now time.Time) (entity.ProviderRelationship, error) {
	source, err := s.repository.GetWorkItemProjection(ctx, workItemLookupFromClientTarget(result.Source))
	if err != nil {
		return entity.ProviderRelationship{}, err
	}
	targetProviderRef := strings.TrimSpace(result.TargetProviderRef)
	var targetID *uuid.UUID
	if result.Target != nil {
		target, targetErr := s.repository.GetWorkItemProjection(ctx, workItemLookupFromClientTarget(*result.Target))
		if targetErr != nil {
			return entity.ProviderRelationship{}, targetErr
		}
		targetID = &target.ID
		if targetProviderRef == "" {
			targetProviderRef = target.ProviderWorkItemID
		}
	}
	relationshipType := strings.TrimSpace(result.RelationshipType)
	if relationshipType == "" || !validRelationshipSources([]enum.RelationshipSource{result.SourceKind}) || !validRelationshipConfidenceLevels([]enum.RelationshipConfidence{result.Confidence}) {
		return entity.ProviderRelationship{}, errs.ErrInvalidArgument
	}
	stableTarget := targetProviderRef
	if targetID != nil {
		stableTarget = targetID.String()
	}
	if stableTarget == "" {
		return entity.ProviderRelationship{}, errs.ErrInvalidArgument
	}
	return entity.ProviderRelationship{
		ID:                stableUUID("relationship", source.ID.String(), relationshipType, stableTarget),
		Version:           1,
		SourceWorkItemID:  source.ID,
		TargetWorkItemID:  targetID,
		TargetProviderRef: targetProviderRef,
		RelationshipType:  relationshipType,
		Source:            result.SourceKind,
		Confidence:        result.Confidence,
		CreatedAt:         now,
	}, nil
}

func (s *Service) providerWriteRelationshipOutbox(plan providerWritePlan, relationship entity.ProviderRelationship) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:      string(plan.providerSlug),
		ExternalAccountID: plan.externalAccountID.String(),
		RelationshipID:    relationship.ID.String(),
		RelationshipType:  relationship.RelationshipType,
		Source:            string(relationship.Source),
		OperationType:     string(plan.operationType),
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventRelationshipSynced, providerAggregateRelationship, relationship.ID, payload, relationship.CreatedAt), nil
}

func (s *Service) providerRepositoryBootstrapCompletedOutbox(plan providerWritePlan, workItem entity.ProviderWorkItemProjection) (entity.OutboxEvent, error) {
	projectID := ""
	if workItem.ProjectID != nil {
		projectID = workItem.ProjectID.String()
	}
	repositoryID := ""
	aggregateID := stableUUID("provider-repository-bootstrap", string(plan.providerSlug), plan.scopeID, workItem.RepositoryFullName)
	if workItem.RepositoryID != nil {
		repositoryID = workItem.RepositoryID.String()
		aggregateID = *workItem.RepositoryID
	}
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(plan.providerSlug),
		ExternalAccountID:    plan.externalAccountID.String(),
		RepositoryFullName:   workItem.RepositoryFullName,
		ProjectID:            projectID,
		RepositoryID:         repositoryID,
		ProviderWorkItemID:   workItem.ProviderWorkItemID,
		WorkItemProjectionID: workItem.ID.String(),
		OperationType:        string(plan.operationType),
		BootstrapMode:        "branch_pr",
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventRepositoryBootstrapCompleted, providerAggregateRepository, aggregateID, payload, workItem.SyncedAt), nil
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

func repositoryTarget(providerSlug enum.ProviderSlug, target ProviderTarget) (ProviderTarget, error) {
	if !validProviderSlug(providerSlug) {
		return ProviderTarget{}, errs.ErrInvalidArgument
	}
	target.ProviderSlug = enum.ProviderSlug(strings.TrimSpace(string(target.ProviderSlug)))
	if target.ProviderSlug == "" {
		target.ProviderSlug = providerSlug
	}
	if target.ProviderSlug != providerSlug {
		return ProviderTarget{}, errs.ErrInvalidArgument
	}
	target.RepositoryFullName = strings.TrimSpace(target.RepositoryFullName)
	target.ProviderRepositoryID = strings.TrimSpace(target.ProviderRepositoryID)
	target.WebURL = strings.TrimSpace(target.WebURL)
	target.ProviderObjectID = strings.TrimSpace(target.ProviderObjectID)
	if target.WorkItemKind != "" || target.Number != 0 || target.ProviderObjectID != "" {
		return ProviderTarget{}, errs.ErrInvalidArgument
	}
	if target.RepositoryFullName == "" && target.ProviderRepositoryID == "" && target.WebURL == "" {
		return ProviderTarget{}, errs.ErrInvalidArgument
	}
	return target, nil
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

func normalizeBootstrapFiles(files []BootstrapFile) ([]BootstrapFile, error) {
	if len(files) == 0 || len(files) > maxBootstrapFiles {
		return nil, errs.ErrInvalidArgument
	}
	result := make([]BootstrapFile, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	totalSize := 0
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if !validBootstrapFilePath(path) {
			return nil, errs.ErrInvalidArgument
		}
		if _, exists := seen[path]; exists {
			return nil, errs.ErrInvalidArgument
		}
		seen[path] = struct{}{}
		contentSize := len([]byte(file.Content))
		if contentSize > maxBootstrapFileContentBytes {
			return nil, errs.ErrInvalidArgument
		}
		totalSize += contentSize
		if totalSize > maxBootstrapTotalContentBytes {
			return nil, errs.ErrInvalidArgument
		}
		result = append(result, BootstrapFile{
			Path:       path,
			Content:    file.Content,
			Executable: file.Executable,
		})
	}
	return result, nil
}

func validBootstrapFilePath(path string) bool {
	if path == "" ||
		strings.HasPrefix(path, "/") ||
		strings.HasSuffix(path, "/") ||
		strings.Contains(path, "\\") ||
		strings.Contains(path, "//") ||
		strings.Contains(path, "\x00") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validCreateProjectRepositoryWriteInput(meta value.CommandMeta, projectID uuid.UUID, repositoryID uuid.UUID, providerSlug enum.ProviderSlug, externalAccountID uuid.UUID) bool {
	return validCommandIdentity(meta) &&
		projectID != uuid.Nil &&
		repositoryID != uuid.Nil &&
		validProviderSlug(providerSlug) &&
		externalAccountID != uuid.Nil
}

func toClientBootstrapFiles(files []BootstrapFile) []providerclient.BootstrapFile {
	result := make([]providerclient.BootstrapFile, 0, len(files))
	for _, file := range files {
		result = append(result, providerclient.BootstrapFile{
			Path:       file.Path,
			Content:    file.Content,
			Executable: file.Executable,
		})
	}
	return result
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
		relationship, err := s.repository.GetRelationshipByIdentity(ctx, query.RelationshipLookup{
			SourceWorkItemID:  source.ID,
			TargetWorkItemID:  targetID,
			TargetProviderRef: targetProviderRef,
			RelationshipType:  strings.TrimSpace(input.RelationshipType),
		})
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				return errs.ErrConflict
			}
			return err
		}
		if relationship.Version != *expectedVersion {
			return errs.ErrConflict
		}
		return nil
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

func workItemLookupFromClientTarget(target providerclient.Target) query.ProviderTargetLookup {
	return query.ProviderTargetLookup{
		ProviderSlug:       target.ProviderSlug,
		RepositoryFullName: strings.TrimSpace(target.RepositoryFullName),
		Kind:               target.WorkItemKind,
		Number:             target.Number,
		ProviderObjectID:   strings.TrimSpace(target.ProviderObjectID),
		WebURL:             strings.TrimSpace(target.WebURL),
	}
}
