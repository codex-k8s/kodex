package resource

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type scheduledOccurrenceExecutionBinding struct {
	SessionID              string
	SessionVersion         uint64
	TurnID                 string
	TurnVersion            uint64
	ProcessRunID           string
	ProcessVersion         uint64
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	InputSHA256            string
}

type scheduledOccurrenceClaimBinding struct {
	WorkloadID          string
	AuthorityGeneration uint64
	TokenSHA256         string
	ClaimKeySHA256      string
	LeaseExpiresAt      time.Time
}

func materializeScheduledOccurrence(
	occurrence *domainrepo.ScheduleOccurrence,
	snapshotSHA256 string,
	execution scheduledOccurrenceExecutionBinding,
	claim scheduledOccurrenceClaimBinding,
	now time.Time,
) error {
	if occurrence == nil || occurrence.State != "QUEUED" ||
		occurrence.Attempt == 0 || occurrence.EffectiveInputSHA256 != snapshotSHA256 ||
		!validSHA256Text(snapshotSHA256) || occurrenceHasExecutionBinding(*occurrence) ||
		occurrence.ClaimantWorkloadID != "" || occurrence.AuthorityGeneration != 0 ||
		occurrence.TokenHash != "" || occurrence.ClaimKeySHA256 != "" ||
		!occurrence.LeaseExpiresAt.IsZero() || !validScheduledExecutionBinding(execution) ||
		value.ValidateStableKey(claim.WorkloadID) != nil ||
		claim.AuthorityGeneration == 0 || !validSHA256Text(claim.TokenSHA256) ||
		!validSHA256Text(claim.ClaimKeySHA256) || !claim.LeaseExpiresAt.After(now) {
		return errs.ErrStateConflict
	}
	occurrence.State = "CLAIMED"
	occurrence.ClaimantWorkloadID = claim.WorkloadID
	occurrence.AuthorityGeneration = claim.AuthorityGeneration
	occurrence.TokenHash = claim.TokenSHA256
	occurrence.ClaimKeySHA256 = claim.ClaimKeySHA256
	occurrence.LeaseExpiresAt = claim.LeaseExpiresAt
	setScheduledExecutionBinding(occurrence, execution)
	occurrence.Outcome = ""
	occurrence.ResultArtifactID = ""
	occurrence.UpdatedAt = now
	return nil
}

func validateQueuedScheduledOccurrence(
	occurrence domainrepo.ScheduleOccurrence,
	schedule entity.Resource,
) error {
	spec, ok := schedule.Spec.(entity.ScheduleSpec)
	if !ok || schedule.Kind != enum.KindSchedule ||
		occurrence.ScheduleID != schedule.ID || occurrence.State != "QUEUED" ||
		occurrence.Attempt == 0 || occurrence.EffectiveInputSHA256 != spec.EffectiveInputSHA ||
		!validSHA256Text(occurrence.EffectiveInputSHA256) ||
		occurrenceHasExecutionBinding(occurrence) ||
		occurrence.ClaimantWorkloadID != "" || occurrence.AuthorityGeneration != 0 ||
		occurrence.TokenHash != "" || occurrence.ClaimKeySHA256 != "" ||
		!occurrence.LeaseExpiresAt.IsZero() {
		return errs.ErrStateConflict
	}
	return nil
}

