package resource

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
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
	capabilities := make(map[string]domainrepo.ScheduleOccurrenceCapability, len(repository.tx.capabilities))
	for key, capability := range repository.tx.capabilities {
		capabilities[key] = capability
	}
	runs := cloneRunMap(repository.tx.runs)
	leases := cloneLeaseMap(repository.tx.leases)
	attempts := cloneAttemptMap(repository.tx.attempts)
	automationProjectCursor := make(map[string]int, len(repository.tx.automationProjectCursor))
	for key, position := range repository.tx.automationProjectCursor {
		automationProjectCursor[key] = position
	}
	audits, events := len(repository.tx.audits), len(repository.tx.events)
	deliveries := len(repository.tx.deliveries)
	if err := apply(repository.tx); err != nil {
		repository.tx.resources = resources
		repository.tx.receipts = receipts
		repository.tx.runtimes = runtimes
		repository.tx.occurrences = occurrences
		repository.tx.capabilities = capabilities
		repository.tx.runs = runs
		repository.tx.leases = leases
		repository.tx.attempts = attempts
		repository.tx.automationProjectCursor = automationProjectCursor
		repository.tx.audits = repository.tx.audits[:audits]
		repository.tx.events = repository.tx.events[:events]
		repository.tx.deliveries = repository.tx.deliveries[:deliveries]
		return err
	}
	return nil
}

