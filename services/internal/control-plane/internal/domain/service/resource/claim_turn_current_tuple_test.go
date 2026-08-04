package resource

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type currentTupleTestRepository struct {
	domainrepo.Repository
	tx *currentTupleTestTransaction
}

func (repository *currentTupleTestRepository) Transact(
	_ context.Context,
	_ domainrepo.Scope,
	apply func(domainrepo.Transaction) error,
) error {
	resources := cloneResourceMap(repository.tx.resources)
	receipts := cloneReceiptMap(repository.tx.receipts)
	runtimes := cloneRuntimeMap(repository.tx.runtimes)
	occurrences := cloneOccurrenceMap(repository.tx.occurrences)
	runs := cloneRunMap(repository.tx.runs)
	leases := cloneLeaseMap(repository.tx.leases)
	attempts := cloneAttemptMap(repository.tx.attempts)
	audits, events := len(repository.tx.audits), len(repository.tx.events)
	if err := apply(repository.tx); err != nil {
		repository.tx.resources = resources
		repository.tx.receipts = receipts
		repository.tx.runtimes = runtimes
		repository.tx.occurrences = occurrences
		repository.tx.runs = runs
		repository.tx.leases = leases
		repository.tx.attempts = attempts
		repository.tx.audits = repository.tx.audits[:audits]
		repository.tx.events = repository.tx.events[:events]
		return err
	}
	return nil
}

func (repository *currentTupleTestRepository) ResolveRuntimeAgentBindingIntent(
	_ context.Context,
	organizationID, projectID, actorID, sourceRef string,
) (entity.Resource, entity.Resource, entity.Resource, error) {
	var session, turn, revision entity.Resource
	for _, candidate := range repository.tx.resources {
		spec, ok := candidate.Spec.(entity.TurnSpec)
		if !ok || candidate.OrganizationID != organizationID || candidate.ProjectID != projectID ||
			candidate.OwnerActorID != actorID || candidate.State != enum.StateQueued ||
			spec.SourceRef != sourceRef {
			continue
		}
		if turn.ID != "" {
			return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
		}
		turn = candidate
		session = repository.tx.resources[spec.SessionID]
		revision = repository.tx.resources[spec.RuntimeRevisionID]
	}
	if turn.ID == "" || session.ID == "" || revision.ID == "" {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrNotFound
	}
	return session, turn, revision, nil
}

type currentTupleTestTransaction struct {
	domainrepo.Transaction
	now           time.Time
	resources     map[string]entity.Resource
	receipts      map[string]domainrepo.Receipt
	runtimes      map[string]RuntimeExecution
	occurrences   map[string]domainrepo.ScheduleOccurrence
	runs          map[string]domainrepo.ScheduledRun
	leases        map[string]domainrepo.TurnLease
	attempts      map[string]domainrepo.TurnAttempt
	agentBindings map[string]domainrepo.RuntimeAgentBinding
	retention     domainrepo.ResourceRetentionPolicy
	audits        []domainrepo.Audit
	events        []event.Change
}

func (tx *currentTupleTestTransaction) CurrentTime(context.Context) (time.Time, error) {
	return tx.now, nil
}

func (tx *currentTupleTestTransaction) SessionHasUnverifiedRuntimeArchive(
	context.Context, string, string, string,
) (bool, error) {
	return false, nil
}

func (tx *currentTupleTestTransaction) SessionHasActiveRuntimeCleanup(
	context.Context, string, string, string,
) (bool, error) {
	return false, nil
}

