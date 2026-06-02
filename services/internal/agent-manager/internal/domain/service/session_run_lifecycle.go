package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type agentSessionCommandPayload struct {
	Session entity.AgentSession `json:"session"`
}

type agentRunCommandPayload struct {
	Run          entity.AgentRun               `json:"run"`
	RunnerReport *runnerRunStateCommandPayload `json:"runner_report,omitempty"`
}

type runnerRunStateCommandPayload struct {
	RunID            string `json:"run_id"`
	SessionID        string `json:"session_id"`
	RuntimeSlotRef   string `json:"runtime_slot_ref"`
	RuntimeJobRef    string `json:"runtime_job_ref"`
	State            string `json:"state"`
	SafeSummary      string `json:"safe_summary,omitempty"`
	FailureCode      string `json:"failure_code,omitempty"`
	DiagnosticDigest string `json:"diagnostic_digest,omitempty"`
	StartedAt        string `json:"started_at,omitempty"`
	FinishedAt       string `json:"finished_at,omitempty"`
}

type sessionSnapshotCommandPayload struct {
	Snapshot entity.AgentSessionStateSnapshot `json:"snapshot"`
	Session  entity.AgentSession              `json:"session"`
}

func (s *Service) StartAgentSession(ctx context.Context, input StartAgentSessionInput) (entity.AgentSession, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AgentSession{}, err
	}
	if err := validateScope(input.Scope); err != nil {
		return entity.AgentSession{}, err
	}
	if strings.TrimSpace(input.CreatedByActorRef) == "" {
		return entity.AgentSession{}, errs.ErrInvalidArgument
	}
	if input.CurrentStageID != nil && input.FlowVersionID == nil {
		return entity.AgentSession{}, errs.ErrInvalidArgument
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationStartAgentSession, enum.CommandAggregateTypeSession, sessionFromPayload, verifyScopedReplay(uuid.Nil, &input.Scope, s.repository.GetAgentSession, sessionID, sessionScope)); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	providerWorkItemRef := strings.TrimSpace(input.ProviderWorkItemRef)
	if providerWorkItemRef != "" {
		existing, err := s.repository.FindActiveAgentSessionByProviderWorkItem(ctx, input.Scope, providerWorkItemRef)
		switch {
		case err == nil:
			return existing, s.recordSessionCommandResult(ctx, input.Meta, existing, now)
		case errors.Is(err, errs.ErrNotFound):
		case err != nil:
			return entity.AgentSession{}, err
		}
	}
	session := entity.AgentSession{
		VersionedBase:       entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:               input.Scope,
		ProviderWorkItemRef: providerWorkItemRef,
		FlowVersionID:       input.FlowVersionID,
		CurrentStageID:      input.CurrentStageID,
		Status:              enum.AgentSessionStatusOpen,
		CreatedByActorRef:   strings.TrimSpace(input.CreatedByActorRef),
	}
	payload, err := marshalCommandPayload(agentSessionCommandPayload{Session: session})
	if err != nil {
		return entity.AgentSession{}, err
	}
	result, err := commandResult(input.Meta, operationStartAgentSession, enum.CommandAggregateTypeSession, session.ID, payload, now)
	if err != nil {
		return entity.AgentSession{}, err
	}
	event, err := sessionCreatedEvent(s.idGenerator.New(), session, now)
	if err != nil {
		return entity.AgentSession{}, err
	}
	err = s.repository.CreateAgentSessionWithResult(ctx, session, result, event)
	if err == nil || providerWorkItemRef == "" || !errors.Is(err, errs.ErrConflict) {
		return session, err
	}
	existing, findErr := s.repository.FindActiveAgentSessionByProviderWorkItem(ctx, input.Scope, providerWorkItemRef)
	if findErr != nil {
		return entity.AgentSession{}, err
	}
	return existing, s.recordSessionCommandResult(ctx, input.Meta, existing, now)
}

func (s *Service) GetAgentSession(ctx context.Context, id uuid.UUID) (entity.AgentSession, error) {
	return getByID(ctx, s, id, s.repository.GetAgentSession)
}