func (repository *currentTupleTestRepository) Get(
	ctx context.Context,
	organizationID, projectID, id string,
	kind enum.Kind,
) (entity.Resource, error) {
	resource, err := repository.tx.Get(ctx, organizationID, projectID, id)
	if err != nil {
		return entity.Resource{}, err
	}
	if resource.OrganizationID != organizationID || resource.ProjectID != projectID || resource.Kind != kind {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

type currentTupleTestTransaction struct {
	domainrepo.Transaction
	now                     time.Time
	resources               map[string]entity.Resource
	receipts                map[string]domainrepo.Receipt
	runtimes                map[string]RuntimeExecution
	occurrences             map[string]domainrepo.ScheduleOccurrence
	capabilities            map[string]domainrepo.ScheduleOccurrenceCapability
	runs                    map[string]domainrepo.ScheduledRun
	leases                  map[string]domainrepo.TurnLease
	attempts                map[string]domainrepo.TurnAttempt
	retention               domainrepo.ResourceRetentionPolicy
	audits                  []domainrepo.Audit
	events                  []event.Change
	deliveries              []domainrepo.InteractionDeliveryWork
	automationProjectCursor map[string]int
}

func (tx *currentTupleTestTransaction) CurrentTime(context.Context) (time.Time, error) {
	return tx.now, nil
}

func (tx *currentTupleTestTransaction) NextAutomationProject(
	_ context.Context, organizationID, operation string,
) (string, error) {
	projects := make([]string, 0)
	for _, resource := range tx.resources {
		if resource.Kind == enum.KindProject && resource.OrganizationID == organizationID &&
			resource.State == enum.StateActive {
			projects = append(projects, resource.ID)
		}
	}
	if len(projects) == 0 {
		return "", errs.ErrNotFound
	}
	sort.Strings(projects)
	key := organizationID + "\x00" + operation
	position := tx.automationProjectCursor[key] % len(projects)
	tx.automationProjectCursor[key] = position + 1
	return projects[position], nil
}

func (tx *currentTupleTestTransaction) EnqueueInteractionDelivery(
	_ context.Context,
	work domainrepo.InteractionDeliveryWork,
) error {
	tx.deliveries = append(tx.deliveries, work)
	return nil
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
			enum.KindIntegration, enum.KindChat, enum.KindRoleImageRecipe,
			enum.KindImageBuild, enum.KindImageArtifact:
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func (*currentTupleTestTransaction) LockScheduleSessionProjectFence(
	context.Context, string, string,
) error {
	return nil
}

func (tx *currentTupleTestTransaction) ListScheduleSessionConversationForUpdate(
	_ context.Context,
	organizationID, projectID, conversationID string,
) ([]entity.Resource, error) {
	resources := make([]entity.Resource, 0, 1)
	for _, resource := range tx.resources {
		spec, ok := resource.Spec.(entity.SessionSpec)
		if !ok || resource.Kind != enum.KindSession || resource.OrganizationID != organizationID ||
			resource.ProjectID != projectID || spec.ConversationID != conversationID {
			continue
		}
		switch resource.State {
		case enum.StateActive, enum.StatePaused, enum.StateQueued, enum.StateClaimed, enum.StateRunning,
			enum.StateWaitingExternal, enum.StateWaitingOwner, enum.StateBlocked:
			resources = append(resources, resource)
		}
	}
	slices.SortFunc(resources, func(left, right entity.Resource) int {
		return strings.Compare(left.ID, right.ID)
	})
	return resources, nil
}

func (tx *currentTupleTestTransaction) NextImageBuild(
	context.Context, string, string, time.Time,
) (entity.Resource, error) {
	return entity.Resource{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) NextImageAdmission(
	context.Context, string, string, time.Time,
) (entity.Resource, error) {
	return entity.Resource{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) NextImagePromotion(
	_ context.Context, organizationID, projectID string,
	policyRevision uint64, policySHA256 string, now time.Time,
) (entity.Resource, error) {
	var candidates []entity.Resource
	for _, resource := range tx.resources {
		spec, ok := resource.Spec.(entity.ImageArtifactSpec)
		if resource.OrganizationID == organizationID && resource.ProjectID == projectID &&
			resource.Kind == enum.KindImageArtifact && resource.State == enum.StateWaitingExternal && ok &&
			spec.AdmissionVerdict == entity.ImageAdmissionAccepted && spec.PolicyRevision == policyRevision &&
			spec.PolicySHA256 == policySHA256 && spec.PromotedReference == "" &&
			(spec.PromotionClaimJTISHA256 == "" || !now.Before(spec.PromotionClaimExpiresAt)) {
			candidates = append(candidates, resource)
		}
	}
	if len(candidates) == 0 {
		return entity.Resource{}, errs.ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	return candidates[0], nil
}

func (tx *currentTupleTestTransaction) PromotedImageArtifactBySpec(
	_ context.Context, organizationID, projectID, ownerActorID, specSHA256 string,
	policyRevision uint64, policySHA256 string,
) (entity.Resource, error) {
	for _, resource := range tx.resources {
		spec, ok := resource.Spec.(entity.ImageArtifactSpec)
		if resource.OrganizationID == organizationID && resource.ProjectID == projectID &&
			resource.OwnerActorID == ownerActorID &&
			resource.Kind == enum.KindImageArtifact && resource.State == enum.StateActive && ok &&
			spec.SpecSHA256 == specSHA256 && spec.PolicyRevision == policyRevision &&
			spec.PolicySHA256 == policySHA256 && spec.PromotedReference != "" {
			return resource, nil
		}
	}
	return entity.Resource{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) ImageBuildsForRecipeForUpdate(
	_ context.Context, organizationID, projectID, recipeID string,
) ([]entity.Resource, error) {
	resources := make([]entity.Resource, 0)
	for _, resource := range tx.resources {
		spec, ok := resource.Spec.(entity.ImageBuildSpec)
		if resource.OrganizationID == organizationID && resource.ProjectID == projectID &&
			resource.Kind == enum.KindImageBuild && ok && spec.RecipeID == recipeID &&
			(resource.State == enum.StateQueued || resource.State == enum.StateClaimed ||
				resource.State == enum.StateRunning || resource.State == enum.StateBlocked) {
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

func (tx *currentTupleTestTransaction) ImageArtifactsForRecipeForUpdate(
	_ context.Context, organizationID, projectID, recipeID string,
) ([]entity.Resource, error) {
	resources := make([]entity.Resource, 0)
	for _, resource := range tx.resources {
		spec, ok := resource.Spec.(entity.ImageArtifactSpec)
		if resource.OrganizationID == organizationID && resource.ProjectID == projectID &&
			resource.Kind == enum.KindImageArtifact && resource.State == enum.StateWaitingExternal &&
			ok && spec.RecipeID == recipeID {
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
			(occurrence.State == "RESERVED" || occurrence.State == "CLAIMED" || occurrence.State == "WAITING_OWNER" ||
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
	ctx context.Context, organizationID, projectID string, now time.Time, limit int,
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
		if len(result) == limit {
			break
		}
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
			(occurrence.State == "RESERVED" || occurrence.State == "CLAIMED") &&
			!occurrence.LeaseExpiresAt.After(now) {
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
	candidates := make([]domainrepo.ScheduleOccurrence, 0)
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
			candidates = append(candidates, occurrence)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if !candidates[left].AvailableAt.Equal(candidates[right].AvailableAt) {
			return candidates[left].AvailableAt.Before(candidates[right].AvailableAt)
		}
		if !candidates[left].ScheduledFor.Equal(candidates[right].ScheduledFor) {
			return candidates[left].ScheduledFor.Before(candidates[right].ScheduledFor)
		}
		return candidates[left].ID < candidates[right].ID
	})
	if len(candidates) > 0 {
		return candidates[0], nil
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
	current.Version = occurrence.Version
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
	current.RecoveryEvidenceSHA256 = occurrence.RecoveryEvidenceSHA256
	current.RecoveryBlockedAt = occurrence.RecoveryBlockedAt
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

func (tx *currentTupleTestTransaction) InsertScheduleOccurrenceCapability(
	_ context.Context, capability domainrepo.ScheduleOccurrenceCapability,
) error {
	if _, exists := tx.capabilities[capability.TokenSHA256]; exists {
		return errs.ErrStateConflict
	}
	tx.capabilities[capability.TokenSHA256] = capability
	return nil
}

func (tx *currentTupleTestTransaction) GetScheduleOccurrenceCapabilityForUpdate(
	_ context.Context, tokenSHA256 string,
) (domainrepo.ScheduleOccurrenceCapability, error) {
	capability, ok := tx.capabilities[tokenSHA256]
	if !ok {
		return domainrepo.ScheduleOccurrenceCapability{}, errs.ErrNotFound
	}
	return capability, nil
}

func (tx *currentTupleTestTransaction) GetScheduleOccurrenceCapabilityByOccurrenceForUpdate(
	_ context.Context, occurrenceID string, attempt uint32, fullMethod string, generation uint64,
) (domainrepo.ScheduleOccurrenceCapability, error) {
	for _, capability := range tx.capabilities {
		if capability.OccurrenceID == occurrenceID && capability.Attempt == attempt &&
			capability.FullMethod == fullMethod && capability.AuthorityGeneration == generation {
			return capability, nil
		}
	}
	return domainrepo.ScheduleOccurrenceCapability{}, errs.ErrNotFound
}

func (tx *currentTupleTestTransaction) UpdateScheduleOccurrenceCapability(
	_ context.Context, capability domainrepo.ScheduleOccurrenceCapability, expectedState string,
) error {
	current, ok := tx.capabilities[capability.TokenSHA256]
	if !ok || current.State != expectedState || current.ID != capability.ID {
		return errs.ErrStateConflict
	}
	tx.capabilities[capability.TokenSHA256] = capability
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

func (tx *currentTupleTestTransaction) WaitScheduledRun(
	_ context.Context, waiting domainrepo.ScheduledRun,
) error {
	key := turnAttemptMapKey(waiting.OccurrenceID, waiting.Attempt)
	run, ok := tx.runs[key]
	if !ok || (run.State != "CLAIMED" && run.State != "CONTINUATION") {
		return errs.ErrStateConflict
	}
	run.State = "WAITING_OWNER"
	run.Outcome = waiting.Outcome
	run.ResultArtifactID = waiting.ResultArtifactID
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
	_ context.Context, organizationID, projectID, processID string,
) (entity.Resource, error) {
	for _, resource := range tx.resources {
		if resource.OrganizationID == organizationID && resource.ProjectID == projectID &&
			resource.ParentID == processID && resource.Kind == enum.KindOwnerGate &&
			resource.State == enum.StateWaitingOwner {
			return resource, nil
		}
	}
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

type currentTupleTestObserver struct {
	maintenance []string
}

func (*currentTupleTestObserver) ObserveMutation(enum.Kind, string) {}
func (observer *currentTupleTestObserver) ObserveScheduleMaintenance(effect string) {
	observer.maintenance = append(observer.maintenance, effect)
}

type currentTupleReadbackIssuer struct{}

func (currentTupleReadbackIssuer) Issue(_ context.Context, claims InteractionReadbackClaims) (InteractionReadbackCredential, error) {
	return InteractionReadbackCredential{
		Compact: "synthetic-readback-credential", SHA256: hashString(claims.DeliveryID),
		ProducerID: "control-plane", Purpose: "interaction-delivery-readback",
		WorkloadID: "interaction-gateway", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		Operation: "interaction.delivery.readback", Permission: "control.interaction.delivery.readback",
		KeysetSHA256: hashString("test-readback-keyset"), Generation: 1, KeysetRevision: 1, KeysetHighWatermark: 1,
	}, nil
}

func (currentTupleReadbackIssuer) Check(context.Context) error { return nil }

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
	chatID        string
	inputSHA256   string
	claimKey      string
	grant         uint64
	runtimeWorker string
	runtimeSPIFFE string
	observer      *currentTupleTestObserver
}

func newCurrentTupleFixture(t *testing.T) currentTupleFixture {
	t.Helper()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	organization, project, actor := uuid.NewString(), uuid.NewString(), uuid.NewString()
	sessionID, turnID, chatID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	revisionID, artifactID, roleID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	imageRecipeID, imageBuildID, imageArtifactID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	instructionArtifactID := uuid.NewString()
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
	chat, err := entity.New(chatID, organization, project, "", actor, enum.KindChat, "Scheduled room",
		entity.ChatSpec{
			StableKey: "scheduled-room", RoomType: "RUNS", DefaultAgentID: roleID,
			ExternalChannelRef: "mattermost:scheduled-room", WorkPolicy: "scheduled delivery",
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, now)
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}
	prompt, err := entity.New(
		promptID, organization, project, "", actor,
		enum.KindPromptProfile, "Prompt profile",
		entity.PromptProfileSpec{
			Revision: 1, ContentSHA256: digest, SourceRef: "prompt:test", Locale: "ru",
			ContentArtifactID: instructionArtifactID, ContentArtifactVersion: 1,
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
	runtimeCredentials := make([]entity.Resource, 0, 7)
	credentialIDs := []string{bindingID}
	for _, purpose := range []string{
		"control-plane-application-grant", "runtime-materialization-application-grant", "mcp-token",
		"handoff-private-key", "control-plane-client-tls", "interaction-gateway-client-tls", "mcp-client-tls",
	} {
		credentialID := uuid.NewString()
		parentID := ""
		principalRef := "runtime:" + purpose
		ownership := entity.ConfigurationOwnership{ManagedBy: "GIT", SourceRef: "git:runtime-credentials", SourceRevision: 1}
		if purpose == "mcp-token" {
			credentialID = sessionMCPBindingID(sessionID)
			parentID = sessionID
			principalRef = "bot-agent-session:test-session"
			ownership = entity.ConfigurationOwnership{ManagedBy: "UI", SourceRef: "agent-session:test-session", SourceRevision: 1}
		}
		credential, credentialErr := entity.New(
			credentialID, organization, project, parentID, actor, enum.KindCredentialBinding, "Runtime credential "+purpose,
			entity.CredentialBindingSpec{
				Purpose: purpose, SecretRef: "vault://runtime/" + purpose,
				PrincipalRef: principalRef, Revision: 1,
				ImmutableSecretRef:     "k8s-immutable-secret://mattercodex-system/test-" + purpose,
				ProviderContentVersion: "runtime:" + purpose + ":v1", ContentSHA256: digest,
				Ownership: ownership,
			}, now,
		)
		if credentialErr != nil {
			t.Fatalf("create runtime credential %s: %v", purpose, credentialErr)
		}
		runtimeCredentials = append(runtimeCredentials, credential)
		credentialIDs = append(credentialIDs, credential.ID)
	}
	role, err := entity.New(
		roleID, organization, project, "", actor, enum.KindRole, "Runtime role",
		entity.RoleSpec{
			StableKey: "runtime-role", Capabilities: []string{"runtime.execute"},
			RoleImageRecipeID:            imageRecipeID,
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
	imageRecipe, err := entity.New(
		imageRecipeID, organization, project, "", actor, enum.KindRoleImageRecipe, "Runtime role image",
		entity.RoleImageRecipeSpec{Input: entity.RoleImageRecipeInput{
			BaseImageReference: "registry.example.test/base/runtime", BaseImageDigest: "sha256:" + digest,
			SourceRef: "git://example.test/runtime", SourceRevision: "v1", SourceSHA256: digest,
			ContextRef: "oci://example.test/context@sha256:" + digest, ContextSHA256: digest, BuilderSHA256: digest,
			FrontendSHA256: digest, Platforms: []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
			InstallationBlock: "true", ToolchainSHA256: digest,
		}, Generation: 1, SpecSHA256: digest, PolicyRevision: 1, PolicySHA256: digest,
			RoleRuntimeContractRevision: 1, RoleRuntimeContractSHA256: digest}, now,
	)
	if err != nil {
		t.Fatalf("create role image recipe: %v", err)
	}
	imageBuild, err := entity.New(
		imageBuildID, organization, project, imageRecipeID, actor, enum.KindImageBuild, "Runtime role image build",
		entity.ImageBuildSpec{RecipeID: imageRecipeID, RecipeVersion: 1, RecipeGeneration: 1,
			SpecSHA256: digest, Attempt: 1, Stage: entity.ImageBuildStageCompleted, ProgressPercent: 100,
			StagingReference: "registry.example.test/staging/role@sha256:" + digest,
			ManifestDigest:   "sha256:" + digest, ProvenanceSHA256: digest, ImmutableBuildSHA256: digest,
			AvailableAt: now, MaximumAttempts: 3}, now,
	)
	if err != nil {
		t.Fatalf("create role image build: %v", err)
	}
	imageBuild.State = enum.StateSucceeded
	imageArtifact, err := entity.New(
		imageArtifactID, organization, project, imageBuildID, actor, enum.KindImageArtifact, "Runtime role image artifact",
		entity.ImageArtifactSpec{RecipeID: imageRecipeID, RecipeVersion: 1, RecipeGeneration: 1,
			SpecSHA256: digest, BuildID: imageBuildID, BuildVersion: imageBuild.Version, BuildAttempt: 1,
			StagingReference: "registry.example.test/staging/role@sha256:" + digest,
			ManifestDigest:   "sha256:" + digest, ProvenanceSHA256: digest, ImmutableBuildSHA256: digest,
			BaseImageDigest: "sha256:" + digest, SourceSHA256: digest, ContextSHA256: digest,
			BuilderSHA256: digest, FrontendSHA256: digest, ToolchainSHA256: digest,
			Platforms:  []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
			SBOMSHA256: digest, VulnerabilityEvidenceSHA256: digest, PolicyRevision: 1, PolicySHA256: digest,
			AdmissionVerdict: entity.ImageAdmissionAccepted, SignatureIdentity: "test-signer", SignatureSHA256: digest,
			AdmissionRevision: 1, AdmissionReceiptSHA256: digest,
			AdmissionReceiptOCIManifestDigest: "sha256:" + digest,
			RoleRuntimeContractRevision:       1, RoleRuntimeContractSHA256: digest,
			PromotedReference:       "registry.example.test/promoted/role@sha256:" + digest,
			PromotionReadbackSHA256: digest, PromotedAt: now}, now,
	)
	if err != nil {
		t.Fatalf("create role image artifact: %v", err)
	}
	imageArtifact.State = enum.StateActive
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
	chatSHA, err := entity.ProjectionSHA256(chat)
	if err != nil {
		t.Fatalf("hash chat: %v", err)
	}
	components := []entity.EffectiveResourceRef{{
		Kind: enum.KindRole, ResourceID: role.ID, Version: role.Version,
		ProjectionSHA256: roleSHA,
	}, {
		Kind: enum.KindRoleImageRecipe, ResourceID: imageRecipe.ID, Version: imageRecipe.Version,
		ProjectionSHA256: digest,
	}, {
		Kind: enum.KindImageBuild, ResourceID: imageBuild.ID, Version: imageBuild.Version,
		ProjectionSHA256: digest,
	}, {
		Kind: enum.KindImageArtifact, ResourceID: imageArtifact.ID, Version: imageArtifact.Version,
		ProjectionSHA256: digest,
	}, {
		Kind: enum.KindPromptProfile, ResourceID: prompt.ID, Version: prompt.Version,
		ProjectionSHA256: promptSHA,
	}, {
		Kind: enum.KindCredentialBinding, ResourceID: binding.ID, Version: binding.Version,
		ProjectionSHA256: bindingSHA,
	}, {
		Kind: enum.KindChat, ResourceID: chat.ID, Version: chat.Version,
		ProjectionSHA256: chatSHA,
	}}
	for _, credential := range runtimeCredentials {
		credentialSHA, hashErr := entity.ProjectionSHA256(credential)
		if hashErr != nil {
			t.Fatalf("hash runtime credential: %v", hashErr)
		}
		components = append(components, entity.EffectiveResourceRef{
			Kind: enum.KindCredentialBinding, ResourceID: credential.ID, Version: credential.Version,
			ProjectionSHA256: credentialSHA,
		})
	}
	for _, kind := range []enum.Kind{enum.KindIntegration, enum.KindMemoryRecord} {
		components = append(components, entity.EffectiveResourceRef{
			Kind: kind, ResourceID: uuid.NewString(), Version: 1,
			ProjectionSHA256: digest,
		})
	}
	revision, err := entity.New(
		revisionID, organization, project, sessionID, actor,
		enum.KindRuntimeRevision, "Runtime revision",
		entity.RuntimeRevisionSpec{
			ProviderAccountName: "test",
			ManifestSHA256:      digest, ImageReference: "registry.example.test/roles/test@sha256:" + digest,
			RoleImageRecipeID: imageRecipeID, RoleImageRecipeVersion: 1, RoleImageSpecSHA256: digest,
			ImageBuildID: imageBuildID, ImageBuildVersion: 1, ImageBuildAttempt: 1,
			ImageArtifactID: imageArtifactID, ImageArtifactVersion: 1, ImageManifestDigest: "sha256:" + digest,
			ImageAdmissionRevision: 1, ImageAdmissionReceiptSHA256: digest,
			ImageAdmissionReceiptOCIManifestDigest: "sha256:" + digest,
			ImagePolicyRevision:                    1, ImagePolicySHA256: digest, ImageSignatureSHA256: digest,
			ImagePromotionReadbackSHA256: digest,
			RoleRuntimeContractRevision:  1, RoleRuntimeContractSHA256: digest,
			PromptProfileID: promptID, PromptRevision: 1,
			CredentialBindingIDs:   credentialIDs,
			AuthorityPolicyVersion: 1, AuthorityPolicySHA256: digest,
			Components: components, CreatedAt: now, SessionID: sessionID,
			RoleID: roleID, ProviderCredentialBindingID: bindingID,
			ChatID:                 chatID,
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
			ConversationID:  chatID,
			AgentSessionKey: "test-session", AgentSessionID: 1,
			AgentSessionBindingVersion: 1, AgentSessionBindingSHA256: digest,
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
			SizeBytes: 1, MediaType: "text/markdown", SHA256: digest,
			ScanStatus: "CLEAN", RetentionPolicyRef: "retention:test",
			ScanPolicyRevision: 1, ScanEvidenceSHA256: digest,
			ScannerWorkloadID: "artifact-scanner", ScannedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	instructionArtifact, err := entity.New(
		instructionArtifactID, organization, project, roleID, actor, enum.KindArtifact, "Instructions",
		entity.ArtifactSpec{
			ArtifactKind: "instruction-set", Direction: "INPUT", StorageRef: "s3://test/instructions",
			SizeBytes: 1, MediaType: "text/markdown", SHA256: digest,
			ScanStatus: "CLEAN", RetentionPolicyRef: "retention:test",
			ScanPolicyRevision: 1, ScanEvidenceSHA256: digest,
			ScannerWorkloadID: "artifact-scanner", ScannedAt: now,
		}, now,
	)
	if err != nil {
		t.Fatalf("create instruction artifact: %v", err)
	}
	tx := &currentTupleTestTransaction{
		now: now,
		resources: map[string]entity.Resource{
			projectResource.ID: projectResource, chat.ID: chat, prompt.ID: prompt, binding.ID: binding,
			role.ID: role, revision.ID: revision, session.ID: session,
			imageRecipe.ID: imageRecipe, imageBuild.ID: imageBuild, imageArtifact.ID: imageArtifact,
			turn.ID: turn, artifact.ID: artifact, instructionArtifact.ID: instructionArtifact,
		},
		receipts:                make(map[string]domainrepo.Receipt),
		automationProjectCursor: make(map[string]int),
		runtimes:                make(map[string]RuntimeExecution),
		occurrences:             make(map[string]domainrepo.ScheduleOccurrence),
		capabilities:            make(map[string]domainrepo.ScheduleOccurrenceCapability),
		runs:                    make(map[string]domainrepo.ScheduledRun),
		leases:                  make(map[string]domainrepo.TurnLease),
		attempts: map[string]domainrepo.TurnAttempt{
			turnAttemptMapKey(turn.ID, 1): {
				TurnID: turn.ID, Attempt: 1, WorkloadID: "unassigned",
				AuthorityGeneration: 1, State: "QUEUED", InputSHA256: digest,
				LeaseFence: turn.Version, StartedAt: now,
			},
		},
		retention: domainrepo.ResourceRetentionPolicy{
			ID: "runtime-default", Version: 1,
			PVCRetentionSeconds:     uint64((7 * 24 * time.Hour) / time.Second),
			ArchiveRetentionSeconds: uint64((90 * 24 * time.Hour) / time.Second),
			EffectiveFrom:           now.Add(-time.Hour),
		},
	}
	for _, credential := range runtimeCredentials {
		tx.resources[credential.ID] = credential
	}
	repository := &currentTupleTestRepository{tx: tx}
	const runtimeWorker = "runtime-controller"
	const runtimeSPIFFE = "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller"
	observer := &currentTupleTestObserver{}
	service, err := New(repository, Config{
		LeaseSigningKey:            []byte("0123456789abcdef0123456789abcdef"),
		RuntimeAdmissionSigningKey: ed25519.NewKeyFromSeed([]byte("runtime-admission-signing-test!!")),
		RuntimeArchiveSigningKey:   ed25519.NewKeyFromSeed([]byte("runtime-archive-signing-test-key")),
		RuntimeRestoreSigningKey:   ed25519.NewKeyFromSeed([]byte("runtime-restore-signing-test-key")),
		TurnLeaseDuration:          time.Minute, MaximumScheduleClaims: 10,
		ImagePolicyRevision: 1, ImagePolicySHA256: digest,
		ImageBuildLeaseDuration: time.Minute, ImageAdmissionClaimTTL: time.Minute,
		ImagePromotionClaimTTL: time.Minute, ImageMaximumAttempts: 3,
		StagingImageRepository:      "registry.example.test/staging/roles",
		PromotedImageRepository:     "registry.example.test/promoted/roles",
		RoleImageInputRepository:    "registry.example.test/role-image-inputs",
		TrustedRoleBaseRepository:   "registry.example.test/base/runtime",
		TrustedRoleBaseDigest:       "sha256:" + digest,
		RoleRuntimeContractRevision: 1, RoleRuntimeContractSHA256: digest,
		ImageBuilderWorkload:    "role-image-builder",
		ImageBuilderSPIFFEID:    "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder",
		ImageAdmissionWorkload:  "image-admission",
		ImageAdmissionSPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/image-admission",
		ImagePromotionWorkload:  "image-promotion",
		ImagePromotionSPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion",
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
		ArchiveWorkload:            "runtime-archive",
		ArchiveSPIFFEID:            "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-archive",
		IntegrationGatewayWorkload: "integration-gateway",
		IntegrationGatewaySPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		RestoreVerifierWorkload:    "restore-verifier",
		RestoreVerifierSPIFFEID:    "spiffe://mattercodex.local/ns/mattercodex-system/sa/restore-verifier",
		CleanupAuthorizerWorkload:  "cleanup-authorizer",
		CleanupAuthorizerSPIFFEID:  "spiffe://mattercodex.local/ns/mattercodex-system/sa/cleanup-authorizer",
		PendingRescheduleDelay:     30 * time.Second,
		InteractionReadbackIssuer:  currentTupleReadbackIssuer{},
		Observer:                   observer,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	service.now = func() time.Time { return tx.now }
	return currentTupleFixture{
		service: service, tx: tx, organization: organization, project: project,
		actor: actor, sessionID: sessionID, turnID: turnID, revisionID: revisionID,
		artifactID: artifactID, roleID: roleID, promptID: promptID, bindingID: bindingID,
		chatID:      chatID,
		inputSHA256: digest,
		claimKey:    "claim-turn-current-tuple", grant: 7,
		runtimeWorker: runtimeWorker, runtimeSPIFFE: runtimeSPIFFE, observer: observer,
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
	principal := value.Principal{
		ActorID: fixture.actor, OrganizationID: fixture.organization,
		ProjectID: fixture.project, Permission: permission,
		CorrelationID: uuid.NewString(), PolicyRevision: 1, AuthorityGeneration: 1,
		CallerWorkload: workload, CallerSPIFFEID: spiffe,
		AuthoritySource: "AGENT_SESSION", AuthorityReference: authorityReference,
		AuthorityRevision: authorityRevision, AuthorityDigest: authorityDigest,
		AuthorityGrantGeneration: grant,
	}
	if workload == "scheduler" {
		principal.ProjectID = ""
		principal.AuthoritySource = "AUTOMATION_OCCURRENCE"
		principal.AuthorityGrantGeneration = 0
	}
	return principal
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
	claim          MaterializeScheduleOccurrenceResult
	turn           entity.Resource
	process        entity.Resource
	runtime        entity.Resource
}

func (fixture currentTupleFixture) produceScheduledGraph(t *testing.T) scheduledProducerPath {
	return fixture.produceScheduledGraphWithMaximumAttempts(t, 3)
}

func (fixture currentTupleFixture) produceScheduledGraphForTarget(
	t *testing.T, targetType string,
) scheduledProducerPath {
	return fixture.produceScheduledGraphWithTarget(t, 3, targetType)
}

func (fixture currentTupleFixture) materializeReservedOccurrence(
	t *testing.T, reserved ScheduleOccurrenceResult, key string,
) MaterializeScheduleOccurrenceResult {
	t.Helper()
	result, err := fixture.service.MaterializeScheduleOccurrence(context.Background(),
		MaterializeScheduleOccurrenceInput{
			Principal: fixture.principalFor(permissionUseScheduleCapability, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				reserved.Occurrence.ID, uint64(reserved.Occurrence.Attempt),
				reserved.Occurrence.EffectiveInputSHA256, fixture.grant),
			IdempotencyKey: key, OccurrenceID: reserved.Occurrence.ID,
			ProjectID: reserved.ProjectID, ExpectedAttempt: reserved.Occurrence.Attempt,
			MaterializationCapability: reserved.MaterializationCapability,
		})
	if err != nil {
		t.Fatalf("MaterializeScheduleOccurrence: %v", err)
	}
	return result
}

func (fixture currentTupleFixture) produceScheduledGraphWithMaximumAttempts(
	t *testing.T,
	maximumAttempts uint32,
) scheduledProducerPath {
	return fixture.produceScheduledGraphWithTarget(t, maximumAttempts, "PLAYBOOK")
}

func (fixture currentTupleFixture) produceScheduledGraphWithTarget(
	t *testing.T,
	maximumAttempts uint32,
	targetType string,
) scheduledProducerPath {
	t.Helper()
	managePrincipal := fixture.principal(
		permissionManageSchedule, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
	)
	spec := entity.ScheduleSpec{
		TargetResourceID: fixture.roleID, Interval: time.Hour,
		Timezone: "UTC", Calendar: "GREGORIAN", OverlapPolicy: "FORBID",
		MisfirePolicy: "RUN_ONCE", NextRunAt: fixture.tx.now.Add(time.Minute),
		DeliveryPolicy: "AT_LEAST_ONCE", MaximumAttempts: maximumAttempts,
		InitialBackoff: time.Second, MaximumBackoff: time.Minute,
		DeadLetterAfter: time.Hour, PromptProfileID: fixture.promptID,
		PromptRevision: 1, SessionPolicy: "PERSISTENT",
		ExecutionSessionID: fixture.sessionID, RoomID: fixture.chatID,
		NotificationPolicy:       "ON_ACTION_OR_FAILURE",
		MaximumExecutionDuration: time.Minute, Coalesce: true,
		RuntimeRevisionID: fixture.revisionID, TargetType: targetType,
		PromptArtifactID: fixture.artifactID,
		Ownership:        entity.ConfigurationOwnership{ManagedBy: "UI"},
	}
	if targetType == "PLAYBOOK" {
		spec.PlaybookRef, spec.PlaybookVersion = "playbook:test", 1
	}
	schedule, err := fixture.service.ManageSchedule(context.Background(), ManageScheduleInput{
		Principal: managePrincipal, IdempotencyKey: "create-schedule-current-digest",
		Action: "CREATE", Name: "Scheduled production path",
		Spec: spec,
	})
	if err != nil {
		t.Fatalf("ManageSchedule CREATE: %v", err)
	}
	scheduleSpec := schedule.Spec.(entity.ScheduleSpec)
	fixture.tx.now = scheduleSpec.NextRunAt.Add(time.Microsecond)
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
	rotatedDuePrincipal := schedulerPrincipal
	rotatedDuePrincipal.CorrelationID, rotatedDuePrincipal.AuthorityReference = uuid.NewString(), uuid.NewString()
	rotatedDuePrincipal.AuthorityRevision, rotatedDuePrincipal.AuthorityDigest = 99, hashString("rotated-due-grant")
	dueReplay, err := fixture.service.ClaimDueSchedules(context.Background(), ClaimDueSchedulesInput{
		Principal: rotatedDuePrincipal, IdempotencyKey: "claim-due-current-digest", Limit: 1,
	})
	if err != nil || !reflect.DeepEqual(dueReplay, due) {
		t.Fatalf("ClaimDueSchedules rotation replay: %v %+v", err, dueReplay)
	}
	snapshotDigest := due.Occurrences[0].EffectiveInputSHA256
	claimPrincipal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, fixture.tx.resources[schedule.ID].Version,
		snapshotDigest, fixture.grant,
	)
	auditsBeforeClaim, eventsBeforeClaim := len(fixture.tx.audits), len(fixture.tx.events)
	reserved, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: claimPrincipal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if err != nil {
		t.Fatalf("ClaimScheduleOccurrence: %v", err)
	}
	rotatedClaimPrincipal := claimPrincipal
	rotatedClaimPrincipal.CorrelationID, rotatedClaimPrincipal.AuthorityReference = uuid.NewString(), uuid.NewString()
	rotatedClaimPrincipal.AuthorityRevision, rotatedClaimPrincipal.AuthorityDigest = 100, hashString("rotated-claim-grant")
	reservedReplay, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: rotatedClaimPrincipal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if err != nil || reservedReplay != reserved {
		t.Fatalf("ClaimScheduleOccurrence rotation replay: %v %+v", err, reservedReplay)
	}
	materializePrincipal := claimPrincipal
	materializePrincipal.Permission = permissionUseScheduleCapability
	materializePrincipal.ProjectID = ""
	materializeKey := reserved.MaterializationIdempotencyKey
	claimed, err := fixture.service.MaterializeScheduleOccurrence(context.Background(),
		MaterializeScheduleOccurrenceInput{
			Principal: materializePrincipal, IdempotencyKey: materializeKey,
			OccurrenceID: reserved.Occurrence.ID, ProjectID: reserved.ProjectID,
			ExpectedAttempt:           reserved.Occurrence.Attempt,
			MaterializationCapability: reserved.MaterializationCapability,
		})
	if err != nil {
		t.Fatalf("MaterializeScheduleOccurrence: %v", err)
	}
	if claimed.Occurrence.EffectiveInputSHA256 == snapshotDigest {
		t.Fatal("materialized execution digest was not separated from schedule snapshot")
	}
	auditsAfterClaim, eventsAfterClaim := len(fixture.tx.audits), len(fixture.tx.events)
	rotatedMaterializePrincipal := materializePrincipal
	rotatedMaterializePrincipal.CorrelationID, rotatedMaterializePrincipal.AuthorityReference =
		uuid.NewString(), uuid.NewString()
	rotatedMaterializePrincipal.AuthorityRevision, rotatedMaterializePrincipal.AuthorityDigest =
		101, hashString("rotated-materialize-grant")
	replayed, err := fixture.service.MaterializeScheduleOccurrence(context.Background(),
		MaterializeScheduleOccurrenceInput{
			Principal: rotatedMaterializePrincipal, IdempotencyKey: materializeKey,
			OccurrenceID: reserved.Occurrence.ID, ProjectID: reserved.ProjectID,
			ExpectedAttempt:           reserved.Occurrence.Attempt,
			MaterializationCapability: reserved.MaterializationCapability,
		})
	if err != nil || replayed != claimed {
		t.Fatalf("MaterializeScheduleOccurrence replay: %v %+v", err, replayed)
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
	runtimeSpec := runtimeRevision.Spec.(entity.RuntimeRevisionSpec)
	if turnSpec.ScheduledResultContract == nil || runtimeSpec.ScheduledResultContract == nil ||
		*turnSpec.ScheduledResultContract != *runtimeSpec.ScheduledResultContract ||
		turnSpec.ScheduledResultContract.Validate() != nil {
		t.Fatalf("scheduled result contract is not pinned in Turn/RuntimeRevision: turn=%+v revision=%+v",
			turnSpec.ScheduledResultContract, runtimeSpec.ScheduledResultContract)
	}
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
		permissionUseScheduleCapability, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	completed, err := fixture.service.CompleteScheduleOccurrence(
		context.Background(), CompleteScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "complete-occurrence-" + suffix,
			OccurrenceID:         produced.claim.Occurrence.ID,
			CompletionCapability: produced.claim.CompletionCapability,
			ExpectedAttempt:      produced.claim.Occurrence.Attempt,
			ProjectID:            fixture.project,
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
	// Первый next run назначает server по текущему PostgreSQL time. Короткий
	// interval создаёт независимую due-строку, не перенося clock за deadline
	// проверяемой occurrence.
	spec.Interval = time.Minute
	spec.Cron = ""
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
	runtimePrincipal := fixture.principal(
		permissionRuntimeClaim, fixture.runtimeWorker, fixture.runtimeSPIFFE,
	)
	execution, err := fixture.service.ClaimRuntimeExecution(
		context.Background(), runtimePrincipal, "claim-runtime-current-tuple",
	)
	if err != nil {
		t.Fatalf("ClaimRuntimeExecution before role Pod: %v", err)
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

	if _, resolveErr := fixture.service.resolveBoundExecution(
		context.Background(), fixture.tx, runtimePrincipal,
	); resolveErr != nil {
		turn := fixture.tx.resources[fixture.turnID]
		attempt := fixture.tx.attempts[turnAttemptMapKey(fixture.turnID, 1)]
		lease := fixture.tx.leases[fixture.turnID]
		t.Fatalf("resolve runtime graph before claim: %v; turn=%+v attempt=%+v lease=%+v", resolveErr, turn, attempt, lease)
	}
	recoveredExecution, err := fixture.service.ClaimRuntimeExecution(
		context.Background(), runtimePrincipal, "claim-runtime-current-tuple",
	)
	if err != nil {
		t.Fatalf("ClaimRuntimeExecution replay after real ClaimTurn: %v", err)
	}
	if !reflect.DeepEqual(recoveredExecution, execution) {
		t.Fatalf("runtime bootstrap replay changed execution: before=%+v after=%+v", execution, recoveredExecution)
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
	if !reflect.DeepEqual(recovered, execution) || len(fixture.tx.runtimes) != 1 {
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
	for _, targetType := range []string{"PLAYBOOK", "AGENT"} {
		t.Run(targetType, func(t *testing.T) {
			fixture := newCurrentTupleFixture(t)
			produced := fixture.produceScheduledGraphForTarget(t, targetType)
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
			if scheduleEvents != 0 {
				t.Fatalf("polling-only Schedule received an outbox event: %d", scheduleEvents)
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
			payload := []byte("Требуется решение владельца.")
			resultSHA256 := hashString(string(payload))
			resultID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:runtime-output:"+
				execution.ID+":FINAL_MARKDOWN:1:"+resultSHA256)).String()
			archiveSHA256 := hashString("scheduled-runtime-archive")
			archivePath := ".matter-codex/state/codex-home/sessions/2026/08/05/rollout-scheduled.jsonl"
			deliveriesBefore := len(fixture.tx.deliveries)
			completePrincipal := admitPrincipal
			completePrincipal.Permission = permissionRuntimeComplete
			waiting, err := fixture.service.CompleteRuntimeExecution(
				context.Background(), CompleteRuntimeExecutionInput{
					RuntimeExecutionInput: RuntimeExecutionInput{
						Principal: completePrincipal, IdempotencyKey: "complete-scheduled-requires-owner",
						ExecutionID: admitted.Execution.ID, ExpectedVersion: admitted.Execution.Version,
						ExpectedFence: admitted.Execution.Fence, ExpectedGrantGeneration: fixture.grant,
						LeaseToken: admitted.LeaseToken,
					},
					Outcome: "SUCCEEDED", ScheduledOutcome: "requires_human",
					TerminalReference: "codex://sessions/" + fixture.sessionID + "/executions/" + execution.ID,
					TerminalSHA256:    hashString("scheduled-runtime-terminal"),
					Outputs: []RuntimeOutput{{
						Kind: "FINAL_MARKDOWN", ArtifactID: resultID,
						ArtifactVersion: 1, ArtifactSHA256: resultSHA256, ArtifactName: "result.md",
						ArtifactMediaType: "text/markdown", ArtifactPayload: payload,
						ArtifactSizeBytes: uint64(len(payload)), Sequence: 1, Total: 1,
					}},
					CodexSessionID: uuid.NewString(), ArchiveRelativePath: archivePath,
					ArchiveSHA256: archiveSHA256,
					ArchiveProvenance: "codex-app-server-rollout-v1:" + execution.ID + ":" +
						archivePath + ":" + archiveSHA256,
				},
			)
			if err != nil || waiting.State != "SUSPENDED" || waiting.TerminalOutcome != "SUSPENDED" {
				t.Fatalf("scheduled requires_human completion: %v %+v", err, waiting)
			}
			waitingTurn := fixture.tx.resources[produced.turn.ID]
			waitingProcess := fixture.tx.resources[produced.process.ID]
			waitingOccurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
			waitingRun := fixture.tx.runs[turnAttemptMapKey(waitingOccurrence.ID, waitingOccurrence.Attempt)]
			gate, gateErr := fixture.tx.ActiveOwnerGateForProcess(
				context.Background(), fixture.organization, fixture.project, produced.process.ID,
			)
			gateSpec, gateOK := gate.Spec.(entity.OwnerGateSpec)
			if gateErr != nil || !gateOK || waitingTurn.State != enum.StateWaitingOwner ||
				waitingProcess.State != enum.StateWaitingOwner || waitingOccurrence.State != "WAITING_OWNER" ||
				waitingRun.State != "WAITING_OWNER" || waitingRun.Outcome != "requires_human" ||
				waitingRun.ResultArtifactID != resultID || gateSpec.NotificationRoomID != fixture.chatID ||
				len(fixture.tx.deliveries) != deliveriesBefore {
				t.Fatalf("requires_human graph/route is incomplete: gate=%+v turn=%+v process=%+v occurrence=%+v run=%+v",
					gate, waitingTurn, waitingProcess, waitingOccurrence, waitingRun)
			}
			gateSpec.DeliveryFence = 1
			gateSpec.DeliveryClaimTokenSHA256 = hashString("scheduled-owner-gate-token")
			gateSpec.DeliveryClaimKeySHA256 = hashString("scheduled-owner-gate-key")
			gateSpec.DeliveryClaimExpiresAt = fixture.tx.now.Add(time.Minute)
			gateSpec.MattermostPostID = "post-scheduled-owner-gate"
			gateSpec.MattermostChannelID = "channel-scheduled-owner-gate"
			gateSpec.MattermostRootPostID = "root-scheduled-owner-gate"
			gateSpec.DeliveryProviderReceiptSHA256 = hashString("scheduled-owner-gate-provider-readback")
			gateSpec.DeliveredAt = fixture.tx.now
			deliveredGate, err := gate.Update(gate.Name, gateSpec, fixture.tx.now)
			if err != nil {
				t.Fatalf("materialize owner-gate delivery receipt: %v", err)
			}
			fixture.tx.resources[gate.ID] = deliveredGate
			decisionPrincipal := fixture.principal(permissionResolveGate,
				controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID)
			decision, err := fixture.service.ResolveOwnerGate(context.Background(), ResolveOwnerGateInput{
				Principal: decisionPrincipal, IdempotencyKey: "approve-scheduled-owner-gate",
				OwnerGateID: deliveredGate.ID, ExpectedVersion: deliveredGate.Version,
				Decision: "APPROVED", Reason: "Результат принят владельцем.",
			})
			if err != nil {
				t.Fatalf("approve scheduled owner gate: %v", err)
			}
			approvedOccurrence := fixture.tx.occurrences[waitingOccurrence.ID]
			approvedRun := fixture.tx.runs[turnAttemptMapKey(waitingOccurrence.ID, waitingOccurrence.Attempt)]
			if decision.OwnerGate.State != enum.StateSucceeded || decision.Process.State != enum.StateSucceeded ||
				fixture.tx.resources[waitingTurn.ID].State != enum.StateSucceeded ||
				approvedOccurrence.State != "SUCCEEDED" || approvedOccurrence.Outcome != "action_taken" ||
				approvedRun.State != "SUCCEEDED" || approvedRun.Outcome != "action_taken" {
				t.Fatalf("owner decision did not close exact scheduled graph: decision=%+v occurrence=%+v run=%+v",
					decision, approvedOccurrence, approvedRun)
			}
		})
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
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		pausedSchedule.ID, pausedSchedule.Version,
		pausedSpec.EffectiveInputSHA, fixture.grant,
	)
	if result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: scheduler, IdempotencyKey: "claim-paused-retry",
		},
	); !errors.Is(err, errs.ErrStateConflict) || result.MaterializationCapability != "" {
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
		permissionClaimSchedule, "scheduler",
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
	deleted, err := fixture.manageScheduleAction(
		"DELETE", produced.schedule.ID, "delete-terminal-graph",
	)
	if err != nil || deleted.State != enum.StateDeleted {
		t.Fatalf("DELETE after terminal archive: %v %+v", err, deleted)
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
		expectedMetric     string
	}{
		{
			name: "expired live graph requeues", maximumAttempts: 3,
			expectedState: "QUEUED", expectedAttempt: 2,
			expectedRunOutcome: "failed", expectedMetric: "requeue",
		},
		{
			name: "terminal winner reaches dead letter", maximumAttempts: 1,
			terminalTurn: true, expectedState: "DEAD_LETTER", expectedAttempt: 1,
			expectedRunOutcome: "failed", expectedMetric: "dead_letter",
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
						permissionClaimSchedule, "scheduler",
						"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
						schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
					),
					IdempotencyKey: "claim-after-no-next-recovery",
				},
			)
			if !errors.Is(err, errs.ErrNotFound) || result.MaterializationCapability != "" {
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
			if !slices.Contains(fixture.observer.maintenance, scenario.expectedMetric) {
				t.Fatalf("committed maintenance effect is absent after NotFound: %v",
					fixture.observer.maintenance)
			}
			stale, staleErr := fixture.service.ClaimScheduleOccurrence(
				context.Background(), ClaimScheduleOccurrenceInput{
					Principal: fixture.principalFor(
						permissionClaimSchedule, "scheduler",
						"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
						schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
					),
					IdempotencyKey: "claim-occurrence-current-digest",
				},
			)
			if staleErr != nil || stale.Disposition != ScheduleOccurrenceClaimRetired ||
				stale.MaterializationCapability != "" {
				t.Fatalf("recovery exposed stale scheduler authority: %v %+v", staleErr, stale)
			}
		})
	}
}

func TestInvalidExpiredOccurrenceRecoveryDoesNotBlockBacklog(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	valid := fixture.createNextDueOccurrence(t, produced, "behind-invalid-recovery")
	invalid := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	invalid.ExecutionTurnID = ""
	invalid.LeaseExpiresAt = fixture.tx.now.Add(-time.Microsecond)
	fixture.tx.occurrences[invalid.ID] = invalid

	schedule := fixture.tx.resources[valid.ScheduleID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	principal := fixture.principalFor(
		permissionClaimSchedule,
		"scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID,
		schedule.Version,
		spec.EffectiveInputSHA,
		fixture.grant,
	)
	auditsBefore := len(fixture.tx.audits)
	claimed, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(),
		ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-behind-invalid-recovery",
		},
	)
	if err != nil || claimed.Occurrence.ID != valid.ID || claimed.MaterializationCapability == "" {
		t.Fatalf("invalid recovery row blocked valid backlog: %v %+v", err, claimed)
	}
	if len(fixture.tx.audits) <= auditsBefore {
		t.Fatal("blocked recovery audit is missing")
	}
	blockedAudits := 0
	for _, audit := range fixture.tx.audits[auditsBefore:] {
		if audit.ResourceID == invalid.ID &&
			audit.Action == "repair_schedule_occurrence_binding" {
			blockedAudits++
		}
	}
	if blockedAudits != 1 {
		t.Fatalf("repaired recovery audit cardinality is %d", blockedAudits)
	}

	auditsAfter := len(fixture.tx.audits)
	if _, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(),
		ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "repeat-invalid-recovery",
		},
	); !errors.Is(err, errs.ErrNotFound) || len(fixture.tx.audits) <= auditsAfter {
		t.Fatalf("repaired recovery did not reach a bounded owner disposition: err=%v audits=%d->%d",
			err, auditsAfter, len(fixture.tx.audits))
	}
	recovered := fixture.tx.occurrences[invalid.ID]
	if recovered.State == "CLAIMED" && recovered.ExecutionTurnID == "" &&
		!recovered.LeaseExpiresAt.After(fixture.tx.now) {
		t.Fatalf("broken expired occurrence remained an open blocker: %+v", recovered)
	}
	auditsAfter = len(fixture.tx.audits)
	if _, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(),
		ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "repeat-repaired-recovery-stable",
		},
	); !errors.Is(err, errs.ErrNotFound) || len(fixture.tx.audits) != auditsAfter {
		t.Fatalf("repaired recovery repeated audit or exposed work: %v", err)
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
				permissionClaimSchedule, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
			),
			IdempotencyKey: "claim-after-only-overlap-skip",
		},
	)
	if !errors.Is(err, errs.ErrNotFound) || result.MaterializationCapability != "" {
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
				permissionClaimSchedule, "scheduler",
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

func TestInvalidQueuedOccurrenceDoesNotBlockNextSchedule(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	invalid := fixture.createNextDueOccurrence(t, produced, "invalid-row")
	valid := fixture.createNextDueOccurrence(t, produced, "valid-row")

	invalid.PromptProfileID = uuid.NewString()
	invalid.AvailableAt = fixture.tx.now.Add(-2 * time.Second)
	invalid.ScheduledFor = invalid.AvailableAt
	fixture.tx.occurrences[invalid.ID] = invalid
	valid.AvailableAt = fixture.tx.now.Add(-time.Second)
	valid.ScheduledFor = valid.AvailableAt
	fixture.tx.occurrences[valid.ID] = valid

	schedule := fixture.tx.resources[valid.ScheduleID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	auditsBefore := len(fixture.tx.audits)
	result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(),
		ClaimScheduleOccurrenceInput{
			Principal: fixture.principalFor(
				permissionClaimSchedule,
				"scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID,
				schedule.Version,
				spec.EffectiveInputSHA,
				fixture.grant,
			),
			IdempotencyKey: "claim-after-invalid-row",
		},
	)
	if err != nil || result.Occurrence.ID != valid.ID || result.MaterializationCapability == "" {
		t.Fatalf("valid occurrence behind invalid row was not claimed: %v %+v", err, result)
	}
	isolated := fixture.tx.occurrences[invalid.ID]
	if isolated.State != "DEAD_LETTER" || isolated.Outcome != "materialization_invalid" ||
		occurrenceHasExecutionBinding(isolated) || len(fixture.tx.audits) <= auditsBefore {
		t.Fatalf("invalid occurrence was not isolated by owner transaction: %+v", isolated)
	}
	foundIsolationAudit := false
	for _, audit := range fixture.tx.audits[auditsBefore:] {
		if audit.ResourceID == invalid.ID &&
			audit.Action == "dead_letter_invalid_schedule_occurrence" {
			foundIsolationAudit = true
		}
	}
	if !foundIsolationAudit {
		t.Fatal("invalid occurrence isolation audit is missing")
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
		permissionClaimSchedule, "scheduler",
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
	if !errors.Is(err, errs.ErrNotFound) || blocked.MaterializationCapability != "" ||
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
		claimed.Occurrence.State != "RESERVED" || claimed.MaterializationCapability == "" {
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
		permissionClaimSchedule, "scheduler",
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
		finishedRun.State != "FAILED" || finishedRun.Outcome != "failed" ||
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
		permissionUseScheduleCapability, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		staleSchedule.ID, staleSchedule.Version,
		staleSpec.EffectiveInputSHA, fixture.grant,
	)
	stale, err := fixture.service.CompleteScheduleOccurrence(
		context.Background(), CompleteScheduleOccurrenceInput{
			Principal: stalePrincipal, IdempotencyKey: "stale-after-watchdog-winner",
			OccurrenceID: previous.ID, CompletionCapability: produced.claim.CompletionCapability,
			ExpectedAttempt: previous.Attempt,
			ProjectID:       fixture.project,
		},
	)
	if !errors.Is(err, errs.ErrPermissionDenied) || stale.TokenHash != "" {
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
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	reservedNext, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: scheduler, IdempotencyKey: "claim-after-watchdog-retry",
		},
	)
	if err != nil || reservedNext.Occurrence.ID != previous.ID ||
		reservedNext.Occurrence.Attempt != 2 || reservedNext.Occurrence.State != "RESERVED" {
		t.Fatalf("watchdog retry was not reserved: %v %+v", err, reservedNext)
	}
	next := fixture.materializeReservedOccurrence(t, reservedNext, "materialize-after-watchdog-retry")
	if next.Occurrence.ID != previous.ID || next.Occurrence.Attempt != 2 ||
		next.Occurrence.EffectiveInputSHA256 == produced.snapshotDigest {
		t.Fatalf("watchdog retry did not rematerialize execution: %+v", next)
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
				permissionClaimSchedule, "scheduler",
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
		run.State != "FAILED" || run.Outcome != "failed" {
		t.Fatalf("watchdog retry limit left open authority: occurrence=%+v run=%+v",
			deadLetter, run)
	}
}

func TestDeliveryRecoverySourceRequiresExactFailedTerminalReference(t *testing.T) {
	executionID, sessionID := uuid.NewString(), uuid.NewString()
	lineage := domainrepo.CodexLineage{
		ExecutionID: executionID, SessionID: sessionID,
		TerminalOutcome: "FAILED", TerminalReference: "codex://sessions/" + sessionID +
			"/executions/" + executionID + "/delivery-recovery",
	}
	if actual := deliveryRecoverySource(lineage); actual != executionID {
		t.Fatalf("delivery recovery source = %q", actual)
	}
	originalSource := uuid.NewString()
	lineage.TerminalReference += "/source/" + originalSource
	if actual := deliveryRecoverySource(lineage); actual != originalSource {
		t.Fatalf("retained delivery recovery source = %q", actual)
	}
	lineage.TerminalOutcome = "SUCCEEDED"
	if actual := deliveryRecoverySource(lineage); actual != "" {
		t.Fatalf("successful terminal authorized recovery: %q", actual)
	}
	lineage.TerminalOutcome = "FAILED"
	lineage.TerminalReference = "codex://sessions/" + sessionID + "/executions/" + executionID
	if actual := deliveryRecoverySource(lineage); actual != "" {
		t.Fatalf("ordinary failed terminal authorized delivery recovery: %q", actual)
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
			previousProcess := fixture.tx.resources[produced.process.ID]
			previousProcessSpec := previousProcess.Spec.(entity.ProcessRunSpec)
			previousProcessSpec.Outcome = scenario.outcome
			closedProcess, transitionErr := previousProcess.ReplaceAndTransition(
				previousProcessSpec, enum.StateFailed, fixture.tx.now,
			)
			if transitionErr != nil {
				t.Fatalf("close previous ProcessRun: %v", transitionErr)
			}
			fixture.tx.resources[closedProcess.ID] = closedProcess

			scheduler := fixture.principalFor(
				permissionClaimSchedule, "scheduler",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
				schedule.ID, schedule.Version, produced.snapshotDigest, fixture.grant,
			)
			stale, err := fixture.service.ClaimScheduleOccurrence(
				context.Background(), ClaimScheduleOccurrenceInput{
					Principal: scheduler, IdempotencyKey: "claim-occurrence-current-digest",
				},
			)
			if err != nil || stale.Disposition != ScheduleOccurrenceClaimRetired ||
				stale.MaterializationCapability != "" {
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
			materialized := fixture.materializeReservedOccurrence(t, next,
				"materialize-requeued-"+scenario.name)
			if materialized.Occurrence.Attempt != previousAttempt+1 ||
				materialized.Occurrence.EffectiveInputSHA256 == produced.snapshotDigest ||
				materialized.Occurrence.EffectiveInputSHA256 == previousDigest {
				t.Fatalf("next attempt did not materialize a fresh execution digest: %+v", materialized)
			}
			if materialized.Occurrence.ExecutionProcessRunID != produced.process.ID {
				t.Fatalf("retry created a second root ProcessRun: %+v", materialized.Occurrence)
			}
			nextRun := fixture.tx.runs[turnAttemptMapKey(materialized.Occurrence.ID, materialized.Occurrence.Attempt)]
			if nextRun.EffectiveInputSHA256 != produced.snapshotDigest ||
				nextRun.CurrentInputSHA256 != materialized.Occurrence.EffectiveInputSHA256 {
				t.Fatalf("next run lost snapshot/current digest separation: %+v", nextRun)
			}
			turn := fixture.tx.resources[materialized.Occurrence.ExecutionTurnID]
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
		{"specialized.go", "MaterializeScheduleOccurrence", "materializeScheduledOccurrence"},
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
	fixture.tx.receipts[receiptMapKey("complete_schedule_occurrence", keyHash)] = domainrepo.Receipt{
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

func TestScheduledClaimRejoinsCommittedMaterializationWithoutSecondGraph(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	principal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	processesBefore := 0
	for _, current := range fixture.tx.resources {
		if current.Kind == enum.KindProcessRun {
			processesBefore++
		}
	}
	auditsBefore, eventsBefore, runsBefore :=
		len(fixture.tx.audits), len(fixture.tx.events), len(fixture.tx.runs)

	rejoined, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if err != nil || rejoined.Disposition != ScheduleOccurrenceClaimMaterialized ||
		rejoined.Occurrence.ID != produced.claim.Occurrence.ID ||
		rejoined.MaterializationCapability == "" {
		t.Fatalf("committed materialization did not rejoin: %v %+v", err, rejoined)
	}
	materializePrincipal := principal
	materializePrincipal.Permission = permissionUseScheduleCapability
	materialized, err := fixture.service.MaterializeScheduleOccurrence(
		context.Background(), MaterializeScheduleOccurrenceInput{
			Principal:      materializePrincipal,
			IdempotencyKey: rejoined.MaterializationIdempotencyKey,
			OccurrenceID:   rejoined.Occurrence.ID, ProjectID: rejoined.ProjectID,
			ExpectedAttempt:           rejoined.Occurrence.Attempt,
			MaterializationCapability: rejoined.MaterializationCapability,
		},
	)
	if err != nil || materialized != produced.claim {
		t.Fatalf("exact materialization replay failed: %v %+v", err, materialized)
	}
	processesAfter := 0
	for _, current := range fixture.tx.resources {
		if current.Kind == enum.KindProcessRun {
			processesAfter++
		}
	}
	if processesBefore != 1 || processesAfter != processesBefore ||
		len(fixture.tx.runs) != runsBefore || len(fixture.tx.audits) != auditsBefore ||
		len(fixture.tx.events) != eventsBefore {
		t.Fatalf("rejoin repeated graph effects: processes=%d/%d runs=%d/%d audits=%d/%d events=%d/%d",
			processesBefore, processesAfter, runsBefore, len(fixture.tx.runs),
			auditsBefore, len(fixture.tx.audits), eventsBefore, len(fixture.tx.events))
	}
}

func TestExpiredScheduleReservationRetiresKeyAndAdvancesGeneration(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	queued := fixture.createNextDueOccurrence(t, produced, "expired-reservation")
	schedule := fixture.tx.resources[queued.ScheduleID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	principal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)
	// Сохраняем независимый исходный graph live, чтобы проверка наблюдала именно
	// release просроченной reservation side schedule, а не retry другой строки.
	original := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	original.LeaseExpiresAt = fixture.tx.now.Add(time.Hour)
	fixture.tx.occurrences[original.ID] = original
	reserved, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "expired-reservation-old-key",
		},
	)
	if err != nil || reserved.Occurrence.ID != queued.ID ||
		reserved.Disposition != ScheduleOccurrenceClaimReserved {
		t.Fatalf("reserve occurrence before expiry: %v %+v", err, reserved)
	}
	oldGeneration := reserved.Occurrence.AuthorityGeneration
	fixture.tx.now = reserved.CapabilityExpiresAt.Add(time.Microsecond)
	retired, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "expired-reservation-old-key",
		},
	)
	if err != nil || retired.Disposition != ScheduleOccurrenceClaimRetired ||
		retired.MaterializationCapability != "" || retired.Occurrence.ID != "" {
		t.Fatalf("expired reservation did not return closed retirement: %v %+v", err, retired)
	}
	advanced, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "expired-reservation-new-key",
		},
	)
	if err != nil || advanced.Occurrence.ID != queued.ID ||
		advanced.Disposition != ScheduleOccurrenceClaimReserved ||
		advanced.Occurrence.AuthorityGeneration != oldGeneration+1 ||
		advanced.MaterializationCapability == reserved.MaterializationCapability {
		t.Fatalf("new key did not release/recover backlog with fresh generation: %v %+v current=%+v capabilities=%+v maintenance=%v",
			err, advanced, fixture.tx.occurrences[queued.ID], fixture.tx.capabilities,
			fixture.observer.maintenance)
	}
	if !slices.Contains(fixture.observer.maintenance, "reservation_expired") {
		t.Fatalf("reservation watchdog effect is not observable: %v", fixture.observer.maintenance)
	}
}

func TestScheduleClaimMismatchNeverBecomesRetiredSuccess(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	schedule := fixture.tx.resources[produced.schedule.ID]
	spec := schedule.Spec.(entity.ScheduleSpec)
	principal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, spec.EffectiveInputSHA, fixture.grant,
	)

	deniedPrincipal := principal
	deniedPrincipal.Permission = permissionClaimTurn
	denied, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: deniedPrincipal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if !errors.Is(err, errs.ErrPermissionDenied) || denied.Disposition != "" {
		t.Fatalf("permission mismatch became disposition success: %v %+v", err, denied)
	}
	receiptKey := receiptMapKey(
		"claim_schedule_occurrence", hashString("claim-occurrence-current-digest"),
	)
	receipt := fixture.tx.receipts[receiptKey]
	originalRequestHash := receipt.RequestHash
	receipt.RequestHash = hashString("different-semantic-intent")
	fixture.tx.receipts[receiptKey] = receipt
	conflicted, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if !errors.Is(err, errs.ErrIdempotencyConflict) || conflicted.Disposition != "" {
		t.Fatalf("idempotency mismatch became disposition success: %v %+v", err, conflicted)
	}
	receipt.RequestHash = originalRequestHash
	fixture.tx.receipts[receiptKey] = receipt
	for tokenHash, capability := range fixture.tx.capabilities {
		if capability.OccurrenceID == produced.claim.Occurrence.ID &&
			capability.FullMethod == materializeScheduleOccurrenceMethod {
			delete(fixture.tx.capabilities, tokenHash)
		}
	}
	broken, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if !errors.Is(err, errs.ErrStateConflict) || broken.Disposition != "" {
		t.Fatalf("missing exact tuple became retired/empty success: %v %+v", err, broken)
	}
}

func TestScheduledProducerReplayAfterLeaseExpiryReturnsRetiredWithoutReceiptExposure(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	fixture.tx.now = produced.claim.Occurrence.LeaseExpiresAt.Add(time.Microsecond)
	schedule := fixture.tx.resources[produced.schedule.ID]
	principal := fixture.principalFor(
		permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		schedule.ID, schedule.Version, produced.snapshotDigest, fixture.grant,
	)
	audits, events := len(fixture.tx.audits), len(fixture.tx.events)
	result, err := fixture.service.ClaimScheduleOccurrence(
		context.Background(), ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "claim-occurrence-current-digest",
		},
	)
	if err != nil || result.Disposition != ScheduleOccurrenceClaimRetired ||
		result.MaterializationCapability != "" {
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