func (tx *currentTupleTestTransaction) LatestSessionRuntimeArchiveForRestore(
	context.Context, string, string, string,
) (domainrepo.RuntimeExecution, error) {
	return domainrepo.RuntimeExecution{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) GetRuntimeAgentBindingForUpdate(
	_ context.Context,
	turnID string,
	attempt uint32,
) (domainrepo.RuntimeAgentBinding, error) {
	binding, ok := tx.agentBindings[turnAttemptMapKey(turnID, attempt)]
	if !ok {
		return domainrepo.RuntimeAgentBinding{}, errs.ErrNotFound
	}
	return binding, nil
}

func (tx *currentTupleTestTransaction) GetCurrentResourceRetentionPolicy(
	context.Context,
	string,
	string,
) (domainrepo.ResourceRetentionPolicy, error) {
	return tx.retention, nil
}

func receiptMapKey(scope, key string) string { return scope + "\x00" + key }

func (tx *currentTupleTestTransaction) GetReceipt(
	_ context.Context,
	_ string,
	scope, key string,
) (domainrepo.Receipt, error) {
	receipt, ok := tx.receipts[receiptMapKey(scope, key)]
	if !ok {
		return domainrepo.Receipt{}, errs.ErrNotFound
	}
	return receipt, nil
}

func (tx *currentTupleTestTransaction) SaveReceipt(
	_ context.Context,
	receipt domainrepo.Receipt,
) error {
	key := receiptMapKey(receipt.Scope, receipt.KeyHash)
	if _, exists := tx.receipts[key]; exists {
		return errs.ErrStateConflict
	}
	tx.receipts[key] = receipt
	return nil
}

func (tx *currentTupleTestTransaction) Get(
	_ context.Context,
	_, _, id string,
) (entity.Resource, error) {
	resource, ok := tx.resources[id]
	if !ok {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

func (tx *currentTupleTestTransaction) GetForUpdate(
	ctx context.Context,
	organizationID, projectID, id string,
) (entity.Resource, error) {
	return tx.Get(ctx, organizationID, projectID, id)
}

func (tx *currentTupleTestTransaction) Insert(
	_ context.Context,
	resource entity.Resource,
) error {
	if _, exists := tx.resources[resource.ID]; exists {
		return errs.ErrStateConflict
	}
	tx.resources[resource.ID] = resource
	return nil
}

func (tx *currentTupleTestTransaction) Update(
	_ context.Context,
	resource entity.Resource,
	expectedVersion uint64,
) error {
	current, ok := tx.resources[resource.ID]
	if !ok || current.Version != expectedVersion || resource.Version != expectedVersion+1 {
		return errs.ErrStateConflict
	}
	tx.resources[resource.ID] = resource
	return nil
}

func (tx *currentTupleTestTransaction) AppendAudit(
	_ context.Context,
	audit domainrepo.Audit,
) error {
	tx.audits = append(tx.audits, audit)
	return nil
}

func (tx *currentTupleTestTransaction) AppendEvent(
	_ context.Context,
	change event.Change,
) error {
	for _, current := range tx.events {
		if current.ResourceKind == change.ResourceKind &&
			current.ResourceID == change.ResourceID &&
			current.EventSequence == change.EventSequence {
			return errs.ErrStateConflict
		}
	}
	tx.events = append(tx.events, change)
	return nil
}

func (tx *currentTupleTestTransaction) ListSnapshotResources(
	_ context.Context,
	organizationID, projectID string,
) ([]entity.Resource, error) {
	resources := make([]entity.Resource, 0, len(tx.resources))
	for _, resource := range tx.resources {
		if resource.OrganizationID != organizationID || resource.ProjectID != projectID {
			continue
		}
		switch resource.Kind {
		case enum.KindProject, enum.KindRole, enum.KindPromptProfile,
			enum.KindCredentialBinding, enum.KindRepositoryWorkspace,
			enum.KindIntegration, enum.KindChat:
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func (tx *currentTupleTestTransaction) LatestRuntimeRevision(
	_ context.Context,
	organizationID, projectID string,
) (entity.Resource, error) {
	var latest entity.Resource
	for _, resource := range tx.resources {
		if resource.OrganizationID != organizationID || resource.ProjectID != projectID ||
			resource.Kind != enum.KindRuntimeRevision {
			continue
		}
		if latest.ID == "" || resource.CreatedAt.After(latest.CreatedAt) ||
			(resource.CreatedAt.Equal(latest.CreatedAt) && resource.ID > latest.ID) {
			latest = resource
		}
	}
	if latest.ID == "" {
		return entity.Resource{}, errs.ErrNotFound
	}
	return latest, nil
}

func (tx *currentTupleTestTransaction) GetRuntimeExecutionByTurn(
	_ context.Context,
	turnID string,
	attempt uint32,
) (RuntimeExecution, error) {
	for _, execution := range tx.runtimes {
		if execution.TurnID == turnID && execution.Attempt == attempt {
			return execution, nil
		}
	}
	return RuntimeExecution{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) GetRuntimeExecutionByTurnForUpdate(
	ctx context.Context,
	turnID string,
	attempt uint32,
) (RuntimeExecution, error) {
	return tx.GetRuntimeExecutionByTurn(ctx, turnID, attempt)
}

func (tx *currentTupleTestTransaction) GetRuntimeExecutionForUpdate(
	_ context.Context,
	executionID string,
) (RuntimeExecution, error) {
	execution, ok := tx.runtimes[executionID]
	if !ok {
		return RuntimeExecution{}, errs.ErrNotFound
	}
	return execution, nil
}

func (tx *currentTupleTestTransaction) InsertRuntimeExecution(
	_ context.Context,
	execution RuntimeExecution,
) error {
	if _, exists := tx.runtimes[execution.ID]; exists {
		return errs.ErrStateConflict
	}
	tx.runtimes[execution.ID] = execution
	return nil
}

func (tx *currentTupleTestTransaction) UpdateRuntimeExecution(
	_ context.Context,
	execution RuntimeExecution,
	expectedVersion, expectedFence uint64,
) error {
	current, ok := tx.runtimes[execution.ID]
	if !ok || current.Version != expectedVersion || current.Fence != expectedFence ||
		execution.Version != expectedVersion+1 || execution.Fence != expectedFence+1 {
		return errs.ErrStateConflict
	}
	tx.runtimes[execution.ID] = execution
	return nil
}

func (tx *currentTupleTestTransaction) ExpiredClaimedTurnCandidates(
	context.Context, string, string, string, int, time.Time,
) ([]domainrepo.ExpiredTurn, error) {
	return nil, nil
}

func (tx *currentTupleTestTransaction) NextQueuedTurn(
	_ context.Context,
	_, _, turnID string,
) (entity.Resource, error) {
	turn, ok := tx.resources[turnID]
	if !ok || turn.Kind != enum.KindTurn || turn.State != enum.StateQueued {
		return entity.Resource{}, errs.ErrNotFound
	}
	return turn, nil
}

func (tx *currentTupleTestTransaction) SaveTurnLease(
	_ context.Context,
	lease domainrepo.TurnLease,
) error {
	if _, exists := tx.leases[lease.TurnID]; exists {
		return errs.ErrStateConflict
	}
	tx.leases[lease.TurnID] = lease
	return nil
}

func (tx *currentTupleTestTransaction) GetTurnLeaseForUpdate(
	_ context.Context,
	turnID string,
) (domainrepo.TurnLease, error) {
	lease, ok := tx.leases[turnID]
	if !ok {
		return domainrepo.TurnLease{}, errs.ErrNotFound
	}
	return lease, nil
}

func (tx *currentTupleTestTransaction) ValidateTurnLease(
	_ context.Context,
	turnID, tokenHash, workloadID string,
	authorityGeneration uint64,
	attempt uint32,
	now time.Time,
) (domainrepo.TurnLease, error) {
	lease, ok := tx.leases[turnID]
	if !ok || lease.TokenHash != tokenHash || lease.WorkloadID != workloadID ||
		lease.AuthorityGeneration != authorityGeneration || lease.Attempt != attempt ||
		!lease.ExpiresAt.After(now) {
		return domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	return lease, nil
}

func (tx *currentTupleTestTransaction) DeleteTurnLease(
	_ context.Context,
	turnID string,
	fence uint64,
) error {
	lease, ok := tx.leases[turnID]
	if !ok || lease.Fence != fence {
		return errs.ErrStateConflict
	}
	delete(tx.leases, turnID)
	return nil
}

func (tx *currentTupleTestTransaction) RenewTurnLease(
	_ context.Context,
	lease domainrepo.TurnLease,
	now time.Time,
) (domainrepo.TurnLease, error) {
	current, ok := tx.leases[lease.TurnID]
	if !ok || current.TokenHash != lease.TokenHash || current.Attempt != lease.Attempt ||
		current.Fence != lease.Fence || !current.ExpiresAt.After(now) {
		return domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	tx.leases[lease.TurnID] = lease
	return lease, nil
}

func turnAttemptMapKey(turnID string, attempt uint32) string {
	return turnID + "\x00" + strconv.FormatUint(uint64(attempt), 10)
}

func (tx *currentTupleTestTransaction) SaveTurnAttempt(
	_ context.Context,
	attempt domainrepo.TurnAttempt,
) error {
	key := turnAttemptMapKey(attempt.TurnID, attempt.Attempt)
	if current, exists := tx.attempts[key]; exists && current.State != "QUEUED" {
		return errs.ErrStateConflict
	}
	tx.attempts[key] = attempt
	return nil
}

func (tx *currentTupleTestTransaction) GetTurnAttemptForUpdate(
	_ context.Context,
	turnID string,
	attempt uint32,
) (domainrepo.TurnAttempt, error) {
	current, ok := tx.attempts[turnAttemptMapKey(turnID, attempt)]
	if !ok {
		return domainrepo.TurnAttempt{}, errs.ErrNotFound
	}
	return current, nil
}

func (tx *currentTupleTestTransaction) FinishTurnAttempt(
	_ context.Context,
	attempt domainrepo.TurnAttempt,
) error {
	key := turnAttemptMapKey(attempt.TurnID, attempt.Attempt)
	current, ok := tx.attempts[key]
	if !ok || current.WorkloadID != attempt.WorkloadID ||
		current.AuthorityGeneration != attempt.AuthorityGeneration ||
		current.LeaseFence != attempt.LeaseFence || !current.FinishedAt.IsZero() ||
		attempt.FinishedAt.IsZero() {
		return errs.ErrStateConflict
	}
	tx.attempts[key] = attempt
	return nil
}

func (tx *currentTupleTestTransaction) GetScheduleOccurrenceByCurrentTurn(
	_ context.Context,
	_, _, turnID string,
) (domainrepo.ScheduleOccurrence, error) {
	for _, occurrence := range tx.occurrences {
		if occurrence.ExecutionTurnID == turnID {
			return occurrence, nil
		}
	}
	return domainrepo.ScheduleOccurrence{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) GetScheduleOccurrenceForUpdate(
	_ context.Context,
	_, _, occurrenceID string,
) (domainrepo.ScheduleOccurrence, error) {
	occurrence, ok := tx.occurrences[occurrenceID]
	if !ok {
		return domainrepo.ScheduleOccurrence{}, errs.ErrNotFound
	}
	return occurrence, nil
}

func (tx *currentTupleTestTransaction) GetScheduleOccurrence(
	ctx context.Context,
	organizationID, projectID, occurrenceID string,
) (domainrepo.ScheduleOccurrence, error) {
	return tx.GetScheduleOccurrenceForUpdate(ctx, organizationID, projectID, occurrenceID)
}

func (tx *currentTupleTestTransaction) DueSchedules(
	_ context.Context,
	organizationID, projectID string,
	limit int,
	now time.Time,
) ([]entity.Resource, error) {
	result := make([]entity.Resource, 0, limit)
	for _, resource := range tx.resources {
		spec, ok := resource.Spec.(entity.ScheduleSpec)
		if !ok || resource.Kind != enum.KindSchedule || resource.State != enum.StateActive ||
			resource.OrganizationID != organizationID || resource.ProjectID != projectID ||
			spec.NextRunAt.After(now) {
			continue
		}
		result = append(result, resource)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (tx *currentTupleTestTransaction) SaveScheduleOccurrence(
	_ context.Context,
	occurrence domainrepo.ScheduleOccurrence,
) error {
	if _, exists := tx.occurrences[occurrence.ID]; exists {
		return errs.ErrStateConflict
	}
	tx.occurrences[occurrence.ID] = occurrence
	return nil
}

func (tx *currentTupleTestTransaction) HasOpenScheduleOccurrence(
	_ context.Context,
	organizationID, projectID, scheduleID string,
) (bool, error) {
	for _, occurrence := range tx.occurrences {
		if occurrence.OrganizationID == organizationID && occurrence.ProjectID == projectID &&
			occurrence.ScheduleID == scheduleID &&
			occurrence.State != "SUCCEEDED" && occurrence.State != "FAILED" &&
			occurrence.State != "CANCELLED" && occurrence.State != "DEAD_LETTER" &&
			occurrence.State != "SKIPPED" {
			return true, nil
		}
		if occurrence.OrganizationID != organizationID ||
			occurrence.ProjectID != projectID || occurrence.ScheduleID != scheduleID {
			continue
		}
		for key, run := range tx.runs {
			if strings.HasPrefix(key, occurrence.ID+"\x00") &&
				(run.State == "CLAIMED" || run.State == "WAITING_OWNER" ||
					run.State == "CONTINUATION") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (tx *currentTupleTestTransaction) HasBlockingScheduleExecution(
	_ context.Context,
	organizationID, projectID, scheduleID, candidateOccurrenceID string,
) (bool, error) {
	for _, occurrence := range tx.occurrences {
		if occurrence.OrganizationID != organizationID ||
			occurrence.ProjectID != projectID || occurrence.ScheduleID != scheduleID {
			continue
		}
		if occurrence.ID != candidateOccurrenceID &&
			(occurrence.State == "CLAIMED" || occurrence.State == "WAITING_OWNER" ||
				occurrence.State == "CONTINUATION") {
			return true, nil
		}
		for key, run := range tx.runs {
			if strings.HasPrefix(key, occurrence.ID+"\x00") &&
				(run.State == "CLAIMED" || run.State == "WAITING_OWNER" ||
					run.State == "CONTINUATION") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (tx *currentTupleTestTransaction) SkipOverlappedScheduleOccurrences(
	ctx context.Context, organizationID, projectID string, now time.Time,
) ([]domainrepo.ScheduleOccurrence, error) {
	result := make([]domainrepo.ScheduleOccurrence, 0)
	for id, occurrence := range tx.occurrences {
		if occurrence.OrganizationID != organizationID ||
			occurrence.ProjectID != projectID || occurrence.State != "QUEUED" ||
			occurrence.OverlapPolicy != "SKIP" || occurrence.AvailableAt.After(now) {
			continue
		}
		blocking, err := tx.HasBlockingScheduleExecution(
			ctx, organizationID, projectID, occurrence.ScheduleID, occurrence.ID,
		)
		if err != nil {
			return nil, err
		}
		if !blocking {
			continue
		}
		occurrence.State = "SKIPPED"
		occurrence.Outcome = "overlap"
		occurrence.UpdatedAt = now
		tx.occurrences[id] = occurrence
		result = append(result, occurrence)
	}
	return result, nil
}

func (tx *currentTupleTestTransaction) ExpiredScheduleOccurrenceCandidates(
	_ context.Context,
	organizationID, projectID string,
	now time.Time,
) ([]domainrepo.ScheduleOccurrence, error) {
	result := make([]domainrepo.ScheduleOccurrence, 0)
	for _, occurrence := range tx.occurrences {
		if occurrence.OrganizationID == organizationID && occurrence.ProjectID == projectID &&
			occurrence.State == "CLAIMED" && !occurrence.LeaseExpiresAt.After(now) {
			result = append(result, occurrence)
		}
	}
	return result, nil
}

func (tx *currentTupleTestTransaction) NextScheduleOccurrence(
	ctx context.Context,
	organizationID, projectID string,
	now time.Time,
) (domainrepo.ScheduleOccurrence, error) {
	for _, occurrence := range tx.occurrences {
		if occurrence.OrganizationID == organizationID && occurrence.ProjectID == projectID &&
			occurrence.State == "QUEUED" && !occurrence.AvailableAt.After(now) {
			blocking, err := tx.HasBlockingScheduleExecution(
				ctx, organizationID, projectID,
				occurrence.ScheduleID, occurrence.ID,
			)
			if err != nil {
				return domainrepo.ScheduleOccurrence{}, err
			}
			if blocking {
				continue
			}
			return occurrence, nil
		}
	}
	return domainrepo.ScheduleOccurrence{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) GetScheduleOccurrenceByClaimKey(
	_ context.Context,
	organizationID, projectID, keyHash string,
) (domainrepo.ScheduleOccurrence, error) {
	for _, occurrence := range tx.occurrences {
		if occurrence.OrganizationID == organizationID && occurrence.ProjectID == projectID &&
			occurrence.ClaimKeySHA256 == keyHash {
			return occurrence, nil
		}
	}
	return domainrepo.ScheduleOccurrence{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) UpdateScheduleOccurrence(
	_ context.Context,
	occurrence domainrepo.ScheduleOccurrence,
	expectedAttempt uint32,
	expectedTokenHash string,
) error {
	current, ok := tx.occurrences[occurrence.ID]
	if !ok || current.Attempt != expectedAttempt ||
		(expectedTokenHash != "" && current.TokenHash != expectedTokenHash) {
		return errs.ErrStateConflict
	}
	// Модель fake повторяет поле-за-полем production UPDATE, чтобы новое
	// изменяемое поле нельзя было случайно "проверить" заменой всей структуры.
	current.State = occurrence.State
	current.Attempt = occurrence.Attempt
	current.EffectiveInputSHA256 = occurrence.EffectiveInputSHA256
	current.ClaimantWorkloadID = occurrence.ClaimantWorkloadID
	current.AuthorityGeneration = occurrence.AuthorityGeneration
	current.TokenHash = occurrence.TokenHash
	current.ClaimKeySHA256 = occurrence.ClaimKeySHA256
	current.LeaseExpiresAt = occurrence.LeaseExpiresAt
	current.AvailableAt = occurrence.AvailableAt
	current.Outcome = occurrence.Outcome
	current.ResultArtifactID = occurrence.ResultArtifactID
	current.ExecutionSessionID = occurrence.ExecutionSessionID
	current.ExecutionSessionVersion = occurrence.ExecutionSessionVersion
	current.ExecutionTurnID = occurrence.ExecutionTurnID
	current.ExecutionTurnVersion = occurrence.ExecutionTurnVersion
	current.ExecutionProcessRunID = occurrence.ExecutionProcessRunID
	current.ExecutionProcessVersion = occurrence.ExecutionProcessVersion
	current.ExecutionRuntimeRevisionID = occurrence.ExecutionRuntimeRevisionID
	current.ExecutionRuntimeRevisionVersion = occurrence.ExecutionRuntimeRevisionVersion
	current.UpdatedAt = occurrence.UpdatedAt
	tx.occurrences[occurrence.ID] = current
	return nil
}

func (tx *currentTupleTestTransaction) GetScheduledRunForUpdate(
	_ context.Context,
	occurrenceID string,
	attempt uint32,
) (domainrepo.ScheduledRun, error) {
	run, ok := tx.runs[turnAttemptMapKey(occurrenceID, attempt)]
	if !ok {
		return domainrepo.ScheduledRun{}, errs.ErrNotFound
	}
	return run, nil
}

func (tx *currentTupleTestTransaction) SaveScheduledRun(
	_ context.Context,
	run domainrepo.ScheduledRun,
) error {
	key := turnAttemptMapKey(run.OccurrenceID, run.Attempt)
	if _, exists := tx.runs[key]; exists {
		return errs.ErrStateConflict
	}
	tx.runs[key] = run
	return nil
}

func (tx *currentTupleTestTransaction) RebindScheduledRun(
	_ context.Context,
	run domainrepo.ScheduledRun,
	expectedTurnID string,
	expectedTurnAttempt uint32,
) error {
	key := turnAttemptMapKey(run.OccurrenceID, run.Attempt)
	current, ok := tx.runs[key]
	if !ok || current.CurrentTurnID != expectedTurnID ||
		current.CurrentTurnAttempt != expectedTurnAttempt {
		return errs.ErrStateConflict
	}
	tx.runs[key] = run
	return nil
}

func (tx *currentTupleTestTransaction) FinishScheduledRun(
	_ context.Context,
	run domainrepo.ScheduledRun,
) error {
	key := turnAttemptMapKey(run.OccurrenceID, run.Attempt)
	current, ok := tx.runs[key]
	if !ok || (current.State != "CLAIMED" && current.State != "WAITING_OWNER" &&
		current.State != "CONTINUATION") || run.FinishedAt.IsZero() {
		return errs.ErrStateConflict
	}
	current.State = run.State
	current.Outcome = run.Outcome
	current.ResultArtifactID = run.ResultArtifactID
	current.FinishedAt = run.FinishedAt
	tx.runs[key] = current
	return nil
}

func (tx *currentTupleTestTransaction) ActiveWorkClaimsForUpdate(
	context.Context, string, string, string, string,
) ([]entity.Resource, error) {
	return nil, nil
}

func (tx *currentTupleTestTransaction) ProcessHasOpenWork(
	context.Context, string, string, string, string, string,
) (bool, error) {
	return false, nil
}

func (tx *currentTupleTestTransaction) HasActiveChildProcesses(
	context.Context, string, string, string,
) (bool, error) {
	return false, nil
}

func (tx *currentTupleTestTransaction) ActiveOwnerGateForProcess(
	context.Context, string, string, string,
) (entity.Resource, error) {
	return entity.Resource{}, errs.ErrNotFound
}

func cloneResourceMap(source map[string]entity.Resource) map[string]entity.Resource {
	result := make(map[string]entity.Resource, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

func cloneReceiptMap(source map[string]domainrepo.Receipt) map[string]domainrepo.Receipt {
	result := make(map[string]domainrepo.Receipt, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

func cloneRuntimeMap(source map[string]RuntimeExecution) map[string]RuntimeExecution {
	result := make(map[string]RuntimeExecution, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

func cloneOccurrenceMap(
	source map[string]domainrepo.ScheduleOccurrence,
) map[string]domainrepo.ScheduleOccurrence {
	result := make(map[string]domainrepo.ScheduleOccurrence, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

func cloneRunMap(source map[string]domainrepo.ScheduledRun) map[string]domainrepo.ScheduledRun {
	result := make(map[string]domainrepo.ScheduledRun, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

func cloneLeaseMap(source map[string]domainrepo.TurnLease) map[string]domainrepo.TurnLease {
	result := make(map[string]domainrepo.TurnLease, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

func cloneAttemptMap(source map[string]domainrepo.TurnAttempt) map[string]domainrepo.TurnAttempt {
	result := make(map[string]domainrepo.TurnAttempt, len(source))
	for key, item := range source {
		result[key] = item
	}
	return result
}

type currentTupleTestObserver struct{}

func (currentTupleTestObserver) ObserveMutation(enum.Kind, string) {}

type currentTupleFixture struct {
	service       *Service
	tx            *currentTupleTestTransaction
	organization  string
	project       string
	actor         string
	sessionID     string
	turnID        string
	revisionID    string
	artifactID    string
	roleID        string
	promptID      string
	bindingID     string
	inputSHA256   string
	claimKey      string
	grant         uint64
	runtimeWorker string
	runtimeSPIFFE string
}

func newCurrentTupleFixture(t *testing.T) currentTupleFixture {
	t.Helper()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	organization, project, actor := uuid.NewString(), uuid.NewString(), uuid.NewString()
	sessionID, turnID := uuid.NewString(), uuid.NewString()
	revisionID, artifactID, roleID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	promptID, bindingID := uuid.NewString(), uuid.NewString()
	digest := hashString("claim-turn-current-tuple")
	projectResource, err := entity.New(
		project, organization, project, "", actor, enum.KindProject, "Project",
		entity.ProjectSpec{
			Slug: "test-project", Locale: "ru",
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, now,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	prompt, err := entity.New(
		promptID, organization, project, "", actor,
		enum.KindPromptProfile, "Prompt profile",
		entity.PromptProfileSpec{
			Revision: 1, ContentSHA256: digest, SourceRef: "prompt:test", Locale: "ru",
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, now,
	)
	if err != nil {
		t.Fatalf("create prompt profile: %v", err)
	}
	binding, err := entity.New(
		bindingID, organization, project, "", actor,
		enum.KindCredentialBinding, "Provider account",
		entity.CredentialBindingSpec{
			Purpose: "provider-account", SecretRef: "vault://provider/account",
			PrincipalRef: "provider:test", Revision: 1, ProviderEligible: true,
			ProviderCapabilities: []string{"chat"}, ProviderObservedLimit: 10,
			ProviderObservationRevision: 1, ProviderObservedAt: now,
			ImmutableSecretRef:     "k8s-immutable-secret://mattercodex-system/runtime-provider-test-v1",
			ProviderContentVersion: "provider:test:v1", ContentSHA256: digest,
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, now,
	)
	if err != nil {
		t.Fatalf("create credential binding: %v", err)
	}
	role, err := entity.New(
		roleID, organization, project, "", actor, enum.KindRole, "Runtime role",
		entity.RoleSpec{
			StableKey: "runtime-role", Capabilities: []string{"runtime.execute"},
			PromptProfileID:              promptID,
			ProviderCredentialBindingIDs: []string{bindingID},
			ProviderAccountPool: entity.ProviderAccountPool{
				Policy: "least_used", PolicyRevision: 1,
				ObservationMaxAge: time.Minute,
				Bindings: []entity.ProviderAccountPoolBinding{{
					CredentialBindingID: bindingID, Weight: 1,
				}},
			},
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, now,
	)
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	roleSHA, err := entity.ProjectionSHA256(role)
	if err != nil {
		t.Fatalf("hash role: %v", err)
	}
	promptSHA, err := entity.ProjectionSHA256(prompt)
	if err != nil {
		t.Fatalf("hash prompt profile: %v", err)
	}
	bindingSHA, err := entity.ProjectionSHA256(binding)
	if err != nil {
		t.Fatalf("hash credential binding: %v", err)
	}
	components := []entity.EffectiveResourceRef{{
		Kind: enum.KindRole, ResourceID: role.ID, Version: role.Version,
		ProjectionSHA256: roleSHA,
	}, {
		Kind: enum.KindPromptProfile, ResourceID: prompt.ID, Version: prompt.Version,
		ProjectionSHA256: promptSHA,
	}, {
		Kind: enum.KindCredentialBinding, ResourceID: binding.ID, Version: binding.Version,
		ProjectionSHA256: bindingSHA,
	}}
	for _, kind := range []enum.Kind{
		enum.KindRepositoryWorkspace, enum.KindIntegration,
	} {
		components = append(components, entity.EffectiveResourceRef{
			Kind: kind, ResourceID: uuid.NewString(), Version: 1,
			ProjectionSHA256: digest,
		})
	}
	revision, err := entity.New(
		revisionID, organization, project, sessionID, actor,
		enum.KindRuntimeRevision, "Runtime revision",
		entity.RuntimeRevisionSpec{
			ManifestSHA256: digest, ImageDigest: "sha256:" + digest,
			PromptProfileID: promptID, PromptRevision: 1,
			CredentialBindingIDs:   []string{bindingID},
			AuthorityPolicyVersion: 1, AuthorityPolicySHA256: digest,
			Components: components, CreatedAt: now, SessionID: sessionID,
			RoleID: roleID, ProviderCredentialBindingID: bindingID,
			EffectiveRuntimeSHA256: digest,
		}, now,
	)
	if err != nil {
		t.Fatalf("create runtime revision: %v", err)
	}
	session, err := entity.New(
		sessionID, organization, project, "", actor, enum.KindSession, "Session",
		entity.SessionSpec{
			AgentID: roleID, ProviderAccountBindingID: bindingID, LastTurnSequence: 1,
		}, now,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	turn, err := entity.New(
		turnID, organization, project, sessionID, actor, enum.KindTurn, "Turn",
		entity.TurnSpec{
			SessionID: sessionID, Sequence: 1, SourceRef: "test:turn",
			PromptArtifactID: artifactID, RuntimeRevisionID: revisionID,
			Attempt: 1, EffectiveInputSHA256: digest,
		}, now,
	)
	if err != nil {
		t.Fatalf("create turn: %v", err)
	}
	artifact, err := entity.New(
		artifactID, organization, project, sessionID, actor, enum.KindArtifact, "Input",
		entity.ArtifactSpec{
			ArtifactKind: "prompt", Direction: "INPUT", StorageRef: "s3://test/input",
			SizeBytes: 1, MediaType: "text/plain", SHA256: digest,
			ScanStatus: "CLEAN", RetentionPolicyRef: "retention:test",
			ScanPolicyRevision: 1, ScanEvidenceSHA256: digest,
			ScannerWorkloadID: "artifact-scanner", ScannedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	revisionSHA256, err := entity.ProjectionSHA256(revision)
	if err != nil {
		t.Fatalf("hash runtime revision: %v", err)
	}
	tx := &currentTupleTestTransaction{
		now: now,
		resources: map[string]entity.Resource{
			projectResource.ID: projectResource, prompt.ID: prompt, binding.ID: binding,
			role.ID: role, revision.ID: revision, session.ID: session,
			turn.ID: turn, artifact.ID: artifact,
		},
		receipts:    make(map[string]domainrepo.Receipt),
		runtimes:    make(map[string]RuntimeExecution),
		occurrences: make(map[string]domainrepo.ScheduleOccurrence),
		runs:        make(map[string]domainrepo.ScheduledRun),
		leases:      make(map[string]domainrepo.TurnLease),
		attempts: map[string]domainrepo.TurnAttempt{
			turnAttemptMapKey(turn.ID, 1): {
				TurnID: turn.ID, Attempt: 1, WorkloadID: "unassigned",
				AuthorityGeneration: 1, State: "QUEUED", InputSHA256: digest,
				LeaseFence: turn.Version, StartedAt: now,
			},
		},
		agentBindings: map[string]domainrepo.RuntimeAgentBinding{
			turnAttemptMapKey(turn.ID, 1): {
				OrganizationID: organization, ProjectID: project, SessionID: sessionID,
				TurnID: turnID, Attempt: 1, InputSHA256: digest,
				RuntimeRevisionID: revisionID, RuntimeRevisionVersion: revision.Version,
				RuntimeRevisionSHA256: revisionSHA256,
				AgentSessionKey:       "agent-session-test", AgentSessionID: 101,
				AgentSessionVersion: 1, AgentSessionBindingSHA256: digest,
				AgentSessionTurnID: 201, AgentRunID: "agent-run-test",
				AgentSessionTurnVersion: 1, AgentTurnBindingSHA256: digest,
				CreatedAt: now,
			},
		},
		retention: domainrepo.ResourceRetentionPolicy{
			ID: "runtime-default", Version: 1,
			PVCRetentionSeconds:     uint64((7 * 24 * time.Hour) / time.Second),
			ArchiveRetentionSeconds: uint64((90 * 24 * time.Hour) / time.Second),
			EffectiveFrom:           now.Add(-time.Hour),
		},
	}
	repository := &currentTupleTestRepository{tx: tx}
	const runtimeWorker = "runtime-controller"
	const runtimeSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller"
	service, err := New(repository, Config{
		LeaseSigningKey:            []byte("0123456789abcdef0123456789abcdef"),
		RuntimeAdmissionSigningKey: ed25519.NewKeyFromSeed([]byte("runtime-admission-signing-test!!")),
		RuntimeArchiveSigningKey:   ed25519.NewKeyFromSeed([]byte("runtime-archive-signing-test-key")),
		RuntimeRestoreSigningKey:   ed25519.NewKeyFromSeed([]byte("runtime-restore-signing-test-key")),
		TurnLeaseDuration:          time.Minute, MaximumScheduleClaims: 10,
		RuntimeImageDigest:      "sha256:" + digest,
		AuthorityPolicyRevision: 1, AuthorityPolicySHA256: digest,
		OwnerGateDeliveryWorkload:  "interaction-gateway",
		OwnerGateDeliverySPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		ScannerWorkload:            "artifact-scanner",
		ScannerSPIFFEID:            "spiffe://mattercodex.local/ns/mattercodex-system/sa/artifact-scanner",
		SchedulerWorkload:          "scheduler",
		SchedulerSPIFFEID:          "spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		MemoryIndexerWorkload:      "memory-indexer",
		MemoryIndexerSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/memory-indexer",
		RuntimeControllerWorkload:  runtimeWorker,
		RuntimeControllerSPIFFEID:  runtimeSPIFFE,
		BotServiceWorkload:         "bot-service",
		BotServiceSPIFFEID:         "spiffe://mattercodex.local/ns/mattercodex-system/sa/bot-service",
		ArchiveWorkload:            "runtime-archive",
		ArchiveSPIFFEID:            "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-archive",
		IntegrationGatewayWorkload: "integration-gateway",
		IntegrationGatewaySPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		RestoreVerifierWorkload:    "restore-verifier",
		RestoreVerifierSPIFFEID:    "spiffe://mattercodex.local/ns/mattercodex-system/sa/restore-verifier",
		CleanupAuthorizerWorkload:  "cleanup-authorizer",
		CleanupAuthorizerSPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/cleanup-authorizer",
		PendingRescheduleDelay:     30 * time.Second,
		Observer:                   currentTupleTestObserver{},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return tx.now }
	return currentTupleFixture{
		service: service, tx: tx, organization: organization, project: project,
		actor: actor, sessionID: sessionID, turnID: turnID, revisionID: revisionID,
		artifactID: artifactID, roleID: roleID, promptID: promptID, bindingID: bindingID,
		inputSHA256: digest,
		claimKey:    "claim-turn-current-tuple", grant: 7,
		runtimeWorker: runtimeWorker, runtimeSPIFFE: runtimeSPIFFE,
	}
}

func (fixture currentTupleFixture) principal(
	permission, workload, spiffe string,
) value.Principal {
	return fixture.principalFor(
		permission, workload, spiffe, fixture.turnID, 1,
		fixture.inputSHA256, fixture.grant,
	)
}

func (fixture currentTupleFixture) principalFor(
	permission, workload, spiffe, authorityReference string,
	authorityRevision uint64,
	authorityDigest string,
	grant uint64,
) value.Principal {
	return value.Principal{
		ActorID: fixture.actor, OrganizationID: fixture.organization,
		ProjectID: fixture.project, Permission: permission,
		CorrelationID: uuid.NewString(), PolicyRevision: 1, AuthorityGeneration: 1,
		CallerWorkload: workload, CallerSPIFFEID: spiffe,
		AuthoritySource: "AGENT_SESSION", AuthorityReference: authorityReference,
		AuthorityRevision: authorityRevision, AuthorityDigest: authorityDigest,
		AuthorityGrantGeneration: grant,
	}
}

func (fixture currentTupleFixture) startRootProcess(t *testing.T) entity.Resource {
	t.Helper()
	principal := fixture.principal(
		permissionStartProcess, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
	)
	process, err := fixture.service.StartProcess(context.Background(), StartProcessInput{
		Principal: principal, IdempotencyKey: "start-process-current-tuple",
		Name: "Process", PlaybookRef: "playbook:test", PolicyRevision: 1,
		RootTriggerRef: "manual:test", RootSessionID: fixture.sessionID,
		RootTurnID: fixture.turnID, RootAttempt: 1, InputArtifactID: fixture.artifactID,
	})
	if err != nil {
		t.Fatalf("StartProcess: %v", err)
	}
	return process
}

type scheduledProducerPath struct {
	schedule       entity.Resource
	snapshotDigest string
	claim          ScheduleOccurrenceResult
	turn           entity.Resource
	process        entity.Resource
	runtime        entity.Resource
}

func (fixture currentTupleFixture) produceScheduledGraph(t *testing.T) scheduledProducerPath {
	return fixture.produceScheduledGraphWithMaximumAttempts(t, 3)
}

func (fixture currentTupleFixture) produceScheduledGraphWithMaximumAttempts(
	t *testing.T,
	maximumAttempts uint32,
) scheduledProducerPath {
	t.Helper()
	managePrincipal := fixture.principal(
		permissionManageSchedule, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
	)
	schedule, err := fixture.service.ManageSchedule(context.Background(), ManageScheduleInput{
		Principal: managePrincipal, IdempotencyKey: "create-schedule-current-digest",
		Action: "CREATE", Name: "Scheduled production path",
		Spec: entity.ScheduleSpec{
			TargetResourceID: fixture.roleID, Interval: time.Hour,
			Timezone: "UTC", Calendar: "GREGORIAN", OverlapPolicy: "FORBID",
			MisfirePolicy: "RUN_ONCE", NextRunAt: fixture.tx.now.Add(time.Minute),
			DeliveryPolicy: "AT_LEAST_ONCE", MaximumAttempts: maximumAttempts,
			InitialBackoff: time.Second, MaximumBackoff: time.Minute,
			DeadLetterAfter: time.Hour, PromptProfileID: fixture.promptID,
			PromptRevision: 1, SessionPolicy: "PERSISTENT",
			ExecutionSessionID: fixture.sessionID, NotificationPolicy: "AUDIT_ONLY",
			MaximumExecutionDuration: time.Minute, Coalesce: true,
			RuntimeRevisionID: fixture.revisionID, TargetType: "PLAYBOOK",
			PlaybookRef: "playbook:test", PlaybookVersion: 1,
			PromptArtifactID: fixture.artifactID,
			Ownership:        entity.ConfigurationOwnership{ManagedBy: "UI"},
		},
	})
	if err != nil {
		t.Fatalf("ManageSchedule CREATE: %v", err)
	}
	scheduleSpec := schedule.Spec.(entity.ScheduleSpec)
	fixture.tx.now = fixture.tx.now.Add(2 * time.Minute)
	schedulerPrincipal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, scheduleSpec.EffectiveInputSHA, fixture.grant,
	)
	due, err := fixture.service.ClaimDueSchedules(context.Background(), ClaimDueSchedulesInput{
		Principal: schedulerPrincipal, IdempotencyKey: "claim-due-current-digest", Limit: 1,
	})
	if err != nil || len(due.Occurrences) != 1 {
		t.Fatalf("ClaimDueSchedules: %v %+v", err, due)
	}
	snapshotDigest := due.Occurrences[0].EffectiveInputSHA256
	claimPrincipal := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, fixture.tx.resources[schedule.ID].Version,
		snapshotDigest, fixture.grant,
	)
	auditsBeforeClaim, eventsBeforeClaim := len(fixture.tx.audits), len(fixture.tx.events)
	claimed, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: claimPrincipal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if err != nil {
		t.Fatalf("ClaimScheduleOccurrence: %v", err)
	}
	if claimed.Occurrence.EffectiveInputSHA256 == snapshotDigest {
		t.Fatal("materialized execution digest was not separated from schedule snapshot")
	}
	auditsAfterClaim, eventsAfterClaim := len(fixture.tx.audits), len(fixture.tx.events)
	replayed, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: claimPrincipal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if err != nil || replayed != claimed {
		t.Fatalf("ClaimScheduleOccurrence replay: %v %+v", err, replayed)
	}
	if len(fixture.tx.audits) == auditsBeforeClaim || len(fixture.tx.events) == eventsBeforeClaim {
		t.Fatal("initial scheduler claim did not persist its graph records")
	}
	if len(fixture.tx.audits) != auditsAfterClaim || len(fixture.tx.events) != eventsAfterClaim {
		t.Fatal("scheduler replay repeated audit/outbox effects")
	}
	turn := fixture.tx.resources[claimed.Occurrence.ExecutionTurnID]
	process := fixture.tx.resources[claimed.Occurrence.ExecutionProcessRunID]
	runtimeRevision := fixture.tx.resources[claimed.Occurrence.ExecutionRuntimeRevisionID]
	turnSpec := turn.Spec.(entity.TurnSpec)
	run := fixture.tx.runs[turnAttemptMapKey(claimed.Occurrence.ID, claimed.Occurrence.Attempt)]
	if turnSpec.EffectiveInputSHA256 != claimed.Occurrence.EffectiveInputSHA256 ||
		run.CurrentInputSHA256 != turnSpec.EffectiveInputSHA256 ||
		run.EffectiveInputSHA256 != snapshotDigest {
		t.Fatalf("scheduled digest provenance is inconsistent: turn=%s occurrence=%s run=%+v snapshot=%s",
			turnSpec.EffectiveInputSHA256, claimed.Occurrence.EffectiveInputSHA256,
			run, snapshotDigest)
	}
	return scheduledProducerPath{
		schedule: schedule, snapshotDigest: snapshotDigest, claim: claimed,
		turn: turn, process: process, runtime: runtimeRevision,
	}
}

func (fixture currentTupleFixture) failScheduledTurn(
	t *testing.T,
	produced scheduledProducerPath,
	suffix string,
) ClaimTurnResult {
	t.Helper()
	turn := fixture.tx.resources[produced.turn.ID]
	turnSpec := turn.Spec.(entity.TurnSpec)
	principal := fixture.principalFor(
		permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
		turn.ID, uint64(turnSpec.Attempt), turnSpec.EffectiveInputSHA256, fixture.grant,
	)
	claim, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: principal, IdempotencyKey: "claim-scheduled-failure-" + suffix,
	})
	if err != nil {
		t.Fatalf("ClaimTurn before scheduled failure: %v", err)
	}
	principal.Permission = permissionCompleteTurn
	completed, err := fixture.service.CompleteTurn(context.Background(), CompleteTurnInput{
		Principal: principal, IdempotencyKey: "complete-scheduled-failure-" + suffix,
		TurnID: claim.Turn.ID, LeaseToken: claim.LeaseToken,
		ExpectedVersion: claim.Turn.Version, TerminalState: enum.StateFailed,
		Outcome: "execution_failed", Attempt: claim.Attempt,
		AuthorityGeneration: claim.AuthorityGeneration,
	})
	if err != nil || completed.State != enum.StateFailed {
		t.Fatalf("CompleteTurn FAILED: %v %+v", err, completed)
	}
	process := fixture.tx.resources[produced.process.ID]
	if process.State != enum.StateFailed {
		t.Fatalf("runner terminal did not close ProcessRun: %+v", process)
	}
	return claim
}

func (fixture currentTupleFixture) completeScheduledOccurrence(
	t *testing.T,
	produced scheduledProducerPath,
	suffix string,
) domainrepo.ScheduleOccurrence {
	t.Helper()
	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	principal := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	completed, err := fixture.service.CompleteScheduleOccurrence(
		context.Background(), CompleteScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "complete-occurrence-" + suffix,
			OccurrenceID:    produced.claim.Occurrence.ID,
			LeaseToken:      produced.claim.LeaseToken,
			ExpectedAttempt: produced.claim.Occurrence.Attempt,
		},
	)
	if err != nil {
		t.Fatalf("CompleteScheduleOccurrence: %v", err)
	}
	return completed
}

func (fixture currentTupleFixture) manageScheduleAction(
	action, scheduleID, idempotencyKey string,
) (entity.Resource, error) {
	current := fixture.tx.resources[scheduleID]
	return fixture.service.ManageSchedule(context.Background(), ManageScheduleInput{
		Principal: fixture.principal(
			permissionManageSchedule, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
		),
		IdempotencyKey: idempotencyKey, Action: action,
		ScheduleID: scheduleID, ExpectedVersion: current.Version,
	})
}

func (fixture currentTupleFixture) createNextDueOccurrence(
	t *testing.T,
	produced scheduledProducerPath,
	suffix string,
) domainrepo.ScheduleOccurrence {
	t.Helper()
	original := fixture.tx.resources[produced.schedule.ID]
	spec := original.Spec.(entity.ScheduleSpec)
	spec.NextRunAt = fixture.tx.now.Add(time.Minute)
	second, err := fixture.service.ManageSchedule(context.Background(), ManageScheduleInput{
		Principal: fixture.principal(
			permissionManageSchedule, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
		),
		IdempotencyKey: "create-watchdog-side-schedule-" + suffix,
		Action:         "CREATE", Name: "Watchdog transaction side schedule", Spec: spec,
	})
	if err != nil {
		t.Fatalf("ManageSchedule side schedule: %v", err)
	}
	secondSpec := second.Spec.(entity.ScheduleSpec)
	fixture.tx.now = secondSpec.NextRunAt.Add(time.Microsecond)
	principal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		second.ID, second.Version, secondSpec.EffectiveInputSHA, fixture.grant,
	)
	result, err := fixture.service.ClaimDueSchedules(context.Background(), ClaimDueSchedulesInput{
		Principal: principal, IdempotencyKey: "claim-next-due-" + suffix, Limit: 10,
	})
	if err != nil || len(result.Occurrences) == 0 {
		t.Fatalf("ClaimDueSchedules next occurrence: %v %+v", err, result)
	}
	for _, occurrence := range result.Occurrences {
		if occurrence.ScheduleID == second.ID {
			return fixture.tx.occurrences[occurrence.OccurrenceID]
		}
	}
	t.Fatalf("ClaimDueSchedules did not create side occurrence: %+v", result)
	return domainrepo.ScheduleOccurrence{}
}

func (fixture currentTupleFixture) addQueuedOccurrenceForSchedule(
	t *testing.T,
	produced scheduledProducerPath,
	overlapPolicy string,
) domainrepo.ScheduleOccurrence {
	t.Helper()
	queued := produced.claim.Occurrence
	queued.ID = uuid.NewString()
	queued.ScheduledFor = fixture.tx.now
	queued.EffectiveInputSHA256 = produced.snapshotDigest
	queued.State = "QUEUED"
	queued.Attempt = 1
	queued.OverlapPolicy = overlapPolicy
	queued.ClaimantWorkloadID = ""
	queued.AuthorityGeneration = 0
	queued.TokenHash = ""
	queued.ClaimKeySHA256 = ""
	queued.LeaseExpiresAt = time.Time{}
	queued.AvailableAt = fixture.tx.now
	queued.Outcome = ""
	queued.ResultArtifactID = ""
	clearScheduledExecutionBinding(&queued)
	queued.CreatedAt = fixture.tx.now
	queued.UpdatedAt = fixture.tx.now
	fixture.tx.occurrences[queued.ID] = queued
	return queued
}

func (fixture currentTupleFixture) installScheduledProcess(t *testing.T) entity.Resource {
	t.Helper()
	turn := fixture.tx.resources[fixture.turnID]
	turnSpec := turn.Spec.(entity.TurnSpec)
	processID, scheduleID, occurrenceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	turnSpec.ProcessRunID = processID
	boundTurn, err := turn.Update(turn.Name, turnSpec, fixture.tx.now)
	if err != nil {
		t.Fatalf("bind scheduled turn: %v", err)
	}
	fixture.tx.resources[turn.ID] = boundTurn
	process, err := entity.New(
		processID, fixture.organization, fixture.project, scheduleID, fixture.actor,
		enum.KindProcessRun, "Scheduled process",
		entity.ProcessRunSpec{
			PlaybookRef: "playbook:test", PolicyRevision: 1,
			RootTriggerRef:       "schedule-occurrence:" + occurrenceID,
			RootInitiatorActorID: fixture.actor,
			RootSessionID:        fixture.sessionID,
			RootSessionVersion:   fixture.tx.resources[fixture.sessionID].Version,
			RootTurnID:           boundTurn.ID, RootTurnVersion: boundTurn.Version, RootAttempt: 1,
			ImmutableInputSHA256: fixture.inputSHA256,
			RuntimeRevisionID:    fixture.revisionID,
			ScheduleID:           scheduleID, OccurrenceID: occurrenceID,
			CurrentSessionID:      fixture.sessionID,
			CurrentSessionVersion: fixture.tx.resources[fixture.sessionID].Version,
			CurrentTurnID:         boundTurn.ID, CurrentTurnVersion: boundTurn.Version,
			CurrentAttempt: 1, CurrentRuntimeRevisionID: fixture.revisionID,
			CurrentRuntimeRevisionVersion: fixture.tx.resources[fixture.revisionID].Version,
			CurrentInputSHA256:            fixture.inputSHA256,
		}, fixture.tx.now,
	)
	if err != nil {
		t.Fatalf("create scheduled process: %v", err)
	}
	fixture.tx.resources[process.ID] = process
	fixture.tx.resources[scheduleID] = entity.Resource{
		ID: scheduleID, OrganizationID: fixture.organization, ProjectID: fixture.project,
		OwnerActorID: fixture.actor, Kind: enum.KindSchedule, Name: "Schedule",
		State: enum.StateActive, Version: 1, CreatedAt: fixture.tx.now,
		UpdatedAt: fixture.tx.now,
	}
	occurrence := domainrepo.ScheduleOccurrence{
		ID: occurrenceID, ScheduleID: scheduleID,
		OrganizationID: fixture.organization, ProjectID: fixture.project,
		State: "CLAIMED", Attempt: 1, ClaimantWorkloadID: "scheduler",
		AuthorityGeneration: 1, TokenHash: hashString("schedule-token"),
		LeaseExpiresAt:          fixture.tx.now.Add(time.Minute),
		ExecutionSessionID:      fixture.sessionID,
		ExecutionSessionVersion: fixture.tx.resources[fixture.sessionID].Version,
		ExecutionTurnID:         boundTurn.ID, ExecutionTurnVersion: boundTurn.Version,
		ExecutionProcessRunID: process.ID, ExecutionProcessVersion: process.Version,
		ExecutionRuntimeRevisionID:      fixture.revisionID,
		ExecutionRuntimeRevisionVersion: fixture.tx.resources[fixture.revisionID].Version,
		EffectiveInputSHA256:            fixture.inputSHA256,
		CreatedAt:                       fixture.tx.now, UpdatedAt: fixture.tx.now,
	}
	fixture.tx.occurrences[occurrence.ID] = occurrence
	fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)] = domainrepo.ScheduledRun{
		OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
		SessionID:      occurrence.ExecutionSessionID,
		SessionVersion: occurrence.ExecutionSessionVersion,
		TurnID:         occurrence.ExecutionTurnID, TurnVersion: occurrence.ExecutionTurnVersion,
		ProcessRunID: process.ID, ProcessVersion: process.Version,
		RuntimeRevisionID:      fixture.revisionID,
		RuntimeRevisionVersion: occurrence.ExecutionRuntimeRevisionVersion,
		EffectiveInputSHA256:   fixture.inputSHA256, State: "CLAIMED",
		CurrentSessionID:      occurrence.ExecutionSessionID,
		CurrentSessionVersion: occurrence.ExecutionSessionVersion,
		CurrentTurnID:         occurrence.ExecutionTurnID,
		CurrentTurnVersion:    occurrence.ExecutionTurnVersion,
		CurrentTurnAttempt:    1, CurrentProcessRunID: process.ID,
		CurrentProcessVersion:         process.Version,
		CurrentRuntimeRevisionID:      fixture.revisionID,
		CurrentRuntimeRevisionVersion: occurrence.ExecutionRuntimeRevisionVersion,
		CurrentInputSHA256:            fixture.inputSHA256, CreatedAt: fixture.tx.now,
	}
	return process
}

func (fixture currentTupleFixture) claimAndCreateRuntime(
	t *testing.T,
	process entity.Resource,
) (ClaimTurnResult, RuntimeExecution) {
	t.Helper()
	claimPrincipal := fixture.principal(
		permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
	)
	graph, err := fixture.service.lockOwnerGraphByTurn(
		context.Background(), fixture.tx, claimPrincipal, fixture.turnID,
	)
	if err != nil {
		t.Fatalf("pre-claim owner graph: %v", err)
	}
	if err := requireCurrentTurnBinding(graph); err != nil {
		t.Fatalf("pre-claim current tuple: %v", err)
	}
	claim, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: claimPrincipal, IdempotencyKey: fixture.claimKey,
	})
	if err != nil {
		t.Fatalf("ClaimTurn: %v", err)
	}
	persistedProcess := fixture.tx.resources[process.ID]
	processSpec := persistedProcess.Spec.(entity.ProcessRunSpec)
	if processSpec.CurrentTurnVersion != claim.Turn.Version {
		t.Fatalf("ProcessRun current Turn version = %d, Turn = %d",
			processSpec.CurrentTurnVersion, claim.Turn.Version)
	}
	if persistedProcess.Version != process.Version+1 {
		t.Fatalf("ProcessRun version = %d, want %d", persistedProcess.Version, process.Version+1)
	}
	attempt := fixture.tx.attempts[turnAttemptMapKey(fixture.turnID, 1)]
	if attempt.WorkloadID != agentRunnerWorkload || attempt.LeaseFence != claim.Turn.Version {
		t.Fatalf("claim authority was not bound to agent-runner/new fence: %+v", attempt)
	}

	replayed, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: claimPrincipal, IdempotencyKey: fixture.claimKey,
	})
	if err != nil {
		t.Fatalf("ClaimTurn replay: %v", err)
	}
	if replayed.Turn.Version != claim.Turn.Version ||
		fixture.tx.resources[process.ID].Version != persistedProcess.Version {
		t.Fatal("ClaimTurn replay created another graph version bump")
	}

	runtimePrincipal := fixture.principal(
		permissionRuntimeClaim, fixture.runtimeWorker, fixture.runtimeSPIFFE,
	)
	if _, resolveErr := fixture.service.resolveBoundExecution(
		context.Background(), fixture.tx, runtimePrincipal,
	); resolveErr != nil {
		turn := fixture.tx.resources[fixture.turnID]
		attempt := fixture.tx.attempts[turnAttemptMapKey(fixture.turnID, 1)]
		lease := fixture.tx.leases[fixture.turnID]
		t.Fatalf("resolve runtime graph before claim: %v; turn=%+v attempt=%+v lease=%+v", resolveErr, turn, attempt, lease)
	}
	execution, err := fixture.service.ClaimRuntimeExecution(
		context.Background(), runtimePrincipal, "claim-runtime-current-tuple",
	)
	if err != nil {
		t.Fatalf("ClaimRuntimeExecution after real ClaimTurn: %v", err)
	}
	if execution.WorkloadID != fixture.runtimeWorker ||
		execution.WorkloadSPIFFEID != fixture.runtimeSPIFFE || execution.State != "PENDING" {
		t.Fatalf("runtime authority is not executor-owned: %+v", execution)
	}
	return claim, execution
}

func TestProductionClaimTurnPropagatesUnscheduledCurrentTuple(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	process := fixture.startRootProcess(t)
	claim, execution := fixture.claimAndCreateRuntime(t, process)
	if execution.TurnID != claim.Turn.ID || execution.ProcessID != process.ID {
		t.Fatalf("runtime execution does not use claimed graph: %+v", execution)
	}
	recovered, err := fixture.service.ClaimRuntimeExecution(
		context.Background(),
		fixture.principal(permissionRuntimeClaim, fixture.runtimeWorker, fixture.runtimeSPIFFE),
		"claim-runtime-after-controller-crash",
	)
	if err != nil {
		t.Fatalf("ClaimRuntimeExecution durable PENDING recovery: %v", err)
	}
	if recovered != execution || len(fixture.tx.runtimes) != 1 {
		t.Fatalf("durable PENDING recovery created a different execution: %+v", recovered)
	}

	runtimePrincipal := fixture.principal(
		permissionRuntimeAdmit, fixture.runtimeWorker, fixture.runtimeSPIFFE,
	)
	admitted, err := fixture.service.AdmitRuntimeExecution(context.Background(), RuntimeExecutionInput{
		Principal: runtimePrincipal, IdempotencyKey: "admit-runtime-current-tuple",
		ExecutionID: execution.ID, ExpectedVersion: execution.Version,
		ExpectedFence:           execution.Fence,
		ExpectedGrantGeneration: fixture.grant,
	})
	if err != nil {
		t.Fatalf("AdmitRuntimeExecution after real ClaimTurn: %v", err)
	}
	if admitted.Execution.State != "ADMITTED" || len(admitted.LeaseToken) != 64 {
		t.Fatalf("runtime admission is incomplete: %+v", admitted.Execution)
	}

	resolved := resolvedExecution{
		Turn:        fixture.tx.resources[fixture.turnID],
		TurnSpec:    fixture.tx.resources[fixture.turnID].Spec.(entity.TurnSpec),
		Session:     fixture.tx.resources[fixture.sessionID],
		Process:     fixture.tx.resources[process.ID],
		ProcessSpec: fixture.tx.resources[process.ID].Spec.(entity.ProcessRunSpec),
		Revision:    fixture.tx.resources[fixture.revisionID],
	}
	suspendedSession, err := resolved.Session.Transition(enum.StateWaitingExternal, fixture.tx.now)
	if err != nil {
		t.Fatalf("prepare suspended Session: %v", err)
	}
	suspendedTurn, err := resolved.Turn.Transition(enum.StateWaitingExternal, fixture.tx.now)
	if err != nil {
		t.Fatalf("prepare suspended Turn: %v", err)
	}
	suspendedProcess, err := suspendIntegrationProcessRun(
		resolved, suspendedSession, suspendedTurn, fixture.tx.now,
	)
	if err != nil {
		t.Fatalf("integration suspension tuple became unreachable: %v", err)
	}
	suspendedProcessSpec := suspendedProcess.Spec.(entity.ProcessRunSpec)
	if suspendedProcessSpec.CurrentSessionVersion != suspendedSession.Version ||
		suspendedProcessSpec.CurrentTurnVersion != suspendedTurn.Version {
		t.Fatal("integration suspension did not retain the propagated current tuple")
	}

	continuationID := uuid.NewString()
	requestSHA256 := hashString("integration-request-after-real-claim")
	suspendedRuntime := admitted.Execution
	suspendedRuntime.State = "SUSPENDED"
	suspendedRuntime.TerminalOutcome = "SUSPENDED"
	suspendedRuntime.TerminalReference = continuationID
	suspendedRuntime.TerminalSHA256 = requestSHA256
	suspendedRuntime.LeaseID = ""
	suspendedRuntime.LeaseTokenSHA256 = ""
	suspendedRuntime.LeaseExpiresAt = time.Time{}
	continuation := IntegrationContinuation{
		ID: continuationID, OrganizationID: fixture.organization,
		ProjectID: fixture.project, ProcessID: process.ID,
		SessionID: fixture.sessionID, ThreadID: suspendedRuntime.ThreadID,
		RoleID: suspendedRuntime.RoleID, TurnID: fixture.turnID,
		TurnVersion: suspendedTurn.Version, Attempt: suspendedRuntime.Attempt,
		RuntimeRevisionID:      suspendedRuntime.RuntimeRevisionID,
		RuntimeRevisionVersion: suspendedRuntime.RuntimeRevisionVersion,
		RuntimeRevisionSHA256:  suspendedRuntime.RuntimeRevisionSHA256,
		ImmutableInputSHA256:   suspendedRuntime.ImmutableInputSHA256,
		GrantGeneration:        suspendedRuntime.GrantGeneration,
		RequestSHA256:          requestSHA256, ContinuationState: "SUSPENDED",
	}
	replaced, err := replaceIntegrationPredecessor(
		continuation, suspendedRuntime, suspendedTurn, domainrepo.TurnAttempt{
			TurnID: fixture.turnID, Attempt: suspendedRuntime.Attempt,
			WorkloadID:          agentRunnerWorkload,
			AuthorityGeneration: suspendedRuntime.GrantGeneration,
			State:               "WAITING_EXTERNAL", InputSHA256: suspendedRuntime.ImmutableInputSHA256,
			LeaseFence: claim.Turn.Version, StartedAt: fixture.tx.now.Add(-time.Second),
			FinishedAt: fixture.tx.now, Outcome: "integration_approval",
		}, agentRunnerWorkload, fixture.runtimeWorker, fixture.runtimeSPIFFE,
		fixture.tx.now,
	)
	if err != nil || replaced.State != enum.StateCancelled ||
		replaced.Version != suspendedTurn.Version+1 {
		t.Fatalf("integration predecessor replacement became unreachable: %v %+v", err, replaced)
	}
}

func TestResolveRuntimeAgentBindingIntentPrecedesFirstClaim(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	principal := fixture.principal(
		permissionRuntimeAgentBind,
		"bot-service",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/bot-service",
	)
	intent, err := fixture.service.ResolveRuntimeAgentBindingIntent(
		t.Context(), principal, "test:turn",
	)
	if err != nil {
		t.Fatalf("resolve first bot turn owner tuple: %v", err)
	}
	if intent.SessionID != fixture.sessionID || intent.TurnID != fixture.turnID ||
		intent.RuntimeRevisionID != fixture.revisionID || intent.Attempt != 1 ||
		intent.InputSHA256 != fixture.inputSHA256 || intent.RuntimeRevisionSHA256 == "" {
		t.Fatalf("resolved intent lost exact owner tuple: %+v", intent)
	}
}

func TestProductionClaimTurnPropagatesScheduledCurrentTuple(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	process := fixture.installScheduledProcess(t)
	claim, execution := fixture.claimAndCreateRuntime(t, process)
	process = fixture.tx.resources[process.ID]
	processSpec := process.Spec.(entity.ProcessRunSpec)
	occurrence := fixture.tx.occurrences[processSpec.OccurrenceID]
	run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
	if occurrence.ExecutionTurnVersion != claim.Turn.Version ||
		run.CurrentTurnVersion != claim.Turn.Version ||
		occurrence.ExecutionProcessVersion != process.Version ||
		run.CurrentProcessVersion != process.Version {
		t.Fatalf("scheduled current tuple was not propagated: occurrence=%+v run=%+v",
			occurrence, run)
	}
	if execution.TurnID != claim.Turn.ID || execution.ProcessID != process.ID {
		t.Fatalf("scheduled runtime execution does not use claimed graph: %+v", execution)
	}
	runtimePrincipal := fixture.principal(
		permissionRuntimeAdmit, fixture.runtimeWorker, fixture.runtimeSPIFFE,
	)
	admitted, err := fixture.service.AdmitRuntimeExecution(context.Background(), RuntimeExecutionInput{
		Principal: runtimePrincipal, IdempotencyKey: "admit-scheduled-current-tuple",
		ExecutionID: execution.ID, ExpectedVersion: execution.Version,
		ExpectedFence: execution.Fence, ExpectedGrantGeneration: fixture.grant,
	})
	if err != nil || admitted.Execution.State != "ADMITTED" {
		t.Fatalf("scheduled runtime admission after real ClaimTurn: %v %+v", err, admitted)
	}
}

func TestScheduledProducerClaimTurnRuntimePathPreservesDigestAndOutboxSemantics(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	turnSpec := produced.turn.Spec.(entity.TurnSpec)
	claimPrincipal := fixture.principalFor(
		permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
		produced.turn.ID, uint64(turnSpec.Attempt),
		turnSpec.EffectiveInputSHA256, fixture.grant,
	)
	auditsBeforeClaim, eventsBeforeClaim := len(fixture.tx.audits), len(fixture.tx.events)
	claimed, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: claimPrincipal, IdempotencyKey: "claim-produced-scheduled-turn",
	})
	if err != nil {
		t.Fatalf("ClaimTurn after scheduled producers: %v", err)
	}
	auditsAfterClaim, eventsAfterClaim := len(fixture.tx.audits), len(fixture.tx.events)
	if auditsAfterClaim <= auditsBeforeClaim || eventsAfterClaim <= eventsBeforeClaim {
		t.Fatal("ClaimTurn did not atomically persist graph records")
	}
	replayed, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: claimPrincipal, IdempotencyKey: "claim-produced-scheduled-turn",
	})
	if err != nil || replayed.Turn.Version != claimed.Turn.Version {
		t.Fatalf("scheduled ClaimTurn replay: %v %+v", err, replayed)
	}
	if len(fixture.tx.audits) != auditsAfterClaim || len(fixture.tx.events) != eventsAfterClaim {
		t.Fatal("scheduled ClaimTurn replay repeated graph effects")
	}

	process := fixture.tx.resources[produced.process.ID]
	processSpec := process.Spec.(entity.ProcessRunSpec)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
	if processSpec.CurrentTurnVersion != claimed.Turn.Version ||
		occurrence.ExecutionTurnVersion != claimed.Turn.Version ||
		run.CurrentTurnVersion != claimed.Turn.Version ||
		occurrence.ExecutionProcessVersion != process.Version ||
		run.CurrentProcessVersion != process.Version ||
		occurrence.EffectiveInputSHA256 != turnSpec.EffectiveInputSHA256 ||
		run.CurrentInputSHA256 != turnSpec.EffectiveInputSHA256 ||
		run.EffectiveInputSHA256 != produced.snapshotDigest {
		t.Fatalf("scheduled current tuple/digest was not propagated: process=%+v occurrence=%+v run=%+v",
			processSpec, occurrence, run)
	}

	scheduleEvents := 0
	seen := make(map[string]struct{}, len(fixture.tx.events))
	for _, change := range fixture.tx.events {
		key := string(change.ResourceKind) + "\x00" + change.ResourceID + "\x00" +
			strconv.FormatUint(change.EventSequence, 10)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate outbox sequence: %s", key)
		}
		seen[key] = struct{}{}
		if change.EventSequence != change.ResourceVersion {
			t.Fatalf("outbox sequence %d differs from resource version %d: %+v",
				change.EventSequence, change.ResourceVersion, change)
		}
		if change.ResourceKind == enum.KindSchedule && change.ResourceID == produced.schedule.ID {
			scheduleEvents++
		}
	}
	if scheduleEvents != 2 {
		t.Fatalf("unchanged Schedule received an outbox event; got %d, want create+due only", scheduleEvents)
	}

	runtimePrincipal := fixture.principalFor(
		permissionRuntimeClaim, fixture.runtimeWorker, fixture.runtimeSPIFFE,
		produced.turn.ID, uint64(turnSpec.Attempt),
		turnSpec.EffectiveInputSHA256, fixture.grant,
	)
	execution, err := fixture.service.ClaimRuntimeExecution(
		context.Background(), runtimePrincipal, "claim-produced-scheduled-runtime",
	)
	if err != nil {
		t.Fatalf("ClaimRuntimeExecution after scheduled ClaimTurn: %v", err)
	}
	if execution.WorkloadID != fixture.runtimeWorker ||
		execution.WorkloadSPIFFEID != fixture.runtimeSPIFFE {
		t.Fatalf("runtime authority does not belong to exact executor: %+v", execution)
	}
	admitPrincipal := fixture.principalFor(
		permissionRuntimeAdmit, fixture.runtimeWorker, fixture.runtimeSPIFFE,
		produced.turn.ID, uint64(turnSpec.Attempt),
		turnSpec.EffectiveInputSHA256, fixture.grant,
	)
	admitted, err := fixture.service.AdmitRuntimeExecution(
		context.Background(), RuntimeExecutionInput{
			Principal: admitPrincipal, IdempotencyKey: "admit-produced-scheduled-runtime",
			ExecutionID: execution.ID, ExpectedVersion: execution.Version,
			ExpectedFence: execution.Fence, ExpectedGrantGeneration: fixture.grant,
		},
	)
	if err != nil || admitted.Execution.State != "ADMITTED" {
		t.Fatalf("AdmitRuntimeExecution after scheduled producers: %v %+v", err, admitted)
	}
}

func TestScheduleArchiveRejectsOpenGraphAndPausedRetryWaitsForResume(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	scheduleID := produced.schedule.ID

	if _, err := fixture.manageScheduleAction(
		"ARCHIVE", scheduleID, "archive-claimed-occurrence",
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("ARCHIVE with CLAIMED occurrence returned %v", err)
	}
	if _, exists := fixture.tx.receipts[receiptMapKey(
		"manage_schedule_ARCHIVE", hashString("archive-claimed-occurrence"),
	)]; exists {
		t.Fatal("rejected ARCHIVE persisted a receipt")
	}

	fixture.failScheduledTurn(t, produced, "archive-race")
	if _, err := fixture.manageScheduleAction(
		"PAUSE", scheduleID, "pause-before-requeue",
	); err != nil {
		t.Fatalf("PAUSE with terminal runner graph: %v", err)
	}
	completed := fixture.completeScheduledOccurrence(t, produced, "archive-race")
	if completed.State != "QUEUED" || completed.Attempt != 2 ||
		completed.EffectiveInputSHA256 != produced.snapshotDigest ||
		occurrenceHasExecutionBinding(completed) {
		t.Fatalf("paused retry was not requeued as a closed snapshot: %+v", completed)
	}
	audits, events := len(fixture.tx.audits), len(fixture.tx.events)
	replayed := fixture.completeScheduledOccurrence(t, produced, "archive-race")
	if replayed != completed || len(fixture.tx.audits) != audits ||
		len(fixture.tx.events) != events {
		t.Fatal("exact completion replay repeated retry disposition effects")
	}
	if _, err := fixture.manageScheduleAction(
		"ARCHIVE", scheduleID, "archive-queued-occurrence",
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("ARCHIVE with QUEUED retry returned %v", err)
	}

	fixture.tx.now = completed.AvailableAt
	pausedSchedule := fixture.tx.resources[scheduleID]
	pausedSpec := pausedSchedule.Spec.(entity.ScheduleSpec)
	scheduler := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		pausedSchedule.ID, pausedSchedule.Version,
		pausedSpec.EffectiveInputSHA, fixture.grant,
	)
	if result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: scheduler, IdempotencyKey: "claim-paused-retry",
		},
	); !errors.Is(err, errs.ErrStateConflict) || result.LeaseToken != "" {
		t.Fatalf("paused Schedule exposed queued retry: %v %+v", err, result)
	}
	if _, err := fixture.manageScheduleAction(
		"ACTIVATE", scheduleID, "resume-queued-retry",
	); err != nil {
		t.Fatalf("ACTIVATE queued retry: %v", err)
	}
	activeSchedule := fixture.tx.resources[scheduleID]
	activeSpec := activeSchedule.Spec.(entity.ScheduleSpec)
	scheduler = fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		activeSchedule.ID, activeSchedule.Version,
		activeSpec.EffectiveInputSHA, fixture.grant,
	)
	next, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: scheduler, IdempotencyKey: "claim-resumed-retry",
		},
	)
	if err != nil || next.Occurrence.ID != completed.ID || next.Occurrence.Attempt != 2 {
		t.Fatalf("resumed retry was not claimable: %v %+v", err, next)
	}
}

func TestScheduleArchiveSucceedsOnlyAfterTerminalGraph(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraphWithMaximumAttempts(t, 1)
	fixture.failScheduledTurn(t, produced, "archive-terminal")
	completed := fixture.completeScheduledOccurrence(t, produced, "archive-terminal")
	if completed.State != "DEAD_LETTER" || completed.Attempt != 1 {
		t.Fatalf("retry limit did not dead-letter occurrence: %+v", completed)
	}
	open, err := fixture.tx.HasOpenScheduleOccurrence(
		context.Background(), fixture.organization, fixture.project, produced.schedule.ID,
	)
	if err != nil || open {
		t.Fatalf("terminal graph remained open: open=%t err=%v", open, err)
	}
	archived, err := fixture.manageScheduleAction(
		"ARCHIVE", produced.schedule.ID, "archive-terminal-graph",
	)
	if err != nil || archived.State != enum.StateArchived {
		t.Fatalf("ARCHIVE after terminal graph: %v %+v", err, archived)
	}
	deletionPending, err := fixture.manageScheduleAction(
		"DELETE", produced.schedule.ID, "delete-terminal-graph",
	)
	if err != nil || deletionPending.State != enum.StateDeletionPending {
		t.Fatalf("DELETE after terminal archive: %v %+v", err, deletionPending)
	}
}

func TestScheduleRecoveryCommitsBeforeNoNextCandidate(t *testing.T) {
	for _, scenario := range []struct {
		name               string
		maximumAttempts    uint32
		terminalTurn       bool
		expectedState      string
		expectedAttempt    uint32
		expectedRunOutcome string
	}{
		{
			name: "expired live graph requeues", maximumAttempts: 3,
			expectedState: "QUEUED", expectedAttempt: 2,
			expectedRunOutcome: "scheduler_lease_expired",
		},
		{
			name: "terminal winner reaches dead letter", maximumAttempts: 1,
			terminalTurn: true, expectedState: "DEAD_LETTER", expectedAttempt: 1,
			expectedRunOutcome: "execution_failed",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newCurrentTupleFixture(t)
			produced := fixture.produceScheduledGraphWithMaximumAttempts(
				t, scenario.maximumAttempts,
			)
			if scenario.terminalTurn {
				fixture.failScheduledTurn(t, produced, "no-next-terminal")
			}
			previous := fixture.tx.occurrences[produced.claim.Occurrence.ID]
			fixture.tx.now = previous.LeaseExpiresAt.Add(time.Microsecond)
			schedule := fixture.tx.resources[produced.schedule.ID]
			spec := schedule.Spec.(entity.ScheduleSpec)
			auditsBefore := len(fixture.tx.audits)
			result, err := fixture.service.ClaimScheduleOccurrence(
				context.Background(), ClaimScheduleOccurrenceInput{
					Principal: fixture.principalFor(
						permissionExecuteSchedule, "scheduler",
						"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
						schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
					),
					IdempotencyKey: "claim-after-no-next-recovery",
				},
			)
			if !errors.Is(err, errs.ErrNotFound) || result.LeaseToken != "" {
				t.Fatalf("no-next poll returned %v %+v", err, result)
			}
			recovered := fixture.tx.occurrences[previous.ID]
			finishedRun := fixture.tx.runs[turnAttemptMapKey(previous.ID, previous.Attempt)]
			if recovered.State != scenario.expectedState ||
				recovered.Attempt != scenario.expectedAttempt ||
				recovered.TokenHash != "" || recovered.ClaimKeySHA256 != "" ||
				recovered.AuthorityGeneration != 0 ||
				!recovered.LeaseExpiresAt.IsZero() ||
				finishedRun.State != "FAILED" ||
				finishedRun.Outcome != scenario.expectedRunOutcome ||
				finishedRun.FinishedAt.IsZero() || len(fixture.tx.audits) <= auditsBefore {
				t.Fatalf("recovery was rolled back by no-next: occurrence=%+v run=%+v",
					recovered, finishedRun)
			}
			if recovered.State == "QUEUED" &&
				(recovered.EffectiveInputSHA256 != produced.snapshotDigest ||
					occurrenceHasExecutionBinding(recovered)) {
				t.Fatalf("queued recovery did not close the old graph: %+v", recovered)
			}
			stale, staleErr := fixture.service.ClaimScheduleOccurrence(
				context.Background(), ClaimScheduleOccurrenceInput{
					Principal: fixture.principalFor(
						permissionExecuteSchedule, "scheduler",
						"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
						schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
					),
					IdempotencyKey: "claim-occurrence-current-digest",
				},
			)
			if !errors.Is(staleErr, errs.ErrStateConflict) || stale.LeaseToken != "" {
				t.Fatalf("recovery exposed stale scheduler authority: %v %+v", staleErr, stale)
			}
		})
	}
}

func TestSkipOverlapCommitsBeforeNoNextCandidate(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	skippedCandidate := fixture.addQueuedOccurrenceForSchedule(t, produced, "SKIP")
	auditsBefore := len(fixture.tx.audits)
	eventsBefore := len(fixture.tx.events)
	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: fixture.principalFor(
				permissionExecuteSchedule, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
			),
			IdempotencyKey: "claim-after-only-overlap-skip",
		},
	)
	if !errors.Is(err, errs.ErrNotFound) || result.LeaseToken != "" {
		t.Fatalf("skip-only poll returned %v %+v", err, result)
	}
	skipped := fixture.tx.occurrences[skippedCandidate.ID]
	if skipped.State != "SKIPPED" || skipped.Outcome != "overlap" ||
		len(fixture.tx.audits) != auditsBefore+1 ||
		len(fixture.tx.events) != eventsBefore ||
		fixture.tx.audits[len(fixture.tx.audits)-1].Action != "skip_schedule_occurrence" {
		t.Fatalf("skip mutation was rolled back by no-next: occurrence=%+v audits=%+v",
			skipped, fixture.tx.audits[auditsBefore:])
	}
	auditsAfter := len(fixture.tx.audits)
	if _, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: fixture.principalFor(
				permissionExecuteSchedule, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
			),
			IdempotencyKey: "repeat-after-only-overlap-skip",
		},
	); !errors.Is(err, errs.ErrNotFound) || len(fixture.tx.audits) != auditsAfter ||
		len(fixture.tx.events) != eventsBefore {
		t.Fatalf("repeat after committed skip created another effect: %v", err)
	}
}

func TestHistoricalOpenScheduledRunBlocksQueueMaterialization(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	fixture.failScheduledTurn(t, produced, "historical-open-run")
	historical := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	historical.State = "FAILED"
	historical.Outcome = "execution_failed"
	fixture.tx.occurrences[historical.ID] = historical
	candidate := fixture.addQueuedOccurrenceForSchedule(t, produced, "QUEUE")
	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	principal := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	resourcesBefore := len(fixture.tx.resources)
	auditsBefore := len(fixture.tx.audits)
	blocked, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-behind-historical-open-run",
		},
	)
	if !errors.Is(err, errs.ErrNotFound) || blocked.LeaseToken != "" ||
		len(fixture.tx.resources) != resourcesBefore || len(fixture.tx.audits) != auditsBefore {
		t.Fatalf("historical open run did not block materialization: %v %+v", err, blocked)
	}
	runKey := turnAttemptMapKey(historical.ID, historical.Attempt)
	run := fixture.tx.runs[runKey]
	run.State = "FAILED"
	run.Outcome = "execution_failed"
	run.FinishedAt = fixture.tx.now
	fixture.tx.runs[runKey] = run
	claimed, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-after-historical-run-close",
		},
	)
	if err != nil || claimed.Occurrence.ID != candidate.ID ||
		claimed.Occurrence.State != "CLAIMED" || claimed.LeaseToken == "" {
		t.Fatalf("closed historical run did not unblock materialization: %v %+v", err, claimed)
	}
}

func TestTerminalWinnerWatchdogRecoveryUsesCompletionRetryDisposition(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	fixture.failScheduledTurn(t, produced, "watchdog-terminal-winner")
	previous := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	previousRun := fixture.tx.runs[turnAttemptMapKey(previous.ID, previous.Attempt)]
	second := fixture.createNextDueOccurrence(t, produced, "watchdog-terminal-winner")

	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	scheduler := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	claimedSecond, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: scheduler, IdempotencyKey: "watchdog-recovers-terminal-winner",
		},
	)
	if err != nil || claimedSecond.Occurrence.ID != second.ID {
		t.Fatalf("watchdog recovery transaction: %v %+v", err, claimedSecond)
	}
	recovered := fixture.tx.occurrences[previous.ID]
	finishedRun := fixture.tx.runs[turnAttemptMapKey(previous.ID, previous.Attempt)]
	if recovered.State != "QUEUED" || recovered.Attempt != previous.Attempt+1 ||
		recovered.EffectiveInputSHA256 != produced.snapshotDigest ||
		occurrenceHasExecutionBinding(recovered) || recovered.TokenHash != "" ||
		finishedRun.State != "FAILED" || finishedRun.Outcome != "execution_failed" ||
		finishedRun.FinishedAt.IsZero() || previousRun.State != "CLAIMED" {
		t.Fatalf("terminal-winner recovery diverged from completion: occurrence=%+v run=%+v",
			recovered, finishedRun)
	}
	completeFirstFixture := newCurrentTupleFixture(t)
	completeFirstProduced := completeFirstFixture.produceScheduledGraph(t)
	completeFirstFixture.failScheduledTurn(t, completeFirstProduced, "complete-first-winner")
	completeFirst := completeFirstFixture.completeScheduledOccurrence(
		t, completeFirstProduced, "complete-first-winner",
	)
	completeFirstRun := completeFirstFixture.tx.runs[turnAttemptMapKey(
		completeFirst.ID, previous.Attempt,
	)]
	if completeFirst.State != recovered.State || completeFirst.Attempt != recovered.Attempt ||
		completeFirst.EffectiveInputSHA256 != completeFirstProduced.snapshotDigest ||
		recovered.EffectiveInputSHA256 != produced.snapshotDigest ||
		completeFirst.Outcome != recovered.Outcome || completeFirst.TokenHash != recovered.TokenHash ||
		completeFirst.ClaimKeySHA256 != recovered.ClaimKeySHA256 ||
		occurrenceHasExecutionBinding(completeFirst) != occurrenceHasExecutionBinding(recovered) ||
		completeFirstRun.State != finishedRun.State || completeFirstRun.Outcome != finishedRun.Outcome {
		t.Fatalf("complete-first and expiry-first did not converge: complete=%+v recovery=%+v",
			completeFirst, recovered)
	}

	staleSchedule := fixture.tx.resources[produced.schedule.ID]
	staleSpec := staleSchedule.Spec.(entity.ScheduleSpec)
	stalePrincipal := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		staleSchedule.ID, staleSchedule.Version,
		staleSpec.EffectiveInputSHA, fixture.grant,
	)
	stale, err := fixture.service.CompleteScheduleOccurrence(
		context.Background(), CompleteScheduleOccurrenceInput{
			Principal: stalePrincipal, IdempotencyKey: "stale-after-watchdog-winner",
			OccurrenceID: previous.ID, LeaseToken: produced.claim.LeaseToken,
			ExpectedAttempt: previous.Attempt,
		},
	)
	if !errors.Is(err, errs.ErrStateConflict) || stale.TokenHash != "" {
		t.Fatalf("stale scheduler token survived watchdog winner: %v %+v", err, stale)
	}

	if _, err := fixture.service.CancelScheduleOccurrence(
		context.Background(), CancelScheduleOccurrenceInput{
			Principal: fixture.principal(
				permissionManageSchedule, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
			),
			IdempotencyKey:  "cancel-watchdog-side-occurrence",
			OccurrenceID:    claimedSecond.Occurrence.ID,
			ExpectedAttempt: claimedSecond.Occurrence.Attempt,
			ReasonCode:      "test_cleanup",
		},
	); err != nil {
		t.Fatalf("CancelScheduleOccurrence side occurrence: %v", err)
	}
	fixture.tx.now = recovered.AvailableAt
	schedule = fixture.tx.resources[produced.schedule.ID]
	spec = schedule.Spec.(entity.ScheduleSpec)
	scheduler = fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	next, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: scheduler, IdempotencyKey: "claim-after-watchdog-retry",
		},
	)
	if err != nil || next.Occurrence.ID != previous.ID || next.Occurrence.Attempt != 2 ||
		next.Occurrence.EffectiveInputSHA256 == produced.snapshotDigest {
		t.Fatalf("watchdog retry did not rematerialize execution: %v %+v", err, next)
	}
	turn := fixture.tx.resources[next.Occurrence.ExecutionTurnID]
	turnSpec := turn.Spec.(entity.TurnSpec)
	claimedTurn, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: fixture.principalFor(
			permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
			turn.ID, uint64(turnSpec.Attempt), turnSpec.EffectiveInputSHA256, fixture.grant,
		),
		IdempotencyKey: "claim-turn-after-watchdog-retry",
	})
	if err != nil || claimedTurn.Turn.State != enum.StateClaimed {
		t.Fatalf("ClaimTurn after watchdog retry: %v %+v", err, claimedTurn)
	}
}

func TestTerminalWinnerWatchdogRecoveryHonorsRetryLimit(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraphWithMaximumAttempts(t, 1)
	fixture.failScheduledTurn(t, produced, "watchdog-dead-letter")
	previous := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	second := fixture.createNextDueOccurrence(t, produced, "watchdog-dead-letter")
	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: fixture.principalFor(
				permissionExecuteSchedule, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
			),
			IdempotencyKey: "watchdog-dead-letter-winner",
		},
	)
	if err != nil || result.Occurrence.ID != second.ID {
		t.Fatalf("watchdog dead-letter transaction: %v %+v", err, result)
	}
	deadLetter := fixture.tx.occurrences[previous.ID]
	run := fixture.tx.runs[turnAttemptMapKey(previous.ID, previous.Attempt)]
	if deadLetter.State != "DEAD_LETTER" || deadLetter.Attempt != previous.Attempt ||
		deadLetter.TokenHash != "" || deadLetter.ClaimKeySHA256 != "" ||
		run.State != "FAILED" || run.Outcome != "execution_failed" {
		t.Fatalf("watchdog retry limit left open authority: occurrence=%+v run=%+v",
			deadLetter, run)
	}
}