func (s *Service) StartAgentRun(ctx context.Context, input StartAgentRunInput) (entity.AgentRun, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AgentRun{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return entity.AgentRun{}, err
	}
	if err := validateID(input.RoleProfileID); err != nil {
		return entity.AgentRun{}, err
	}
	if err := validateID(input.PromptTemplateVersionID); err != nil {
		return entity.AgentRun{}, err
	}
	if replay, ok, err := s.findStartAgentRunReplay(ctx, input.Meta, input.SessionID); ok || err != nil {
		if err != nil {
			return replay, err
		}
		return s.retryRuntimeJobDispatchForReplay(ctx, input.Meta, replay)
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if session.Status == enum.AgentSessionStatusCompleted || session.Status == enum.AgentSessionStatusFailed || session.Status == enum.AgentSessionStatusCancelled {
		return entity.AgentRun{}, errs.ErrPreconditionFailed
	}
	role, err := s.repository.GetRoleProfile(ctx, input.RoleProfileID)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if role.Status != enum.RoleStatusActive {
		return entity.AgentRun{}, errs.ErrPreconditionFailed
	}
	promptVersion, err := s.repository.GetPromptTemplateVersion(ctx, input.PromptTemplateVersionID)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if promptVersion.RoleProfileID != role.ID || promptVersion.Status != enum.PromptVersionStatusActive {
		return entity.AgentRun{}, errs.ErrPreconditionFailed
	}
	roleDigest, err := roleProfileDigest(role)
	if err != nil {
		return entity.AgentRun{}, err
	}
	now := s.clock.Now()
	flowVersionID := chooseUUID(input.FlowVersionID, session.FlowVersionID)
	stageID := chooseUUID(input.StageID, session.CurrentStageID)
	if err := s.validateRunStageRoleBinding(ctx, flowVersionID, stageID, role.ID); err != nil {
		return entity.AgentRun{}, err
	}
	providerTarget := input.ProviderTarget
	if strings.TrimSpace(providerTarget.WorkItemRef) == "" {
		providerTarget.WorkItemRef = session.ProviderWorkItemRef
	}
	guidanceRefs, err := s.guidanceResolver.ResolveGuidanceRefs(ctx, GuidanceResolutionInput{
		Meta:  input.Meta,
		Scope: session.Scope,
		Hints: input.GuidanceSelectionHints,
	})
	if err != nil {
		return entity.AgentRun{}, err
	}
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: s.idGenerator.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		SessionID:               session.ID,
		FlowVersionID:           flowVersionID,
		StageID:                 stageID,
		RoleProfileID:           role.ID,
		RoleProfileVersion:      role.Version,
		RoleProfileDigest:       roleDigest,
		PromptTemplateVersionID: promptVersion.ID,
		PromptTemplateDigest:    promptVersion.TemplateDigest,
		ProviderTarget:          providerTarget,
		GuidanceRefs:            guidanceRefs,
		Status:                  enum.AgentRunStatusRequested,
	}
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: run})
	if err != nil {
		return entity.AgentRun{}, err
	}
	result, err := commandResult(input.Meta, operationStartAgentRun, enum.CommandAggregateTypeRun, run.ID, payload, now)
	if err != nil {
		return entity.AgentRun{}, err
	}
	event, err := runRequestedEvent(s.idGenerator.New(), run, now)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if err := s.repository.CreateAgentRunWithResult(ctx, run, result, event); err != nil {
		return run, err
	}
	return s.prepareRuntimeForRun(ctx, input.Meta, session, role, promptVersion, run)
}