// deadLetterQueuedScheduleOccurrence изолирует только не материализованную
// queued-строку после отката неуспешной owner transaction. Повторная проверка
// receipt, blocker и execution binding не позволяет quarantine затронуть
// конкурентно созданный либо уже исполняемый graph.
func (service *Service) deadLetterQueuedScheduleOccurrence(
	ctx context.Context,
	scope domainrepo.Scope,
	principal value.Principal,
	candidate domainrepo.ScheduleOccurrence,
	claimKeyHash string,
) error {
	return service.repository.Transact(
		ctx,
		scope,
		func(tx domainrepo.Transaction) error {
			occurrence, err := tx.GetScheduleOccurrenceForUpdate(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				candidate.ID,
			)
			if err != nil {
				return err
			}
			if occurrence.Attempt != candidate.Attempt || occurrence.State != "QUEUED" ||
				occurrenceHasExecutionBinding(occurrence) || occurrence.ClaimantWorkloadID != "" ||
				occurrence.AuthorityGeneration != 0 || occurrence.TokenHash != "" ||
				occurrence.ClaimKeySHA256 != "" || !occurrence.LeaseExpiresAt.IsZero() {
				return errs.ErrStateConflict
			}
			schedule, err := tx.GetForUpdate(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				occurrence.ScheduleID,
			)
			if err != nil {
				return err
			}
			if schedule.Kind != enum.KindSchedule || schedule.State != enum.StateActive {
				return errs.ErrStateConflict
			}
			blocking, err := tx.HasBlockingScheduleExecution(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				occurrence.ScheduleID,
				occurrence.ID,
			)
			if err != nil {
				return err
			}
			if blocking {
				return errs.ErrStateConflict
			}
			_, receiptErr := tx.GetReceipt(
				ctx,
				principal.OrganizationID,
				"claim_schedule_occurrence",
				claimKeyHash,
			)
			switch {
			case receiptErr == nil:
				return errs.ErrStateConflict
			case errors.Is(receiptErr, errs.ErrNotFound):
			default:
				return receiptErr
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			occurrence.State = "DEAD_LETTER"
			occurrence.Outcome = "materialization_invalid"
			occurrence.ResultArtifactID = ""
			occurrence.UpdatedAt = now.UTC().Truncate(time.Microsecond)
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				candidate.Attempt,
				"",
			); err != nil {
				return err
			}
			return appendScheduleOccurrenceAudit(
				ctx,
				tx,
				principal,
				"dead_letter_invalid_schedule_occurrence",
				occurrence,
			)
		},
	)
}

func rebindScheduledOccurrence(
	occurrence *domainrepo.ScheduleOccurrence,
	targetState string,
	execution scheduledOccurrenceExecutionBinding,
	outcome string,
	now time.Time,
) error {
	if occurrence == nil || !occurrenceHasExecutionBinding(*occurrence) ||
		!validSHA256Text(occurrence.EffectiveInputSHA256) ||
		!validScheduledExecutionBinding(execution) ||
		!scheduledOccurrenceRebindState(occurrence.State, targetState) {
		return errs.ErrStateConflict
	}
	occurrence.State = targetState
	setScheduledExecutionBinding(occurrence, execution)
	occurrence.Outcome = outcome
	occurrence.ResultArtifactID = ""
	occurrence.UpdatedAt = now
	return nil
}

func requeueScheduledOccurrence(
	occurrence *domainrepo.ScheduleOccurrence,
	schedule entity.Resource,
	run domainrepo.ScheduledRun,
	nextAttempt uint32,
	availableAt time.Time,
	outcome string,
	now time.Time,
) error {
	spec, ok := schedule.Spec.(entity.ScheduleSpec)
	if occurrence == nil || !ok || schedule.Kind != enum.KindSchedule ||
		!scheduleAllowsQueuedOccurrence(schedule.State) ||
		occurrence.ScheduleID != schedule.ID || occurrence.State != "CLAIMED" ||
		!scheduledOccurrenceRetryPolicyMatches(*occurrence, spec) ||
		nextAttempt != occurrence.Attempt+1 || !availableAt.After(now) ||
		run.OccurrenceID != occurrence.ID || run.Attempt != occurrence.Attempt ||
		run.State != "CLAIMED" || !occurrenceHasExecutionBinding(*occurrence) ||
		run.CurrentInputSHA256 != occurrence.EffectiveInputSHA256 ||
		run.EffectiveInputSHA256 != spec.EffectiveInputSHA ||
		!validSHA256Text(run.EffectiveInputSHA256) {
		return errs.ErrStateConflict
	}
	occurrence.State = "QUEUED"
	occurrence.Attempt = nextAttempt
	occurrence.EffectiveInputSHA256 = run.EffectiveInputSHA256
	occurrence.AvailableAt = availableAt
	occurrence.Outcome = outcome
	occurrence.ResultArtifactID = ""
	occurrence.ClaimantWorkloadID = ""
	occurrence.AuthorityGeneration = 0
	occurrence.TokenHash = ""
	occurrence.ClaimKeySHA256 = ""
	occurrence.LeaseExpiresAt = time.Time{}
	clearScheduledExecutionBinding(occurrence)
	occurrence.UpdatedAt = now
	return nil
}