func TestScheduledRequeueRestoresSnapshotAndNextAttemptReachesClaimTurn(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		outcome string
	}{
		{name: "failed_completion", outcome: "execution_failed"},
		{name: "scheduler_lease_expiry", outcome: "scheduler_lease_expired"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newCurrentTupleFixture(t)
			produced := fixture.produceScheduledGraph(t)
			occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
			run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
			schedule := fixture.tx.resources[produced.schedule.ID]
			previousDigest := occurrence.EffectiveInputSHA256
			previousClaimKey := occurrence.ClaimKeySHA256
			previousAttempt := occurrence.Attempt
			availableAt := fixture.tx.now.Add(time.Second)
			if err := requeueScheduledOccurrence(
				&occurrence, schedule, run, previousAttempt+1, availableAt,
				scenario.outcome, fixture.tx.now,
			); err != nil {
				t.Fatalf("requeueScheduledOccurrence: %v", err)
			}
			if err := fixture.tx.UpdateScheduleOccurrence(
				context.Background(), occurrence, previousAttempt,
				produced.claim.Occurrence.TokenHash,
			); err != nil {
				t.Fatalf("persist requeued occurrence: %v", err)
			}
			if err := fixture.tx.FinishScheduledRun(
				context.Background(), domainrepo.ScheduledRun{
					OccurrenceID: occurrence.ID, Attempt: previousAttempt,
					State: "FAILED", Outcome: scenario.outcome, FinishedAt: fixture.tx.now,
				},
			); err != nil {
				t.Fatalf("persist terminal predecessor run: %v", err)
			}
			persisted := fixture.tx.occurrences[occurrence.ID]
			if persisted.State != "QUEUED" || persisted.Attempt != previousAttempt+1 ||
				persisted.EffectiveInputSHA256 != produced.snapshotDigest ||
				persisted.ClaimKeySHA256 != "" || occurrenceHasExecutionBinding(persisted) {
				t.Fatalf("requeue did not restore closed snapshot state: %+v", persisted)
			}
			if previousDigest == persisted.EffectiveInputSHA256 || previousClaimKey == "" {
				t.Fatal("test did not start from a materialized claimed occurrence")
			}

			scheduler := fixture.principalFor(
				permissionExecuteSchedule, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID, schedule.Version, produced.snapshotDigest, fixture.grant,
			)
			stale, err := fixture.service.ClaimScheduleOccurrence(
				context.Background(), ClaimScheduleOccurrenceInput{
					Principal: scheduler, IdempotencyKey: "claim-occurrence-current-digest",
				},
			)
			if !errors.Is(err, errs.ErrStateConflict) || stale.LeaseToken != "" {
				t.Fatalf("stale previous claim exposed authority: %v %+v", err, stale)
			}

			fixture.tx.now = availableAt
			next, err := fixture.service.ClaimScheduleOccurrence(
				context.Background(), ClaimScheduleOccurrenceInput{
					Principal:      scheduler,
					IdempotencyKey: "claim-requeued-" + scenario.name,
				},
			)
			if err != nil {
				t.Fatalf("ClaimScheduleOccurrence next attempt: %v", err)
			}
			if next.Occurrence.Attempt != previousAttempt+1 ||
				next.Occurrence.EffectiveInputSHA256 == produced.snapshotDigest ||
				next.Occurrence.EffectiveInputSHA256 == previousDigest {
				t.Fatalf("next attempt did not materialize a fresh execution digest: %+v", next)
			}
			nextRun := fixture.tx.runs[turnAttemptMapKey(next.Occurrence.ID, next.Occurrence.Attempt)]
			if nextRun.EffectiveInputSHA256 != produced.snapshotDigest ||
				nextRun.CurrentInputSHA256 != next.Occurrence.EffectiveInputSHA256 {
				t.Fatalf("next run lost snapshot/current digest separation: %+v", nextRun)
			}
			turn := fixture.tx.resources[next.Occurrence.ExecutionTurnID]
			turnSpec := turn.Spec.(entity.TurnSpec)
			claimed, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
				Principal: fixture.principalFor(
					permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
					turn.ID, uint64(turnSpec.Attempt), turnSpec.EffectiveInputSHA256,
					fixture.grant,
				),
				IdempotencyKey: "claim-requeued-turn-" + scenario.name,
			})
			if err != nil || claimed.Turn.State != enum.StateClaimed {
				t.Fatalf("ClaimTurn after requeue: %v %+v", err, claimed)
			}
		})
	}
}