func (s *Service) RecordRunState(ctx context.Context, input RecordRunStateInput) (entity.AgentRun, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AgentRun{}, err
	}
	if err := validateID(input.RunID); err != nil {
		return entity.AgentRun{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if input.Status == "" || input.Status == enum.AgentRunStatusRequested {
		return entity.AgentRun{}, errs.ErrInvalidArgument
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRecordRunState, enum.CommandAggregateTypeRun, runFromPayload, verifyReplay(input.RunID, s.repository.GetAgentRun, runID, acceptAnyRun)); ok || err != nil {
		return replay, err
	}
	run, err := s.repository.GetAgentRun(ctx, input.RunID)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if run.Version != previousVersion {
		return entity.AgentRun{}, errs.ErrConflict
	}

	return s.recordRunStateUpdate(ctx, input.Meta, operationRecordRunState, run, runStatePatch{
		Status:         input.Status,
		RuntimeContext: input.RuntimeContext,
		ProviderTarget: input.ProviderTarget,
		ResultSummary:  input.ResultSummary,
		FailureCode:    input.FailureCode,
		ReasonCode:     input.ReasonCode,
		StartedAt:      input.StartedAt,
		FinishedAt:     input.FinishedAt,
	})
}

func (s *Service) ReportAgentRunState(ctx context.Context, input ReportAgentRunStateInput) (entity.AgentRun, error) {
	if err := s.requireRepository(); err != nil {
		return entity.AgentRun{}, err
	}
	report, err := runnerReportCommandPayload(input)
	if err != nil {
		return entity.AgentRun{}, err
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if replay, ok, err := s.findRunnerReportReplay(ctx, input.Meta, input.RunID, report); ok || err != nil {
		return replay, err
	}
	run, err := s.repository.GetAgentRun(ctx, input.RunID)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if run.Version != previousVersion {
		return entity.AgentRun{}, errs.ErrConflict
	}
	if err := validateRunnerReportBinding(run, report); err != nil {
		return entity.AgentRun{}, err
	}
	status, err := runnerReportRunStatus(report.State)
	if err != nil {
		return entity.AgentRun{}, err
	}
	patch := runStatePatch{
		Status:       status,
		RunnerReport: &report,
		StartedAt:    input.StartedAt,
		FinishedAt:   input.FinishedAt,
	}
	if summary := runnerReportSummary(report); summary != "" {
		patch.ResultSummary = &summary
	}
	if report.FailureCode != "" {
		patch.FailureCode = &report.FailureCode
	}

	return s.recordRunStateUpdate(ctx, input.Meta, operationReportAgentRunState, run, patch)
}

type runStatePatch struct {
	Status         enum.AgentRunStatus
	RuntimeContext *value.RuntimeContextRef
	ProviderTarget *value.ProviderTargetRef
	ResultSummary  *string
	FailureCode    *string
	ReasonCode     *string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	RunnerReport   *runnerRunStateCommandPayload
}

func (s *Service) recordRunStateUpdate(ctx context.Context, meta value.CommandMeta, operation string, run entity.AgentRun, patch runStatePatch) (entity.AgentRun, error) {
	if err := validateRunStatusTransition(run.Status, patch.Status); err != nil {
		return entity.AgentRun{}, err
	}
	now := s.clock.Now()
	previousVersion := run.Version
	previousStatus := string(run.Status)
	run.Status = patch.Status
	if patch.RuntimeContext != nil {
		runtimeContext, err := mergedRuntimeContext(run.RuntimeContext, *patch.RuntimeContext)
		if err != nil {
			return entity.AgentRun{}, err
		}
		run.RuntimeContext = runtimeContext
	}
	if patch.ProviderTarget != nil {
		run.ProviderTarget = *patch.ProviderTarget
	}
	if patch.ResultSummary != nil {
		summary, err := normalizedRunSafeSummary(*patch.ResultSummary)
		if err != nil {
			return entity.AgentRun{}, err
		}
		run.ResultSummary = summary
	}
	if patch.FailureCode != nil {
		failureCode, err := normalizedRunFailureCode(*patch.FailureCode, run.Status == enum.AgentRunStatusFailed)
		if err != nil {
			return entity.AgentRun{}, err
		}
		run.FailureCode = failureCode
	}
	reasonCode, err := normalizedRunReasonCode(patch.ReasonCode)
	if err != nil {
		return entity.AgentRun{}, err
	}
	if err := validateRunStatePayload(run, reasonCode); err != nil {
		return entity.AgentRun{}, err
	}
	if patch.StartedAt != nil {
		run.StartedAt = patch.StartedAt
	} else if run.StartedAt == nil && (patch.Status == enum.AgentRunStatusStarting || patch.Status == enum.AgentRunStatusRunning) {
		run.StartedAt = &now
	}
	if patch.FinishedAt != nil {
		run.FinishedAt = patch.FinishedAt
	} else if run.FinishedAt == nil && isTerminalRunStatus(patch.Status) {
		run.FinishedAt = &now
	}
	run.Version++
	run.UpdatedAt = now
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: run, RunnerReport: patch.RunnerReport})
	if err != nil {
		return entity.AgentRun{}, err
	}
	result, err := commandResult(meta, operation, enum.CommandAggregateTypeRun, run.ID, payload, now)
	if err != nil {
		return entity.AgentRun{}, err
	}
	event, err := runStateEvent(s.idGenerator.New(), previousStatus, run, reasonCode, now)
	if err != nil {
		return entity.AgentRun{}, err
	}
	return run, s.repository.UpdateAgentRunWithResult(ctx, run, previousVersion, result, event)
}

