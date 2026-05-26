package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	followUpTitleLimit   = 200
	followUpSummaryLimit = 1000
	followUpHintLimit    = 128
	followUpTypeLimit    = 64
	followUpBodyLimit    = 4000
	followUpRefTextLimit = 512
)

type followUpIntentCommandPayload struct {
	FollowUpIntent entity.FollowUpIntent `json:"follow_up_intent"`
}

type followUpProviderCommandPayload struct {
	FollowUpIntent entity.FollowUpIntent       `json:"follow_up_intent"`
	CreateIssue    followUpCreateIssueSnapshot `json:"create_issue"`
}

type followUpCreateIssueSnapshot struct {
	FollowUpIntentID       string                         `json:"follow_up_intent_id"`
	ProjectID              string                         `json:"project_id"`
	RepositoryID           string                         `json:"repository_id"`
	ProviderSlug           string                         `json:"provider_slug"`
	ExternalAccountID      string                         `json:"external_account_id"`
	RepositoryTarget       ProviderCommandTarget          `json:"repository_target"`
	Title                  string                         `json:"title"`
	BodyDigest             string                         `json:"body_digest"`
	Labels                 []string                       `json:"labels,omitempty"`
	AssigneeProviderLogins []string                       `json:"assignee_provider_logins,omitempty"`
	Milestone              string                         `json:"milestone,omitempty"`
	WorkItemType           string                         `json:"work_item_type"`
	WatermarkJSON          string                         `json:"watermark_json,omitempty"`
	OperationPolicyContext ProviderOperationPolicyContext `json:"operation_policy_context"`
	ApprovalGateRef        ProviderApprovalGateReference  `json:"approval_gate_ref,omitempty"`
}

