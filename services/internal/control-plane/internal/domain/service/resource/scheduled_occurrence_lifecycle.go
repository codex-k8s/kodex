package resource

import (
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
		occurrence.ScheduleID != schedule.ID || occurrence.State != "CLAIMED" ||
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