func scheduleAllowsQueuedOccurrence(state enum.State) bool {
	return state == enum.StateActive || state == enum.StatePaused
}

func scheduleMutationRequiresClosedGraph(action string) bool {
	switch action {
	case "UPDATE", "ARCHIVE", "DELETE":
		return true
	default:
		return false
	}
}

func scheduledOccurrenceRetryPolicyMatches(
	occurrence domainrepo.ScheduleOccurrence,
	spec entity.ScheduleSpec,
) bool {
	return occurrence.MaximumAttempts == spec.MaximumAttempts &&
		occurrence.InitialBackoff == spec.InitialBackoff &&
		occurrence.MaximumBackoff == spec.MaximumBackoff &&
		occurrence.MaximumExecution == spec.MaximumExecutionDuration &&
		occurrence.DeadLetterAt.Equal(
			occurrence.ScheduledFor.Add(spec.DeadLetterAfter),
		)
}

func scheduledOccurrenceBackoff(
	occurrence domainrepo.ScheduleOccurrence,
	attempt uint32,
) time.Duration {
	delay := occurrence.InitialBackoff
	for current := uint32(2); current < attempt &&
		delay < occurrence.MaximumBackoff; current++ {
		if delay > occurrence.MaximumBackoff/2 {
			return occurrence.MaximumBackoff
		}
		delay *= 2
	}
	if delay > occurrence.MaximumBackoff {
		return occurrence.MaximumBackoff
	}
	return delay
}

// applyScheduledTerminalDisposition — единая server-owned disposition
// scheduler execution. Complete и watchdog recovery передают уже полученный
// canonical owner graph; helper не открывает дополнительных строк и поэтому
// сохраняет единый порядок блокировок.
func (service *Service) applyScheduledTerminalDisposition(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	schedule entity.Resource,
	occurrence domainrepo.ScheduleOccurrence,
	run domainrepo.ScheduledRun,
	terminalState, outcome, resultArtifactID string,
	now time.Time,
	auditAction string,
) (domainrepo.ScheduleOccurrence, error) {
	spec, ok := schedule.Spec.(entity.ScheduleSpec)
	if !ok || schedule.Kind != enum.KindSchedule ||
		!scheduleAllowsQueuedOccurrence(schedule.State) ||
		occurrence.ScheduleID != schedule.ID || occurrence.State != "CLAIMED" ||
		!scheduledOccurrenceRetryPolicyMatches(occurrence, spec) ||
		occurrence.TokenHash == "" || occurrence.Attempt == 0 || outcome == "" ||
		validateScheduledRunBinding(occurrence, run) != nil || run.State != "CLAIMED" {
		return domainrepo.ScheduleOccurrence{}, errs.ErrStateConflict
	}
	switch terminalState {
	case "SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED":
	default:
		return domainrepo.ScheduleOccurrence{}, errs.ErrStateConflict
	}

	expectedAttempt := occurrence.Attempt
	expectedToken := occurrence.TokenHash
	if err := tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
		OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
		State: terminalState, Outcome: outcome,
		ResultArtifactID: resultArtifactID, FinishedAt: now,
	}); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}

	retryable := terminalState == "FAILED" || terminalState == "EXPIRED"
	if retryable && occurrence.Attempt < occurrence.MaximumAttempts &&
		now.Before(occurrence.DeadLetterAt) {
		nextAttempt := occurrence.Attempt + 1
		if err := requeueScheduledOccurrence(
			&occurrence,
			schedule,
			run,
			nextAttempt,
			now.Add(scheduledOccurrenceBackoff(occurrence, nextAttempt)),
			outcome,
			now,
		); err != nil {
			return domainrepo.ScheduleOccurrence{}, err
		}
	} else {
		occurrence.State = terminalState
		if retryable {
			occurrence.State = "DEAD_LETTER"
		}
		occurrence.Outcome = outcome
		occurrence.ResultArtifactID = resultArtifactID
		occurrence.ClaimantWorkloadID = ""
		occurrence.AuthorityGeneration = 0
		occurrence.TokenHash = ""
		occurrence.ClaimKeySHA256 = ""
		occurrence.LeaseExpiresAt = time.Time{}
		occurrence.UpdatedAt = now
	}
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, expectedAttempt, expectedToken,
	); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	if err := appendScheduleOccurrenceAudit(
		ctx, tx, principal, auditAction, occurrence,
	); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	return occurrence, nil
}