func (s *Service) CreateFollowUpIntent(ctx context.Context, input CreateFollowUpIntentInput) (entity.FollowUpIntent, error) {
	if err := s.requireRepository(); err != nil {
		return entity.FollowUpIntent{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.FollowUpIntent{}, err
	}
	idempotencyKey, err := followUpIntentIdempotencyKey(input.Meta)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	intent, err := s.normalizeFollowUpIntent(ctx, session, input, idempotencyKey)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreateFollowUpIntent, enum.CommandAggregateTypeFollowUp, followUpIntentFromPayload, verifyFollowUpIntentReplay(intent, s.repository.GetFollowUpIntent)); ok || err != nil {
		return replay, err
	}
	if isTerminalSessionStatus(session.Status) {
		return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	intent.ID = s.idGenerator.New()
	intent.Version = 1
	intent.CreatedAt = now
	intent.UpdatedAt = now
	payload, err := marshalCommandPayload(followUpIntentCommandPayload{FollowUpIntent: intent})
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	result, err := commandResult(input.Meta, operationCreateFollowUpIntent, enum.CommandAggregateTypeFollowUp, intent.ID, payload, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	event, err := followUpRequestedEvent(s.idGenerator.New(), intent, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	return intent, s.repository.CreateFollowUpIntentWithResult(ctx, intent, result, event)
}

func (s *Service) DispatchFollowUpIntent(ctx context.Context, input DispatchFollowUpIntentInput) (entity.FollowUpIntent, error) {
	if err := s.requireRepository(); err != nil {
		return entity.FollowUpIntent{}, err
	}
	if err := validateID(input.FollowUpIntentID); err != nil {
		return entity.FollowUpIntent{}, err
	}
	if _, err := expectedVersion(input.Meta); err != nil {
		return entity.FollowUpIntent{}, err
	}
	intent, err := s.repository.GetFollowUpIntent(ctx, input.FollowUpIntentID)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	createIssue, snapshot, err := normalizeFollowUpCreateIssueCommand(intent, input)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationDispatchFollowUpIntent, enum.CommandAggregateTypeFollowUp, followUpProviderIntentFromPayload, verifyFollowUpProviderReplay(snapshot, s.repository.GetFollowUpIntent)); ok || err != nil {
		return replay, err
	}
	previousVersion := *input.Meta.ExpectedVersion
	if intent.Version != previousVersion {
		return entity.FollowUpIntent{}, errs.ErrConflict
	}
	if !dispatchableFollowUpStatus(intent.Status) {
		return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
	}
	providerResult, err := s.providerIssueCreator.CreateIssue(ctx, createIssue)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	next, err := applyFollowUpProviderIssueResult(intent, providerResult)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	now := s.clock.Now()
	previousStatus := string(intent.Status)
	next.Version++
	next.UpdatedAt = now
	payload, err := marshalCommandPayload(followUpProviderCommandPayload{FollowUpIntent: next, CreateIssue: snapshot})
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	result, err := commandResult(input.Meta, operationDispatchFollowUpIntent, enum.CommandAggregateTypeFollowUp, next.ID, payload, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	event, err := followUpResultEvent(s.idGenerator.New(), previousStatus, next, now)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	return next, s.repository.UpdateFollowUpIntentWithResult(ctx, next, previousVersion, result, event)
}

func (s *Service) normalizeFollowUpIntent(ctx context.Context, session entity.AgentSession, input CreateFollowUpIntentInput, idempotencyKey string) (entity.FollowUpIntent, error) {
	runID := input.RunID
	fromStageID := input.FromStageID
	toStageID := input.ToStageID
	acceptanceID := input.AcceptanceResultID
	var run *entity.AgentRun
	if acceptanceID != nil {
		acceptance, err := s.repository.GetAcceptanceResult(ctx, *acceptanceID)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		if acceptance.SessionID != session.ID {
			return entity.FollowUpIntent{}, errs.ErrConflict
		}
		if !followUpAcceptanceStatus(acceptance.Status) {
			return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
		}
		if err := bindOptionalUUID(&runID, acceptance.RunID); err != nil {
			return entity.FollowUpIntent{}, err
		}
		if err := bindOptionalUUID(&fromStageID, acceptance.StageID); err != nil {
			return entity.FollowUpIntent{}, err
		}
	}
	if runID != nil {
		loaded, err := s.repository.GetAgentRun(ctx, *runID)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		if loaded.SessionID != session.ID {
			return entity.FollowUpIntent{}, errs.ErrConflict
		}
		if !followUpRunStatus(loaded.Status) {
			return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
		}
		if err := bindOptionalUUID(&fromStageID, loaded.StageID); err != nil {
			return entity.FollowUpIntent{}, err
		}
		run = &loaded
	}
	providerTarget, err := normalizeFollowUpProviderTarget(input.ProviderTarget)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	if run != nil {
		runTarget, err := normalizeFollowUpProviderTarget(run.ProviderTarget)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		providerTarget = mergeProviderTarget(providerTarget, runTarget)
	}
	if providerTarget.WorkItemRef == "" && session.ProviderWorkItemRef != "" {
		ref, err := normalizeAcceptanceTargetRef(session.ProviderWorkItemRef)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		providerTarget.WorkItemRef = ref
	}
	if providerTargetEmpty(providerTarget) {
		return entity.FollowUpIntent{}, errs.ErrInvalidArgument
	}
	providerOperationRef, err := normalizeFollowUpOptionalRef(input.ProviderOperationRef)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	instructionDigest, err := normalizeFollowUpDigest(input.InstructionBodyDigest)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	safeTitle, err := normalizeFollowUpText(input.SafeTitle, followUpTitleLimit, true)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	safeSummary, err := normalizeFollowUpText(input.SafeSummary, followUpSummaryLimit, false)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	roleHint, err := normalizeFollowUpHint(input.RoleHint)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	stageHint, err := normalizeFollowUpHint(input.StageHint)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	flowVersionID := session.FlowVersionID
	if run != nil {
		flowVersionID = chooseUUID(run.FlowVersionID, flowVersionID)
	}
	providerWorkItemType, err := s.normalizeFollowUpTypeForStages(ctx, flowVersionID, fromStageID, toStageID, input.ProviderWorkItemType)
	if err != nil {
		return entity.FollowUpIntent{}, err
	}
	return entity.FollowUpIntent{
		SessionID:             session.ID,
		RunID:                 runID,
		FromStageID:           fromStageID,
		ToStageID:             toStageID,
		AcceptanceResultID:    acceptanceID,
		ProviderTarget:        providerTarget,
		ProviderWorkItemType:  providerWorkItemType,
		ProviderOperationRef:  providerOperationRef,
		InstructionBodyDigest: instructionDigest,
		SafeTitle:             safeTitle,
		SafeSummary:           safeSummary,
		RoleHint:              roleHint,
		StageHint:             stageHint,
		IdempotencyKey:        idempotencyKey,
		Status:                enum.FollowUpIntentStatusRequested,
	}, nil
}

func followUpIntentIdempotencyKey(meta value.CommandMeta) (string, error) {
	identity, err := commandIdentity(meta, operationCreateFollowUpIntent)
	if err != nil {
		return "", err
	}
	return commandResultKey(identity), nil
}

func dispatchableFollowUpStatus(status enum.FollowUpIntentStatus) bool {
	return status == enum.FollowUpIntentStatusPlanned || status == enum.FollowUpIntentStatusRequested
}

func normalizeFollowUpCreateIssueCommand(intent entity.FollowUpIntent, input DispatchFollowUpIntentInput) (ProviderCreateIssueInput, followUpCreateIssueSnapshot, error) {
	if input.ProjectID == uuid.Nil || input.RepositoryID == uuid.Nil || input.ExternalAccountID == uuid.Nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, errs.ErrInvalidArgument
	}
	providerSlug, err := normalizeFollowUpWorkItemType(input.ProviderSlug)
	if err != nil || providerSlug == "" {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, errs.ErrInvalidArgument
	}
	repositoryTarget, err := normalizeFollowUpRepositoryTarget(providerSlug, input.RepositoryTarget)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	labels, err := normalizeFollowUpStringSet(input.Labels)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	assignees, err := normalizeFollowUpStringSet(input.AssigneeProviderLogins)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	milestone, err := normalizeFollowUpText(input.Milestone, followUpHintLimit, false)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	bodyHint, err := normalizeFollowUpText(input.SafeBodyHint, followUpBodyLimit, false)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	body := followUpIssueBody(intent, bodyHint)
	watermarkJSON, err := normalizeFollowUpCommandJSON(input.WatermarkJSON)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	changedFields := followUpCreateIssueChangedFields(milestone != "", intent.ProviderWorkItemType != "", len(watermarkJSON) > 0)
	policy, err := normalizeFollowUpProviderPolicy(input.OperationPolicyContext, input.ProjectID, input.RepositoryID, providerSlug, changedFields)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	approval, err := normalizeFollowUpApprovalGateRef(input.ApprovalGateRef)
	if err != nil {
		return ProviderCreateIssueInput{}, followUpCreateIssueSnapshot{}, err
	}
	command := ProviderCreateIssueInput{
		Meta:                   input.Meta,
		ProjectID:              input.ProjectID,
		RepositoryID:           input.RepositoryID,
		ProviderSlug:           providerSlug,
		RepositoryTarget:       repositoryTarget,
		Title:                  intent.SafeTitle,
		Body:                   body,
		Labels:                 labels,
		AssigneeProviderLogins: assignees,
		Milestone:              milestone,
		WorkItemType:           intent.ProviderWorkItemType,
		WatermarkJSON:          watermarkJSON,
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
		ExternalAccountID:      input.ExternalAccountID,
	}
	snapshot := followUpCreateIssueSnapshot{
		FollowUpIntentID:       intent.ID.String(),
		ProjectID:              input.ProjectID.String(),
		RepositoryID:           input.RepositoryID.String(),
		ProviderSlug:           providerSlug,
		ExternalAccountID:      input.ExternalAccountID.String(),
		RepositoryTarget:       repositoryTarget,
		Title:                  command.Title,
		BodyDigest:             followUpBodyDigest(command.Body),
		Labels:                 append([]string(nil), labels...),
		AssigneeProviderLogins: append([]string(nil), assignees...),
		Milestone:              milestone,
		WorkItemType:           command.WorkItemType,
		WatermarkJSON:          string(watermarkJSON),
		OperationPolicyContext: policy,
		ApprovalGateRef:        approval,
	}
	return command, snapshot, nil
}

func normalizeFollowUpRepositoryTarget(providerSlug string, target ProviderCommandTarget) (ProviderCommandTarget, error) {
	target.ProviderSlug = strings.TrimSpace(target.ProviderSlug)
	if target.ProviderSlug == "" {
		target.ProviderSlug = providerSlug
	}
	if target.ProviderSlug != providerSlug || target.WorkItemKind != "" || target.Number != 0 || target.ProviderObjectID != "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	var err error
	if target.RepositoryFullName, err = normalizeFollowUpRefText(target.RepositoryFullName, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.ProviderRepositoryID, err = normalizeFollowUpRefText(target.ProviderRepositoryID, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.WebURL, err = normalizeFollowUpRefText(target.WebURL, false); err != nil {
		return ProviderCommandTarget{}, err
	}
	if target.RepositoryFullName == "" && target.ProviderRepositoryID == "" && target.WebURL == "" {
		return ProviderCommandTarget{}, errs.ErrInvalidArgument
	}
	return target, nil
}

func normalizeFollowUpRefText(value string, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", errs.ErrInvalidArgument
	}
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > followUpRefTextLimit || unsafeFollowUpText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if char < 32 || char == 127 {
			return "", errs.ErrInvalidArgument
		}
		if !safeFollowUpProviderChar(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
}

func safeFollowUpProviderChar(char rune) bool {
	if asciiLetter(char) || asciiDigit(char) {
		return true
	}
	switch char {
	case '-', '.', '_', ':', '/', '#', '@', '+', '=', ',', '%':
		return true
	default:
		return false
	}
}

func normalizeFollowUpStringSet(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, err := normalizeFollowUpHint(value)
		if err != nil {
			return nil, err
		}
		if item == "" {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeFollowUpCommandJSON(payload []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, nil
	}
	normalized, err := normalizeActivityJSON(trimmed, false)
	if err != nil {
		return nil, err
	}
	if string(normalized) == "{}" {
		return nil, nil
	}
	return normalized, nil
}

func followUpIssueBody(intent entity.FollowUpIntent, bodyHint string) string {
	if bodyHint != "" {
		return bodyHint
	}
	if intent.SafeSummary != "" {
		return intent.SafeSummary
	}
	return intent.SafeTitle
}

func followUpBodyDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func followUpCreateIssueChangedFields(hasMilestone bool, hasWorkItemType bool, hasWatermark bool) []string {
	fields := []string{"assignee_provider_logins", "body", "labels", "title"}
	if hasMilestone {
		fields = append(fields, "milestone")
	}
	if hasWorkItemType {
		fields = append(fields, "work_item_type")
	}
	if hasWatermark {
		fields = append(fields, "watermark_json")
	}
	sort.Strings(fields)
	return fields
}

func normalizeFollowUpProviderPolicy(policy ProviderOperationPolicyContext, projectID uuid.UUID, repositoryID uuid.UUID, providerSlug string, changedFields []string) (ProviderOperationPolicyContext, error) {
	if policy.RiskLevel == "" {
		return ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	policy.ProjectID = projectID.String()
	policy.RepositoryID = repositoryID.String()
	policy.OperationType = ProviderOperationTypeCreateIssue
	policy.TargetRef = providerSlug + ":repository:" + repositoryID.String()
	var err error
	policy.ChangedFields, err = normalizeFollowUpPolicyList(policy.ChangedFields)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if len(policy.ChangedFields) == 0 {
		policy.ChangedFields = append([]string(nil), changedFields...)
	}
	sort.Strings(policy.ChangedFields)
	if !sameStringSet(policy.ChangedFields, changedFields) {
		return ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	policy.RiskTags, err = normalizeFollowUpPolicyList(policy.RiskTags)
	if err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.Stage, err = normalizeFollowUpHint(policy.Stage); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.RoleID, err = normalizeFollowUpRefText(policy.RoleID, false); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.RoleKey, err = normalizeFollowUpHint(policy.RoleKey); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.PolicyVersion, err = normalizeFollowUpRefText(policy.PolicyVersion, false); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if policy.PolicySnapshotRef, err = normalizeFollowUpOptionalRef(policy.PolicySnapshotRef); err != nil {
		return ProviderOperationPolicyContext{}, err
	}
	if !validFollowUpRiskLevel(policy.RiskLevel) {
		return ProviderOperationPolicyContext{}, errs.ErrInvalidArgument
	}
	return policy, nil
}

func normalizeFollowUpPolicyList(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, err := normalizeFollowUpHint(value)
		if err != nil {
			return nil, err
		}
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result, nil
}

func validFollowUpRiskLevel(level string) bool {
	switch strings.TrimSpace(level) {
	case ProviderRiskLevelLow, ProviderRiskLevelMedium, ProviderRiskLevelHigh, ProviderRiskLevelCritical:
		return true
	default:
		return false
	}
}

func normalizeFollowUpApprovalGateRef(reference ProviderApprovalGateReference) (ProviderApprovalGateReference, error) {
	var err error
	if reference.ApprovalID, err = normalizeFollowUpRefText(reference.ApprovalID, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.GateType, err = normalizeFollowUpHint(reference.GateType); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.Decision, err = normalizeFollowUpHint(reference.Decision); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.DecidedByActorID, err = normalizeFollowUpRefText(reference.DecidedByActorID, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.DecidedAt, err = normalizeFollowUpRefText(reference.DecidedAt, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.EvidenceRef, err = normalizeFollowUpOptionalRef(reference.EvidenceRef); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	if reference.PolicyVersion, err = normalizeFollowUpRefText(reference.PolicyVersion, false); err != nil {
		return ProviderApprovalGateReference{}, err
	}
	empty := reference.ApprovalID == "" && reference.GateType == "" && reference.Decision == "" && reference.DecidedByActorID == "" && reference.DecidedAt == "" && reference.EvidenceRef == "" && reference.PolicyVersion == ""
	if empty {
		return ProviderApprovalGateReference{}, nil
	}
	if reference.ApprovalID == "" || reference.GateType == "" || reference.Decision == "" {
		return ProviderApprovalGateReference{}, errs.ErrInvalidArgument
	}
	return reference, nil
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func applyFollowUpProviderIssueResult(intent entity.FollowUpIntent, result ProviderIssueCommandResult) (entity.FollowUpIntent, error) {
	operationRef, err := normalizeFollowUpOptionalRef(result.ProviderOperationRef)
	if err != nil || operationRef == "" {
		return entity.FollowUpIntent{}, errs.ErrDependencyUnavailable
	}
	intent.ProviderOperationRef = operationRef
	switch result.Status {
	case ProviderOperationStatusSucceeded:
		workItemRef, err := followUpProviderResultRef(result)
		if err != nil {
			return entity.FollowUpIntent{}, err
		}
		intent.ProviderTarget.WorkItemRef = workItemRef
		intent.Status = enum.FollowUpIntentStatusCreated
	case ProviderOperationStatusFailed, ProviderOperationStatusRetryableFailed, ProviderOperationStatusDenied:
		intent.Status = enum.FollowUpIntentStatusFailed
	default:
		return entity.FollowUpIntent{}, errs.ErrDependencyUnavailable
	}
	return intent, nil
}

func followUpProviderResultRef(result ProviderIssueCommandResult) (string, error) {
	candidates := []string{
		result.ResultRef,
		result.Target.WebURL,
	}
	if result.Target.ProviderSlug != "" && result.Target.RepositoryFullName != "" && result.Target.Number > 0 {
		candidates = append(candidates, result.Target.ProviderSlug+":repo:"+result.Target.RepositoryFullName+":issue:"+strconv.FormatInt(result.Target.Number, 10))
	}
	if result.Target.ProviderSlug != "" && result.Target.ProviderObjectID != "" {
		candidates = append(candidates, result.Target.ProviderSlug+":object:"+result.Target.ProviderObjectID)
	}
	for _, candidate := range candidates {
		ref, err := normalizeFollowUpOptionalRef(candidate)
		if err == nil && ref != "" {
			return ref, nil
		}
	}
	return "", errs.ErrDependencyUnavailable
}

func bindOptionalUUID(target **uuid.UUID, candidate *uuid.UUID) error {
	if candidate == nil {
		return nil
	}
	if *target != nil && **target != *candidate {
		return errs.ErrConflict
	}
	*target = candidate
	return nil
}

func followUpRunStatus(status enum.AgentRunStatus) bool {
	return status == enum.AgentRunStatusCompleted || status == enum.AgentRunStatusFailed
}

func followUpAcceptanceStatus(status enum.AcceptanceStatus) bool {
	return status == enum.AcceptanceStatusPassed || status == enum.AcceptanceStatusSkipped
}

func (s *Service) normalizeFollowUpTypeForStages(ctx context.Context, flowVersionID *uuid.UUID, fromStageID *uuid.UUID, toStageID *uuid.UUID, requestedType string) (string, error) {
	workItemType, err := normalizeFollowUpWorkItemType(requestedType)
	if err != nil {
		return "", err
	}
	if fromStageID == nil && toStageID == nil {
		if workItemType == "" {
			return "", errs.ErrInvalidArgument
		}
		return workItemType, nil
	}
	if flowVersionID == nil {
		return "", errs.ErrInvalidArgument
	}
	version, err := s.repository.GetFlowVersion(ctx, *flowVersionID)
	if err != nil {
		return "", err
	}
	if !flowVersionHasStage(version, fromStageID) || !flowVersionHasStage(version, toStageID) {
		return "", errs.ErrInvalidArgument
	}
	transitionType, transitionFound := followUpTransitionType(version, fromStageID, toStageID)
	if fromStageID != nil && toStageID != nil && !transitionFound {
		return "", errs.ErrPreconditionFailed
	}
	if workItemType == "" {
		workItemType, err = normalizeFollowUpWorkItemType(transitionType)
		if err != nil {
			return "", err
		}
	}
	if workItemType == "" {
		return "", errs.ErrInvalidArgument
	}
	if transitionType != "" && workItemType != transitionType {
		return "", errs.ErrConflict
	}
	return workItemType, nil
}

func flowVersionHasStage(version entity.FlowVersion, stageID *uuid.UUID) bool {
	if stageID == nil {
		return true
	}
	for _, stage := range version.Stages {
		if stage.ID == *stageID {
			return true
		}
	}
	return false
}

func followUpTransitionType(version entity.FlowVersion, fromStageID *uuid.UUID, toStageID *uuid.UUID) (string, bool) {
	if toStageID == nil {
		return "", false
	}
	for _, transition := range version.Transitions {
		if sameOptionalUUID(transition.FromStageID, fromStageID) && transition.ToStageID == *toStageID {
			return transition.FollowUpType, true
		}
	}
	return "", false
}

func normalizeFollowUpProviderTarget(target value.ProviderTargetRef) (value.ProviderTargetRef, error) {
	var result value.ProviderTargetRef
	var err error
	if result.WorkItemRef, err = normalizeFollowUpOptionalRef(target.WorkItemRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	if result.PullRequestRef, err = normalizeFollowUpOptionalRef(target.PullRequestRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	if result.CommentRef, err = normalizeFollowUpOptionalRef(target.CommentRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	if result.ReviewSignalRef, err = normalizeFollowUpOptionalRef(target.ReviewSignalRef); err != nil {
		return value.ProviderTargetRef{}, err
	}
	return result, nil
}

func normalizeFollowUpOptionalRef(ref string) (string, error) {
	return normalizeAcceptanceTargetRef(ref)
}

func mergeProviderTarget(primary value.ProviderTargetRef, fallback value.ProviderTargetRef) value.ProviderTargetRef {
	if primary.WorkItemRef == "" {
		primary.WorkItemRef = fallback.WorkItemRef
	}
	if primary.PullRequestRef == "" {
		primary.PullRequestRef = fallback.PullRequestRef
	}
	if primary.CommentRef == "" {
		primary.CommentRef = fallback.CommentRef
	}
	if primary.ReviewSignalRef == "" {
		primary.ReviewSignalRef = fallback.ReviewSignalRef
	}
	return primary
}

func providerTargetEmpty(target value.ProviderTargetRef) bool {
	return target.WorkItemRef == "" && target.PullRequestRef == "" && target.CommentRef == "" && target.ReviewSignalRef == ""
}

func normalizeFollowUpWorkItemType(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > followUpTypeLimit {
		return "", errs.ErrInvalidArgument
	}
	for idx, char := range trimmed {
		if idx == 0 && !asciiLetter(char) {
			return "", errs.ErrInvalidArgument
		}
		if !asciiLetter(char) && !asciiDigit(char) && char != '-' && char != '_' && char != '.' {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
}

func normalizeFollowUpDigest(value string) (string, error) {
	return normalizeSHA256Digest(value)
}

func normalizeFollowUpText(value string, limit int, required bool) (string, error) {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		return "", errs.ErrInvalidArgument
	}
	if utf8.RuneCountInString(trimmed) > limit {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if char < 32 || char == 127 {
			return "", errs.ErrInvalidArgument
		}
	}
	if unsafeFollowUpText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func normalizeFollowUpHint(value string) (string, error) {
	return normalizeSafeIdentifier(value, followUpHintLimit, unsafeFollowUpText)
}

func unsafeFollowUpText(value string) bool {
	lower := strings.ToLower(value)
	markers := []string{
		"raw_provider_payload",
		"provider_payload",
		"workspace_file",
		"workspace_files",
		"prompt_text",
		"prompt_template",
		"flow_file",
		"transcript",
		"session_dump",
		"large_report",
		"report_body",
		"raw_report",
		"stdout",
		"stderr",
		"-----begin",
		"authorization:",
		"bearer ",
		"ghp_",
		"glpat-",
		"xoxb-",
		"akia",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func asciiLetter(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func asciiDigit(char rune) bool {
	return char >= '0' && char <= '9'
}

func asciiHex(char rune) bool {
	return asciiDigit(char) || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')
}

func followUpIntentFromPayload(payload []byte) (entity.FollowUpIntent, error) {
	var result followUpIntentCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.FollowUpIntent, err
}

func followUpProviderIntentFromPayload(payload []byte) (entity.FollowUpIntent, error) {
	var result followUpProviderCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.FollowUpIntent, err
}

func verifyFollowUpIntentReplay(expected entity.FollowUpIntent, load func(context.Context, uuid.UUID) (entity.FollowUpIntent, error)) func(context.Context, entity.CommandResult, entity.FollowUpIntent) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.FollowUpIntent) error {
		if replay.ID != result.AggregateID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if stored.ID != replay.ID || !sameFollowUpIntentRequest(stored, expected) {
			return errs.ErrConflict
		}
		return nil
	}
}

func verifyFollowUpProviderReplay(expected followUpCreateIssueSnapshot, load func(context.Context, uuid.UUID) (entity.FollowUpIntent, error)) func(context.Context, entity.CommandResult, entity.FollowUpIntent) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.FollowUpIntent) error {
		if replay.ID != result.AggregateID || replay.ID.String() != expected.FollowUpIntentID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		var payload followUpProviderCommandPayload
		if err := json.Unmarshal(result.ResultPayload, &payload); err != nil {
			return err
		}
		if stored.ID != replay.ID || !sameFollowUpCreateIssueSnapshot(payload.CreateIssue, expected) {
			return errs.ErrConflict
		}
		return nil
	}
}

func sameFollowUpIntentRequest(stored entity.FollowUpIntent, expected entity.FollowUpIntent) bool {
	return stored.SessionID == expected.SessionID &&
		sameOptionalUUID(stored.RunID, expected.RunID) &&
		sameOptionalUUID(stored.FromStageID, expected.FromStageID) &&
		sameOptionalUUID(stored.ToStageID, expected.ToStageID) &&
		sameOptionalUUID(stored.AcceptanceResultID, expected.AcceptanceResultID) &&
		stored.ProviderTarget == expected.ProviderTarget &&
		stored.ProviderWorkItemType == expected.ProviderWorkItemType &&
		stored.ProviderOperationRef == expected.ProviderOperationRef &&
		stored.InstructionBodyDigest == expected.InstructionBodyDigest &&
		stored.SafeTitle == expected.SafeTitle &&
		stored.SafeSummary == expected.SafeSummary &&
		stored.RoleHint == expected.RoleHint &&
		stored.StageHint == expected.StageHint &&
		stored.IdempotencyKey == expected.IdempotencyKey &&
		stored.Status == expected.Status
}

func sameFollowUpCreateIssueSnapshot(left followUpCreateIssueSnapshot, right followUpCreateIssueSnapshot) bool {
	left.Labels = append([]string(nil), left.Labels...)
	right.Labels = append([]string(nil), right.Labels...)
	left.AssigneeProviderLogins = append([]string(nil), left.AssigneeProviderLogins...)
	right.AssigneeProviderLogins = append([]string(nil), right.AssigneeProviderLogins...)
	sort.Strings(left.Labels)
	sort.Strings(right.Labels)
	sort.Strings(left.AssigneeProviderLogins)
	sort.Strings(right.AssigneeProviderLogins)
	return left.FollowUpIntentID == right.FollowUpIntentID &&
		left.ProjectID == right.ProjectID &&
		left.RepositoryID == right.RepositoryID &&
		left.ProviderSlug == right.ProviderSlug &&
		left.ExternalAccountID == right.ExternalAccountID &&
		left.RepositoryTarget == right.RepositoryTarget &&
		left.Title == right.Title &&
		left.BodyDigest == right.BodyDigest &&
		sameStringSet(left.Labels, right.Labels) &&
		sameStringSet(left.AssigneeProviderLogins, right.AssigneeProviderLogins) &&
		left.Milestone == right.Milestone &&
		left.WorkItemType == right.WorkItemType &&
		left.WatermarkJSON == right.WatermarkJSON &&
		left.OperationPolicyContext.ProjectID == right.OperationPolicyContext.ProjectID &&
		left.OperationPolicyContext.RepositoryID == right.OperationPolicyContext.RepositoryID &&
		left.OperationPolicyContext.Stage == right.OperationPolicyContext.Stage &&
		left.OperationPolicyContext.RoleID == right.OperationPolicyContext.RoleID &&
		left.OperationPolicyContext.RoleKey == right.OperationPolicyContext.RoleKey &&
		left.OperationPolicyContext.OperationType == right.OperationPolicyContext.OperationType &&
		left.OperationPolicyContext.TargetRef == right.OperationPolicyContext.TargetRef &&
		sameStringSet(left.OperationPolicyContext.ChangedFields, right.OperationPolicyContext.ChangedFields) &&
		sameStringSet(left.OperationPolicyContext.RiskTags, right.OperationPolicyContext.RiskTags) &&
		left.OperationPolicyContext.RiskLevel == right.OperationPolicyContext.RiskLevel &&
		left.OperationPolicyContext.ApprovalRequired == right.OperationPolicyContext.ApprovalRequired &&
		left.OperationPolicyContext.PolicyVersion == right.OperationPolicyContext.PolicyVersion &&
		left.OperationPolicyContext.PolicySnapshotRef == right.OperationPolicyContext.PolicySnapshotRef &&
		left.ApprovalGateRef == right.ApprovalGateRef
}

func sameOptionalUUID(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