func (s *Service) RecordSessionStateSnapshot(ctx context.Context, input RecordSessionStateSnapshotInput) (SessionSnapshotResult, error) {
	if err := s.requireRepository(); err != nil {
		return SessionSnapshotResult{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return SessionSnapshotResult{}, err
	}
	if input.SnapshotKind == "" || strings.TrimSpace(input.Object.ObjectURI) == "" || strings.TrimSpace(input.Object.ObjectDigest) == "" || input.CapturedAt.IsZero() {
		return SessionSnapshotResult{}, errs.ErrInvalidArgument
	}
	if input.Object.ObjectSizeBytes != nil && *input.Object.ObjectSizeBytes < 0 {
		return SessionSnapshotResult{}, errs.ErrInvalidArgument
	}
	if input.TurnIndex != nil && *input.TurnIndex < 0 {
		return SessionSnapshotResult{}, errs.ErrInvalidArgument
	}
	previousVersion, err := expectedVersion(input.Meta)
	if err != nil {
		return SessionSnapshotResult{}, err
	}
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationRecordSessionSnapshot, enum.CommandAggregateTypeSessionStateSnapshot, sessionSnapshotResultFromPayload, verifySnapshotResultReplay(input.SessionID, s.repository.GetSessionStateSnapshot)); ok || err != nil {
		return replay, err
	}
	session, err := s.repository.GetAgentSession(ctx, input.SessionID)
	if err != nil {
		return SessionSnapshotResult{}, err
	}
	if session.Version != previousVersion {
		return SessionSnapshotResult{}, errs.ErrConflict
	}
	if input.RunID != nil {
		run, err := s.repository.GetAgentRun(ctx, *input.RunID)
		if err != nil {
			return SessionSnapshotResult{}, err
		}
		if run.SessionID != session.ID {
			return SessionSnapshotResult{}, errs.ErrConflict
		}
	}
	now := s.clock.Now()
	snapshot := entity.AgentSessionStateSnapshot{
		ID:           s.idGenerator.New(),
		SessionID:    session.ID,
		RunID:        input.RunID,
		SnapshotKind: input.SnapshotKind,
		TurnIndex:    input.TurnIndex,
		Object:       input.Object,
		CapturedAt:   input.CapturedAt.UTC(),
		CreatedAt:    now,
	}
	session.LatestStateSnapshotID = &snapshot.ID
	session.Version++
	session.UpdatedAt = now
	output := SessionSnapshotResult{Snapshot: snapshot, Session: session}
	payload, err := marshalCommandPayload(sessionSnapshotCommandPayload{Snapshot: snapshot, Session: session})
	if err != nil {
		return SessionSnapshotResult{}, err
	}
	result, err := commandResult(input.Meta, operationRecordSessionSnapshot, enum.CommandAggregateTypeSessionStateSnapshot, snapshot.ID, payload, now)
	if err != nil {
		return SessionSnapshotResult{}, err
	}
	event, err := sessionSnapshotRecordedEvent(s.idGenerator.New(), snapshot, session, now)
	if err != nil {
		return SessionSnapshotResult{}, err
	}
	err = s.repository.CreateSessionStateSnapshotWithResult(ctx, snapshot, session, previousVersion, result, event)
	return output, err
}

