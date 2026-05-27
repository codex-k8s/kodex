package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	humanGateCodeLimit    = 128
	humanGateSummaryLimit = 1000
)

type humanGateCommandPayload struct {
	HumanGateRequest entity.HumanGateRequest `json:"human_gate_request"`
}

func (s *Service) RequestHumanGate(ctx context.Context, input RequestHumanGateInput) (entity.HumanGateRequest, error) {
	if err := s.requireRepository(); err != nil {
		return entity.HumanGateRequest{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.HumanGateRequest{}, err
	}
	idempotencyKey, err := humanGateIdempotencyKey(input.Meta, operationRequestHumanGate)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	gate, err := s.normalizeHumanGateRequest(ctx, session, input, idempotencyKey)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	verifyReplay := verifyEntityRequestReplay(gate, s.repository.GetHumanGateRequest, humanGateID, sameHumanGateRequest)
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRequestHumanGate, enum.CommandAggregateTypeHumanGate, humanGateFromPayload, verifyReplay); ok || err != nil {
		return replay, err
	}
	if isTerminalSessionStatus(session.Status) {
		return entity.HumanGateRequest{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	gate.ID = s.idGenerator.New()
	gate.Version = 1
	gate.CreatedAt = now
	gate.UpdatedAt = now
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: gate})
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	result, err := commandResult(input.Meta, operationRequestHumanGate, enum.CommandAggregateTypeHumanGate, gate.ID, payload, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	event, err := humanGateRequestedEvent(s.idGenerator.New(), gate, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	return gate, s.repository.CreateHumanGateRequestWithResult(ctx, gate, result, event)
}

func (s *Service) RecordHumanGateDecision(ctx context.Context, input RecordHumanGateDecisionInput) (entity.HumanGateRequest, error) {
	if err := s.requireRepository(); err != nil {
		return entity.HumanGateRequest{}, err
	}
	if err := validateID(input.HumanGateRequestID); err != nil {
		return entity.HumanGateRequest{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	if err := validateHumanGateDecisionStatus(input.Status); err != nil {
		return entity.HumanGateRequest{}, err
	}
	outcome, err := normalizeHumanGateOutcome(input.Outcome)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	refs, err := normalizeHumanGateDecisionRefs(input)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	summary, err := normalizeHumanGateSummary(input.SafeSummary, false)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRecordHumanGateDecision, enum.CommandAggregateTypeHumanGate, humanGateFromPayload, verifyHumanGateDecisionReplay(input.HumanGateRequestID, s.repository.GetHumanGateRequest)); ok || err != nil {
		return replay, err
	}
	gate, err := s.repository.GetHumanGateRequest(ctx, input.HumanGateRequestID)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	if gate.Version != previousVersion {
		return entity.HumanGateRequest{}, errs.ErrConflict
	}
	if !humanGateAwaitingDecision(gate.Status) {
		return entity.HumanGateRequest{}, errs.ErrPreconditionFailed
	}
	now := s.clock.Now()
	previousStatus := string(gate.Status)
	gate.Status = enum.HumanGateStatusResolved
	gate.Outcome = outcome
	gate.InteractionRequestRef = chooseString(refs.interactionRequestRef, gate.InteractionRequestRef)
	gate.InteractionResponseRef = chooseString(refs.interactionResponseRef, gate.InteractionResponseRef)
	gate.GovernanceGateRequestRef = chooseString(refs.governanceGateRequestRef, gate.GovernanceGateRequestRef)
	gate.GovernanceDecisionRef = chooseString(refs.governanceDecisionRef, gate.GovernanceDecisionRef)
	gate.SafeSummary = chooseString(summary, gate.SafeSummary)
	gate.ResolvedAt = &now
	gate.Version++
	gate.UpdatedAt = now
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: gate})
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	result, err := commandResult(input.Meta, operationRecordHumanGateDecision, enum.CommandAggregateTypeHumanGate, gate.ID, payload, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	event, err := humanGateResultEvent(s.idGenerator.New(), previousStatus, gate, now)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	return gate, s.repository.UpdateHumanGateRequestWithResult(ctx, gate, previousVersion, result, event)
}

func (s *Service) GetHumanGateRequest(ctx context.Context, id uuid.UUID) (entity.HumanGateRequest, error) {
	return getByID(ctx, s, id, s.repository.GetHumanGateRequest)
}

func (s *Service) ListHumanGateRequests(ctx context.Context, filter query.HumanGateFilter) ([]entity.HumanGateRequest, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.repository.ListHumanGateRequests)
}

func (s *Service) normalizeHumanGateRequest(ctx context.Context, session entity.AgentSession, input RequestHumanGateInput, idempotencyKey string) (entity.HumanGateRequest, error) {
	runID := input.RunID
	stageID := input.StageID
	acceptanceID := input.AcceptanceResultID
	if acceptanceID != nil {
		acceptance, err := s.repository.GetAcceptanceResult(ctx, *acceptanceID)
		if err != nil {
			return entity.HumanGateRequest{}, err
		}
		if acceptance.SessionID != session.ID {
			return entity.HumanGateRequest{}, errs.ErrConflict
		}
		if acceptance.CheckKind != enum.AcceptanceCheckKindHumanGate || acceptance.Status != enum.AcceptanceStatusWaiting {
			return entity.HumanGateRequest{}, errs.ErrPreconditionFailed
		}
		if err := bindOptionalUUID(&runID, acceptance.RunID); err != nil {
			return entity.HumanGateRequest{}, err
		}
		if err := bindOptionalUUID(&stageID, acceptance.StageID); err != nil {
			return entity.HumanGateRequest{}, err
		}
	}
	resolvedRunID, resolvedStageID, err := s.acceptanceRefs(ctx, session, runID, stageID)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	providerTarget, err := normalizeFollowUpProviderTarget(input.ProviderTarget)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	targetRef, err := normalizeAcceptanceTargetRef(input.TargetRef)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	requestKind, err := normalizeHumanGateCode(input.RequestKind, true)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	reasonCode, err := normalizeHumanGateCode(input.ReasonCode, true)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	summary, err := normalizeHumanGateSummary(input.SafeSummary, false)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	interactionRequestRef, err := normalizeFollowUpOptionalRef(input.InteractionRequestRef)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	governanceGateRequestRef, err := normalizeFollowUpOptionalRef(input.GovernanceGateRequestRef)
	if err != nil {
		return entity.HumanGateRequest{}, err
	}
	return entity.HumanGateRequest{
		SessionID:                session.ID,
		RunID:                    resolvedRunID,
		StageID:                  resolvedStageID,
		AcceptanceResultID:       acceptanceID,
		ProviderTarget:           providerTarget,
		TargetRef:                targetRef,
		RequestKind:              requestKind,
		ReasonCode:               reasonCode,
		SafeSummary:              summary,
		InteractionRequestRef:    interactionRequestRef,
		GovernanceGateRequestRef: governanceGateRequestRef,
		IdempotencyKey:           idempotencyKey,
		Status:                   enum.HumanGateStatusWaiting,
		Outcome:                  enum.HumanGateOutcomeNone,
	}, nil
}

type humanGateDecisionRefs struct {
	interactionRequestRef    string
	interactionResponseRef   string
	governanceGateRequestRef string
	governanceDecisionRef    string
}

func normalizeHumanGateDecisionRefs(input RecordHumanGateDecisionInput) (humanGateDecisionRefs, error) {
	interactionRequestRef, err := normalizeFollowUpOptionalRef(input.InteractionRequestRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	interactionResponseRef, err := normalizeFollowUpOptionalRef(input.InteractionResponseRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	governanceGateRequestRef, err := normalizeFollowUpOptionalRef(input.GovernanceGateRequestRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	governanceDecisionRef, err := normalizeFollowUpOptionalRef(input.GovernanceDecisionRef)
	if err != nil {
		return humanGateDecisionRefs{}, err
	}
	if interactionResponseRef == "" && governanceDecisionRef == "" {
		return humanGateDecisionRefs{}, errs.ErrInvalidArgument
	}
	return humanGateDecisionRefs{
		interactionRequestRef:    interactionRequestRef,
		interactionResponseRef:   interactionResponseRef,
		governanceGateRequestRef: governanceGateRequestRef,
		governanceDecisionRef:    governanceDecisionRef,
	}, nil
}

func humanGateIdempotencyKey(meta value.CommandMeta, operation string) (string, error) {
	return safeCommandResultKey(meta, operation, unsafeHumanGateText)
}

func normalizeHumanGateCode(value string, required bool) (string, error) {
	normalized, err := normalizeSafeIdentifier(value, humanGateCodeLimit, unsafeHumanGateText)
	if err != nil {
		return "", err
	}
	if required && normalized == "" {
		return "", errs.ErrInvalidArgument
	}
	return normalized, nil
}

func normalizeHumanGateSummary(value string, required bool) (string, error) {
	return normalizeBoundedSafeText(value, humanGateSummaryLimit, required, unsafeHumanGateText)
}

func normalizeHumanGateOutcome(outcome enum.HumanGateOutcome) (enum.HumanGateOutcome, error) {
	switch outcome {
	case enum.HumanGateOutcomeApprove,
		enum.HumanGateOutcomeReject,
		enum.HumanGateOutcomeRequestChanges,
		enum.HumanGateOutcomeAnswer:
		return outcome, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func validateHumanGateDecisionStatus(status enum.HumanGateStatus) error {
	if status != enum.HumanGateStatusResolved {
		return errs.ErrInvalidArgument
	}
	return nil
}

func humanGateAwaitingDecision(status enum.HumanGateStatus) bool {
	return status == enum.HumanGateStatusRequested || status == enum.HumanGateStatusWaiting
}

func unsafeHumanGateText(value string) bool {
	if unsafeFollowUpText(value) || unsafeAcceptanceTargetRef(value) {
		return true
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"email",
		"phone",
		"address",
		"pii",
		"interaction_payload",
		"governance_payload",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func humanGateFromPayload(payload []byte) (entity.HumanGateRequest, error) {
	var result humanGateCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.HumanGateRequest, err
}

func verifyHumanGateDecisionReplay(expectedID uuid.UUID, load func(context.Context, uuid.UUID) (entity.HumanGateRequest, error)) func(context.Context, entity.CommandResult, entity.HumanGateRequest) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.HumanGateRequest) error {
		if replay.ID != result.AggregateID || replay.ID != expectedID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if stored.ID != replay.ID || stored.Version != replay.Version || stored.Status != replay.Status || stored.Outcome != replay.Outcome {
			return errs.ErrConflict
		}
		return nil
	}
}

func sameHumanGateRequest(stored entity.HumanGateRequest, expected entity.HumanGateRequest) bool {
	return stored.SessionID == expected.SessionID &&
		sameUUIDPtr(stored.RunID, expected.RunID) &&
		sameUUIDPtr(stored.StageID, expected.StageID) &&
		sameUUIDPtr(stored.AcceptanceResultID, expected.AcceptanceResultID) &&
		stored.ProviderTarget == expected.ProviderTarget &&
		stored.TargetRef == expected.TargetRef &&
		stored.RequestKind == expected.RequestKind &&
		stored.ReasonCode == expected.ReasonCode &&
		stored.SafeSummary == expected.SafeSummary &&
		stored.InteractionRequestRef == expected.InteractionRequestRef &&
		stored.GovernanceGateRequestRef == expected.GovernanceGateRequestRef &&
		stored.IdempotencyKey == expected.IdempotencyKey
}

func humanGateID(gate entity.HumanGateRequest) uuid.UUID {
	return gate.ID
}

func sameUUIDPtr(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func chooseString(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