func TestScheduledDigestLifecycleUsesClosedHelpers(t *testing.T) {
	checks := []struct {
		file, function, helper string
	}{
		{"specialized.go", "ClaimScheduleOccurrence", "materializeScheduledOccurrence"},
		{"specialized.go", "CompleteScheduleOccurrence", "applyScheduledTerminalDisposition"},
		{"final_owner_wave.go", "recoverExpiredScheduleOccurrence", "applyScheduledTerminalDisposition"},
		{"current_execution.go", "prepareRetriedExecution", "rebindScheduledOccurrence"},
		{"current_execution.go", "rebindStandaloneScheduledRetry", "rebindScheduledOccurrence"},
		{"final_owner_wave.go", "continueOwnerGateGraph", "rebindScheduledOccurrence"},
		{"runtime_continuation.go", "materializeIntegrationScheduledGraph", "rebindScheduledOccurrence"},
	}
	for _, check := range checks {
		source := productionFunctionSource(t, check.file, check.function)
		if !strings.Contains(source, check.helper+"(") {
			t.Fatalf("%s bypasses %s", check.function, check.helper)
		}
		if strings.Contains(source, "occurrence.EffectiveInputSHA256 =") {
			t.Fatalf("%s assigns scheduled digest outside the closed helper", check.function)
		}
	}
	manage := productionFunctionSource(t, "specialized.go", "ManageSchedule")
	if !strings.Contains(manage, "scheduleMutationRequiresClosedGraph(") ||
		!strings.Contains(manage, "withValidatedResourceReceipt(") {
		t.Fatal("ManageSchedule bypasses the closed open-graph/receipt validation matrix")
	}
}