func (s *Service) ListAgentRuns(ctx context.Context, filter query.AgentRunFilter) ([]entity.AgentRun, value.PageResult, error) {
	return listFromRepository(ctx, s, filter, s.repository.ListAgentRuns)
}

func (s *Service) GetSessionStateSnapshot(ctx context.Context, id uuid.UUID) (entity.AgentSessionStateSnapshot, error) {
	return getByID(ctx, s, id, s.repository.GetSessionStateSnapshot)
}

func chooseUUID(primary *uuid.UUID, fallback *uuid.UUID) *uuid.UUID {
	if primary != nil {
		return primary
	}
	return fallback
}

func (s *Service) recordSessionCommandResult(ctx context.Context, meta value.CommandMeta, session entity.AgentSession, now time.Time) error {
	payload, err := marshalCommandPayload(agentSessionCommandPayload{Session: session})
	if err != nil {
		return err
	}
	result, err := commandResult(meta, operationStartAgentSession, enum.CommandAggregateTypeSession, session.ID, payload, now)
	if err != nil {
		return err
	}
	return s.repository.RecordCommandResult(ctx, result)
}

func (s *Service) validateRunStageRoleBinding(ctx context.Context, flowVersionID *uuid.UUID, stageID *uuid.UUID, roleProfileID uuid.UUID) error {
	if stageID == nil {
		return nil
	}
	if flowVersionID == nil {
		return errs.ErrInvalidArgument
	}
	version, err := s.repository.GetFlowVersion(ctx, *flowVersionID)
	if err != nil {
		return err
	}
	stageFound := false
	for _, stage := range version.Stages {
		if stage.ID == *stageID {
			stageFound = true
			break
		}
	}
	if !stageFound {
		return errs.ErrInvalidArgument
	}
	for _, binding := range version.RoleBindings {
		if binding.StageID == *stageID && binding.RoleProfileID == roleProfileID {
			return nil
		}
	}
	return errs.ErrPreconditionFailed
}

func isTerminalRunStatus(status enum.AgentRunStatus) bool {
	return status == enum.AgentRunStatusCompleted || status == enum.AgentRunStatusFailed || status == enum.AgentRunStatusCancelled
}

func validateRunStatusTransition(current enum.AgentRunStatus, next enum.AgentRunStatus) error {
	if current == next && !isTerminalRunStatus(current) {
		return nil
	}
	allowed := map[enum.AgentRunStatus][]enum.AgentRunStatus{
		enum.AgentRunStatusRequested: {enum.AgentRunStatusStarting, enum.AgentRunStatusWaiting, enum.AgentRunStatusFailed, enum.AgentRunStatusCancelled},
		enum.AgentRunStatusStarting:  {enum.AgentRunStatusRunning, enum.AgentRunStatusWaiting, enum.AgentRunStatusFailed, enum.AgentRunStatusCancelled},
		enum.AgentRunStatusRunning:   {enum.AgentRunStatusWaiting, enum.AgentRunStatusCompleted, enum.AgentRunStatusFailed, enum.AgentRunStatusCancelled},
		enum.AgentRunStatusWaiting:   {enum.AgentRunStatusStarting, enum.AgentRunStatusRunning, enum.AgentRunStatusFailed, enum.AgentRunStatusCancelled},
	}
	for _, candidate := range allowed[current] {
		if candidate == next {
			return nil
		}
	}
	return errs.ErrPreconditionFailed
}

func validateRunStatePayload(run entity.AgentRun, reasonCode string) error {
	switch run.Status {
	case enum.AgentRunStatusStarting, enum.AgentRunStatusRunning:
		if strings.TrimSpace(run.RuntimeContext.SlotRef) == "" {
			return errs.ErrInvalidArgument
		}
	case enum.AgentRunStatusWaiting:
		if reasonCode == "" {
			return errs.ErrInvalidArgument
		}
	case enum.AgentRunStatusFailed:
		if run.FailureCode == "" {
			return errs.ErrInvalidArgument
		}
	}
	return nil
}