func setScheduledExecutionBinding(
	occurrence *domainrepo.ScheduleOccurrence,
	binding scheduledOccurrenceExecutionBinding,
) {
	occurrence.ExecutionSessionID = binding.SessionID
	occurrence.ExecutionSessionVersion = binding.SessionVersion
	occurrence.ExecutionTurnID = binding.TurnID
	occurrence.ExecutionTurnVersion = binding.TurnVersion
	occurrence.ExecutionProcessRunID = binding.ProcessRunID
	occurrence.ExecutionProcessVersion = binding.ProcessVersion
	occurrence.ExecutionRuntimeRevisionID = binding.RuntimeRevisionID
	occurrence.ExecutionRuntimeRevisionVersion = binding.RuntimeRevisionVersion
	occurrence.EffectiveInputSHA256 = binding.InputSHA256
}

func clearScheduledExecutionBinding(occurrence *domainrepo.ScheduleOccurrence) {
	occurrence.ExecutionSessionID = ""
	occurrence.ExecutionSessionVersion = 0
	occurrence.ExecutionTurnID = ""
	occurrence.ExecutionTurnVersion = 0
	occurrence.ExecutionProcessRunID = ""
	occurrence.ExecutionProcessVersion = 0
	occurrence.ExecutionRuntimeRevisionID = ""
	occurrence.ExecutionRuntimeRevisionVersion = 0
}

func validScheduledExecutionBinding(binding scheduledOccurrenceExecutionBinding) bool {
	return value.ValidateID(binding.SessionID) == nil && binding.SessionVersion > 0 &&
		value.ValidateID(binding.TurnID) == nil && binding.TurnVersion > 0 &&
		(binding.ProcessRunID == "") == (binding.ProcessVersion == 0) &&
		(binding.ProcessRunID == "" || value.ValidateID(binding.ProcessRunID) == nil) &&
		value.ValidateID(binding.RuntimeRevisionID) == nil &&
		binding.RuntimeRevisionVersion > 0 && validSHA256Text(binding.InputSHA256)
}

func occurrenceHasExecutionBinding(occurrence domainrepo.ScheduleOccurrence) bool {
	return occurrence.ExecutionSessionID != "" || occurrence.ExecutionSessionVersion != 0 ||
		occurrence.ExecutionTurnID != "" || occurrence.ExecutionTurnVersion != 0 ||
		occurrence.ExecutionProcessRunID != "" || occurrence.ExecutionProcessVersion != 0 ||
		occurrence.ExecutionRuntimeRevisionID != "" ||
		occurrence.ExecutionRuntimeRevisionVersion != 0
}

func scheduledOccurrenceRebindState(source, target string) bool {
	switch source {
	case "CLAIMED":
		return target == "CLAIMED" || target == "CONTINUATION"
	case "WAITING_OWNER", "FAILED":
		return target == "CONTINUATION"
	case "CONTINUATION":
		return target == "CONTINUATION"
	default:
		return false
	}
}