func TestRequeuedScheduleCompletionReceiptRequiresCurrentQueuedSnapshot(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
	schedule := fixture.tx.resources[produced.schedule.ID]
	previousAttempt := occurrence.Attempt
	if err := requeueScheduledOccurrence(
		&occurrence, schedule, run, previousAttempt+1, fixture.tx.now.Add(time.Second),
		"execution_failed", fixture.tx.now,
	); err != nil {
		t.Fatalf("requeueScheduledOccurrence: %v", err)
	}
	payload, err := json.Marshal(scheduleOccurrenceReceipt{Occurrence: occurrence})
	if err != nil {
		t.Fatalf("marshal completion receipt: %v", err)
	}
	keyHash := hashString("complete-requeued")
	requestHash := hashString("complete-request")
	fixture.tx.receipts[receiptMapKey("complete_schedule_occurrence", keyHash)] =
		domainrepo.Receipt{
			OrganizationID: fixture.organization,
			Scope:          "complete_schedule_occurrence",
			KeyHash:        keyHash,
			RequestHash:    requestHash,
			Payload:        payload,
		}
	replayed, err := replayRequeuedScheduleCompletion(
		context.Background(), fixture.tx, fixture.organization, occurrence, schedule,
		previousAttempt, keyHash, requestHash,
	)
	if err != nil || replayed != occurrence {
		t.Fatalf("current queued completion replay: %v %+v", err, replayed)
	}
	stale := occurrence
	stale.EffectiveInputSHA256 = hashString("stale-requeue-snapshot")
	if _, err := replayRequeuedScheduleCompletion(
		context.Background(), fixture.tx, fixture.organization, stale, schedule,
		previousAttempt, keyHash, requestHash,
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("stale queued completion replay returned %v", err)
	}
}