func runnerReportCommandPayload(input ReportAgentRunStateInput) (runnerRunStateCommandPayload, error) {
	if err := validateID(input.RunID); err != nil {
		return runnerRunStateCommandPayload{}, err
	}
	if err := validateID(input.SessionID); err != nil {
		return runnerRunStateCommandPayload{}, err
	}
	state := strings.TrimSpace(string(input.State))
	if _, err := runnerReportRunStatus(state); err != nil {
		return runnerRunStateCommandPayload{}, err
	}
	slotRef := strings.TrimSpace(input.RuntimeSlotRef)
	jobRef := strings.TrimSpace(input.RuntimeJobRef)
	if !safeRuntimeJobRef(slotRef, true) || !safeRuntimeJobRef(jobRef, true) {
		return runnerRunStateCommandPayload{}, errs.ErrInvalidArgument
	}
	summary, err := normalizedRunSafeSummary(optionalStringValue(input.SafeSummary))
	if err != nil {
		return runnerRunStateCommandPayload{}, err
	}
	reportState := RunnerRunState(state)
	failureCode := strings.TrimSpace(optionalStringValue(input.FailureCode))
	failureRequired := runnerReportFailureCodeRequired(reportState)
	if !failureRequired && failureCode != "" {
		return runnerRunStateCommandPayload{}, errs.ErrInvalidArgument
	}
	if failureCode == "" {
		failureCode = runnerReportDefaultFailureCode(reportState)
	}
	failureCode, err = normalizedRunFailureCode(failureCode, failureRequired)
	if err != nil {
		return runnerRunStateCommandPayload{}, err
	}
	diagnosticDigest := strings.TrimSpace(optionalStringValue(input.DiagnosticDigest))
	if diagnosticDigest != "" && (!safeRuntimeJobRef(diagnosticDigest, false) || unsafeRunnerReportText(diagnosticDigest)) {
		return runnerRunStateCommandPayload{}, errs.ErrInvalidArgument
	}
	if input.StartedAt != nil && input.FinishedAt != nil && input.FinishedAt.Before(*input.StartedAt) {
		return runnerRunStateCommandPayload{}, errs.ErrInvalidArgument
	}
	return runnerRunStateCommandPayload{
		RunID:            input.RunID.String(),
		SessionID:        input.SessionID.String(),
		RuntimeSlotRef:   slotRef,
		RuntimeJobRef:    jobRef,
		State:            state,
		SafeSummary:      summary,
		FailureCode:      failureCode,
		DiagnosticDigest: diagnosticDigest,
		StartedAt:        optionalRunTimeText(input.StartedAt),
		FinishedAt:       optionalRunTimeText(input.FinishedAt),
	}, nil
}

