package service

import (
	"context"
	"encoding/json"
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
	followUpDigestLength = 71
)

type followUpIntentCommandPayload struct {
	FollowUpIntent entity.FollowUpIntent `json:"follow_up_intent"`
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
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) != followUpDigestLength || !strings.HasPrefix(trimmed, "sha256:") {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed[len("sha256:"):] {
		if !asciiHex(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
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
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > followUpHintLimit || unsafeFollowUpText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if !safeAcceptanceRefChar(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return trimmed, nil
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

func sameOptionalUUID(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
