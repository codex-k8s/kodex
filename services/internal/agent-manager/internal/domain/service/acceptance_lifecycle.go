package service

import (
	"bytes"
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
	acceptanceDetailsLimit      = 4096
	acceptanceFailureReasonCode = "machine_acceptance_failed"
)

type acceptanceCommandPayload struct {
	AcceptanceResult entity.AcceptanceResult `json:"acceptance_result"`
}

type acceptanceDetailsObject map[string]json.RawMessage

func (s *Service) RequestAcceptance(ctx context.Context, input RequestAcceptanceInput) (entity.AcceptanceResult, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AcceptanceResult{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.AcceptanceResult{}, err
	}
	checkKind, err := singleAcceptanceCheckKind(input.CheckKinds)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRequestAcceptance, enum.CommandAggregateTypeAcceptance, acceptanceFromPayload, verifyAcceptanceReplay(input.SessionID, uuid.Nil, s.repository.GetAcceptanceResult)); ok || err != nil {
		return replay, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	if isTerminalSessionStatus(session.Status) {
		return entity.AcceptanceResult{}, errs.ErrPreconditionFailed
	}
	runID, stageID, err := s.acceptanceRefs(ctx, session, input.RunID, input.StageID)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	now := s.clock.Now()
	acceptance := entity.AcceptanceResult{
		VersionedBase: entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		SessionID:     session.ID,
		RunID:         runID,
		StageID:       stageID,
		CheckKind:     checkKind,
		Status:        enum.AcceptanceStatusPending,
		TargetRef:     strings.TrimSpace(input.TargetRef),
		DetailsJSON:   []byte("{}"),
	}
	payload, err := marshalCommandPayload(acceptanceCommandPayload{AcceptanceResult: acceptance})
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	result, err := commandResult(input.Meta, operationRequestAcceptance, enum.CommandAggregateTypeAcceptance, acceptance.ID, payload, now)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	event, err := acceptanceRequestedEvent(s.idGenerator.New(), acceptance, now)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	return acceptance, s.repository.CreateAcceptanceResultWithResult(ctx, acceptance, result, event)
}

func (s *Service) RecordAcceptanceResult(ctx context.Context, input RecordAcceptanceResultInput) (entity.AcceptanceResult, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AcceptanceResult{}, err
	}
	if err := validateID(input.AcceptanceResultID); err != nil {
		return entity.AcceptanceResult{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	if err := validateAcceptanceResultStatus(input.Status); err != nil {
		return entity.AcceptanceResult{}, err
	}
	details, err := normalizeAcceptanceDetails(input.DetailsJSON)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRecordAcceptanceResult, enum.CommandAggregateTypeAcceptance, acceptanceFromPayload, verifyAcceptanceReplay(uuid.Nil, input.AcceptanceResultID, s.repository.GetAcceptanceResult)); ok || err != nil {
		return replay, err
	}
	acceptance, err := s.repository.GetAcceptanceResult(ctx, input.AcceptanceResultID)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	if acceptance.Version != previousVersion {
		return entity.AcceptanceResult{}, errs.ErrConflict
	}
	if err := validateAcceptanceStatusTransition(acceptance.Status, input.Status); err != nil {
		return entity.AcceptanceResult{}, err
	}
	now := s.clock.Now()
	previousStatus := string(acceptance.Status)
	acceptance.Status = input.Status
	if targetRef := strings.TrimSpace(input.TargetRef); targetRef != "" {
		acceptance.TargetRef = targetRef
	}
	acceptance.DetailsJSON = details
	acceptance.Version++
	acceptance.UpdatedAt = now
	payload, err := marshalCommandPayload(acceptanceCommandPayload{AcceptanceResult: acceptance})
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	result, err := commandResult(input.Meta, operationRecordAcceptanceResult, enum.CommandAggregateTypeAcceptance, acceptance.ID, payload, now)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	event, err := acceptanceResultEvent(s.idGenerator.New(), previousStatus, acceptance, now)
	if err != nil {
		return entity.AcceptanceResult{}, err
	}
	return acceptance, s.repository.UpdateAcceptanceResultWithResult(ctx, acceptance, previousVersion, result, event)
}

func (s *Service) GetAcceptanceResult(ctx context.Context, id uuid.UUID) (entity.AcceptanceResult, error) {
	return getByID(ctx, s, id, s.repository.GetAcceptanceResult)
}

func (s *Service) ListAcceptanceResults(ctx context.Context, filter query.AcceptanceResultFilter) ([]entity.AcceptanceResult, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.repository.ListAcceptanceResults)
}

func (s *Service) acceptanceRefs(ctx context.Context, session entity.AgentSession, requestedRunID *uuid.UUID, requestedStageID *uuid.UUID) (*uuid.UUID, *uuid.UUID, error) {
	stageID := requestedStageID
	if requestedRunID == nil {
		if err := s.validateAcceptanceStage(ctx, session, nil, stageID); err != nil {
			return nil, nil, err
		}
		return nil, stageID, nil
	}
	run, err := s.repository.GetAgentRun(ctx, *requestedRunID)
	if err != nil {
		return nil, nil, err
	}
	if run.SessionID != session.ID {
		return nil, nil, errs.ErrConflict
	}
	if requestedStageID != nil && run.StageID != nil && *requestedStageID != *run.StageID {
		return nil, nil, errs.ErrConflict
	}
	stageID = chooseUUID(requestedStageID, run.StageID)
	if err := s.validateAcceptanceStage(ctx, session, run.FlowVersionID, stageID); err != nil {
		return nil, nil, err
	}
	return uuidPtr(run.ID), stageID, nil
}