func (s *Service) findRunnerReportReplay(ctx context.Context, meta value.CommandMeta, runID uuid.UUID, expected runnerRunStateCommandPayload) (entity.AgentRun, bool, error) {
	identity, err := commandIdentity(meta, operationReportAgentRunState)
	if err != nil {
		return entity.AgentRun{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return entity.AgentRun{}, false, nil
	case err != nil:
		return entity.AgentRun{}, false, err
	}
	if !matchesReplay(result, operationReportAgentRunState, enum.CommandAggregateTypeRun) {
		return entity.AgentRun{}, true, errs.ErrConflict
	}
	var replay agentRunCommandPayload
	if err := json.Unmarshal(result.ResultPayload, &replay); err != nil {
		return entity.AgentRun{}, true, err
	}
	if result.AggregateID != runID || replay.Run.ID != result.AggregateID || replay.RunnerReport == nil {
		return entity.AgentRun{}, true, errs.ErrConflict
	}
	if !sameRunnerReport(*replay.RunnerReport, expected) {
		return entity.AgentRun{}, true, errs.ErrConflict
	}
	stored, err := s.repository.GetAgentRun(ctx, result.AggregateID)
	if err != nil {
		return entity.AgentRun{}, true, err
	}
	if stored.ID != replay.Run.ID {
		return entity.AgentRun{}, true, errs.ErrConflict
	}
	return replay.Run, true, nil
}

func validateRunnerReportBinding(run entity.AgentRun, report runnerRunStateCommandPayload) error {
	if run.ID.String() != report.RunID || run.SessionID.String() != report.SessionID {
		return errs.ErrConflict
	}
	if strings.TrimSpace(run.RuntimeContext.SlotRef) == "" || strings.TrimSpace(run.RuntimeContext.JobRef) == "" {
		return errs.ErrPreconditionFailed
	}
	if run.RuntimeContext.SlotRef != report.RuntimeSlotRef || run.RuntimeContext.JobRef != report.RuntimeJobRef {
		return errs.ErrConflict
	}
	if !safeRuntimeJobRef(run.RuntimeContext.SlotRef, true) || !safeRuntimeJobRef(run.RuntimeContext.JobRef, true) {
		return errs.ErrConflict
	}
	return nil
}

func runnerReportRunStatus(state string) (enum.AgentRunStatus, error) {
	switch RunnerRunState(strings.TrimSpace(state)) {
	case RunnerRunStateQueued:
		return enum.AgentRunStatusStarting, nil
	case RunnerRunStateStarted, RunnerRunStateRunning:
		return enum.AgentRunStatusRunning, nil
	case RunnerRunStateCompleted:
		return enum.AgentRunStatusCompleted, nil
	case RunnerRunStateFailed:
		return enum.AgentRunStatusFailed, nil
	case RunnerRunStateCancelled:
		return enum.AgentRunStatusCancelled, nil
	case RunnerRunStateTimedOut:
		return enum.AgentRunStatusFailed, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func runnerReportFailureCodeRequired(state RunnerRunState) bool {
	switch state {
	case RunnerRunStateFailed, RunnerRunStateTimedOut:
		return true
	default:
		return false
	}
}

func runnerReportDefaultFailureCode(state RunnerRunState) string {
	if state == RunnerRunStateTimedOut {
		return "runner_timeout"
	}
	return ""
}

func runnerReportSummary(report runnerRunStateCommandPayload) string {
	parts := make([]string, 0, 2)
	if report.DiagnosticDigest != "" {
		parts = append(parts, "diagnostic_digest="+report.DiagnosticDigest)
	}
	if report.SafeSummary != "" {
		parts = append(parts, report.SafeSummary)
	}
	return safeDiagnosticText(strings.Join(parts, "; "))
}

func sameRunnerReport(left runnerRunStateCommandPayload, right runnerRunStateCommandPayload) bool {
	leftValues := runnerReportReplayFields(left)
	rightValues := runnerReportReplayFields(right)
	for index := range leftValues {
		if leftValues[index] != rightValues[index] {
			return false
		}
	}
	return true
}

func runnerReportReplayFields(report runnerRunStateCommandPayload) []string {
	return []string{
		report.RunID,
		report.SessionID,
		report.RuntimeSlotRef,
		report.RuntimeJobRef,
		report.State,
		report.SafeSummary,
		report.FailureCode,
		report.DiagnosticDigest,
		report.StartedAt,
		report.FinishedAt,
	}
}

func mergedRuntimeContext(existing value.RuntimeContextRef, incoming value.RuntimeContextRef) (value.RuntimeContextRef, error) {
	merged := existing
	fields := []struct {
		current *string
		next    string
	}{
		{current: &merged.SlotRef, next: incoming.SlotRef},
		{current: &merged.JobRef, next: incoming.JobRef},
		{current: &merged.WorkspaceRef, next: incoming.WorkspaceRef},
		{current: &merged.ContextRef, next: incoming.ContextRef},
	}
	for _, field := range fields {
		nextValue := strings.TrimSpace(field.next)
		if nextValue == "" {
			continue
		}
		if !safeRuntimeJobRef(nextValue, false) {
			return value.RuntimeContextRef{}, errs.ErrInvalidArgument
		}
		if strings.TrimSpace(*field.current) != "" && *field.current != nextValue {
			return value.RuntimeContextRef{}, errs.ErrConflict
		}
		*field.current = nextValue
	}
	return merged, nil
}

func normalizedRunSafeSummary(summary string) (string, error) {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) > runtimePrepareSummaryLimit || !utf8.ValidString(trimmed) || strings.ContainsAny(trimmed, "\r\n\t{}") || unsafeRunnerReportText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	return safeDiagnosticText(trimmed), nil
}

func normalizedRunFailureCode(code string, required bool) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		if required {
			return "", errs.ErrInvalidArgument
		}
		return "", nil
	}
	if !safeRuntimeJobKind(trimmed) || unsafeRunnerReportText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func normalizedRunReasonCode(reasonCode *string) (string, error) {
	if reasonCode == nil {
		return "", nil
	}
	trimmed := strings.TrimSpace(*reasonCode)
	if trimmed == "" {
		return "", nil
	}
	if !safeRuntimeJobKind(trimmed) || unsafeRunnerReportText(trimmed) {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func unsafeRunnerReportText(text string) bool {
	if unsafeRuntimeJobText(text) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	markers := []string{
		"://",
		"jdbc:",
		"dsn=",
		"database_url",
		"postgres:",
		"postgresql:",
		"mysql:",
		"mariadb:",
		"redis:",
		"mongodb:",
		"amqp:",
		"nats:",
		"localhost",
		"internal.",
		".internal",
		".local",
		".svc",
		".cluster.local",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, pattern := range privateRunnerAddressPatterns {
		if pattern.MatchString(lower) {
			return true
		}
	}
	return false
}

var privateRunnerAddressPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^0-9])10\.[0-9]{1,3}\.`),
	regexp.MustCompile(`(^|[^0-9])127\.`),
	regexp.MustCompile(`(^|[^0-9])169\.254\.`),
	regexp.MustCompile(`(^|[^0-9])172\.(1[6-9]|2[0-9]|3[0-1])\.`),
	regexp.MustCompile(`(^|[^0-9])192\.168\.`),
}

func optionalRunTimeText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func sessionFromPayload(payload []byte) (entity.AgentSession, error) {
	var result agentSessionCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.Session, err
}

func runFromPayload(payload []byte) (entity.AgentRun, error) {
	var result agentRunCommandPayload
	err := json.Unmarshal(payload, &result)
	return result.Run, err
}

func (s *Service) findStartAgentRunReplay(ctx context.Context, meta value.CommandMeta, sessionID uuid.UUID) (entity.AgentRun, bool, error) {
	identity, err := commandIdentity(meta, operationStartAgentRun)
	if err != nil {
		return entity.AgentRun{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return entity.AgentRun{}, false, nil
	case err != nil:
		return entity.AgentRun{}, false, err
	}
	if !matchesReplay(result, operationStartAgentRun, enum.CommandAggregateTypeRun) {
		return entity.AgentRun{}, true, errs.ErrConflict
	}
	replay, err := runFromPayload(result.ResultPayload)
	if err != nil {
		return entity.AgentRun{}, true, err
	}
	stored, err := s.repository.GetAgentRun(ctx, result.AggregateID)
	if err != nil {
		return entity.AgentRun{}, true, err
	}
	if replay.ID != stored.ID || stored.SessionID != sessionID {
		return entity.AgentRun{}, true, errs.ErrConflict
	}
	return stored, true, nil
}

func sessionSnapshotResultFromPayload(payload []byte) (SessionSnapshotResult, error) {
	var result sessionSnapshotCommandPayload
	err := json.Unmarshal(payload, &result)
	return SessionSnapshotResult(result), err
}

func sessionID(session entity.AgentSession) uuid.UUID { return session.ID }

func sessionScope(session entity.AgentSession) value.ScopeRef { return session.Scope }

func runID(run entity.AgentRun) uuid.UUID { return run.ID }

func verifySnapshotResultReplay(expectedSessionID uuid.UUID, load func(context.Context, uuid.UUID) (entity.AgentSessionStateSnapshot, error)) func(context.Context, entity.CommandResult, SessionSnapshotResult) error {
	return func(ctx context.Context, result entity.CommandResult, replay SessionSnapshotResult) error {
		if replay.Snapshot.ID != result.AggregateID {
			return errs.ErrConflict
		}
		stored, err := load(ctx, result.AggregateID)
		if err != nil {
			return err
		}
		if expectedSessionID != uuid.Nil && stored.SessionID != expectedSessionID {
			return errs.ErrConflict
		}
		return nil
	}
}

func acceptAnyRun(entity.AgentRun) error { return nil }