func TestScheduledRequeueRejectsNewerScheduleSnapshot(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
	schedule := fixture.tx.resources[produced.schedule.ID]
	newerSpec := schedule.Spec.(entity.ScheduleSpec)
	newerSpec.EffectiveInputSHA = hashString("newer-schedule-snapshot")
	schedule.Spec = newerSpec
	schedule.Version++
	original := occurrence
	if err := requeueScheduledOccurrence(
		&occurrence, schedule, run, occurrence.Attempt+1,
		fixture.tx.now.Add(time.Second), "execution_failed", fixture.tx.now,
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("requeue against newer Schedule snapshot returned %v", err)
	}
	if occurrence != original {
		t.Fatal("rejected newer Schedule snapshot partially changed occurrence")
	}
}

func TestScheduledRequeueRejectsChangedScheduleRetryPolicy(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
	schedule := fixture.tx.resources[produced.schedule.ID]
	newerSpec := schedule.Spec.(entity.ScheduleSpec)
	newerSpec.MaximumAttempts++
	schedule.Spec = newerSpec
	schedule.Version++
	original := occurrence
	if err := requeueScheduledOccurrence(
		&occurrence, schedule, run, occurrence.Attempt+1,
		fixture.tx.now.Add(time.Second), "execution_failed", fixture.tx.now,
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("requeue against changed retry policy returned %v", err)
	}
	if occurrence != original {
		t.Fatal("rejected retry-policy change partially changed occurrence")
	}
}