func (s *Service) validateAcceptanceStage(ctx context.Context, session entity.AgentSession, runFlowVersionID *uuid.UUID, stageID *uuid.UUID) error {
	if stageID == nil {
		return nil
	}
	flowVersionID := chooseUUID(runFlowVersionID, session.FlowVersionID)
	if flowVersionID == nil {
		return errs.ErrInvalidArgument
	}
	version, err := s.repository.GetFlowVersion(ctx, *flowVersionID)
	if err != nil {
		return err
	}
	for _, stage := range version.Stages {
		if stage.ID == *stageID {
			return nil
		}
	}
	return errs.ErrInvalidArgument
}

func singleAcceptanceCheckKind(kinds []enum.AcceptanceCheckKind) (enum.AcceptanceCheckKind, error) {
	if len(kinds) != 1 {
		return "", errs.ErrInvalidArgument
	}
	kind := kinds[0]
	if !validAcceptanceCheckKind(kind) {
		return "", errs.ErrInvalidArgument
	}
	return kind, nil
}

func validAcceptanceCheckKind(kind enum.AcceptanceCheckKind) bool {
	switch kind {
	case enum.AcceptanceCheckKindArtifact,
		enum.AcceptanceCheckKindWatermark,
		enum.AcceptanceCheckKindPolicy,
		enum.AcceptanceCheckKindRoleResult,
		enum.AcceptanceCheckKindHumanGate,
		enum.AcceptanceCheckKindFollowUp:
		return true
	default:
		return false
	}
}

func validateAcceptanceResultStatus(status enum.AcceptanceStatus) error {
	switch status {
	case enum.AcceptanceStatusPassed, enum.AcceptanceStatusFailed, enum.AcceptanceStatusWaiting, enum.AcceptanceStatusSkipped:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func validateAcceptanceStatusTransition(current enum.AcceptanceStatus, next enum.AcceptanceStatus) error {
	if current == next && !isTerminalAcceptanceStatus(current) {
		return nil
	}
	allowed := map[enum.AcceptanceStatus][]enum.AcceptanceStatus{
		enum.AcceptanceStatusPending: {enum.AcceptanceStatusPassed, enum.AcceptanceStatusFailed, enum.AcceptanceStatusWaiting, enum.AcceptanceStatusSkipped},
		enum.AcceptanceStatusWaiting: {enum.AcceptanceStatusPassed, enum.AcceptanceStatusFailed, enum.AcceptanceStatusSkipped},
	}
	for _, candidate := range allowed[current] {
		if candidate == next {
			return nil
		}
	}
	return errs.ErrPreconditionFailed
}

func isTerminalAcceptanceStatus(status enum.AcceptanceStatus) bool {
	return status == enum.AcceptanceStatusPassed || status == enum.AcceptanceStatusFailed || status == enum.AcceptanceStatusSkipped
}

func isTerminalSessionStatus(status enum.AgentSessionStatus) bool {
	return status == enum.AgentSessionStatusCompleted || status == enum.AgentSessionStatusFailed || status == enum.AgentSessionStatusCancelled
}

func normalizeAcceptanceDetails(payload []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}
	if len(trimmed) > acceptanceDetailsLimit {
		return nil, errs.ErrInvalidArgument
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if compact.Len() > acceptanceDetailsLimit {
		return nil, errs.ErrInvalidArgument
	}
	var object acceptanceDetailsObject
	if err := json.Unmarshal(compact.Bytes(), &object); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if err := validateAcceptanceDetailsObject(object); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func validateAcceptanceDetailsObject(object acceptanceDetailsObject) error {
	if object == nil {
		return errs.ErrInvalidArgument
	}
	for key, raw := range object {
		if unsafeAcceptanceDetailKey(key) {
			return errs.ErrInvalidArgument
		}
		if err := validateAcceptanceDetailsValue(raw); err != nil {
			return err
		}
	}
	return nil
}

func validateAcceptanceDetailsValue(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	switch trimmed[0] {
	case '{':
		var object acceptanceDetailsObject
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return errs.ErrInvalidArgument
		}
		return validateAcceptanceDetailsObject(object)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return errs.ErrInvalidArgument
		}
		for _, item := range items {
			if err := validateAcceptanceDetailsValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func unsafeAcceptanceDetailKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "raw_provider_payload",
		"provider_payload",
		"workspace_file",
		"workspace_files",
		"prompt_text",
		"prompt_template",
		"flow_file",
		"large_report",
		"report_body",
		"raw_report",
		"secret",
		"token",
		"authorization",
		"email",
		"stdout",
		"stderr",
		"logs":
		return true
	default:
		return false
	}
}

func acceptanceFromPayload(payload []byte) (entity.AcceptanceResult, error) {
	var result acceptanceCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.AcceptanceResult, err
}

func verifyAcceptanceReplay(expectedSessionID uuid.UUID, expectedAcceptanceID uuid.UUID, load func(context.Context, uuid.UUID) (entity.AcceptanceResult, error)) func(context.Context, entity.CommandResult, entity.AcceptanceResult) error {
	return func(ctx context.Context, result entity.CommandResult, replay entity.AcceptanceResult) error {
		if replay.ID != result.AggregateID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if expectedAcceptanceID != uuid.Nil && stored.ID != expectedAcceptanceID {
			return errs.ErrConflict
		}
		if expectedSessionID != uuid.Nil && stored.SessionID != expectedSessionID {
			return errs.ErrConflict
		}
		return nil
	}
}
