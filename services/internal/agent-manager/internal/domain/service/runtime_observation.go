package service

import (
	"context"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

// GetAgentRunRuntimeStatus возвращает безопасную runtime-наблюдаемость для одного run.
func (s *Service) GetAgentRunRuntimeStatus(ctx context.Context, input GetAgentRunRuntimeStatusInput) (AgentRunRuntimeStatusResult, error) {
	run, err := getByID(ctx, s, input.RunID, s.repository.GetAgentRun)
	if err != nil {
		return AgentRunRuntimeStatusResult{}, err
	}
	status := agentRunRuntimeStatusFromRun(run)
	if strings.TrimSpace(status.RuntimeJobRef) != "" {
		status, err = s.observeRuntimeJob(ctx, input.Meta, run, status)
		if err != nil {
			return AgentRunRuntimeStatusResult{}, err
		}
	}
	status, err = s.attachRuntimeWaitSignals(ctx, run, status)
	if err != nil {
		return AgentRunRuntimeStatusResult{}, err
	}
	return AgentRunRuntimeStatusResult{Run: run, RuntimeStatus: status}, nil
}

func agentRunRuntimeStatusFromRun(run entity.AgentRun) AgentRunRuntimeStatus {
	observation := RuntimeObservationStateNotCreated
	jobRef := strings.TrimSpace(run.RuntimeContext.JobRef)
	if jobRef != "" {
		observation = RuntimeObservationStateStoredRef
	}
	return AgentRunRuntimeStatus{
		RunID:            run.ID,
		RunStatus:        run.Status,
		RuntimeContext:   run.RuntimeContext,
		ObservationState: observation,
		RuntimeJobRef:    jobRef,
		SafeErrorCode:    strings.TrimSpace(run.FailureCode),
		SafeSummary:      safeDiagnosticText(run.ResultSummary),
		RunStartedAt:     run.StartedAt,
		RunFinishedAt:    run.FinishedAt,
		RunUpdatedAt:     run.UpdatedAt,
		RunVersion:       run.Version,
	}
}

func (s *Service) observeRuntimeJob(ctx context.Context, meta value.QueryMeta, run entity.AgentRun, current AgentRunRuntimeStatus) (AgentRunRuntimeStatus, error) {
	job, err := s.runtimeJobReader.GetAgentRunJob(ctx, RuntimeJobReadInput{
		Meta:       meta,
		AgentRunID: run.ID,
		JobRef:     current.RuntimeJobRef,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return AgentRunRuntimeStatus{}, err
		}
		failure := classifyRuntimeJobFailure(err)
		current.ObservationState = runtimeObservationFailureState(failure)
		current.SafeErrorCode = failure.code
		current.SafeSummary = failure.summary()
		return current, nil
	}
	current.ObservationState = RuntimeObservationStateLive
	current.RuntimeJobRef = strings.TrimSpace(job.JobRef)
	current.RuntimeJobCommandRef = strings.TrimSpace(job.CommandRef)
	current.RuntimeJobStatus = job.Status
	current.RuntimeJobVersion = job.Version
	current.RuntimeJobCreatedAt = job.CreatedAt
	current.RuntimeJobStartedAt = job.StartedAt
	current.RuntimeJobFinishedAt = job.FinishedAt
	current.RuntimeJobUpdatedAt = job.UpdatedAt
	if code := strings.TrimSpace(job.SafeErrorCode); code != "" {
		current.SafeErrorCode = code
	}
	current.SafeSummary = runtimeJobObservationSummary(run, job)
	return current, nil
}

func runtimeObservationFailureState(failure runtimeOperationFailure) RuntimeObservationState {
	switch strings.TrimSpace(failure.code) {
	case "conflict", "failed_precondition", "not_found":
		return RuntimeObservationStateConflict
	default:
		return RuntimeObservationStateUnavailable
	}
}

func runtimeJobObservationSummary(run entity.AgentRun, job RuntimeJobReadResult) string {
	if summary := safeDiagnosticText(job.SafeSummary); summary != "" {
		return summary
	}
	if summary := safeDiagnosticText(job.SafeErrorSummary); summary != "" {
		return summary
	}
	return safeDiagnosticText(run.ResultSummary)
}

func (s *Service) attachRuntimeWaitSignals(ctx context.Context, run entity.AgentRun, status AgentRunRuntimeStatus) (AgentRunRuntimeStatus, error) {
	waiting := enum.HumanGateStatusWaiting
	gates, _, err := s.repository.ListHumanGateRequests(ctx, query.HumanGateFilter{
		SessionID: run.SessionID,
		RunID:     run.ID,
		Status:    &waiting,
		Page: value.PageRequest{
			PageSize: 1,
		},
	})
	if err != nil {
		return AgentRunRuntimeStatus{}, err
	}
	if len(gates) == 0 {
		return status, nil
	}
	status.HumanGateWaiting = true
	status.HumanGateRequestRef = gates[0].ID.String()
	status.HumanGateReasonCode = strings.TrimSpace(gates[0].ReasonCode)
	return status, nil
}