func TestScheduledRequeueRejectsClosedScheduleStates(t *testing.T) {
	for _, state := range []enum.State{
		enum.StateArchived, enum.StateDeletionPending, enum.StateDeleted,
	} {
		t.Run(string(state), func(t *testing.T) {
			fixture := newCurrentTupleFixture(t)
			produced := fixture.produceScheduledGraph(t)
			occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
			run := fixture.tx.runs[turnAttemptMapKey(occurrence.ID, occurrence.Attempt)]
			schedule := fixture.tx.resources[produced.schedule.ID]
			schedule.State = state
			original := occurrence
			if err := requeueScheduledOccurrence(
				&occurrence, schedule, run, occurrence.Attempt+1,
				fixture.tx.now.Add(time.Second), "execution_failed", fixture.tx.now,
			); !errors.Is(err, errs.ErrStateConflict) {
				t.Fatalf("requeue under %s returned %v", state, err)
			}
			if occurrence != original {
				t.Fatal("rejected closed-schedule requeue changed occurrence")
			}
		})
	}
}

func TestScheduledProducerReplayAfterLeaseExpiryFailsBeforeReceiptExposure(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	fixture.tx.now = produced.claim.Occurrence.LeaseExpiresAt.Add(time.Microsecond)
	schedule := fixture.tx.resources[produced.schedule.ID]
	principal := fixture.principalFor(
		permissionExecuteSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, produced.snapshotDigest, fixture.grant,
	)
	audits, events := len(fixture.tx.audits), len(fixture.tx.events)
	result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if !errors.Is(err, errs.ErrStateConflict) || result.LeaseToken != "" {
		t.Fatalf("expired scheduler replay exposed authority: %v %+v", err, result)
	}
	if len(fixture.tx.audits) != audits || len(fixture.tx.events) != events {
		t.Fatal("expired scheduler replay partially changed graph")
	}
}

func TestScheduledProducerDigestMismatchRollsBackClaimTurn(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	turnSpec := produced.turn.Spec.(entity.TurnSpec)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	occurrence.EffectiveInputSHA256 = hashString("stale-scheduled-current-input")
	fixture.tx.occurrences[occurrence.ID] = occurrence
	audits, events, receipts := len(fixture.tx.audits), len(fixture.tx.events), len(fixture.tx.receipts)
	_, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
		Principal: fixture.principalFor(
			permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
			produced.turn.ID, uint64(turnSpec.Attempt),
			turnSpec.EffectiveInputSHA256, fixture.grant,
		),
		IdempotencyKey: "claim-produced-stale-digest",
	})
	if !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("stale scheduled digest returned %v", err)
	}
	if fixture.tx.resources[produced.turn.ID].Version != produced.turn.Version ||
		fixture.tx.resources[produced.process.ID].Version != produced.process.Version ||
		len(fixture.tx.leases) != 0 || len(fixture.tx.audits) != audits ||
		len(fixture.tx.events) != events || len(fixture.tx.receipts) != receipts {
		t.Fatal("stale scheduled digest partially committed ClaimTurn")
	}
}

func TestOutboxSequenceFakeRejectsDuplicateAggregateVersion(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	change := event.Change{
		ResourceKind: enum.KindSchedule, ResourceID: uuid.NewString(),
		ResourceVersion: 1, EventSequence: 1,
	}
	if err := fixture.tx.AppendEvent(context.Background(), change); err != nil {
		t.Fatalf("first outbox append: %v", err)
	}
	if err := fixture.tx.AppendEvent(context.Background(), change); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("duplicate outbox append returned %v", err)
	}
}

func TestScheduleOutboxMutationRegistryRejectsOwnerRowAliases(t *testing.T) {
	for _, action := range []string{
		"propagate_claim_turn_schedule", "claim_schedule_occurrence",
		"finish_schedule_occurrence", "renew_turn",
	} {
		if scheduleResourceMutationAction(action) {
			t.Fatalf("owner-row action %q can emit a Schedule outbox fact", action)
		}
	}
	for _, action := range []string{
		"create_schedule", "claim_due_schedule", "manage_schedule_UPDATE",
	} {
		if !scheduleResourceMutationAction(action) {
			t.Fatalf("real Schedule transition %q is absent from closed registry", action)
		}
	}
}

func TestClaimTurnReplayRejectsStaleAuthorityTuple(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*currentTupleFixture, *value.Principal)
	}{
		{
			name: "generation",
			mutate: func(_ *currentTupleFixture, principal *value.Principal) {
				principal.AuthorityGrantGeneration++
			},
		},
		{
			name: "input",
			mutate: func(_ *currentTupleFixture, principal *value.Principal) {
				principal.AuthorityDigest = hashString("stale-input")
			},
		},
		{
			name: "fence",
			mutate: func(fixture *currentTupleFixture, _ *value.Principal) {
				lease := fixture.tx.leases[fixture.turnID]
				lease.Fence--
				fixture.tx.leases[fixture.turnID] = lease
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCurrentTupleFixture(t)
			process := fixture.startRootProcess(t)
			principal := fixture.principal(
				permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
			)
			claim, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
				Principal: principal, IdempotencyKey: fixture.claimKey,
			})
			if err != nil {
				t.Fatalf("initial ClaimTurn: %v", err)
			}
			test.mutate(&fixture, &principal)
			_, err = fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
				Principal: principal, IdempotencyKey: fixture.claimKey,
			})
			if err == nil {
				t.Fatal("stale authority replay succeeded")
			}
			if fixture.tx.resources[fixture.turnID].Version != claim.Turn.Version ||
				fixture.tx.resources[process.ID].Version != process.Version+1 {
				t.Fatal("stale authority replay changed current graph")
			}
		})
	}
}

func TestClaimTurnTupleMismatchRollsBackWholeTransaction(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*currentTupleFixture, entity.Resource)
	}{
		{
			name: "stale process",
			mutate: func(fixture *currentTupleFixture, process entity.Resource) {
				spec := process.Spec.(entity.ProcessRunSpec)
				spec.CurrentTurnVersion--
				process.Spec = spec
				fixture.tx.resources[process.ID] = process
			},
		},
		{
			name: "stale occurrence",
			mutate: func(fixture *currentTupleFixture, process entity.Resource) {
				spec := process.Spec.(entity.ProcessRunSpec)
				occurrence := fixture.tx.occurrences[spec.OccurrenceID]
				occurrence.ExecutionTurnVersion--
				fixture.tx.occurrences[occurrence.ID] = occurrence
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCurrentTupleFixture(t)
			var process entity.Resource
			if test.name == "stale process" {
				process = fixture.startRootProcess(t)
			} else {
				process = fixture.installScheduledProcess(t)
			}
			beforeTurn := fixture.tx.resources[fixture.turnID]
			beforeProcess := fixture.tx.resources[process.ID]
			expectedReceipts := len(fixture.tx.receipts)
			test.mutate(&fixture, beforeProcess)
			_, err := fixture.service.ClaimTurn(context.Background(), ClaimTurnInput{
				Principal: fixture.principal(
					permissionClaimTurn, agentRunnerWorkload, agentRunnerSPIFFEID,
				),
				IdempotencyKey: fixture.claimKey,
			})
			if !errors.Is(err, errs.ErrStateConflict) {
				t.Fatalf("stale tuple returned %v", err)
			}
			if fixture.tx.resources[fixture.turnID].Version != beforeTurn.Version ||
				len(fixture.tx.leases) != 0 || len(fixture.tx.receipts) != expectedReceipts {
				t.Fatal("stale ClaimTurn partially committed authority or graph state")
			}
		})
	}
}

func TestClaimTurnProductionPathRequiresCurrentTuplePropagationGuard(t *testing.T) {
	source := productionFunctionSource(t, "runtime.go", "ClaimTurn")
	for _, guard := range []string{
		"requireCurrentTurnBinding(authorityGraph)",
		"service.propagateCurrentTurnTransition(",
	} {
		if !strings.Contains(source, guard) {
			t.Fatalf("ClaimTurn production path lacks %s", guard)
		}
	}
}
