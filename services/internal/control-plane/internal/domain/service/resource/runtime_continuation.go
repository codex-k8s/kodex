package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	minimumApprovalLifetime       = time.Minute
	maximumApprovalLifetime       = 7 * 24 * time.Hour
	cleanupAuthorizationLifetime  = 15 * time.Minute
	integrationPredecessorOutcome = "integration_continuation_materialized"
)

var scheduledGraphLockOrder = [...]string{
	graphLockRuntimeExecution, graphLockOccurrence, graphLockSchedule,
	graphLockScheduledRun, graphLockSession, graphLockTurn, graphLockProcessRun,
	graphLockPinnedResource, graphLockOwnerGate, graphLockContinuation,
}

type resolvedExecution struct {
	Turn         entity.Resource
	TurnSpec     entity.TurnSpec
	Session      entity.Resource
	SessionSpec  entity.SessionSpec
	Process      entity.Resource
	ProcessSpec  entity.ProcessRunSpec
	Revision     entity.Resource
	RevisionSpec entity.RuntimeRevisionSpec
	Role         entity.Resource
	RoleSpec     entity.RoleSpec
	RevisionHash string
	Runtime      *RuntimeExecution
}

type runtimeExecutionIntent struct {
	ExecutionID             string
	ExpectedVersion         uint64
	ExpectedFence           uint64
	ExpectedGrantGeneration uint64
	LeaseTokenSHA256        string
}

func runtimeIntent(input RuntimeExecutionInput) runtimeExecutionIntent {
	leaseTokenSHA256 := ""
	if input.LeaseToken != "" {
		leaseTokenSHA256 = hashString(input.LeaseToken)
	}
	return runtimeExecutionIntent{
		ExecutionID:             input.ExecutionID,
		ExpectedVersion:         input.ExpectedVersion,
		ExpectedFence:           input.ExpectedFence,
		ExpectedGrantGeneration: input.ExpectedGrantGeneration,
		LeaseTokenSHA256:        leaseTokenSHA256,
	}
}

type suspendIntegrationIntent struct {
	InvocationID       string
	ApprovalID         string
	IntegrationID      string
	IntegrationVersion uint64
	IntegrationSHA256  string
	CredentialBindings []PinnedIntegrationResource
	RequestSHA256      string
	ApprovalExpiresAt  time.Time
}

type integrationDecisionIntent struct {
	ContinuationID    string
	ExpectedVersion   uint64
	ExpectedFence     uint64
	InvocationID      string
	ApprovalID        string
	RequestSHA256     string
	DecisionReference string
	DecisionSHA256    string
	Decision          string
}

type integrationExecutionIntent struct {
	ContinuationID  string
	ExpectedVersion uint64
	ExpectedFence   uint64
	InvocationID    string
	RequestSHA256   string
	ResultReference string
	ResultSHA256    string
	ErrorCode       string
	ErrorReference  string
	ErrorSHA256     string
	Target          string
}

type acknowledgeIntegrationIntent struct {
	ExpectedVersion     uint64
	ExpectedFence       uint64
	ExpectedInputSHA256 string
}

type lifecycleReceiptDisposition uint8

const (
	lifecycleReceiptApply lifecycleReceiptDisposition = iota + 1
	lifecycleReceiptReplay
	lifecycleReceiptApplyOrReplay
)

type lifecycleReceiptValidation func(
	domainrepo.Transaction,
) (lifecycleReceiptDisposition, error)

// withLifecycleReceipt сначала разрешает и блокирует актуальное owner state.
// Receipt не является authority: сохранённый result можно прочитать только
// когда validate доказал, что текущая строка всё ещё представляет именно этот
// результат. При отсутствии receipt effect разрешён только из source state.
func (service *Service) withLifecycleReceipt(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey, scope, requestHash string,
	result any,
	validate lifecycleReceiptValidation,
	validateReplay func() error,
	apply func(domainrepo.Transaction) error,
) error {
	keyHash := hashString(idempotencyKey)
	return service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID,
		ProjectID:      principal.ProjectID,
		ActorID:        principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		disposition, err := validate(tx)
		if err != nil {
			return err
		}
		receipt, err := tx.GetReceipt(ctx, principal.OrganizationID, scope, keyHash)
		if err == nil {
			if disposition != lifecycleReceiptReplay &&
				disposition != lifecycleReceiptApplyOrReplay {
				return errs.ErrStateConflict
			}
			if receipt.RequestHash != requestHash ||
				len(receipt.Payload) == 0 {
				return errs.ErrIdempotencyConflict
			}
			if json.Unmarshal(receipt.Payload, result) != nil {
				return errs.ErrInternal
			}
			return validateReplay()
		}
		if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
		if disposition != lifecycleReceiptApply &&
			disposition != lifecycleReceiptApplyOrReplay {
			return errs.ErrStateConflict
		}
		if err := apply(tx); err != nil {
			return err
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: principal.OrganizationID,
			ProjectID:      principal.ProjectID,
			Scope:          scope,
			KeyHash:        keyHash,
			RequestHash:    requestHash,
			Payload:        payload,
			CreatedAt:      service.now().UTC().Truncate(time.Microsecond),
		})
	})
}

func (service *Service) appendLifecycleAudit(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	action, resourceID, resourceKind string,
	version uint64,
	when time.Time,
) error {
	return tx.AppendAudit(ctx, domainrepo.Audit{
		ID:              uuid.NewString(),
		OrganizationID:  principal.OrganizationID,
		ProjectID:       principal.ProjectID,
		ActorID:         principal.ActorID,
		Action:          action,
		ResourceID:      resourceID,
		ResourceKind:    resourceKind,
		ResourceVersion: version,
		Outcome:         "succeeded",
		CorrelationID:   principal.CorrelationID,
		PolicyRevision:  principal.PolicyRevision,
		OccurredAt:      when,
	})
}

func (service *Service) resolveBoundExecution(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
) (resolvedExecution, error) {
	if value.ValidateID(principal.AuthorityReference) != nil ||
		principal.AuthorityRevision == 0 || principal.AuthorityGrantGeneration == 0 ||
		!validSHA256Text(principal.AuthorityDigest) {
		return resolvedExecution{}, errs.ErrPermissionDenied
	}
	graph, err := service.lockOwnerGraphByTurn(
		ctx, tx, principal, principal.AuthorityReference,
	)
	if err != nil {
		return resolvedExecution{}, err
	}
	turn, session, process := graph.Turn, graph.Session, graph.Process
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		turnSpec.Attempt != uint32(principal.AuthorityRevision) ||
		turnSpec.EffectiveInputSHA256 != principal.AuthorityDigest ||
		turnSpec.ProcessRunID == "" {
		return resolvedExecution{}, errs.ErrNotFound
	}
	if graph.Runtime != nil &&
		(graph.Runtime.RuntimeRevisionID != turnSpec.RuntimeRevisionID ||
			graph.Runtime.GrantGeneration != principal.AuthorityGrantGeneration) {
		return resolvedExecution{}, errs.ErrNotFound
	}
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.Kind != enum.KindSession || session.State != enum.StateActive ||
		session.OwnerActorID != turn.OwnerActorID {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		process.OwnerActorID != turn.OwnerActorID || process.State.Terminal() ||
		current.SessionID != session.ID || current.SessionVersion != session.Version ||
		current.TurnID != turn.ID || current.TurnVersion != turn.Version ||
		current.Attempt != turnSpec.Attempt ||
		current.RuntimeRevisionID != turnSpec.RuntimeRevisionID ||
		current.InputSHA256 != turnSpec.EffectiveInputSHA256 {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	// Lease/attempt являются дочерними строками уже заблокированного Turn и
	// берутся только после полного owner graph, чтобы не образовать inversion.
	lease, err := tx.GetTurnLeaseForUpdate(ctx, turn.ID)
	if err != nil {
		return resolvedExecution{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, turnSpec.Attempt)
	if err != nil {
		return resolvedExecution{}, err
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return resolvedExecution{}, err
	}
	if lease.Attempt != turnSpec.Attempt ||
		lease.WorkloadID != agentRunnerWorkload ||
		lease.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		!lease.ExpiresAt.After(now) ||
		attempt.WorkloadID != agentRunnerWorkload ||
		attempt.LeaseFence != lease.Fence ||
		attempt.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		attempt.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
		!attempt.FinishedAt.IsZero() {
		return resolvedExecution{}, errs.ErrNotFound
	}
	revision, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, turnSpec.RuntimeRevisionID,
	)
	if err != nil {
		return resolvedExecution{}, err
	}
	if revision.Kind != enum.KindRuntimeRevision || revision.State != enum.StateActive ||
		revision.Version != current.RuntimeRevisionVersion {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok {
		return resolvedExecution{}, errs.ErrInternal
	}
	revisionHash, err := entity.ProjectionSHA256(revision)
	if err != nil {
		return resolvedExecution{}, errs.ErrInternal
	}
	role, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, sessionSpec.AgentID,
	)
	if err != nil {
		return resolvedExecution{}, err
	}
	roleSpec, ok := role.Spec.(entity.RoleSpec)
	if !ok || role.Kind != enum.KindRole || role.State != enum.StateActive {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	if revisionSpec.SessionID != session.ID || revisionSpec.RoleID != role.ID {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	var roleComponent entity.EffectiveResourceRef
	roleComponentCount := 0
	for _, component := range revisionSpec.Components {
		if component.Kind == enum.KindRole && component.ResourceID == role.ID {
			roleComponent = component
			roleComponentCount++
		}
	}
	roleMatches, matchErr := revisionComponentMatches(role, roleComponent)
	if matchErr != nil {
		return resolvedExecution{}, matchErr
	}
	if roleComponentCount != 1 || !roleMatches {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	resolved := resolvedExecution{
		Turn: turn, TurnSpec: turnSpec, Session: session, SessionSpec: sessionSpec,
		Process: process, ProcessSpec: processSpec, Revision: revision,
		RevisionSpec: revisionSpec,
		Role:         role, RoleSpec: roleSpec, RevisionHash: revisionHash,
	}
	resolved.Runtime = graph.Runtime
	return resolved, nil
}

func runtimeResourcePolicy(role entity.RoleSpec) (string, string) {
	resourceClass := "STANDARD"
	clusterProfile := "NONE"
	if slices.Contains(role.Capabilities, "runtime.resource.high-memory") {
		resourceClass = "HIGH_MEMORY"
	}
	if slices.Contains(role.Capabilities, "runtime.resource.accelerated") {
		resourceClass = "ACCELERATED"
	}
	if slices.Contains(role.Capabilities, "runtime.cluster.read") {
		clusterProfile = "PROJECT_READ_ONLY"
	}
	if slices.Contains(role.Capabilities, "runtime.cluster.admin") {
		clusterProfile = "CLUSTER_ADMIN"
	}
	return resourceClass, clusterProfile
}

func (service *Service) ClaimRuntimeExecution(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey string,
) (RuntimeExecution, error) {
	if err := authorize(principal, permissionRuntimeClaim); err != nil {
		return RuntimeExecution{}, err
	}
	if value.ValidateIdempotencyKey(idempotencyKey) != nil ||
		principal.CallerWorkload != service.runtimeControllerWorkload ||
		principal.CallerSPIFFEID != service.runtimeControllerSPIFFEID {
		return RuntimeExecution{}, errs.ErrPermissionDenied
	}
	requestHash, err := semanticCommandHash(principal, struct{}{})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, principal, idempotencyKey, "claim_runtime_execution", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			resolved, err := service.resolveBoundExecution(ctx, tx, principal)
			if err != nil {
				return 0, err
			}
			if resolved.Runtime == nil {
				return lifecycleReceiptApply, nil
			}
			receiptExecution = *resolved.Runtime
			if receiptExecution.State != "PENDING" || receiptExecution.Version != 1 ||
				receiptExecution.Fence != 1 ||
				receiptExecution.GrantGeneration != principal.AuthorityGrantGeneration ||
				receiptExecution.RuntimeRevisionSHA256 != resolved.RevisionHash ||
				receiptExecution.ImmutableInputSHA256 != principal.AuthorityDigest {
				return 0, errs.ErrStateConflict
			}
			return lifecycleReceiptReplay, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			resolved, err := service.resolveBoundExecution(ctx, tx, principal)
			if err != nil {
				return err
			}
			if resolved.Runtime != nil {
				return errs.ErrStateConflict
			}
			resourceClass, clusterProfile := runtimeResourcePolicy(resolved.RoleSpec)
			now := service.now().UTC().Truncate(time.Microsecond)
			threadID := resolved.SessionSpec.ConversationID
			if threadID == "" {
				threadID = resolved.Session.ID
			}
			result = RuntimeExecution{
				ID: uuid.NewString(), OrganizationID: principal.OrganizationID,
				ProjectID: principal.ProjectID, ProcessID: resolved.Process.ID,
				SessionID: resolved.Session.ID, ThreadID: threadID,
				RoleID: resolved.Role.ID, TurnID: resolved.Turn.ID,
				Attempt:                resolved.TurnSpec.Attempt,
				RuntimeRevisionID:      resolved.Revision.ID,
				RuntimeRevisionVersion: resolved.Revision.Version,
				RuntimeRevisionSHA256:  resolved.RevisionHash,
				ImmutableInputSHA256:   resolved.TurnSpec.EffectiveInputSHA256,
				ResourceClass:          resourceClass, ClusterAccessProfile: clusterProfile,
				WorkloadID:       principal.CallerWorkload,
				WorkloadSPIFFEID: principal.CallerSPIFFEID,
				GrantGeneration:  principal.AuthorityGrantGeneration,
				Version:          1, Fence: 1, State: "PENDING",
				CleanupAuthorizationState: "NONE",
				CreatedAt:                 now, UpdatedAt: now,
			}
			if err := tx.InsertRuntimeExecution(ctx, result); err != nil {
				return err
			}
			return service.appendLifecycleAudit(
				ctx, tx, principal, "claim_runtime_execution", result.ID,
				"RUNTIME_EXECUTION", result.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) GetRuntimeExecution(
	ctx context.Context,
	principal value.Principal,
	executionID string,
	expectedVersion uint64,
) (RuntimeExecution, error) {
	if err := authorize(principal, permissionRuntimeRead); err != nil {
		return RuntimeExecution{}, err
	}
	if value.ValidateID(executionID) != nil || expectedVersion == 0 {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		found, err := tx.GetRuntimeExecutionForUpdate(ctx, executionID)
		if err != nil {
			return err
		}
		if found.Version != expectedVersion || found.WorkloadID != principal.CallerWorkload ||
			found.WorkloadSPIFFEID != principal.CallerSPIFFEID ||
			found.GrantGeneration != principal.AuthorityGrantGeneration ||
			found.TurnID != principal.AuthorityReference ||
			found.Attempt != uint32(principal.AuthorityRevision) ||
			found.ImmutableInputSHA256 != principal.AuthorityDigest {
			return errs.ErrNotFound
		}
		result = found
		return nil
	})
	return result, err
}

func (service *Service) AdmitRuntimeExecution(
	ctx context.Context,
	input RuntimeExecutionInput,
) (AdmitRuntimeExecutionResult, error) {
	if err := validateRuntimeMutation(service, input, permissionRuntimeAdmit, true); err != nil {
		return AdmitRuntimeExecutionResult{}, err
	}
	requestHash, err := semanticCommandHash(input.Principal, runtimeIntent(input))
	if err != nil {
		return AdmitRuntimeExecutionResult{}, errs.ErrInvalidInput
	}
	var result AdmitRuntimeExecutionResult
	var receiptExecution RuntimeExecution
	var receiptTurnLease domainrepo.TurnLease
	var receiptNow time.Time
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "admit_runtime_execution",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil {
				return 0, err
			}
			activeNow, turnLease, err := service.requireActiveRuntimeLeaseGraph(
				ctx, tx, input.Principal, locked.Execution,
			)
			if err != nil {
				return 0, err
			}
			receiptExecution = locked.Execution
			receiptTurnLease = turnLease
			receiptNow = activeNow
			return runtimeMutationReceiptDisposition(
				locked.Execution, input, []string{"PENDING"}, []string{"ADMITTED"}, 1,
			)
		},
		func() error {
			return validateAdmitRuntimeReceipt(
				receiptExecution, receiptTurnLease, result, receiptNow,
			)
		},
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input, "PENDING"); err != nil {
				return err
			}
			now, turnLease, err := service.requireActiveRuntimeLeaseGraph(
				ctx, tx, input.Principal, execution,
			)
			if err != nil {
				return err
			}
			leaseToken := uuid.NewString() + uuid.NewString()[0:28]
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.State = "ADMITTED"
			execution.LeaseID = uuid.NewString()
			execution.LeaseTokenSHA256 = hashString(leaseToken)
			execution.LeaseExpiresAt = now.Add(service.turnLeaseDuration)
			execution.UpdatedAt = now
			turnLease.ExpiresAt = execution.LeaseExpiresAt
			if _, err := tx.RenewTurnLease(ctx, turnLease, now); err != nil {
				return err
			}
			if err := tx.UpdateRuntimeExecution(
				ctx, execution, expectedVersion, expectedFence,
			); err != nil {
				return err
			}
			result = AdmitRuntimeExecutionResult{Execution: execution, LeaseToken: leaseToken}
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "admit_runtime_execution", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) HeartbeatRuntimeExecution(
	ctx context.Context,
	input RuntimeExecutionInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(service, input, permissionRuntimeHeartbeat, true); err != nil ||
		len(input.LeaseToken) != 64 {
		if err != nil {
			return RuntimeExecution{}, err
		}
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, runtimeIntent(input))
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	var receiptTurnLease domainrepo.TurnLease
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "heartbeat_runtime_execution",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil {
				return 0, err
			}
			_, turnLease, err := service.requireActiveRuntimeLeaseGraph(
				ctx, tx, input.Principal, locked.Execution,
			)
			if err != nil {
				return 0, err
			}
			if locked.Execution.LeaseTokenSHA256 != hashString(input.LeaseToken) ||
				!locked.Execution.LeaseExpiresAt.After(locked.Now) {
				return 0, errs.ErrStateConflict
			}
			receiptExecution = locked.Execution
			receiptTurnLease = turnLease
			return runtimeMutationReceiptDisposition(
				locked.Execution, input,
				[]string{"ADMITTED", "RUNNING"}, []string{"RUNNING"}, 1,
			)
		},
		func() error {
			if err := runtimeReceiptMatchesCurrent(receiptExecution, result); err != nil {
				return err
			}
			if !receiptTurnLease.ExpiresAt.Equal(receiptExecution.LeaseExpiresAt) {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			now, turnLease, err := service.requireActiveRuntimeLeaseGraph(
				ctx, tx, input.Principal, execution,
			)
			if err != nil {
				return err
			}
			if execution.State != "ADMITTED" && execution.State != "RUNNING" {
				return errs.ErrStateConflict
			}
			if err := matchRuntimeMutation(execution, input); err != nil ||
				execution.LeaseTokenSHA256 != hashString(input.LeaseToken) ||
				!execution.LeaseExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.State = "RUNNING"
			execution.LeaseExpiresAt = now.Add(service.turnLeaseDuration)
			execution.UpdatedAt = now
			turnLease.ExpiresAt = execution.LeaseExpiresAt
			if _, err := tx.RenewTurnLease(ctx, turnLease, now); err != nil {
				return err
			}
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "heartbeat_runtime_execution", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) RecordRuntimeIncident(
	ctx context.Context,
	input RecordRuntimeIncidentInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeIncident, true,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if value.ValidateID(input.IncidentID) != nil || !validSHA256Text(input.EvidenceSHA256) ||
		(input.Kind != "HEARTBEAT_MISSED" && input.Kind != "RECONCILE_FAILED" &&
			input.Kind != "WORKLOAD_UNAVAILABLE") {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime        runtimeExecutionIntent
		Kind           string
		IncidentID     string
		EvidenceSHA256 string
	}{runtimeIntent(input.RuntimeExecutionInput), input.Kind, input.IncidentID, input.EvidenceSHA256})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "record_runtime_incident",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil {
				return 0, err
			}
			if _, err := service.requireActiveRuntimeGraph(
				ctx, tx, input.Principal, locked.Execution,
			); err != nil {
				return 0, err
			}
			receiptExecution = locked.Execution
			return runtimeMutationReceiptDisposition(
				locked.Execution, input.RuntimeExecutionInput,
				[]string{"ADMITTED", "RUNNING"},
				[]string{"ADMITTED", "RUNNING"}, 1,
			)
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			now, err := service.requireActiveRuntimeGraph(
				ctx, tx, input.Principal, execution,
			)
			if err != nil {
				return err
			}
			if execution.State != "ADMITTED" && execution.State != "RUNNING" {
				return errs.ErrStateConflict
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil {
				return err
			}
			if err := tx.InsertRuntimeIncident(ctx, domainrepo.RuntimeIncident{
				ID: input.IncidentID, OrganizationID: execution.OrganizationID,
				ProjectID: execution.ProjectID, ExecutionID: execution.ID,
				ExecutionFence: execution.Fence, Kind: input.Kind,
				EvidenceSHA256: input.EvidenceSHA256,
				WorkloadID:     input.Principal.CallerWorkload, OccurredAt: now,
			}); err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "record_runtime_incident", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func validateRuntimeMutation(
	service *Service,
	input RuntimeExecutionInput,
	permission string,
	requireRuntimeController bool,
) error {
	if err := authorize(input.Principal, permission); err != nil {
		return err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ExecutionID) != nil || input.ExpectedVersion == 0 ||
		input.ExpectedFence == 0 {
		return errs.ErrInvalidInput
	}
	if requireRuntimeController &&
		(input.Principal.CallerWorkload != service.runtimeControllerWorkload ||
			input.Principal.CallerSPIFFEID != service.runtimeControllerSPIFFEID ||
			input.ExpectedGrantGeneration == 0) {
		return errs.ErrPermissionDenied
	}
	return nil
}

func matchRuntimeMutation(
	execution RuntimeExecution,
	input RuntimeExecutionInput,
	states ...string,
) error {
	if execution.Version != input.ExpectedVersion || execution.Fence != input.ExpectedFence {
		return errs.ErrVersionMismatch
	}
	if len(states) != 0 && !slices.Contains(states, execution.State) {
		return errs.ErrStateConflict
	}
	if input.ExpectedGrantGeneration != 0 &&
		execution.GrantGeneration != input.ExpectedGrantGeneration {
		return errs.ErrStateConflict
	}
	if input.Principal.CallerWorkload == execution.WorkloadID &&
		(input.Principal.CallerSPIFFEID != execution.WorkloadSPIFFEID ||
			input.Principal.AuthorityGrantGeneration != execution.GrantGeneration ||
			input.Principal.AuthorityReference != execution.TurnID ||
			input.Principal.AuthorityRevision != uint64(execution.Attempt) ||
			input.Principal.AuthorityDigest != execution.ImmutableInputSHA256) {
		return errs.ErrNotFound
	}
	return nil
}

type lockedRuntimeReceipt struct {
	Execution RuntimeExecution
	Graph     lockedOwnerGraph
	Now       time.Time
}

func (service *Service) lockRuntimeReceiptAuthority(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	executionID string,
) (lockedRuntimeReceipt, error) {
	execution, err := tx.GetRuntimeExecutionForUpdate(ctx, executionID)
	if err != nil {
		return lockedRuntimeReceipt{}, err
	}
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, execution.TurnID)
	if err != nil {
		return lockedRuntimeReceipt{}, err
	}
	if graph.Runtime == nil || graph.Runtime.ID != execution.ID ||
		graph.Runtime.Version != execution.Version || graph.Runtime.Fence != execution.Fence ||
		graph.Session.ID != execution.SessionID || graph.Turn.ID != execution.TurnID ||
		graph.Process.ID != execution.ProcessID {
		return lockedRuntimeReceipt{}, errs.ErrStateConflict
	}
	if err := service.requireRuntimeOwner(ctx, tx, principal, execution); err != nil {
		return lockedRuntimeReceipt{}, err
	}
	if err := requireExactRuntimeApplicationAuthority(execution, principal); err != nil {
		return lockedRuntimeReceipt{}, err
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return lockedRuntimeReceipt{}, err
	}
	return lockedRuntimeReceipt{Execution: execution, Graph: graph, Now: now}, nil
}

func runtimeMutationReceiptDisposition(
	execution RuntimeExecution,
	input RuntimeExecutionInput,
	sourceStates []string,
	replayStates []string,
	replayVersionDeltas ...uint64,
) (lifecycleReceiptDisposition, error) {
	if input.ExpectedGrantGeneration != 0 &&
		execution.GrantGeneration != input.ExpectedGrantGeneration {
		return 0, errs.ErrStateConflict
	}
	if execution.Version == input.ExpectedVersion && execution.Fence == input.ExpectedFence {
		if slices.Contains(sourceStates, execution.State) {
			return lifecycleReceiptApply, nil
		}
		return 0, errs.ErrStateConflict
	}
	for _, delta := range replayVersionDeltas {
		if delta != 0 && execution.Version == input.ExpectedVersion+delta &&
			execution.Fence == input.ExpectedFence+delta &&
			slices.Contains(replayStates, execution.State) {
			return lifecycleReceiptReplay, nil
		}
	}
	return 0, errs.ErrVersionMismatch
}

func runtimeReceiptMatchesCurrent(current, stored RuntimeExecution) error {
	if current != stored {
		return errs.ErrStateConflict
	}
	return nil
}

func validateAdmitRuntimeReceipt(
	current RuntimeExecution,
	turnLease domainrepo.TurnLease,
	stored AdmitRuntimeExecutionResult,
	now time.Time,
) error {
	if err := runtimeReceiptMatchesCurrent(current, stored.Execution); err != nil {
		return err
	}
	if current.State != "ADMITTED" || current.LeaseTokenSHA256 == "" ||
		!current.LeaseExpiresAt.After(now) ||
		turnLease.TurnID != current.TurnID ||
		turnLease.Attempt != current.Attempt ||
		turnLease.AuthorityGeneration != current.GrantGeneration ||
		!turnLease.ExpiresAt.Equal(current.LeaseExpiresAt) ||
		hashString(stored.LeaseToken) != current.LeaseTokenSHA256 {
		return errs.ErrStateConflict
	}
	return nil
}

func validateClosedRuntimeReceiptGraph(locked lockedRuntimeReceipt) error {
	execution := locked.Execution
	turnSpec, turnOK := locked.Graph.Turn.Spec.(entity.TurnSpec)
	processSpec, processOK := locked.Graph.Process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !turnOK || !processOK || currentErr != nil ||
		turnSpec.Attempt != execution.Attempt ||
		turnSpec.RuntimeRevisionID != execution.RuntimeRevisionID ||
		turnSpec.EffectiveInputSHA256 != execution.ImmutableInputSHA256 ||
		!executionMatchesTurn(current, locked.Graph.Turn, turnSpec) {
		return errs.ErrStateConflict
	}
	want := enum.State(execution.State)
	if execution.State == "SUSPENDED" {
		if (locked.Graph.Turn.State != enum.StateWaitingExternal &&
			locked.Graph.Turn.State != enum.StateWaitingOwner) ||
			(locked.Graph.Process.State != enum.StateWaitingExternal &&
				locked.Graph.Process.State != enum.StateWaitingOwner) {
			return errs.ErrStateConflict
		}
		return nil
	}
	if !runtimeTerminal(execution.State) || execution.State == "RETRIED" ||
		locked.Graph.Turn.State != want || locked.Graph.Process.State != want {
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) CompleteRuntimeExecution(
	ctx context.Context,
	input CompleteRuntimeExecutionInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeComplete, true,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if len(input.LeaseToken) != 64 ||
		(input.Outcome != "SUCCEEDED" && input.Outcome != "FAILED") ||
		!validBoundedReference(input.TerminalReference) ||
		!validSHA256Text(input.TerminalSHA256) {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime           runtimeExecutionIntent
		Outcome           string
		TerminalReference string
		TerminalSHA256    string
	}{runtimeIntent(input.RuntimeExecutionInput), input.Outcome,
		input.TerminalReference, input.TerminalSHA256})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "complete_runtime_execution",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil {
				return 0, err
			}
			disposition, err := runtimeMutationReceiptDisposition(
				locked.Execution, input.RuntimeExecutionInput,
				[]string{"ADMITTED", "RUNNING"}, []string{input.Outcome}, 1,
			)
			if err != nil {
				return 0, err
			}
			if disposition == lifecycleReceiptApply {
				if _, err := service.requireActiveRuntimeGraph(
					ctx, tx, input.Principal, locked.Execution,
				); err != nil || locked.Execution.LeaseTokenSHA256 != hashString(input.LeaseToken) ||
					!locked.Execution.LeaseExpiresAt.After(locked.Now) {
					if err != nil {
						return 0, err
					}
					return 0, errs.ErrStateConflict
				}
			} else if err := validateClosedRuntimeReceiptGraph(locked); err != nil {
				return 0, err
			}
			receiptExecution = locked.Execution
			return disposition, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, execution.TurnID,
			)
			if err != nil || graph.Runtime == nil || graph.Runtime.ID != execution.ID {
				if err != nil {
					return err
				}
				return errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionNonterminal, runtimeDispositionTerminal,
			); err != nil {
				return err
			}
			now, err := service.requireActiveRuntimeGraph(
				ctx, tx, input.Principal, execution,
			)
			if err != nil {
				return err
			}
			if execution.State != "ADMITTED" && execution.State != "RUNNING" {
				return errs.ErrStateConflict
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil ||
				execution.LeaseTokenSHA256 != hashString(input.LeaseToken) ||
				!execution.LeaseExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			turnState := enum.StateSucceeded
			if input.Outcome == "FAILED" {
				turnState = enum.StateFailed
			}
			closedTurn, err := service.closeRuntimeGraph(
				ctx, tx, input.Principal, execution, turnState,
				strings.ToLower(input.Outcome), now,
			)
			if err != nil {
				return err
			}
			if err := service.completeRuntimeProcessFromTurn(
				ctx, tx, input.Principal, closedTurn,
			); err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.State = input.Outcome
			execution.TerminalOutcome = input.Outcome
			execution.TerminalReference = input.TerminalReference
			execution.TerminalSHA256 = input.TerminalSHA256
			execution.LeaseID = ""
			execution.LeaseTokenSHA256 = ""
			execution.LeaseExpiresAt = time.Time{}
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "complete_runtime_execution", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) CancelRuntimeExecution(
	ctx context.Context,
	input CancelRuntimeExecutionInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeCancel, false,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if value.ValidateStableKey(input.ReasonCode) != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime    runtimeExecutionIntent
		ReasonCode string
	}{runtimeIntent(input.RuntimeExecutionInput), input.ReasonCode})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "cancel_runtime_execution",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil {
				return 0, err
			}
			disposition, err := runtimeMutationReceiptDisposition(
				locked.Execution, input.RuntimeExecutionInput,
				[]string{"PENDING", "ADMITTED", "RUNNING"}, []string{"CANCELLED"}, 1,
			)
			if err != nil {
				return 0, err
			}
			if disposition == lifecycleReceiptReplay {
				if err := validateClosedRuntimeReceiptGraph(locked); err != nil {
					return 0, err
				}
			}
			receiptExecution = locked.Execution
			return disposition, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, execution.TurnID,
			)
			if err != nil || graph.Runtime == nil || graph.Runtime.ID != execution.ID {
				if err != nil {
					return err
				}
				return errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionNonterminal, runtimeDispositionTerminal,
			); err != nil {
				return err
			}
			if err := service.requireRuntimeOwner(ctx, tx, input.Principal, execution); err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil {
				return err
			}
			if runtimeTerminal(execution.State) {
				return errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			closedTurn, err := service.closeRuntimeGraph(
				ctx, tx, input.Principal, execution, enum.StateCancelled,
				input.ReasonCode, now,
			)
			if err != nil {
				return err
			}
			if err := service.completeRuntimeProcessFromTurn(
				ctx, tx, input.Principal, closedTurn,
			); err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.State = "CANCELLED"
			execution.TerminalOutcome = "CANCELLED"
			execution.TerminalReference = input.ReasonCode
			execution.TerminalSHA256 = hashString(input.ReasonCode)
			execution.LeaseID = ""
			execution.LeaseTokenSHA256 = ""
			execution.LeaseExpiresAt = time.Time{}
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "cancel_runtime_execution", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) RetryRuntimeExecution(
	ctx context.Context,
	input RuntimeExecutionInput,
) (RetryRuntimeExecutionResult, error) {
	if err := validateRuntimeMutation(service, input, permissionRuntimeRetry, false); err != nil {
		return RetryRuntimeExecutionResult{}, err
	}
	requestHash, err := semanticCommandHash(input.Principal, runtimeIntent(input))
	if err != nil {
		return RetryRuntimeExecutionResult{}, errs.ErrInvalidInput
	}
	var result RetryRuntimeExecutionResult
	var receiptPrevious RuntimeExecution
	var receiptTurn entity.Resource
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "retry_runtime_execution",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return 0, err
			}
			if err := requireExactRuntimeApplicationAuthority(
				execution, input.Principal,
			); err != nil {
				return 0, err
			}
			if execution.Version == input.ExpectedVersion &&
				execution.Fence == input.ExpectedFence {
				if !slices.Contains(
					[]string{"PENDING", "ADMITTED", "RUNNING", "FAILED", "EXPIRED"},
					execution.State,
				) {
					return 0, errs.ErrStateConflict
				}
				graph, err := service.lockOwnerGraphByTurn(
					ctx, tx, input.Principal, execution.TurnID,
				)
				if err != nil || graph.Runtime == nil || graph.Runtime.ID != execution.ID {
					if err != nil {
						return 0, err
					}
					return 0, errs.ErrStateConflict
				}
				return lifecycleReceiptApply, nil
			}
			if execution.Version != input.ExpectedVersion+1 ||
				execution.Fence != input.ExpectedFence+1 || execution.State != "RETRIED" {
				return 0, errs.ErrVersionMismatch
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, execution.TurnID,
			)
			if err != nil {
				return 0, err
			}
			turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
			if !ok || graph.Runtime != nil || turnSpec.Attempt != execution.Attempt+1 ||
				graph.Process.ID != execution.ProcessID ||
				turnSpec.RuntimeRevisionID == execution.RuntimeRevisionID ||
				turnSpec.EffectiveInputSHA256 == execution.ImmutableInputSHA256 {
				return 0, errs.ErrStateConflict
			}
			receiptPrevious = execution
			receiptTurn = graph.Turn
			return lifecycleReceiptReplay, nil
		},
		func() error {
			if err := runtimeReceiptMatchesCurrent(receiptPrevious, result.Previous); err != nil {
				return err
			}
			if result.Turn.ID != receiptTurn.ID || result.Turn.Version != receiptTurn.Version ||
				result.Turn.State != receiptTurn.State {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, execution.TurnID,
			)
			if err != nil || graph.Runtime == nil || graph.Runtime.ID != execution.ID {
				if err != nil {
					return err
				}
				return errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionNonterminal, runtimeDispositionTerminal,
			); err != nil {
				return err
			}
			if err := service.requireRuntimeOwner(ctx, tx, input.Principal, execution); err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input); err != nil {
				return errs.ErrStateConflict
			}
			// Shared rows получены только единым owner graph resolver.
			turn := graph.Turn
			turnSpec, ok := turn.Spec.(entity.TurnSpec)
			if !ok || turn.Kind != enum.KindTurn ||
				turnSpec.SessionID != execution.SessionID ||
				turnSpec.Attempt != execution.Attempt {
				return errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			switch execution.State {
			case "PENDING", "ADMITTED", "RUNNING":
				turn, err = service.closeRuntimeGraph(
					ctx, tx, input.Principal, execution, enum.StateFailed,
					"runtime_retry", now,
				)
				if execution.TerminalOutcome == "" {
					execution.TerminalOutcome = "FAILED"
					execution.TerminalReference = "runtime_retry"
					execution.TerminalSHA256 = hashString("runtime_retry")
				}
			case "FAILED", "EXPIRED":
				turn, err = service.requireRetryableClosedRuntimeGraph(
					ctx, tx, input.Principal, execution,
				)
			default:
				return errs.ErrStateConflict
			}
			if err != nil {
				return err
			}
			open, err := tx.ProcessHasOpenWork(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				execution.ProcessID, execution.TurnID, "",
			)
			if err != nil {
				return err
			}
			if open {
				return errs.ErrStateConflict
			}
			turnSpec, ok = turn.Spec.(entity.TurnSpec)
			if !ok {
				return errs.ErrInternal
			}
			graph.Turn = turn
			retried, retriedSpec, err := service.prepareRetriedExecution(
				ctx, tx, input.Principal, graph, turnSpec, now,
			)
			if err != nil {
				return err
			}
			if err := tx.Update(ctx, retried, turn.Version); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx, tx, input.Principal, "retry_runtime_turn", retried,
			); err != nil {
				return err
			}
			if err := service.rebindIntegrationContinuationRetry(
				ctx, tx, input.Principal, execution, retried, retriedSpec, now,
			); err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.State = "RETRIED"
			execution.LeaseID = ""
			execution.LeaseTokenSHA256 = ""
			execution.LeaseExpiresAt = time.Time{}
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = RetryRuntimeExecutionResult{Previous: execution, Turn: retried}
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "retry_runtime_execution", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) ExpireRuntimeExecution(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey string,
) (RuntimeExecution, error) {
	if err := authorize(principal, permissionRuntimeExpire); err != nil {
		return RuntimeExecution{}, err
	}
	if value.ValidateIdempotencyKey(idempotencyKey) != nil ||
		principal.CallerWorkload != service.runtimeControllerWorkload ||
		principal.CallerSPIFFEID != service.runtimeControllerSPIFFEID {
		return RuntimeExecution{}, errs.ErrPermissionDenied
	}
	requestHash, err := semanticCommandHash(principal, struct{}{})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, principal, idempotencyKey, "expire_runtime_execution", requestHash,
		&result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			execution, err := tx.NextExpiredRuntimeExecution(
				ctx, principal.OrganizationID, principal.ProjectID,
				principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err == nil {
				if err := requireExactRuntimeApplicationAuthority(execution, principal); err != nil {
					return 0, err
				}
				graph, graphErr := service.lockOwnerGraphByTurn(
					ctx, tx, principal, execution.TurnID,
				)
				if graphErr != nil || graph.Runtime == nil || graph.Runtime.ID != execution.ID {
					if graphErr != nil {
						return 0, graphErr
					}
					return 0, errs.ErrStateConflict
				}
				return lifecycleReceiptApply, nil
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return 0, err
			}
			execution, err = tx.GetRuntimeExecutionByTurnForUpdate(
				ctx, principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err != nil {
				return 0, err
			}
			if err := requireExactRuntimeApplicationAuthority(execution, principal); err != nil {
				return 0, err
			}
			graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, execution.TurnID)
			if err != nil {
				return 0, err
			}
			locked := lockedRuntimeReceipt{Execution: execution, Graph: graph}
			if execution.State != "EXPIRED" || graph.Runtime == nil ||
				graph.Runtime.ID != execution.ID || validateClosedRuntimeReceiptGraph(locked) != nil {
				return 0, errs.ErrStateConflict
			}
			receiptExecution = execution
			return lifecycleReceiptReplay, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.NextExpiredRuntimeExecution(
				ctx, principal.OrganizationID, principal.ProjectID,
				principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err != nil {
				return err
			}
			if err := service.prelockRuntimeScheduledGraph(
				ctx, tx, principal, execution,
			); err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if execution.WorkloadID != principal.CallerWorkload ||
				execution.WorkloadSPIFFEID != principal.CallerSPIFFEID {
				return errs.ErrNotFound
			}
			closedTurn, err := service.closeRuntimeGraph(
				ctx, tx, principal, execution, enum.StateExpired,
				"runtime_lease_expired", now,
			)
			if err != nil {
				return err
			}
			if err := service.completeRuntimeProcessFromTurn(
				ctx, tx, principal, closedTurn,
			); err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.State = "EXPIRED"
			execution.TerminalOutcome = "EXPIRED"
			execution.TerminalReference = "database-clock:" + now.Format(time.RFC3339Nano)
			execution.TerminalSHA256 = hashString(execution.TerminalReference)
			execution.LeaseID = ""
			execution.LeaseTokenSHA256 = ""
			execution.LeaseExpiresAt = time.Time{}
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, principal, "expire_runtime_execution", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) RecordRuntimeArchive(
	ctx context.Context,
	input RuntimeArchiveInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeArchive, true,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if !validBoundedReference(input.ArchiveReference) ||
		!validSHA256Text(input.ArchiveSHA256) {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime          runtimeExecutionIntent
		ArchiveReference string
		ArchiveSHA256    string
	}{runtimeIntent(input.RuntimeExecutionInput), input.ArchiveReference,
		input.ArchiveSHA256})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "record_runtime_archive",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil || validateClosedRuntimeReceiptGraph(locked) != nil {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			execution := locked.Execution
			var disposition lifecycleReceiptDisposition
			switch {
			case execution.Version == input.ExpectedVersion &&
				execution.Fence == input.ExpectedFence &&
				execution.ArchiveSHA256 == "":
				disposition = lifecycleReceiptApply
			case execution.Version == input.ExpectedVersion+1 &&
				execution.Fence == input.ExpectedFence+1 &&
				execution.ArchiveReference == input.ArchiveReference &&
				execution.ArchiveSHA256 == input.ArchiveSHA256 &&
				execution.RestoreProofSHA256 == "" &&
				execution.CleanupAuthorizationState == "NONE":
				disposition = lifecycleReceiptReplay
			default:
				return 0, errs.ErrStateConflict
			}
			receiptExecution = execution
			return disposition, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil ||
				!runtimeTerminal(execution.State) || execution.ArchiveSHA256 != "" {
				return errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.ArchiveReference = input.ArchiveReference
			execution.ArchiveSHA256 = input.ArchiveSHA256
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "record_runtime_archive", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) VerifyRuntimeRestore(
	ctx context.Context,
	input RuntimeRestoreInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeRestore, false,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if input.Principal.CallerWorkload != service.restoreVerifierWorkload ||
		input.Principal.CallerSPIFFEID != service.restoreVerifierSPIFFEID {
		return RuntimeExecution{}, errs.ErrPermissionDenied
	}
	if !validSHA256Text(input.ArchiveSHA256) ||
		!validBoundedReference(input.RestoreProofReference) ||
		!validSHA256Text(input.RestoreProofSHA256) {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime               runtimeExecutionIntent
		ArchiveSHA256         string
		RestoreProofReference string
		RestoreProofSHA256    string
	}{runtimeIntent(input.RuntimeExecutionInput), input.ArchiveSHA256,
		input.RestoreProofReference, input.RestoreProofSHA256})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "verify_runtime_restore",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil || validateClosedRuntimeReceiptGraph(locked) != nil {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			execution := locked.Execution
			if execution.ArchiveSHA256 != input.ArchiveSHA256 {
				return 0, errs.ErrStateConflict
			}
			var disposition lifecycleReceiptDisposition
			switch {
			case execution.Version == input.ExpectedVersion &&
				execution.Fence == input.ExpectedFence &&
				execution.RestoreProofSHA256 == "" &&
				execution.CleanupAuthorizationState == "NONE":
				disposition = lifecycleReceiptApply
			case execution.Version == input.ExpectedVersion+1 &&
				execution.Fence == input.ExpectedFence+1 &&
				execution.RestoreProofReference == input.RestoreProofReference &&
				execution.RestoreProofSHA256 == input.RestoreProofSHA256 &&
				execution.RestoreVerifierWorkload == input.Principal.CallerWorkload &&
				execution.RestoreVerifierSPIFFEID == input.Principal.CallerSPIFFEID &&
				execution.CleanupAuthorizationState == "NONE":
				disposition = lifecycleReceiptReplay
			default:
				return 0, errs.ErrStateConflict
			}
			receiptExecution = execution
			return disposition, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			if err := service.requireRuntimeOwner(ctx, tx, input.Principal, execution); err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil ||
				!runtimeTerminal(execution.State) ||
				execution.ArchiveSHA256 != input.ArchiveSHA256 ||
				execution.RestoreProofSHA256 != "" ||
				execution.CleanupAuthorizationState != "NONE" ||
				requireExactRuntimeApplicationAuthority(execution, input.Principal) != nil {
				return errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.RestoreProofReference = input.RestoreProofReference
			execution.RestoreProofSHA256 = input.RestoreProofSHA256
			execution.RestoreVerifierWorkload = input.Principal.CallerWorkload
			execution.RestoreVerifierSPIFFEID = input.Principal.CallerSPIFFEID
			execution.RestoreVerifierGeneration = input.Principal.AuthorityGrantGeneration
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "verify_runtime_restore", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) AuthorizeRuntimeCleanup(
	ctx context.Context,
	input RuntimeCleanupInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeCleanup, false,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if input.Principal.CallerWorkload != service.cleanupAuthorizerWorkload ||
		input.Principal.CallerSPIFFEID != service.cleanupAuthorizerSPIFFEID {
		return RuntimeExecution{}, errs.ErrPermissionDenied
	}
	if !validSHA256Text(input.ArchiveSHA256) ||
		!validSHA256Text(input.RestoreProofSHA256) {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime                   runtimeExecutionIntent
		ArchiveSHA256             string
		RestoreProofSHA256        string
		ExpectedCleanupGeneration uint64
	}{runtimeIntent(input.RuntimeExecutionInput), input.ArchiveSHA256,
		input.RestoreProofSHA256, input.ExpectedCleanupGeneration})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "authorize_runtime_cleanup",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil || validateClosedRuntimeReceiptGraph(locked) != nil {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			execution := locked.Execution
			if execution.ArchiveSHA256 != input.ArchiveSHA256 ||
				execution.RestoreProofSHA256 != input.RestoreProofSHA256 ||
				execution.RestoreVerifierWorkload != service.restoreVerifierWorkload ||
				execution.RestoreVerifierSPIFFEID != service.restoreVerifierSPIFFEID ||
				execution.RestoreVerifierGeneration != execution.GrantGeneration {
				return 0, errs.ErrStateConflict
			}
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, execution.OrganizationID, execution.ProjectID, execution.SessionID,
			)
			if err != nil || blocked {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			if execution.Version == input.ExpectedVersion &&
				execution.Fence == input.ExpectedFence {
				if _, err := cleanupAuthorizationIssueDisposition(
					execution, input.ExpectedCleanupGeneration, locked.Now,
				); err != nil {
					return 0, err
				}
				return lifecycleReceiptApply, nil
			}
			if (execution.Version != input.ExpectedVersion+1 &&
				execution.Version != input.ExpectedVersion+2) ||
				execution.Fence != input.ExpectedFence+(execution.Version-input.ExpectedVersion) ||
				execution.CleanupAuthorizationState != "ACTIVE" ||
				execution.CleanupAuthorizationGeneration != input.ExpectedCleanupGeneration+1 ||
				value.ValidateID(execution.CleanupAuthorizationID) != nil ||
				!execution.CleanupAuthorizationExpiresAt.After(locked.Now) {
				return 0, errs.ErrStateConflict
			}
			receiptExecution = execution
			return lifecycleReceiptReplay, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			if err := service.requireRuntimeOwner(ctx, tx, input.Principal, execution); err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil ||
				!runtimeTerminal(execution.State) ||
				execution.ArchiveSHA256 != input.ArchiveSHA256 ||
				execution.RestoreProofSHA256 != input.RestoreProofSHA256 ||
				execution.RestoreVerifierWorkload != service.restoreVerifierWorkload ||
				execution.RestoreVerifierSPIFFEID != service.restoreVerifierSPIFFEID ||
				execution.RestoreVerifierGeneration != execution.GrantGeneration ||
				requireExactRuntimeApplicationAuthority(execution, input.Principal) != nil {
				return errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := lockRuntimeCleanupSession(
				ctx, tx, input.Principal, execution,
			); err != nil {
				return err
			}
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, execution.OrganizationID, execution.ProjectID, execution.SessionID,
			)
			if err != nil || blocked {
				if err != nil {
					return err
				}
				return errs.ErrStateConflict
			}
			expireBeforeIssue, err := cleanupAuthorizationIssueDisposition(
				execution, input.ExpectedCleanupGeneration, now,
			)
			if err != nil {
				return err
			}
			if expireBeforeIssue {
				if err := service.expireRuntimeCleanupAuthorization(
					ctx, tx, input.Principal, &execution, now,
				); err != nil {
					return err
				}
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.CleanupAuthorizationGeneration++
			execution.CleanupAuthorizationID = uuid.NewString()
			execution.CleanupAuthorizationExpiresAt = now.Add(cleanupAuthorizationLifetime)
			execution.CleanupAuthorizationState = "ACTIVE"
			execution.CleanupConsumedAt = time.Time{}
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "authorize_runtime_cleanup", execution.ID,
				"RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func cleanupAuthorizationIssueDisposition(
	execution RuntimeExecution,
	expectedGeneration uint64,
	now time.Time,
) (bool, error) {
	if execution.CleanupAuthorizationGeneration != expectedGeneration {
		return false, errs.ErrVersionMismatch
	}
	switch execution.CleanupAuthorizationState {
	case "NONE":
		if execution.CleanupAuthorizationGeneration != 0 ||
			execution.CleanupAuthorizationID != "" ||
			!execution.CleanupAuthorizationExpiresAt.IsZero() {
			return false, errs.ErrStateConflict
		}
		return false, nil
	case "EXPIRED":
		if execution.CleanupAuthorizationGeneration == 0 ||
			value.ValidateID(execution.CleanupAuthorizationID) != nil ||
			execution.CleanupAuthorizationExpiresAt.After(now) {
			return false, errs.ErrStateConflict
		}
		return false, nil
	case "ACTIVE":
		if execution.CleanupAuthorizationGeneration == 0 ||
			value.ValidateID(execution.CleanupAuthorizationID) != nil ||
			execution.CleanupAuthorizationExpiresAt.IsZero() {
			return false, errs.ErrStateConflict
		}
		if execution.CleanupAuthorizationExpiresAt.After(now) {
			return false, errs.ErrStateConflict
		}
		return true, nil
	default:
		return false, errs.ErrStateConflict
	}
}

func (service *Service) ConsumeRuntimeCleanupAuthorization(
	ctx context.Context,
	input RuntimeCleanupAuthorizationInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeCleanupConsume, false,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if input.Principal.CallerWorkload != service.runtimeControllerWorkload ||
		input.Principal.CallerSPIFFEID != service.runtimeControllerSPIFFEID ||
		value.ValidateID(input.CleanupAuthorizationID) != nil ||
		input.CleanupAuthorizationGeneration == 0 ||
		!validSHA256Text(input.ArchiveSHA256) ||
		!validSHA256Text(input.RestoreProofSHA256) {
		return RuntimeExecution{}, errs.ErrPermissionDenied
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime                        runtimeExecutionIntent
		CleanupAuthorizationID         string
		CleanupAuthorizationGeneration uint64
		ArchiveSHA256                  string
		RestoreProofSHA256             string
	}{runtimeIntent(input.RuntimeExecutionInput), input.CleanupAuthorizationID,
		input.CleanupAuthorizationGeneration, input.ArchiveSHA256,
		input.RestoreProofSHA256})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey,
		"consume_runtime_cleanup_authorization", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil || validateClosedRuntimeReceiptGraph(locked) != nil {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			execution := locked.Execution
			if execution.CleanupAuthorizationID != input.CleanupAuthorizationID ||
				execution.CleanupAuthorizationGeneration != input.CleanupAuthorizationGeneration ||
				execution.ArchiveSHA256 != input.ArchiveSHA256 ||
				execution.RestoreProofSHA256 != input.RestoreProofSHA256 {
				return 0, errs.ErrStateConflict
			}
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, execution.OrganizationID, execution.ProjectID, execution.SessionID,
			)
			if err != nil || blocked {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			var disposition lifecycleReceiptDisposition
			switch {
			case execution.Version == input.ExpectedVersion &&
				execution.Fence == input.ExpectedFence &&
				execution.CleanupAuthorizationState == "ACTIVE" &&
				execution.CleanupAuthorizationExpiresAt.After(locked.Now):
				disposition = lifecycleReceiptApply
			case execution.Version == input.ExpectedVersion+1 &&
				execution.Fence == input.ExpectedFence+1 &&
				execution.CleanupAuthorizationState == "CONSUMED" &&
				!execution.CleanupConsumedAt.IsZero():
				disposition = lifecycleReceiptReplay
			default:
				return 0, errs.ErrStateConflict
			}
			receiptExecution = execution
			return disposition, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil ||
				requireExactRuntimeApplicationAuthority(execution, input.Principal) != nil ||
				execution.CleanupAuthorizationState != "ACTIVE" ||
				execution.CleanupAuthorizationID != input.CleanupAuthorizationID ||
				execution.CleanupAuthorizationGeneration != input.CleanupAuthorizationGeneration ||
				execution.ArchiveSHA256 != input.ArchiveSHA256 ||
				execution.RestoreProofSHA256 != input.RestoreProofSHA256 ||
				!execution.CleanupAuthorizationExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			if err := lockRuntimeCleanupSession(
				ctx, tx, input.Principal, execution,
			); err != nil {
				return err
			}
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, execution.OrganizationID, execution.ProjectID, execution.SessionID,
			)
			if err != nil || blocked {
				if err != nil {
					return err
				}
				return errs.ErrStateConflict
			}
			expectedVersion, expectedFence := execution.Version, execution.Fence
			execution.Version++
			execution.Fence++
			execution.CleanupAuthorizationState = "CONSUMED"
			execution.CleanupConsumedAt = now
			execution.UpdatedAt = now
			if err := tx.UpdateRuntimeExecution(
				ctx, execution, expectedVersion, expectedFence,
			); err != nil {
				return err
			}
			result = execution
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "consume_runtime_cleanup_authorization",
				execution.ID, "RUNTIME_EXECUTION", execution.Version, now,
			)
		},
	)
	return result, err
}

func lockRuntimeCleanupSession(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
) error {
	session, err := tx.GetForUpdate(
		ctx, execution.OrganizationID, execution.ProjectID, execution.SessionID,
	)
	if err != nil {
		return err
	}
	if session.Kind != enum.KindSession || session.OwnerActorID != principal.ActorID ||
		session.OrganizationID != execution.OrganizationID ||
		session.ProjectID != execution.ProjectID {
		return errs.ErrNotFound
	}
	return nil
}

func (service *Service) ExpireRuntimeCleanupAuthorization(
	ctx context.Context,
	input RuntimeCleanupAuthorizationInput,
) (RuntimeExecution, error) {
	if err := validateRuntimeMutation(
		service, input.RuntimeExecutionInput, permissionRuntimeCleanupExpire, false,
	); err != nil {
		return RuntimeExecution{}, err
	}
	if input.Principal.CallerWorkload != service.cleanupAuthorizerWorkload ||
		input.Principal.CallerSPIFFEID != service.cleanupAuthorizerSPIFFEID ||
		value.ValidateID(input.CleanupAuthorizationID) != nil ||
		input.CleanupAuthorizationGeneration == 0 {
		return RuntimeExecution{}, errs.ErrPermissionDenied
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Runtime                        runtimeExecutionIntent
		CleanupAuthorizationID         string
		CleanupAuthorizationGeneration uint64
		ArchiveSHA256                  string
		RestoreProofSHA256             string
	}{runtimeIntent(input.RuntimeExecutionInput), input.CleanupAuthorizationID,
		input.CleanupAuthorizationGeneration, input.ArchiveSHA256,
		input.RestoreProofSHA256})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	var receiptExecution RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey,
		"expire_runtime_cleanup_authorization", requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockRuntimeReceiptAuthority(
				ctx, tx, input.Principal, input.ExecutionID,
			)
			if err != nil || validateClosedRuntimeReceiptGraph(locked) != nil {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			execution := locked.Execution
			if execution.CleanupAuthorizationID != input.CleanupAuthorizationID ||
				execution.CleanupAuthorizationGeneration != input.CleanupAuthorizationGeneration {
				return 0, errs.ErrStateConflict
			}
			var disposition lifecycleReceiptDisposition
			switch {
			case execution.Version == input.ExpectedVersion &&
				execution.Fence == input.ExpectedFence &&
				execution.CleanupAuthorizationState == "ACTIVE" &&
				!execution.CleanupAuthorizationExpiresAt.After(locked.Now):
				disposition = lifecycleReceiptApply
			case execution.Version == input.ExpectedVersion+1 &&
				execution.Fence == input.ExpectedFence+1 &&
				execution.CleanupAuthorizationState == "EXPIRED":
				disposition = lifecycleReceiptReplay
			default:
				return 0, errs.ErrStateConflict
			}
			receiptExecution = execution
			return disposition, nil
		},
		func() error { return runtimeReceiptMatchesCurrent(receiptExecution, result) },
		func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input.RuntimeExecutionInput); err != nil ||
				requireExactRuntimeApplicationAuthority(execution, input.Principal) != nil ||
				execution.CleanupAuthorizationState != "ACTIVE" ||
				execution.CleanupAuthorizationID != input.CleanupAuthorizationID ||
				execution.CleanupAuthorizationGeneration != input.CleanupAuthorizationGeneration ||
				execution.CleanupAuthorizationExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			if err := service.expireRuntimeCleanupAuthorization(
				ctx, tx, input.Principal, &execution, now,
			); err != nil {
				return err
			}
			result = execution
			return nil
		},
	)
	return result, err
}

func (service *Service) expireRuntimeCleanupAuthorization(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution *RuntimeExecution,
	now time.Time,
) error {
	if execution.CleanupAuthorizationState != "ACTIVE" ||
		execution.CleanupAuthorizationExpiresAt.After(now) {
		return errs.ErrStateConflict
	}
	expectedVersion, expectedFence := execution.Version, execution.Fence
	execution.Version++
	execution.Fence++
	execution.CleanupAuthorizationState = "EXPIRED"
	execution.UpdatedAt = now
	if err := tx.UpdateRuntimeExecution(
		ctx, *execution, expectedVersion, expectedFence,
	); err != nil {
		return err
	}
	return service.appendLifecycleAudit(
		ctx, tx, principal, "expire_runtime_cleanup_authorization",
		execution.ID, "RUNTIME_EXECUTION", execution.Version, now,
	)
}

func requireExactRuntimeApplicationAuthority(
	execution RuntimeExecution,
	principal value.Principal,
) error {
	if execution.OrganizationID != principal.OrganizationID ||
		execution.ProjectID != principal.ProjectID ||
		execution.TurnID != principal.AuthorityReference ||
		execution.Attempt != uint32(principal.AuthorityRevision) ||
		execution.ImmutableInputSHA256 != principal.AuthorityDigest ||
		execution.GrantGeneration != principal.AuthorityGrantGeneration {
		return errs.ErrNotFound
	}
	return nil
}

func (service *Service) requireActiveRuntimeGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
) (time.Time, error) {
	now, _, err := service.requireActiveRuntimeLeaseGraph(ctx, tx, principal, execution)
	return now, err
}

// requireActiveRuntimeLeaseGraph проверяет две части единой runtime lease.
// TurnLease и RuntimeExecution.LeaseExpiresAt должны оставаться одной exact
// server-owned tuple; heartbeat/admit продлевают их атомарно PostgreSQL clock.
func (service *Service) requireActiveRuntimeLeaseGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
) (time.Time, domainrepo.TurnLease, error) {
	graph, err := service.lockOwnerGraphByTurn(
		ctx, tx, principal, execution.TurnID,
	)
	if err != nil {
		return time.Time{}, domainrepo.TurnLease{}, err
	}
	if graph.Runtime == nil || graph.Runtime.ID != execution.ID ||
		graph.Runtime.Version != execution.Version || graph.Runtime.Fence != execution.Fence {
		return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	session, turn, process := graph.Session, graph.Turn, graph.Process
	if session.Kind != enum.KindSession || session.State != enum.StateActive ||
		session.OwnerActorID != principal.ActorID {
		return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		(turn.State != enum.StateClaimed && turn.State != enum.StateRunning) ||
		spec.Attempt != execution.Attempt ||
		spec.ProcessRunID != execution.ProcessID || spec.SessionID != execution.SessionID ||
		spec.RuntimeRevisionID != execution.RuntimeRevisionID ||
		spec.EffectiveInputSHA256 != execution.ImmutableInputSHA256 {
		return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		process.State != enum.StateRunning || process.OwnerActorID != principal.ActorID ||
		!executionMatchesTurn(current, turn, spec) {
		return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	lease, err := tx.GetTurnLeaseForUpdate(ctx, turn.ID)
	if err != nil {
		return time.Time{}, domainrepo.TurnLease{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, execution.Attempt)
	if err != nil {
		return time.Time{}, domainrepo.TurnLease{}, err
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return time.Time{}, domainrepo.TurnLease{}, err
	}
	if lease.Attempt != execution.Attempt ||
		lease.AuthorityGeneration != execution.GrantGeneration ||
		!lease.ExpiresAt.After(now) ||
		attempt.AuthorityGeneration != execution.GrantGeneration ||
		attempt.InputSHA256 != execution.ImmutableInputSHA256 ||
		!attempt.FinishedAt.IsZero() {
		return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	switch execution.State {
	case "PENDING":
		if execution.LeaseID != "" || execution.LeaseTokenSHA256 != "" ||
			!execution.LeaseExpiresAt.IsZero() {
			return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
		}
	case "ADMITTED", "RUNNING":
		if execution.LeaseID == "" || execution.LeaseTokenSHA256 == "" ||
			!execution.LeaseExpiresAt.After(now) ||
			!execution.LeaseExpiresAt.Equal(lease.ExpiresAt) {
			return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
		}
	default:
		return time.Time{}, domainrepo.TurnLease{}, errs.ErrStateConflict
	}
	return now, lease, nil
}

func (service *Service) closeRuntimeGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
	target enum.State,
	outcome string,
	now time.Time,
) (entity.Resource, error) {
	turn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, execution.TurnID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		spec.Attempt != execution.Attempt ||
		spec.RuntimeRevisionID != execution.RuntimeRevisionID ||
		spec.EffectiveInputSHA256 != execution.ImmutableInputSHA256 ||
		turn.State.Terminal() {
		return entity.Resource{}, errs.ErrStateConflict
	}
	lease, err := tx.GetTurnLeaseForUpdate(ctx, turn.ID)
	if err != nil {
		return entity.Resource{}, err
	}
	if lease.Attempt != execution.Attempt ||
		lease.AuthorityGeneration != execution.GrantGeneration {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.DeleteTurnLease(ctx, turn.ID, lease.Fence); err != nil {
		return entity.Resource{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, execution.Attempt)
	if err != nil {
		return entity.Resource{}, err
	}
	if attempt.InputSHA256 != execution.ImmutableInputSHA256 ||
		attempt.AuthorityGeneration != execution.GrantGeneration ||
		!attempt.FinishedAt.IsZero() {
		return entity.Resource{}, errs.ErrStateConflict
	}
	attempt.State = string(target)
	attempt.FinishedAt = now
	attempt.Outcome = outcome
	if err := tx.FinishTurnAttempt(ctx, attempt); err != nil {
		return entity.Resource{}, err
	}
	spec.Outcome = outcome
	updated, err := turn.ReplaceAndTransition(spec, target, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updated, turn.Version); err != nil {
		return entity.Resource{}, err
	}
	if err := service.revokeExecutionClaims(
		ctx, tx, principal, execution.ProcessID, execution.TurnID,
		outcome, now,
	); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "close_runtime_graph", updated,
	); err != nil {
		return entity.Resource{}, err
	}
	return updated, nil
}

func (service *Service) completeRuntimeProcessFromTurn(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turn entity.Resource,
) error {
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || !turn.State.Terminal() {
		return errs.ErrStateConflict
	}
	if err := service.completeProcessFromTurn(ctx, tx, principal, turn, spec); err != nil {
		return err
	}
	if spec.ProcessRunID == "" {
		return nil
	}
	process, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.ProcessRunID,
	)
	if err != nil {
		return err
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		process.State != turn.State || !executionMatchesTurn(current, turn, spec) {
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) requireRetryableClosedRuntimeGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
) (entity.Resource, error) {
	if !retryableRuntimePredecessor(execution.State) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	target := enum.StateFailed
	if execution.State == "EXPIRED" {
		target = enum.StateExpired
	}
	if execution.TerminalOutcome != execution.State ||
		!validBoundedReference(execution.TerminalReference) ||
		!validSHA256Text(execution.TerminalSHA256) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	turn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, execution.TurnID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		turn.State != target || spec.Attempt != execution.Attempt ||
		spec.ProcessRunID != execution.ProcessID || spec.SessionID != execution.SessionID ||
		spec.RuntimeRevisionID != execution.RuntimeRevisionID ||
		spec.EffectiveInputSHA256 != execution.ImmutableInputSHA256 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if _, err := tx.GetTurnLeaseForUpdate(ctx, turn.ID); !errors.Is(err, errs.ErrNotFound) {
		if err == nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return entity.Resource{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, execution.Attempt)
	if err != nil {
		return entity.Resource{}, err
	}
	if attempt.AuthorityGeneration != execution.GrantGeneration ||
		attempt.InputSHA256 != execution.ImmutableInputSHA256 ||
		attempt.State != string(target) || attempt.FinishedAt.IsZero() {
		return entity.Resource{}, errs.ErrStateConflict
	}
	process, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, execution.ProcessID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		process.State != target || !executionMatchesTurn(current, turn, spec) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return turn, nil
}

func retryableRuntimePredecessor(state string) bool {
	return state == "FAILED" || state == "EXPIRED"
}

func integrationOutcomeReadyForDelivery(continuation IntegrationContinuation) bool {
	if continuation.ContinuationState != "READY" &&
		continuation.ContinuationState != "REJOINED" {
		return false
	}
	if continuation.ApprovalState == "APPROVED" {
		return continuation.ExecutionState == "SUCCEEDED" ||
			continuation.ExecutionState == "FAILED"
	}
	return (continuation.ApprovalState == "REJECTED" ||
		continuation.ApprovalState == "EXPIRED" ||
		continuation.ApprovalState == "CANCELLED") &&
		continuation.ExecutionState == "NOT_APPLICABLE"
}

func (service *Service) rebindIntegrationContinuationRetry(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	previous RuntimeExecution,
	retried entity.Resource,
	retriedSpec entity.TurnSpec,
	now time.Time,
) error {
	continuation, err := tx.GetIntegrationContinuationByContinuationTurn(
		ctx, previous.TurnID,
	)
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	revision, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID,
		retriedSpec.RuntimeRevisionID,
	)
	if err != nil {
		return err
	}
	continuation, err = service.lockIntegrationContinuationAfterBindings(
		ctx, tx, principal, continuation, false,
	)
	if err != nil {
		return err
	}
	expectedVersion, expectedFence := continuation.Version, continuation.Fence
	continuation, err = rebindIntegrationDelivery(
		continuation, previous, retried, retriedSpec, revision, now,
	)
	if err != nil {
		return err
	}
	if err := tx.UpdateIntegrationContinuation(
		ctx, continuation, expectedVersion, expectedFence,
	); err != nil {
		return err
	}
	return service.appendLifecycleAudit(
		ctx, tx, principal, "rebind_integration_continuation_retry",
		continuation.ID, "INTEGRATION_CONTINUATION", continuation.Version, now,
	)
}

func rebindIntegrationDelivery(
	continuation IntegrationContinuation,
	previous RuntimeExecution,
	retried entity.Resource,
	retriedSpec entity.TurnSpec,
	revision entity.Resource,
	now time.Time,
) (IntegrationContinuation, error) {
	if !integrationOutcomeReadyForDelivery(continuation) ||
		continuation.ProcessID != previous.ProcessID ||
		continuation.SessionID != previous.SessionID ||
		continuation.ContinuationTurnID != previous.TurnID ||
		continuation.ContinuationAttempt != previous.Attempt ||
		continuation.ContinuationRuntimeRevisionID != previous.RuntimeRevisionID ||
		continuation.ContinuationRuntimeRevisionVersion != previous.RuntimeRevisionVersion ||
		continuation.ContinuationInputSHA256 != previous.ImmutableInputSHA256 ||
		retried.ID != previous.TurnID || retriedSpec.SessionID != previous.SessionID ||
		retriedSpec.ProcessRunID != previous.ProcessID ||
		retriedSpec.Attempt != previous.Attempt+1 ||
		!validSHA256Text(retriedSpec.EffectiveInputSHA256) ||
		revision.Kind != enum.KindRuntimeRevision || revision.State != enum.StateActive ||
		revision.ID != retriedSpec.RuntimeRevisionID {
		return IntegrationContinuation{}, errs.ErrStateConflict
	}
	continuation.Version++
	continuation.Fence++
	continuation.ContinuationState = "READY"
	continuation.ContinuationTurnVersion = retried.Version
	continuation.ContinuationAttempt = retriedSpec.Attempt
	continuation.ContinuationRuntimeRevisionID = revision.ID
	continuation.ContinuationRuntimeRevisionVersion = revision.Version
	continuation.ContinuationInputSHA256 = retriedSpec.EffectiveInputSHA256
	continuation.UpdatedAt = now
	return continuation, nil
}

func scheduledTerminalState(state enum.State) string {
	if state == enum.StateExpired {
		return "FAILED"
	}
	return string(state)
}

func runtimeTerminal(state string) bool {
	return state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED" ||
		state == "EXPIRED" || state == "RETRIED" || state == "SUSPENDED"
}

func validBoundedReference(reference string) bool {
	return len(reference) >= 1 && len(reference) <= 512 &&
		reference == strings.TrimSpace(reference) &&
		!strings.ContainsAny(reference, "\x00\r\n")
}

func (service *Service) ResolveIntegrationSession(
	ctx context.Context,
	principal value.Principal,
) (IntegrationSessionContext, error) {
	if err := authorize(principal, permissionIntegrationResolve); err != nil {
		return IntegrationSessionContext{}, err
	}
	if principal.CallerWorkload != service.integrationGatewayWorkload ||
		principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID {
		return IntegrationSessionContext{}, errs.ErrPermissionDenied
	}
	var result IntegrationSessionContext
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		ActorID: principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		resolved, err := service.resolveBoundExecution(ctx, tx, principal)
		if err != nil {
			return err
		}
		if resolved.Turn.State != enum.StateClaimed || resolved.Session.State != enum.StateActive {
			return errs.ErrStateConflict
		}
		context, err := service.integrationSessionContext(ctx, tx, principal, resolved)
		if err != nil {
			return err
		}
		result = context
		return nil
	})
	return result, err
}

func (service *Service) integrationSessionContext(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	resolved resolvedExecution,
) (IntegrationSessionContext, error) {
	revisionSpec := resolved.RevisionSpec
	if revisionSpec.SessionID != resolved.Session.ID ||
		revisionSpec.RoleID != resolved.Role.ID {
		return IntegrationSessionContext{}, errs.ErrStateConflict
	}
	components := make(map[enum.Kind]map[string]entity.EffectiveResourceRef)
	for _, component := range revisionSpec.Components {
		if components[component.Kind] == nil {
			components[component.Kind] = make(map[string]entity.EffectiveResourceRef)
		}
		if _, exists := components[component.Kind][component.ResourceID]; exists {
			return IntegrationSessionContext{}, errs.ErrStateConflict
		}
		components[component.Kind][component.ResourceID] = component
	}
	roleComponent, ok := components[enum.KindRole][resolved.Role.ID]
	if !ok {
		return IntegrationSessionContext{}, errs.ErrStateConflict
	}
	roleMatches, err := revisionComponentMatches(resolved.Role, roleComponent)
	if err != nil {
		return IntegrationSessionContext{}, err
	}
	if !roleMatches {
		return IntegrationSessionContext{}, errs.ErrStateConflict
	}
	threadID := resolved.SessionSpec.ConversationID
	if threadID == "" {
		threadID = resolved.Session.ID
	}
	result := IntegrationSessionContext{
		OrganizationID: resolved.Turn.OrganizationID,
		ProjectID:      resolved.Turn.ProjectID, OwnerActorID: resolved.Turn.OwnerActorID,
		ProcessID: resolved.Process.ID,
		SessionID: resolved.Session.ID, SessionVersion: resolved.Session.Version,
		ThreadID: threadID,
		TurnID:   resolved.Turn.ID, TurnVersion: resolved.Turn.Version,
		Attempt: resolved.TurnSpec.Attempt, InputSHA256: resolved.TurnSpec.EffectiveInputSHA256,
		RuntimeRevisionID:      resolved.Revision.ID,
		RuntimeRevisionVersion: resolved.Revision.Version,
		RuntimeRevisionSHA256:  resolved.RevisionHash,
		RuntimeManifestSHA256:  revisionSpec.ManifestSHA256,
		RoleID:                 resolved.Role.ID, RoleVersion: resolved.Role.Version,
		RoleCapabilities: slices.Clone(resolved.RoleSpec.Capabilities),
		GrantGeneration:  principal.AuthorityGrantGeneration,
	}
	for _, integrationID := range revisionSpec.IntegrationIDs {
		if !slices.Contains(resolved.RoleSpec.IntegrationIDs, integrationID) {
			return IntegrationSessionContext{}, errs.ErrPermissionDenied
		}
		integration, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, integrationID,
		)
		if err != nil {
			return IntegrationSessionContext{}, err
		}
		integrationSpec, ok := integration.Spec.(entity.IntegrationSpec)
		component, bound := components[enum.KindIntegration][integration.ID]
		matches, matchErr := revisionComponentMatches(integration, component)
		if matchErr != nil {
			return IntegrationSessionContext{}, matchErr
		}
		if !ok || integration.Kind != enum.KindIntegration ||
			integration.State != enum.StateActive || !bound || !matches {
			return IntegrationSessionContext{}, errs.ErrStateConflict
		}
		binding := IntegrationSessionBinding{
			IntegrationID: integration.ID, IntegrationVersion: integration.Version,
			ProjectionSHA256:  component.ProjectionSHA256,
			DefinitionRef:     integrationSpec.DefinitionRef,
			DefinitionVersion: integrationSpec.DefinitionVersion,
			Capabilities:      slices.Clone(integrationSpec.Capabilities),
			EndpointRef:       integrationSpec.EndpointRef,
		}
		for _, credentialID := range integrationSpec.CredentialBindingIDs {
			if !slices.Contains(revisionSpec.CredentialBindingIDs, credentialID) {
				return IntegrationSessionContext{}, errs.ErrPermissionDenied
			}
			credential, err := tx.GetForUpdate(
				ctx, principal.OrganizationID, principal.ProjectID, credentialID,
			)
			if err != nil {
				return IntegrationSessionContext{}, err
			}
			credentialSpec, ok := credential.Spec.(entity.CredentialBindingSpec)
			credentialComponent, bound := components[enum.KindCredentialBinding][credential.ID]
			matches, matchErr := revisionComponentMatches(credential, credentialComponent)
			if matchErr != nil {
				return IntegrationSessionContext{}, matchErr
			}
			if !ok || credential.Kind != enum.KindCredentialBinding ||
				credential.State != enum.StateActive || !bound || !matches {
				return IntegrationSessionContext{}, errs.ErrStateConflict
			}
			binding.CredentialBindings = append(
				binding.CredentialBindings,
				IntegrationCredentialBinding{
					CredentialBindingID:      credential.ID,
					CredentialBindingVersion: credential.Version,
					ProjectionSHA256:         credentialComponent.ProjectionSHA256,
					Purpose:                  credentialSpec.Purpose, SecretRef: credentialSpec.SecretRef,
					PrincipalRef:       credentialSpec.PrincipalRef,
					CredentialRevision: credentialSpec.Revision, ExpiresAt: credentialSpec.ExpiresAt,
				},
			)
		}
		result.Integrations = append(result.Integrations, binding)
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return IntegrationSessionContext{}, err
	}
	for _, integration := range result.Integrations {
		for _, credential := range integration.CredentialBindings {
			if !credential.ExpiresAt.IsZero() && !credential.ExpiresAt.After(now) {
				return IntegrationSessionContext{}, errs.ErrStateConflict
			}
		}
	}
	return result, nil
}

func revisionComponentMatches(
	resource entity.Resource,
	component entity.EffectiveResourceRef,
) (bool, error) {
	if component.ResourceID != resource.ID || component.Kind != resource.Kind ||
		component.Version != resource.Version {
		return false, nil
	}
	digest, err := entity.ProjectionSHA256(resource)
	if err != nil {
		return false, errs.ErrInternal
	}
	return digest == component.ProjectionSHA256, nil
}

func validPinnedIntegrationResources(resources []PinnedIntegrationResource) bool {
	if len(resources) > 16 {
		return false
	}
	previousID := ""
	for _, resource := range resources {
		if value.ValidateID(resource.ResourceID) != nil || resource.Version == 0 ||
			!validSHA256Text(resource.ProjectionSHA256) ||
			(previousID != "" && resource.ResourceID <= previousID) {
			return false
		}
		previousID = resource.ResourceID
	}
	return true
}

func integrationDecisionAllowed(
	continuation IntegrationContinuation,
	decision string,
	now time.Time,
) bool {
	pending := continuation.ApprovalState == "PENDING" &&
		continuation.ExecutionState == "NOT_STARTED" &&
		continuation.ApprovalExpiresAt.After(now)
	approvedCancellation := decision == "CANCELLED" &&
		continuation.ApprovalState == "APPROVED" &&
		continuation.ExecutionState == "NOT_STARTED"
	return pending || approvedCancellation
}

func revisionComponent(
	spec entity.RuntimeRevisionSpec,
	kind enum.Kind,
	resourceID string,
) (entity.EffectiveResourceRef, error) {
	var result entity.EffectiveResourceRef
	count := 0
	for _, component := range spec.Components {
		if component.Kind == kind && component.ResourceID == resourceID {
			result = component
			count++
		}
	}
	if count != 1 {
		return entity.EffectiveResourceRef{}, errs.ErrStateConflict
	}
	return result, nil
}

func (service *Service) resolveSelectedIntegrationBinding(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	resolved resolvedExecution,
	input SuspendIntegrationInput,
) (PinnedIntegrationResource, error) {
	if !slices.Contains(resolved.RoleSpec.IntegrationIDs, input.IntegrationID) ||
		!slices.Contains(resolved.RevisionSpec.IntegrationIDs, input.IntegrationID) {
		return PinnedIntegrationResource{}, errs.ErrPermissionDenied
	}
	integration, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, input.IntegrationID,
	)
	if err != nil {
		return PinnedIntegrationResource{}, err
	}
	integrationSpec, ok := integration.Spec.(entity.IntegrationSpec)
	component, componentErr := revisionComponent(
		resolved.RevisionSpec, enum.KindIntegration, integration.ID,
	)
	if componentErr != nil {
		return PinnedIntegrationResource{}, componentErr
	}
	matches, matchErr := revisionComponentMatches(integration, component)
	if matchErr != nil {
		return PinnedIntegrationResource{}, matchErr
	}
	if !ok || integration.Kind != enum.KindIntegration ||
		integration.State != enum.StateActive || !matches ||
		integration.Version != input.IntegrationVersion ||
		component.ProjectionSHA256 != input.IntegrationSHA256 {
		return PinnedIntegrationResource{}, errs.ErrStateConflict
	}
	credentialExpirations := make([]time.Time, 0, len(input.CredentialBindings))
	for _, selected := range input.CredentialBindings {
		if !slices.Contains(integrationSpec.CredentialBindingIDs, selected.ResourceID) ||
			!slices.Contains(resolved.RevisionSpec.CredentialBindingIDs, selected.ResourceID) {
			return PinnedIntegrationResource{}, errs.ErrPermissionDenied
		}
		credential, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, selected.ResourceID,
		)
		if err != nil {
			return PinnedIntegrationResource{}, err
		}
		credentialSpec, ok := credential.Spec.(entity.CredentialBindingSpec)
		credentialComponent, componentErr := revisionComponent(
			resolved.RevisionSpec, enum.KindCredentialBinding, credential.ID,
		)
		if componentErr != nil {
			return PinnedIntegrationResource{}, componentErr
		}
		matches, matchErr := revisionComponentMatches(credential, credentialComponent)
		if matchErr != nil {
			return PinnedIntegrationResource{}, matchErr
		}
		if !ok || credential.Kind != enum.KindCredentialBinding ||
			credential.State != enum.StateActive || !matches ||
			credential.Version != selected.Version ||
			credentialComponent.ProjectionSHA256 != selected.ProjectionSHA256 {
			return PinnedIntegrationResource{}, errs.ErrStateConflict
		}
		credentialExpirations = append(credentialExpirations, credentialSpec.ExpiresAt)
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return PinnedIntegrationResource{}, err
	}
	for _, expiresAt := range credentialExpirations {
		if !expiresAt.IsZero() && !expiresAt.After(now) {
			return PinnedIntegrationResource{}, errs.ErrStateConflict
		}
	}
	return PinnedIntegrationResource{
		ResourceID: integration.ID, Version: integration.Version,
		ProjectionSHA256: component.ProjectionSHA256,
	}, nil
}

func (service *Service) suspendRuntimeExecutionForIntegration(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	resolved resolvedExecution,
	execution RuntimeExecution,
	invocationID string,
	requestSHA256 string,
	now time.Time,
) error {
	threadID := resolved.SessionSpec.ConversationID
	if threadID == "" {
		threadID = resolved.Session.ID
	}
	if execution.OrganizationID != principal.OrganizationID ||
		execution.ProjectID != principal.ProjectID ||
		execution.ProcessID != resolved.Process.ID ||
		execution.SessionID != resolved.Session.ID ||
		execution.ThreadID != threadID ||
		execution.RoleID != resolved.Role.ID ||
		execution.TurnID != resolved.Turn.ID ||
		execution.Attempt != resolved.TurnSpec.Attempt ||
		execution.RuntimeRevisionID != resolved.Revision.ID ||
		execution.RuntimeRevisionVersion != resolved.Revision.Version ||
		execution.RuntimeRevisionSHA256 != resolved.RevisionHash ||
		execution.ImmutableInputSHA256 != resolved.TurnSpec.EffectiveInputSHA256 ||
		execution.GrantGeneration != principal.AuthorityGrantGeneration ||
		execution.WorkloadID != service.runtimeControllerWorkload ||
		execution.WorkloadSPIFFEID != service.runtimeControllerSPIFFEID ||
		(execution.State != "PENDING" && execution.State != "ADMITTED" &&
			execution.State != "RUNNING") ||
		(execution.State != "PENDING" && !execution.LeaseExpiresAt.After(now)) {
		return errs.ErrStateConflict
	}
	expectedVersion, expectedFence := execution.Version, execution.Fence
	execution.Version++
	execution.Fence++
	execution.State = "SUSPENDED"
	execution.TerminalOutcome = "SUSPENDED"
	execution.TerminalReference = invocationID
	execution.TerminalSHA256 = requestSHA256
	execution.LeaseID = ""
	execution.LeaseTokenSHA256 = ""
	execution.LeaseExpiresAt = time.Time{}
	execution.UpdatedAt = now
	if err := tx.UpdateRuntimeExecution(
		ctx, execution, expectedVersion, expectedFence,
	); err != nil {
		return err
	}
	return service.appendLifecycleAudit(
		ctx, tx, principal, "suspend_runtime_for_integration", execution.ID,
		"RUNTIME_EXECUTION", execution.Version, now,
	)
}

func (service *Service) suspendRuntimeExecutionForOwnerGate(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graph lockedOwnerGraph,
	gateID string,
	requestSHA256 string,
	now time.Time,
) error {
	if graph.Runtime == nil {
		return nil
	}
	execution := *graph.Runtime
	turnSpec, turnOK := graph.Turn.Spec.(entity.TurnSpec)
	if !turnOK || execution.OrganizationID != principal.OrganizationID ||
		execution.ProjectID != principal.ProjectID ||
		execution.ProcessID != graph.Process.ID ||
		execution.SessionID != graph.Session.ID || execution.TurnID != graph.Turn.ID ||
		execution.Attempt != turnSpec.Attempt ||
		execution.RuntimeRevisionID != turnSpec.RuntimeRevisionID ||
		execution.ImmutableInputSHA256 != turnSpec.EffectiveInputSHA256 ||
		execution.GrantGeneration != principal.AuthorityGrantGeneration ||
		(execution.State != "PENDING" && execution.State != "ADMITTED" &&
			execution.State != "RUNNING") {
		return errs.ErrStateConflict
	}
	expectedVersion, expectedFence := execution.Version, execution.Fence
	execution.Version++
	execution.Fence++
	execution.State = "SUSPENDED"
	execution.TerminalOutcome = "SUSPENDED"
	execution.TerminalReference = gateID
	execution.TerminalSHA256 = requestSHA256
	execution.LeaseID = ""
	execution.LeaseTokenSHA256 = ""
	execution.LeaseExpiresAt = time.Time{}
	execution.UpdatedAt = now
	if err := tx.UpdateRuntimeExecution(
		ctx, execution, expectedVersion, expectedFence,
	); err != nil {
		return err
	}
	return service.appendLifecycleAudit(
		ctx, tx, principal, "suspend_runtime_for_owner_gate", execution.ID,
		"RUNTIME_EXECUTION", execution.Version, now,
	)
}

func requireOwnerGateSuspensionLease(
	execution *RuntimeExecution,
	turnLease domainrepo.TurnLease,
	now time.Time,
) error {
	if !turnLease.ExpiresAt.After(now) {
		return errs.ErrStateConflict
	}
	if execution == nil {
		return nil
	}
	switch execution.State {
	case "PENDING":
		if execution.LeaseID != "" || execution.LeaseTokenSHA256 != "" ||
			!execution.LeaseExpiresAt.IsZero() {
			return errs.ErrStateConflict
		}
	case "ADMITTED", "RUNNING":
		if execution.LeaseID == "" || execution.LeaseTokenSHA256 == "" ||
			!execution.LeaseExpiresAt.After(now) ||
			!execution.LeaseExpiresAt.Equal(turnLease.ExpiresAt) {
			return errs.ErrStateConflict
		}
	default:
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) validatePinnedIntegrationContinuation(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	continuation IntegrationContinuation,
	requireActive bool,
) error {
	if continuation.IntegrationVersion == 0 ||
		!validSHA256Text(continuation.IntegrationSHA256) ||
		!validPinnedIntegrationResources(continuation.CredentialBindings) {
		return errs.ErrStateConflict
	}
	revision, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID,
		continuation.RuntimeRevisionID,
	)
	if err != nil {
		return err
	}
	revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok || revision.Kind != enum.KindRuntimeRevision ||
		revision.Version != continuation.RuntimeRevisionVersion ||
		!validSHA256Text(continuation.RuntimeRevisionSHA256) ||
		!validSHA256Text(continuation.ImmutableInputSHA256) ||
		!slices.Contains(revisionSpec.IntegrationIDs, continuation.IntegrationID) {
		return errs.ErrStateConflict
	}
	revisionSHA256, err := entity.ProjectionSHA256(revision)
	if err != nil || revisionSHA256 != continuation.RuntimeRevisionSHA256 {
		return errs.ErrStateConflict
	}
	integrationComponent, err := revisionComponent(
		revisionSpec, enum.KindIntegration, continuation.IntegrationID,
	)
	if err != nil || integrationComponent.Version != continuation.IntegrationVersion ||
		integrationComponent.ProjectionSHA256 != continuation.IntegrationSHA256 {
		return errs.ErrStateConflict
	}
	if requireActive {
		integration, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID,
			continuation.IntegrationID,
		)
		if err != nil {
			return err
		}
		integrationSpec, ok := integration.Spec.(entity.IntegrationSpec)
		matches, matchErr := revisionComponentMatches(integration, integrationComponent)
		if matchErr != nil || !ok || !matches || integration.State != enum.StateActive {
			return errs.ErrStateConflict
		}
		for _, selected := range continuation.CredentialBindings {
			if !slices.Contains(integrationSpec.CredentialBindingIDs, selected.ResourceID) {
				return errs.ErrStateConflict
			}
		}
	}
	credentialExpirations := make([]time.Time, 0, len(continuation.CredentialBindings))
	for _, selected := range continuation.CredentialBindings {
		if !slices.Contains(revisionSpec.CredentialBindingIDs, selected.ResourceID) {
			return errs.ErrStateConflict
		}
		component, err := revisionComponent(
			revisionSpec, enum.KindCredentialBinding, selected.ResourceID,
		)
		if err != nil || component.Version != selected.Version ||
			component.ProjectionSHA256 != selected.ProjectionSHA256 {
			return errs.ErrStateConflict
		}
		if !requireActive {
			continue
		}
		credential, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, selected.ResourceID,
		)
		if err != nil {
			return err
		}
		credentialSpec, ok := credential.Spec.(entity.CredentialBindingSpec)
		matches, matchErr := revisionComponentMatches(credential, component)
		if matchErr != nil || !ok || !matches || credential.State != enum.StateActive {
			return errs.ErrStateConflict
		}
		credentialExpirations = append(credentialExpirations, credentialSpec.ExpiresAt)
	}
	if requireActive {
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		for _, expiresAt := range credentialExpirations {
			if !expiresAt.IsZero() && !expiresAt.After(now) {
				return errs.ErrStateConflict
			}
		}
	}
	return nil
}

func (service *Service) SuspendForIntegrationApproval(
	ctx context.Context,
	input SuspendIntegrationInput,
) (IntegrationContinuation, error) {
	if err := authorize(input.Principal, permissionIntegrationSuspend); err != nil {
		return IntegrationContinuation{}, err
	}
	if input.Principal.CallerWorkload != service.integrationGatewayWorkload ||
		input.Principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.InvocationID) != nil ||
		value.ValidateID(input.ApprovalID) != nil ||
		value.ValidateID(input.IntegrationID) != nil ||
		input.IntegrationVersion == 0 ||
		!validSHA256Text(input.IntegrationSHA256) ||
		!validPinnedIntegrationResources(input.CredentialBindings) ||
		!validSHA256Text(input.RequestSHA256) {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if input.ApprovalExpiresAt.Before(now.Add(minimumApprovalLifetime)) ||
		input.ApprovalExpiresAt.After(now.Add(maximumApprovalLifetime)) {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, suspendIntegrationIntent{
		InvocationID: input.InvocationID, ApprovalID: input.ApprovalID,
		IntegrationID: input.IntegrationID, IntegrationVersion: input.IntegrationVersion,
		IntegrationSHA256:  input.IntegrationSHA256,
		CredentialBindings: append([]PinnedIntegrationResource{}, input.CredentialBindings...),
		RequestSHA256:      input.RequestSHA256,
		ApprovalExpiresAt:  input.ApprovalExpiresAt.UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	continuationID := uuid.NewString()
	var result IntegrationContinuation
	var receiptContinuation IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "suspend_integration_approval",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			execution, err := tx.GetRuntimeExecutionByTurnForUpdate(
				ctx, input.Principal.AuthorityReference,
				uint32(input.Principal.AuthorityRevision),
			)
			if err != nil {
				return 0, err
			}
			if err := requireExactRuntimeApplicationAuthority(
				execution, input.Principal,
			); err != nil {
				return 0, err
			}
			if execution.State == "SUSPENDED" {
				if value.ValidateID(execution.TerminalReference) != nil ||
					execution.TerminalSHA256 != input.RequestSHA256 {
					return 0, errs.ErrStateConflict
				}
				locked, err := service.lockIntegrationReceiptAuthority(
					ctx, tx, input.Principal, execution.TerminalReference, false,
				)
				if err != nil {
					return 0, err
				}
				continuation := locked.Continuation
				databaseNow, nowErr := tx.CurrentTime(ctx)
				if nowErr != nil {
					return 0, nowErr
				}
				if continuation.ContinuationState != "SUSPENDED" ||
					continuation.ApprovalState != "PENDING" ||
					continuation.ExecutionState != "NOT_STARTED" ||
					continuation.InvocationID != input.InvocationID ||
					continuation.ApprovalID != input.ApprovalID ||
					continuation.IntegrationID != input.IntegrationID ||
					continuation.IntegrationVersion != input.IntegrationVersion ||
					continuation.IntegrationSHA256 != input.IntegrationSHA256 ||
					continuation.RequestSHA256 != input.RequestSHA256 ||
					!continuation.ApprovalExpiresAt.After(databaseNow) {
					return 0, errs.ErrStateConflict
				}
				receiptContinuation = continuation
				return lifecycleReceiptReplay, nil
			}
			if execution.State != "PENDING" && execution.State != "ADMITTED" &&
				execution.State != "RUNNING" {
				return 0, errs.ErrStateConflict
			}
			if err := service.prelockRuntimeScheduledGraph(
				ctx, tx, input.Principal, execution,
			); err != nil {
				return 0, err
			}
			resolved, err := service.resolveBoundExecution(ctx, tx, input.Principal)
			if err != nil {
				return 0, err
			}
			if _, err := service.resolveSelectedIntegrationBinding(
				ctx, tx, input.Principal, resolved, input,
			); err != nil {
				return 0, err
			}
			return lifecycleReceiptApply, nil
		},
		func() error {
			return integrationReceiptMatchesCurrent(receiptContinuation, result)
		},
		func(tx domainrepo.Transaction) error {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if input.ApprovalExpiresAt.Before(now.Add(minimumApprovalLifetime)) ||
				input.ApprovalExpiresAt.After(now.Add(maximumApprovalLifetime)) {
				return errs.ErrInvalidInput
			}
			execution, err := tx.GetRuntimeExecutionByTurnForUpdate(
				ctx, input.Principal.AuthorityReference,
				uint32(input.Principal.AuthorityRevision),
			)
			if err != nil {
				return err
			}
			if err := service.prelockRuntimeScheduledGraph(
				ctx, tx, input.Principal, execution,
			); err != nil {
				return err
			}
			resolved, err := service.resolveBoundExecution(ctx, tx, input.Principal)
			if err != nil {
				return err
			}
			if resolved.Turn.State != enum.StateClaimed &&
				resolved.Turn.State != enum.StateRunning {
				return errs.ErrPermissionDenied
			}
			binding, err := service.resolveSelectedIntegrationBinding(
				ctx, tx, input.Principal, resolved, input,
			)
			if err != nil {
				return err
			}
			if err := service.suspendRuntimeExecutionForIntegration(
				ctx, tx, input.Principal, resolved, execution, continuationID,
				input.RequestSHA256, now,
			); err != nil {
				return err
			}
			suspendedTurn, suspendedSession, suspendedProcess, err :=
				service.suspendIntegrationGraph(ctx, tx, input.Principal, resolved, now)
			if err != nil {
				return err
			}
			threadID := resolved.SessionSpec.ConversationID
			if threadID == "" {
				threadID = resolved.Session.ID
			}
			result = IntegrationContinuation{
				ID: continuationID, OrganizationID: input.Principal.OrganizationID,
				ProjectID: input.Principal.ProjectID, ProcessID: suspendedProcess.ID,
				SessionID: suspendedSession.ID, SessionVersion: suspendedSession.Version,
				ThreadID: threadID, RoleID: resolved.Role.ID,
				TurnID: suspendedTurn.ID, TurnVersion: suspendedTurn.Version,
				Attempt:                resolved.TurnSpec.Attempt,
				RuntimeRevisionID:      resolved.Revision.ID,
				RuntimeRevisionVersion: resolved.Revision.Version,
				RuntimeRevisionSHA256:  resolved.RevisionHash,
				ImmutableInputSHA256:   resolved.TurnSpec.EffectiveInputSHA256,
				GrantGeneration:        input.Principal.AuthorityGrantGeneration,
				InvocationID:           input.InvocationID, ApprovalID: input.ApprovalID,
				IntegrationID:      binding.ResourceID,
				IntegrationVersion: binding.Version,
				IntegrationSHA256:  binding.ProjectionSHA256,
				CredentialBindings: append(
					[]PinnedIntegrationResource{}, input.CredentialBindings...,
				),
				RequestSHA256: input.RequestSHA256,
				ApprovalState: "PENDING", ExecutionState: "NOT_STARTED",
				ContinuationState: "SUSPENDED", Version: 1, Fence: 1,
				ApprovalExpiresAt: input.ApprovalExpiresAt.UTC().Truncate(time.Microsecond),
				CreatedAt:         now, UpdatedAt: now,
			}
			if err := tx.InsertIntegrationContinuation(ctx, result); err != nil {
				return err
			}
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "suspend_integration_approval", result.ID,
				"INTEGRATION_CONTINUATION", result.Version, now,
			)
		},
	)
	return result, err
}

// prelockRuntimeScheduledGraph согласует terminal/retry/suspension со scheduler recovery:
// occurrence -> schedule -> run -> session -> turn -> process. RuntimeExecution
// блокируется раньше и scheduler её не изменяет. NotFound означает точный
// unscheduled path; существующий, но несогласованный граф закрыто отклоняется.
func (service *Service) prelockRuntimeScheduledGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
) error {
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, execution.TurnID)
	if err != nil {
		return err
	}
	if graph.Runtime == nil || graph.Runtime.ID != execution.ID ||
		graph.Runtime.Version != execution.Version || graph.Runtime.Fence != execution.Fence ||
		graph.Session.ID != execution.SessionID || graph.Process.ID != execution.ProcessID ||
		graph.Session.Kind != enum.KindSession ||
		graph.Session.OwnerActorID != principal.ActorID {
		return errs.ErrStateConflict
	}
	if graph.Occurrence.ID == "" {
		return nil
	}
	if graph.Occurrence.ExecutionTurnID != execution.TurnID ||
		graph.Occurrence.ExecutionProcessRunID != execution.ProcessID ||
		graph.Occurrence.ExecutionSessionID != execution.SessionID ||
		graph.Occurrence.ExecutionRuntimeRevisionID != execution.RuntimeRevisionID ||
		graph.Occurrence.ExecutionRuntimeRevisionVersion != execution.RuntimeRevisionVersion ||
		graph.Occurrence.EffectiveInputSHA256 != execution.ImmutableInputSHA256 ||
		graph.Schedule.Kind != enum.KindSchedule ||
		graph.Schedule.OwnerActorID != principal.ActorID ||
		!scheduledExecutionMayLockRuntime(graph.Occurrence.State, graph.Run.State) ||
		graph.Run.CurrentTurnAttempt != execution.Attempt {
		return errs.ErrStateConflict
	}
	return nil
}

func scheduledExecutionMayLockRuntime(occurrenceState, runState string) bool {
	return occurrenceState == runState &&
		(occurrenceState == "CLAIMED" || occurrenceState == "CONTINUATION" ||
			occurrenceState == "FAILED")
}

// prelockScheduledGraphByTurn задаёт совместимый поднабор общего порядка для
// legacy Turn-команд: occurrence -> schedule -> run -> session -> turn -> process.
func (service *Service) prelockScheduledGraphByTurn(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turnID string,
) (lockedOwnerGraph, error) {
	return service.lockOwnerGraphByTurn(ctx, tx, principal, turnID)
}

func (service *Service) suspendIntegrationGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	resolved resolvedExecution,
	now time.Time,
) (entity.Resource, entity.Resource, entity.Resource, error) {
	open, err := tx.ProcessHasOpenWork(
		ctx, principal.OrganizationID, principal.ProjectID,
		resolved.Process.ID, resolved.Turn.ID, "",
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if open {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	lease, err := tx.GetTurnLeaseForUpdate(ctx, resolved.Turn.ID)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if lease.Attempt != resolved.TurnSpec.Attempt ||
		lease.WorkloadID != agentRunnerWorkload ||
		lease.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		lease.Fence != resolved.Turn.Version || lease.TokenHash == "" ||
		!lease.ExpiresAt.After(now) {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	attempt, err := tx.GetTurnAttemptForUpdate(
		ctx, resolved.Turn.ID, resolved.TurnSpec.Attempt,
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if attempt.WorkloadID != agentRunnerWorkload ||
		attempt.InputSHA256 != resolved.TurnSpec.EffectiveInputSHA256 ||
		attempt.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		attempt.LeaseFence != lease.Fence || attempt.State != "CLAIMED" ||
		attempt.StartedAt.IsZero() ||
		!attempt.FinishedAt.IsZero() {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	suspendedTurn, err := resolved.Turn.Transition(enum.StateWaitingExternal, now)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	suspendedSession, err := resolved.Session.ReplaceAndTransition(
		resolved.SessionSpec, enum.StateWaitingExternal, now,
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	suspendedProcess, err := suspendIntegrationProcessRun(
		resolved, suspendedSession, suspendedTurn, now,
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}

	// Все exact owner/current проверки выше выполняются до отзыва authority.
	// Дальнейшие записи остаются одной transaction и не могут оставить
	// ProcessRun со старыми версиями уже suspended Session/Turn.
	if err := tx.DeleteTurnLease(ctx, resolved.Turn.ID, lease.Fence); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	attempt.State = "WAITING_EXTERNAL"
	attempt.FinishedAt = now
	attempt.Outcome = "integration_approval"
	if err := tx.FinishTurnAttempt(ctx, attempt); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if err := tx.Update(ctx, suspendedTurn, resolved.Turn.Version); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if err := tx.Update(ctx, suspendedSession, resolved.Session.Version); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if err := tx.Update(ctx, suspendedProcess, resolved.Process.Version); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if err := service.suspendIntegrationScheduledGraph(
		ctx, tx, principal, resolved, suspendedTurn, suspendedSession,
		suspendedProcess, now,
	); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if err := service.revokeExecutionClaims(
		ctx, tx, principal, resolved.Process.ID, resolved.Turn.ID,
		"integration_approval", now,
	); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	for action, changed := range map[string]entity.Resource{
		"suspend_integration_turn":    suspendedTurn,
		"suspend_integration_session": suspendedSession,
		"suspend_integration_process": suspendedProcess,
	} {
		if err := service.appendMutationRecords(ctx, tx, principal, action, changed); err != nil {
			return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
		}
	}
	return suspendedTurn, suspendedSession, suspendedProcess, nil
}

// suspendIntegrationProcessRun переносит полный server-owned current tuple на
// уже увеличенные версии suspended Session/Turn. Root lineage остаётся
// историей, а lifecycle продолжает читать только exact current binding.
func suspendIntegrationProcessRun(
	resolved resolvedExecution,
	suspendedSession entity.Resource,
	suspendedTurn entity.Resource,
	now time.Time,
) (entity.Resource, error) {
	current, err := currentExecution(resolved.ProcessSpec)
	if err != nil || resolved.Process.Kind != enum.KindProcessRun ||
		resolved.Process.State != enum.StateRunning ||
		resolved.Process.OwnerActorID != resolved.Turn.OwnerActorID ||
		resolved.Session.Kind != enum.KindSession ||
		resolved.Session.State != enum.StateActive ||
		(resolved.Turn.State != enum.StateClaimed &&
			resolved.Turn.State != enum.StateRunning) ||
		current.SessionID != resolved.Session.ID ||
		current.SessionVersion != resolved.Session.Version ||
		current.TurnID != resolved.Turn.ID ||
		current.TurnVersion != resolved.Turn.Version ||
		current.Attempt != resolved.TurnSpec.Attempt ||
		current.RuntimeRevisionID != resolved.Revision.ID ||
		current.RuntimeRevisionVersion != resolved.Revision.Version ||
		current.InputSHA256 != resolved.TurnSpec.EffectiveInputSHA256 ||
		suspendedSession.ID != resolved.Session.ID ||
		suspendedSession.State != enum.StateWaitingExternal ||
		suspendedSession.Version != resolved.Session.Version+1 ||
		suspendedTurn.ID != resolved.Turn.ID ||
		suspendedTurn.State != enum.StateWaitingExternal ||
		suspendedTurn.Version != resolved.Turn.Version+1 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	processSpec := resolved.ProcessSpec
	setCurrentExecution(&processSpec, executionTuple{
		SessionID: suspendedSession.ID, SessionVersion: suspendedSession.Version,
		TurnID: suspendedTurn.ID, TurnVersion: suspendedTurn.Version,
		Attempt:                resolved.TurnSpec.Attempt,
		RuntimeRevisionID:      resolved.Revision.ID,
		RuntimeRevisionVersion: resolved.Revision.Version,
		InputSHA256:            resolved.TurnSpec.EffectiveInputSHA256,
	})
	suspended, err := resolved.Process.ReplaceAndTransition(
		processSpec, enum.StateWaitingExternal, now,
	)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return suspended, nil
}

func scheduledExecutionMaySuspendExternal(occurrenceState, runState string) bool {
	return occurrenceState == runState &&
		(occurrenceState == "CLAIMED" || occurrenceState == "CONTINUATION")
}

func (service *Service) suspendIntegrationScheduledGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	resolved resolvedExecution,
	suspendedTurn entity.Resource,
	suspendedSession entity.Resource,
	suspendedProcess entity.Resource,
	now time.Time,
) error {
	if resolved.ProcessSpec.OccurrenceID == "" {
		if resolved.ProcessSpec.ScheduleID != "" {
			return errs.ErrStateConflict
		}
		return nil
	}
	if resolved.ProcessSpec.ScheduleID == "" {
		return errs.ErrStateConflict
	}
	occurrence, err := tx.GetScheduleOccurrenceForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID,
		resolved.ProcessSpec.OccurrenceID,
	)
	if err != nil {
		return err
	}
	schedule, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID,
		resolved.ProcessSpec.ScheduleID,
	)
	if err != nil {
		return err
	}
	if schedule.Kind != enum.KindSchedule ||
		schedule.OwnerActorID != resolved.Process.OwnerActorID {
		return errs.ErrStateConflict
	}
	run, err := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
	if err != nil || validateScheduledRunBinding(occurrence, run) != nil ||
		!scheduledExecutionMaySuspendExternal(occurrence.State, run.State) ||
		occurrence.ScheduleID != resolved.ProcessSpec.ScheduleID ||
		occurrence.ExecutionSessionID != resolved.Session.ID ||
		occurrence.ExecutionSessionVersion != resolved.Session.Version ||
		occurrence.ExecutionTurnID != resolved.Turn.ID ||
		occurrence.ExecutionTurnVersion != resolved.Turn.Version ||
		occurrence.ExecutionProcessRunID != resolved.Process.ID ||
		occurrence.ExecutionProcessVersion != resolved.Process.Version ||
		run.CurrentTurnAttempt != resolved.TurnSpec.Attempt ||
		run.CurrentRuntimeRevisionID != resolved.Revision.ID ||
		run.CurrentRuntimeRevisionVersion != resolved.Revision.Version ||
		run.CurrentInputSHA256 != resolved.TurnSpec.EffectiveInputSHA256 {
		return errs.ErrStateConflict
	}
	expectedToken := occurrence.TokenHash
	occurrence.State = "CONTINUATION"
	occurrence.ClaimantWorkloadID = ""
	occurrence.AuthorityGeneration = 0
	occurrence.TokenHash = ""
	occurrence.LeaseExpiresAt = time.Time{}
	occurrence.ExecutionSessionVersion = suspendedSession.Version
	occurrence.ExecutionTurnVersion = suspendedTurn.Version
	occurrence.ExecutionProcessVersion = suspendedProcess.Version
	occurrence.Outcome = ""
	occurrence.ResultArtifactID = ""
	occurrence.UpdatedAt = now
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, occurrence.Attempt, expectedToken,
	); err != nil {
		return err
	}
	run.CurrentSessionVersion = suspendedSession.Version
	run.CurrentTurnVersion = suspendedTurn.Version
	run.CurrentProcessVersion = suspendedProcess.Version
	if err := tx.SuspendScheduledRun(
		ctx, run, resolved.Turn.ID, resolved.TurnSpec.Attempt,
	); err != nil {
		return err
	}
	return appendScheduleOccurrenceAudit(
		ctx, tx, principal, "suspend_integration_schedule", occurrence,
	)
}

func (service *Service) ApproveIntegrationInvocation(
	ctx context.Context,
	input IntegrationDecisionInput,
) (IntegrationContinuation, error) {
	return service.decideIntegration(ctx, input, "APPROVED", false)
}

func (service *Service) RejectIntegrationInvocation(
	ctx context.Context,
	input IntegrationDecisionInput,
) (IntegrationContinuation, error) {
	return service.decideIntegration(ctx, input, "REJECTED", true)
}

func (service *Service) CancelIntegrationInvocation(
	ctx context.Context,
	input IntegrationDecisionInput,
) (IntegrationContinuation, error) {
	return service.decideIntegration(ctx, input, "CANCELLED", true)
}

func (service *Service) decideIntegration(
	ctx context.Context,
	input IntegrationDecisionInput,
	decision string,
	materialize bool,
) (IntegrationContinuation, error) {
	if err := authorize(input.Principal, permissionIntegrationDecide); err != nil {
		return IntegrationContinuation{}, err
	}
	if err := service.validateIntegrationGateway(input.Principal); err != nil ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ContinuationID) != nil || input.ExpectedVersion == 0 ||
		input.ExpectedFence == 0 || value.ValidateID(input.InvocationID) != nil ||
		value.ValidateID(input.ApprovalID) != nil || !validSHA256Text(input.RequestSHA256) ||
		!validBoundedReference(input.DecisionReference) ||
		!validSHA256Text(input.DecisionSHA256) {
		if err != nil {
			return IntegrationContinuation{}, err
		}
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, integrationDecisionIntent{
		ContinuationID: input.ContinuationID, ExpectedVersion: input.ExpectedVersion,
		ExpectedFence: input.ExpectedFence, InvocationID: input.InvocationID,
		ApprovalID: input.ApprovalID, RequestSHA256: input.RequestSHA256,
		DecisionReference: input.DecisionReference, DecisionSHA256: input.DecisionSHA256,
		Decision: decision,
	})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	var result IntegrationContinuation
	scope := "approve_integration_invocation"
	if decision == "REJECTED" {
		scope = "reject_integration_invocation"
	} else if decision == "CANCELLED" {
		scope = "cancel_integration_invocation"
	}
	var receiptContinuation IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, scope, requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockIntegrationReceiptAuthority(
				ctx, tx, input.Principal, input.ContinuationID, decision == "APPROVED",
			)
			if err != nil {
				return 0, err
			}
			continuation := locked.Continuation
			if continuation.InvocationID != input.InvocationID ||
				continuation.ApprovalID != input.ApprovalID ||
				continuation.RequestSHA256 != input.RequestSHA256 {
				return 0, errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return 0, err
			}
			sourceAllowed := integrationDecisionAllowed(continuation, decision, now) &&
				continuation.ContinuationState == "SUSPENDED"
			replayState := "SUSPENDED"
			if materialize {
				replayState = "READY"
			}
			replayAllowed := continuation.ApprovalState == decision &&
				continuation.DecisionReference == input.DecisionReference &&
				continuation.DecisionSHA256 == input.DecisionSHA256 &&
				continuation.ContinuationState == replayState
			if decision != "APPROVED" {
				replayAllowed = replayAllowed && continuation.ExecutionState == "NOT_APPLICABLE"
			}
			disposition, err := integrationMutationReceiptDisposition(
				continuation, input.ExpectedVersion, input.ExpectedFence,
				sourceAllowed, replayAllowed,
			)
			if err != nil {
				return 0, err
			}
			receiptContinuation = continuation
			return disposition, nil
		},
		func() error {
			return integrationReceiptMatchesCurrent(receiptContinuation, result)
		},
		func(tx domainrepo.Transaction) error {
			var continuation IntegrationContinuation
			var err error
			if materialize {
				snapshot, readErr := tx.GetIntegrationContinuation(ctx, input.ContinuationID)
				if readErr != nil {
					return readErr
				}
				continuation, err = service.prelockIntegrationTerminalGraph(
					ctx, tx, input.Principal, snapshot,
				)
			} else {
				snapshot, readErr := tx.GetIntegrationContinuation(ctx, input.ContinuationID)
				if readErr != nil {
					return readErr
				}
				continuation, err = service.lockIntegrationContinuationAfterBindings(
					ctx, tx, input.Principal, snapshot, decision == "APPROVED",
				)
			}
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if err := matchIntegrationGateway(continuation, input.Principal); err != nil ||
				continuation.Version != input.ExpectedVersion ||
				continuation.Fence != input.ExpectedFence ||
				continuation.InvocationID != input.InvocationID ||
				continuation.ApprovalID != input.ApprovalID ||
				continuation.RequestSHA256 != input.RequestSHA256 {
				return errs.ErrStateConflict
			}
			if !integrationDecisionAllowed(continuation, decision, now) {
				return errs.ErrStateConflict
			}
			if err := service.validatePinnedIntegrationContinuation(
				ctx, tx, input.Principal, continuation, decision == "APPROVED",
			); err != nil {
				return err
			}
			expectedVersion, expectedFence := continuation.Version, continuation.Fence
			continuation.Version++
			continuation.Fence++
			continuation.ApprovalState = decision
			continuation.DecisionReference = input.DecisionReference
			continuation.DecisionSHA256 = input.DecisionSHA256
			continuation.UpdatedAt = now
			if decision != "APPROVED" {
				continuation.ExecutionState = "NOT_APPLICABLE"
			}
			if materialize {
				if err := service.materializeIntegrationContinuation(
					ctx, tx, input.Principal, &continuation, now,
				); err != nil {
					return err
				}
			}
			if err := tx.UpdateIntegrationContinuation(
				ctx, continuation, expectedVersion, expectedFence,
			); err != nil {
				return err
			}
			result = continuation
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, scope, continuation.ID,
				"INTEGRATION_CONTINUATION", continuation.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) ExpireIntegrationInvocation(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey string,
) (IntegrationContinuation, error) {
	if err := authorize(principal, permissionIntegrationDecide); err != nil {
		return IntegrationContinuation{}, err
	}
	if err := service.validateIntegrationGateway(principal); err != nil ||
		value.ValidateIdempotencyKey(idempotencyKey) != nil {
		if err != nil {
			return IntegrationContinuation{}, err
		}
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(principal, struct{}{})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	var result IntegrationContinuation
	var receiptContinuation IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, principal, idempotencyKey, "expire_integration_invocation",
		requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			snapshot, err := tx.NextExpiredIntegrationContinuation(
				ctx, principal.OrganizationID, principal.ProjectID,
				principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err == nil {
				continuation, lockErr := service.prelockIntegrationTerminalGraph(
					ctx, tx, principal, snapshot,
				)
				if lockErr != nil {
					return 0, lockErr
				}
				now, clockErr := tx.CurrentTime(ctx)
				if clockErr != nil {
					return 0, clockErr
				}
				if continuation.ApprovalState != "PENDING" ||
					continuation.ExecutionState != "NOT_STARTED" ||
					continuation.ContinuationState != "SUSPENDED" ||
					continuation.ApprovalExpiresAt.After(now) ||
					matchIntegrationGateway(continuation, principal) != nil {
					return 0, errs.ErrStateConflict
				}
				return lifecycleReceiptApply, nil
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return 0, err
			}
			execution, err := tx.GetRuntimeExecutionByTurnForUpdate(
				ctx, principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err != nil || execution.State != "SUSPENDED" ||
				value.ValidateID(execution.TerminalReference) != nil {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			locked, err := service.lockIntegrationReceiptAuthority(
				ctx, tx, principal, execution.TerminalReference, false,
			)
			if err != nil {
				return 0, err
			}
			continuation := locked.Continuation
			if continuation.ApprovalState != "EXPIRED" ||
				continuation.ExecutionState != "NOT_APPLICABLE" ||
				continuation.ContinuationState != "READY" {
				return 0, errs.ErrStateConflict
			}
			receiptContinuation = continuation
			return lifecycleReceiptReplay, nil
		},
		func() error {
			return integrationReceiptMatchesCurrent(receiptContinuation, result)
		},
		func(tx domainrepo.Transaction) error {
			snapshot, err := tx.NextExpiredIntegrationContinuation(
				ctx, principal.OrganizationID, principal.ProjectID,
				principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err != nil {
				return err
			}
			continuation, err := service.prelockIntegrationTerminalGraph(
				ctx, tx, principal, snapshot,
			)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if continuation.ApprovalState != "PENDING" ||
				continuation.ExecutionState != "NOT_STARTED" ||
				continuation.ContinuationState != "SUSPENDED" ||
				continuation.ApprovalExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			if err := matchIntegrationGateway(continuation, principal); err != nil {
				return err
			}
			if err := service.validatePinnedIntegrationContinuation(
				ctx, tx, principal, continuation, false,
			); err != nil {
				return err
			}
			expectedVersion, expectedFence := continuation.Version, continuation.Fence
			continuation.Version++
			continuation.Fence++
			continuation.ApprovalState = "EXPIRED"
			continuation.ExecutionState = "NOT_APPLICABLE"
			continuation.DecisionReference = "database-clock:" + now.Format(time.RFC3339Nano)
			continuation.DecisionSHA256 = hashString(continuation.DecisionReference)
			continuation.UpdatedAt = now
			if err := service.materializeIntegrationContinuation(
				ctx, tx, principal, &continuation, now,
			); err != nil {
				return err
			}
			if err := tx.UpdateIntegrationContinuation(
				ctx, continuation, expectedVersion, expectedFence,
			); err != nil {
				return err
			}
			result = continuation
			return service.appendLifecycleAudit(
				ctx, tx, principal, "expire_integration_invocation", continuation.ID,
				"INTEGRATION_CONTINUATION", continuation.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) BeginIntegrationExecution(
	ctx context.Context,
	input IntegrationExecutionInput,
) (IntegrationContinuation, error) {
	return service.executeIntegrationTransition(ctx, input, "BEGIN")
}

func (service *Service) CompleteIntegrationExecution(
	ctx context.Context,
	input IntegrationExecutionInput,
) (IntegrationContinuation, error) {
	return service.executeIntegrationTransition(ctx, input, "SUCCEEDED")
}

func (service *Service) FailIntegrationExecution(
	ctx context.Context,
	input IntegrationExecutionInput,
) (IntegrationContinuation, error) {
	return service.executeIntegrationTransition(ctx, input, "FAILED")
}

func (service *Service) executeIntegrationTransition(
	ctx context.Context,
	input IntegrationExecutionInput,
	target string,
) (IntegrationContinuation, error) {
	if err := authorize(input.Principal, permissionIntegrationExecute); err != nil {
		return IntegrationContinuation{}, err
	}
	if err := service.validateIntegrationGateway(input.Principal); err != nil ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ContinuationID) != nil || input.ExpectedVersion == 0 ||
		input.ExpectedFence == 0 || value.ValidateID(input.InvocationID) != nil ||
		!validSHA256Text(input.RequestSHA256) {
		if err != nil {
			return IntegrationContinuation{}, err
		}
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	if target == "SUCCEEDED" &&
		(!validBoundedReference(input.ResultReference) ||
			!validSHA256Text(input.ResultSHA256) || input.ErrorCode != "" ||
			input.ErrorReference != "" || input.ErrorSHA256 != "") {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	if target == "FAILED" &&
		(value.ValidateStableKey(input.ErrorCode) != nil ||
			!validBoundedReference(input.ErrorReference) ||
			!validSHA256Text(input.ErrorSHA256) || input.ResultReference != "" ||
			input.ResultSHA256 != "") {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	if target == "BEGIN" && (input.ResultReference != "" || input.ResultSHA256 != "" ||
		input.ErrorCode != "" || input.ErrorReference != "" || input.ErrorSHA256 != "") {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, integrationExecutionIntent{
		ContinuationID: input.ContinuationID, ExpectedVersion: input.ExpectedVersion,
		ExpectedFence: input.ExpectedFence, InvocationID: input.InvocationID,
		RequestSHA256: input.RequestSHA256, ResultReference: input.ResultReference,
		ResultSHA256: input.ResultSHA256, ErrorCode: input.ErrorCode,
		ErrorReference: input.ErrorReference, ErrorSHA256: input.ErrorSHA256,
		Target: target,
	})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	var result IntegrationContinuation
	scope := "begin_integration_execution"
	if target == "SUCCEEDED" {
		scope = "complete_integration_execution"
	} else if target == "FAILED" {
		scope = "fail_integration_execution"
	}
	var receiptContinuation IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, scope, requestHash, &result,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			locked, err := service.lockIntegrationReceiptAuthority(
				ctx, tx, input.Principal, input.ContinuationID, target == "BEGIN",
			)
			if err != nil {
				return 0, err
			}
			continuation := locked.Continuation
			if continuation.InvocationID != input.InvocationID ||
				continuation.RequestSHA256 != input.RequestSHA256 ||
				continuation.ApprovalState != "APPROVED" {
				return 0, errs.ErrStateConflict
			}
			sourceExecutionState := "NOT_STARTED"
			replayContinuationState := "SUSPENDED"
			if target != "BEGIN" {
				sourceExecutionState = "EXECUTING"
				replayContinuationState = "READY"
			}
			replayExecutionState := target
			if target == "BEGIN" {
				replayExecutionState = "EXECUTING"
			}
			replayAllowed := continuation.ExecutionState == replayExecutionState &&
				continuation.ContinuationState == replayContinuationState
			switch target {
			case "SUCCEEDED":
				replayAllowed = replayAllowed &&
					continuation.ResultReference == input.ResultReference &&
					continuation.ResultSHA256 == input.ResultSHA256
			case "FAILED":
				replayAllowed = replayAllowed && continuation.ErrorCode == input.ErrorCode &&
					continuation.ErrorReference == input.ErrorReference &&
					continuation.ErrorSHA256 == input.ErrorSHA256
			}
			disposition, err := integrationMutationReceiptDisposition(
				continuation, input.ExpectedVersion, input.ExpectedFence,
				continuation.ExecutionState == sourceExecutionState &&
					continuation.ContinuationState == "SUSPENDED",
				replayAllowed,
			)
			if err != nil {
				return 0, err
			}
			receiptContinuation = continuation
			return disposition, nil
		},
		func() error {
			return integrationReceiptMatchesCurrent(receiptContinuation, result)
		},
		func(tx domainrepo.Transaction) error {
			var continuation IntegrationContinuation
			var err error
			if target != "BEGIN" {
				snapshot, readErr := tx.GetIntegrationContinuation(ctx, input.ContinuationID)
				if readErr != nil {
					return readErr
				}
				continuation, err = service.prelockIntegrationTerminalGraph(
					ctx, tx, input.Principal, snapshot,
				)
			} else {
				snapshot, readErr := tx.GetIntegrationContinuation(ctx, input.ContinuationID)
				if readErr != nil {
					return readErr
				}
				continuation, err = service.lockIntegrationContinuationAfterBindings(
					ctx, tx, input.Principal, snapshot, true,
				)
			}
			if err != nil {
				return err
			}
			if err := matchIntegrationGateway(continuation, input.Principal); err != nil ||
				continuation.Version != input.ExpectedVersion ||
				continuation.Fence != input.ExpectedFence ||
				continuation.InvocationID != input.InvocationID ||
				continuation.RequestSHA256 != input.RequestSHA256 ||
				continuation.ApprovalState != "APPROVED" {
				return errs.ErrStateConflict
			}
			if err := service.validatePinnedIntegrationContinuation(
				ctx, tx, input.Principal, continuation, target == "BEGIN",
			); err != nil {
				return err
			}
			if target == "BEGIN" && continuation.ExecutionState != "NOT_STARTED" {
				return errs.ErrStateConflict
			}
			if target != "BEGIN" && continuation.ExecutionState != "EXECUTING" {
				return errs.ErrStateConflict
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			expectedVersion, expectedFence := continuation.Version, continuation.Fence
			continuation.Version++
			continuation.Fence++
			continuation.UpdatedAt = now
			if target == "BEGIN" {
				continuation.ExecutionState = "EXECUTING"
			} else if target == "SUCCEEDED" {
				continuation.ExecutionState = "SUCCEEDED"
				continuation.ResultReference = input.ResultReference
				continuation.ResultSHA256 = input.ResultSHA256
			} else {
				continuation.ExecutionState = "FAILED"
				continuation.ErrorCode = input.ErrorCode
				continuation.ErrorReference = input.ErrorReference
				continuation.ErrorSHA256 = input.ErrorSHA256
			}
			if target != "BEGIN" {
				if err := service.materializeIntegrationContinuation(
					ctx, tx, input.Principal, &continuation, now,
				); err != nil {
					return err
				}
			}
			if err := tx.UpdateIntegrationContinuation(
				ctx, continuation, expectedVersion, expectedFence,
			); err != nil {
				return err
			}
			result = continuation
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, scope, continuation.ID,
				"INTEGRATION_CONTINUATION", continuation.Version, now,
			)
		},
	)
	return result, err
}

func (service *Service) validateIntegrationGateway(principal value.Principal) error {
	if principal.CallerWorkload != service.integrationGatewayWorkload ||
		principal.CallerSPIFFEID != service.integrationGatewaySPIFFEID ||
		principal.AuthorityGrantGeneration == 0 ||
		value.ValidateID(principal.AuthorityReference) != nil ||
		principal.AuthorityRevision == 0 || !validSHA256Text(principal.AuthorityDigest) {
		return errs.ErrPermissionDenied
	}
	return nil
}

func matchIntegrationGateway(
	continuation IntegrationContinuation,
	principal value.Principal,
) error {
	if continuation.TurnID != principal.AuthorityReference ||
		continuation.Attempt != uint32(principal.AuthorityRevision) ||
		continuation.ImmutableInputSHA256 != principal.AuthorityDigest ||
		continuation.GrantGeneration != principal.AuthorityGrantGeneration {
		return errs.ErrNotFound
	}
	return nil
}

type lockedIntegrationReceipt struct {
	Continuation  IntegrationContinuation
	Graph         lockedOwnerGraph
	SourceRuntime RuntimeExecution
}

func (service *Service) lockIntegrationReceiptAuthority(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	continuationID string,
	requireActiveBindings bool,
) (lockedIntegrationReceipt, error) {
	snapshot, err := tx.GetIntegrationContinuation(ctx, continuationID)
	if err != nil {
		return lockedIntegrationReceipt{}, err
	}
	if err := matchIntegrationGateway(snapshot, principal); err != nil {
		return lockedIntegrationReceipt{}, err
	}
	sourceRuntime, err := tx.GetRuntimeExecutionByTurnForUpdate(
		ctx, snapshot.TurnID, snapshot.Attempt,
	)
	if err != nil {
		return lockedIntegrationReceipt{}, err
	}
	if sourceRuntime.OrganizationID != snapshot.OrganizationID ||
		sourceRuntime.ProjectID != snapshot.ProjectID ||
		sourceRuntime.ProcessID != snapshot.ProcessID ||
		sourceRuntime.SessionID != snapshot.SessionID ||
		sourceRuntime.RuntimeRevisionID != snapshot.RuntimeRevisionID ||
		sourceRuntime.ImmutableInputSHA256 != snapshot.ImmutableInputSHA256 ||
		sourceRuntime.State != "SUSPENDED" ||
		sourceRuntime.TerminalReference != snapshot.ID ||
		sourceRuntime.TerminalSHA256 != snapshot.RequestSHA256 {
		return lockedIntegrationReceipt{}, errs.ErrStateConflict
	}
	turnID := snapshot.TurnID
	if snapshot.ContinuationState != "SUSPENDED" {
		turnID = snapshot.ContinuationTurnID
		if value.ValidateID(turnID) != nil {
			return lockedIntegrationReceipt{}, errs.ErrStateConflict
		}
	}
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, turnID)
	if err != nil {
		return lockedIntegrationReceipt{}, err
	}
	if graph.Process.ID != snapshot.ProcessID ||
		graph.Session.ID != snapshot.SessionID {
		return lockedIntegrationReceipt{}, errs.ErrStateConflict
	}
	if snapshot.ContinuationState == "SUSPENDED" {
		if graph.Runtime == nil || graph.Runtime.ID != sourceRuntime.ID {
			return lockedIntegrationReceipt{}, errs.ErrStateConflict
		}
	} else if graph.Runtime != nil {
		// После claim/rebind delivery stored terminal result уже не является
		// текущим response этой команды и не должен replay-иться.
		return lockedIntegrationReceipt{}, errs.ErrStateConflict
	}
	locked, err := service.lockIntegrationContinuationAfterBindings(
		ctx, tx, principal, snapshot, requireActiveBindings,
	)
	if err != nil {
		return lockedIntegrationReceipt{}, err
	}
	return lockedIntegrationReceipt{
		Continuation: locked, Graph: graph, SourceRuntime: sourceRuntime,
	}, nil
}

func integrationReceiptMatchesCurrent(
	current IntegrationContinuation,
	stored IntegrationContinuation,
) error {
	currentHash, err := canonicalHash(current)
	if err != nil {
		return errs.ErrInternal
	}
	storedHash, err := canonicalHash(stored)
	if err != nil {
		return errs.ErrInternal
	}
	if currentHash != storedHash {
		return errs.ErrStateConflict
	}
	return nil
}

func integrationMutationReceiptDisposition(
	continuation IntegrationContinuation,
	expectedVersion uint64,
	expectedFence uint64,
	sourceAllowed bool,
	replayAllowed bool,
) (lifecycleReceiptDisposition, error) {
	if continuation.Version == expectedVersion && continuation.Fence == expectedFence {
		if !sourceAllowed {
			return 0, errs.ErrStateConflict
		}
		return lifecycleReceiptApply, nil
	}
	if continuation.Version == expectedVersion+1 &&
		continuation.Fence == expectedFence+1 {
		if !replayAllowed {
			return 0, errs.ErrStateConflict
		}
		return lifecycleReceiptReplay, nil
	}
	return 0, errs.ErrVersionMismatch
}

// prelockIntegrationTerminalGraph берёт общий scheduled graph до owner row
// continuation. Это устраняет цикл continuation -> Session/Turn/ProcessRun
// против runtime/scheduler path, который приходит к continuation последней.
func (service *Service) prelockIntegrationTerminalGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	snapshot IntegrationContinuation,
) (IntegrationContinuation, error) {
	execution, err := tx.GetRuntimeExecutionByTurnForUpdate(
		ctx, snapshot.TurnID, snapshot.Attempt,
	)
	if err != nil {
		return IntegrationContinuation{}, err
	}
	if execution.OrganizationID != snapshot.OrganizationID ||
		execution.ProjectID != snapshot.ProjectID || execution.ProcessID != snapshot.ProcessID ||
		execution.SessionID != snapshot.SessionID || execution.TurnID != snapshot.TurnID ||
		execution.Attempt != snapshot.Attempt ||
		execution.RuntimeRevisionID != snapshot.RuntimeRevisionID ||
		execution.RuntimeRevisionVersion != snapshot.RuntimeRevisionVersion ||
		execution.ImmutableInputSHA256 != snapshot.ImmutableInputSHA256 ||
		execution.State != "SUSPENDED" {
		return IntegrationContinuation{}, errs.ErrStateConflict
	}
	if err := service.prelockRuntimeScheduledGraph(ctx, tx, principal, execution); err != nil {
		return IntegrationContinuation{}, err
	}
	session, err := tx.GetForUpdate(
		ctx, snapshot.OrganizationID, snapshot.ProjectID, snapshot.SessionID,
	)
	if err != nil {
		return IntegrationContinuation{}, err
	}
	turn, err := tx.GetForUpdate(
		ctx, snapshot.OrganizationID, snapshot.ProjectID, snapshot.TurnID,
	)
	if err != nil {
		return IntegrationContinuation{}, err
	}
	process, err := tx.GetForUpdate(
		ctx, snapshot.OrganizationID, snapshot.ProjectID, snapshot.ProcessID,
	)
	if err != nil {
		return IntegrationContinuation{}, err
	}
	if session.Kind != enum.KindSession || session.OwnerActorID != principal.ActorID ||
		turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		process.Kind != enum.KindProcessRun || process.OwnerActorID != principal.ActorID {
		return IntegrationContinuation{}, errs.ErrNotFound
	}
	return service.lockIntegrationContinuationAfterBindings(
		ctx, tx, principal, snapshot, false,
	)
}

func (service *Service) lockIntegrationContinuationAfterBindings(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	snapshot IntegrationContinuation,
	requireActiveBindings bool,
) (IntegrationContinuation, error) {
	if err := service.validatePinnedIntegrationContinuation(
		ctx, tx, principal, snapshot, requireActiveBindings,
	); err != nil {
		return IntegrationContinuation{}, err
	}
	locked, err := tx.GetIntegrationContinuationForUpdate(ctx, snapshot.ID)
	if err != nil {
		return IntegrationContinuation{}, err
	}
	if locked.Version != snapshot.Version || locked.Fence != snapshot.Fence ||
		locked.TurnID != snapshot.TurnID || locked.Attempt != snapshot.Attempt ||
		locked.SessionID != snapshot.SessionID || locked.ProcessID != snapshot.ProcessID ||
		locked.RuntimeRevisionID != snapshot.RuntimeRevisionID ||
		locked.ImmutableInputSHA256 != snapshot.ImmutableInputSHA256 {
		return IntegrationContinuation{}, errs.ErrStateConflict
	}
	return locked, nil
}

func (service *Service) materializeIntegrationContinuation(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	continuation *IntegrationContinuation,
	now time.Time,
) error {
	if continuation.ContinuationState != "SUSPENDED" ||
		continuation.ContinuationTurnID != "" {
		return errs.ErrStateConflict
	}
	outcomeDigest, err := integrationMaterializationOutcomeDigest(*continuation)
	if err != nil {
		return err
	}
	session, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, continuation.SessionID,
	)
	if err != nil {
		return err
	}
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.Kind != enum.KindSession || session.State != enum.StateWaitingExternal ||
		session.OwnerActorID != principal.ActorID ||
		session.Version != continuation.SessionVersion {
		return errs.ErrStateConflict
	}
	process, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, continuation.ProcessID,
	)
	if err != nil {
		return err
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		process.State != enum.StateWaitingExternal ||
		process.OwnerActorID != principal.ActorID || current.TurnID != continuation.TurnID ||
		current.SessionID != continuation.SessionID ||
		current.SessionVersion != continuation.SessionVersion ||
		current.TurnVersion != continuation.TurnVersion ||
		current.Attempt != continuation.Attempt ||
		current.RuntimeRevisionID != continuation.RuntimeRevisionID ||
		current.RuntimeRevisionVersion != continuation.RuntimeRevisionVersion ||
		current.InputSHA256 != continuation.ImmutableInputSHA256 {
		return errs.ErrStateConflict
	}
	previousTurn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, continuation.TurnID,
	)
	if err != nil {
		return err
	}
	previousSpec, ok := previousTurn.Spec.(entity.TurnSpec)
	if !ok || previousTurn.Kind != enum.KindTurn ||
		previousTurn.State != enum.StateWaitingExternal ||
		previousTurn.OwnerActorID != principal.ActorID {
		return errs.ErrStateConflict
	}
	// prelockIntegrationTerminalGraph уже держит RuntimeExecution первым в
	// каноническом порядке. Здесь выполняется только exact readback; поздний
	// FOR UPDATE после Session/Turn/ProcessRun запрещён.
	sourceRuntime, err := tx.GetRuntimeExecutionByTurn(
		ctx, continuation.TurnID, continuation.Attempt,
	)
	if err != nil {
		return err
	}
	sourceAttempt, err := tx.GetTurnAttemptForUpdate(
		ctx, continuation.TurnID, continuation.Attempt,
	)
	if err != nil {
		return err
	}
	if _, leaseErr := tx.GetTurnLeaseForUpdate(ctx, continuation.TurnID); leaseErr == nil {
		return errs.ErrStateConflict
	} else if !errors.Is(leaseErr, errs.ErrNotFound) {
		return leaseErr
	}
	replacedTurn, err := replaceIntegrationPredecessor(
		*continuation, sourceRuntime, previousTurn, sourceAttempt,
		agentRunnerWorkload, service.runtimeControllerWorkload,
		service.runtimeControllerSPIFFEID, now,
	)
	if err != nil {
		return err
	}
	open, err := tx.ProcessHasOpenWork(
		ctx, principal.OrganizationID, principal.ProjectID,
		process.ID, previousTurn.ID, "",
	)
	if err != nil {
		return err
	}
	if open {
		return errs.ErrStateConflict
	}
	if _, err := service.requireCleanArtifact(
		ctx, tx, principal, previousSpec.PromptArtifactID,
	); err != nil {
		return err
	}
	revision, err := service.createRuntimeRevision(ctx, tx, principal, session, sessionSpec)
	if err != nil {
		return err
	}
	revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok {
		return errs.ErrInternal
	}
	sourceReference := "integration-continuation:" + continuation.ID
	inputDigest := hashString(
		sourceReference + "\x00" + continuation.RequestSHA256 + "\x00" +
			outcomeDigest + "\x00" + revisionSpec.ManifestSHA256,
	)
	sessionSpec.LastTurnSequence++
	turn, err := entity.New(
		uuid.NewString(), principal.OrganizationID, principal.ProjectID,
		session.ID, principal.ActorID, enum.KindTurn,
		fmt.Sprintf("Integration continuation %d", sessionSpec.LastTurnSequence),
		entity.TurnSpec{
			SessionID: session.ID, Sequence: sessionSpec.LastTurnSequence,
			SourceRef: sourceReference, PromptArtifactID: previousSpec.PromptArtifactID,
			RuntimeRevisionID: revision.ID, ProcessRunID: process.ID,
			Attempt: 1, EffectiveInputSHA256: inputDigest,
			PredecessorTurnID: previousTurn.ID,
		},
		now,
	)
	if err != nil {
		return errs.ErrInternal
	}
	queuedSession, err := session.ReplaceAndTransition(sessionSpec, enum.StateQueued, now)
	if err != nil {
		return errs.ErrStateConflict
	}
	tuple := executionTuple{
		SessionID: session.ID, SessionVersion: queuedSession.Version,
		TurnID: turn.ID, TurnVersion: turn.Version, Attempt: 1,
		RuntimeRevisionID: revision.ID, RuntimeRevisionVersion: revision.Version,
		InputSHA256: inputDigest,
	}
	setCurrentExecution(&processSpec, tuple)
	if err := processSpec.SetIntegrationContinuation(entity.ProcessContinuationBinding{
		TurnID: turn.ID, TurnVersion: turn.Version, Attempt: 1,
		RuntimeRevisionID: revision.ID, RuntimeRevisionVersion: revision.Version,
		InputSHA256: inputDigest,
	}, continuation.ID, outcomeDigest); err != nil {
		return errs.ErrStateConflict
	}
	runningProcess, err := process.ReplaceAndTransition(processSpec, enum.StateRunning, now)
	if err != nil {
		return errs.ErrStateConflict
	}
	if err := tx.Update(ctx, replacedTurn, previousTurn.Version); err != nil {
		return err
	}
	if err := tx.Insert(ctx, turn); err != nil {
		return err
	}
	if err := tx.Update(ctx, queuedSession, session.Version); err != nil {
		return err
	}
	if err := tx.Update(ctx, runningProcess, process.Version); err != nil {
		return err
	}
	if err := service.materializeIntegrationScheduledGraph(
		ctx, tx, principal, processSpec, process, previousTurn,
		continuation.Attempt, turn, queuedSession, runningProcess, revision,
		inputDigest, now,
	); err != nil {
		return err
	}
	for action, changed := range map[string]entity.Resource{
		"replace_integration_predecessor_turn":    replacedTurn,
		"create_integration_continuation_turn":    turn,
		"queue_integration_continuation_session":  queuedSession,
		"rebind_integration_continuation_process": runningProcess,
	} {
		if err := service.appendMutationRecords(ctx, tx, principal, action, changed); err != nil {
			return err
		}
	}
	continuation.ContinuationState = "READY"
	continuation.ContinuationTurnID = turn.ID
	continuation.ContinuationTurnVersion = turn.Version
	continuation.ContinuationAttempt = 1
	continuation.ContinuationRuntimeRevisionID = revision.ID
	continuation.ContinuationRuntimeRevisionVersion = revision.Version
	continuation.ContinuationInputSHA256 = inputDigest
	return nil
}

func integrationMaterializationOutcomeDigest(
	continuation IntegrationContinuation,
) (string, error) {
	var digest string
	switch continuation.ApprovalState {
	case "REJECTED", "EXPIRED", "CANCELLED":
		if continuation.ExecutionState != "NOT_APPLICABLE" {
			return "", errs.ErrStateConflict
		}
		digest = continuation.DecisionSHA256
	case "APPROVED":
		switch continuation.ExecutionState {
		case "SUCCEEDED":
			digest = continuation.ResultSHA256
		case "FAILED":
			digest = continuation.ErrorSHA256
		default:
			return "", errs.ErrStateConflict
		}
	default:
		return "", errs.ErrStateConflict
	}
	if !validSHA256Text(digest) {
		return "", errs.ErrStateConflict
	}
	return digest, nil
}

func replaceIntegrationPredecessor(
	continuation IntegrationContinuation,
	sourceRuntime RuntimeExecution,
	previousTurn entity.Resource,
	sourceAttempt domainrepo.TurnAttempt,
	claimantWorkload string,
	runtimeWorkload string,
	runtimeSPIFFEID string,
	now time.Time,
) (entity.Resource, error) {
	previousSpec, ok := previousTurn.Spec.(entity.TurnSpec)
	if !ok || continuation.ContinuationState != "SUSPENDED" ||
		continuation.ContinuationTurnID != "" ||
		previousTurn.Kind != enum.KindTurn ||
		previousTurn.State != enum.StateWaitingExternal ||
		previousTurn.OrganizationID != continuation.OrganizationID ||
		previousTurn.ProjectID != continuation.ProjectID ||
		previousTurn.ParentID != continuation.SessionID ||
		previousTurn.ID != continuation.TurnID ||
		previousTurn.Version != continuation.TurnVersion ||
		previousSpec.SessionID != continuation.SessionID ||
		previousSpec.ProcessRunID != continuation.ProcessID ||
		previousSpec.Attempt != continuation.Attempt ||
		previousSpec.RuntimeRevisionID != continuation.RuntimeRevisionID ||
		previousSpec.EffectiveInputSHA256 != continuation.ImmutableInputSHA256 ||
		previousSpec.Outcome != "" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if claimantWorkload != agentRunnerWorkload ||
		runtimeWorkload == "" || runtimeSPIFFEID == "" ||
		runtimeWorkload == claimantWorkload || runtimeSPIFFEID == agentRunnerSPIFFEID ||
		sourceRuntime.WorkloadID != runtimeWorkload ||
		sourceRuntime.WorkloadSPIFFEID != runtimeSPIFFEID ||
		sourceRuntime.OrganizationID != continuation.OrganizationID ||
		sourceRuntime.ProjectID != continuation.ProjectID ||
		sourceRuntime.ProcessID != continuation.ProcessID ||
		sourceRuntime.SessionID != continuation.SessionID ||
		sourceRuntime.ThreadID != continuation.ThreadID ||
		sourceRuntime.RoleID != continuation.RoleID ||
		sourceRuntime.TurnID != continuation.TurnID ||
		sourceRuntime.Attempt != continuation.Attempt ||
		sourceRuntime.RuntimeRevisionID != continuation.RuntimeRevisionID ||
		sourceRuntime.RuntimeRevisionVersion != continuation.RuntimeRevisionVersion ||
		sourceRuntime.RuntimeRevisionSHA256 != continuation.RuntimeRevisionSHA256 ||
		sourceRuntime.ImmutableInputSHA256 != continuation.ImmutableInputSHA256 ||
		sourceRuntime.GrantGeneration != continuation.GrantGeneration ||
		sourceRuntime.State != "SUSPENDED" ||
		sourceRuntime.TerminalOutcome != "SUSPENDED" ||
		sourceRuntime.TerminalReference != continuation.ID ||
		sourceRuntime.TerminalSHA256 != continuation.RequestSHA256 ||
		sourceRuntime.LeaseID != "" || sourceRuntime.LeaseTokenSHA256 != "" ||
		!sourceRuntime.LeaseExpiresAt.IsZero() {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if sourceAttempt.TurnID != continuation.TurnID ||
		sourceAttempt.Attempt != continuation.Attempt ||
		sourceAttempt.WorkloadID != claimantWorkload ||
		sourceAttempt.AuthorityGeneration != continuation.GrantGeneration ||
		sourceAttempt.LeaseFence == 0 || continuation.TurnVersion <= 1 ||
		sourceAttempt.LeaseFence != continuation.TurnVersion-1 ||
		sourceAttempt.State != "WAITING_EXTERNAL" ||
		sourceAttempt.InputSHA256 != continuation.ImmutableInputSHA256 ||
		sourceAttempt.StartedAt.IsZero() || sourceAttempt.FinishedAt.IsZero() ||
		sourceAttempt.FinishedAt.Before(sourceAttempt.StartedAt) ||
		sourceAttempt.Outcome != "integration_approval" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	previousSpec.Outcome = integrationPredecessorOutcome
	replaced, err := previousTurn.ReplaceAndTransition(
		previousSpec, enum.StateCancelled, now,
	)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return replaced, nil
}

func (service *Service) materializeIntegrationScheduledGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	processSpec entity.ProcessRunSpec,
	previousProcess entity.Resource,
	previousTurn entity.Resource,
	previousAttempt uint32,
	continuationTurn entity.Resource,
	queuedSession entity.Resource,
	runningProcess entity.Resource,
	revision entity.Resource,
	inputSHA256 string,
	now time.Time,
) error {
	if processSpec.OccurrenceID == "" {
		if processSpec.ScheduleID != "" {
			return errs.ErrStateConflict
		}
		return nil
	}
	if processSpec.ScheduleID == "" {
		return errs.ErrStateConflict
	}
	continuationSpec, ok := continuationTurn.Spec.(entity.TurnSpec)
	if !ok || continuationSpec.Attempt == 0 ||
		continuationSpec.EffectiveInputSHA256 != inputSHA256 {
		return errs.ErrStateConflict
	}
	previousSpec, ok := previousTurn.Spec.(entity.TurnSpec)
	if !ok || previousSpec.Attempt != previousAttempt {
		return errs.ErrStateConflict
	}
	occurrence, err := tx.GetScheduleOccurrenceForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, processSpec.OccurrenceID,
	)
	if err != nil {
		return err
	}
	schedule, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, processSpec.ScheduleID,
	)
	if err != nil {
		return err
	}
	if schedule.Kind != enum.KindSchedule ||
		schedule.OwnerActorID != previousProcess.OwnerActorID {
		return errs.ErrStateConflict
	}
	run, err := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
	if err != nil || validateScheduledRunBinding(occurrence, run) != nil ||
		occurrence.State != "CONTINUATION" || run.State != "CONTINUATION" ||
		occurrence.ScheduleID != processSpec.ScheduleID ||
		occurrence.ExecutionSessionVersion+1 != queuedSession.Version ||
		occurrence.ExecutionProcessRunID != previousProcess.ID ||
		occurrence.ExecutionProcessVersion != previousProcess.Version ||
		occurrence.ExecutionTurnID != previousTurn.ID ||
		occurrence.ExecutionTurnVersion != previousTurn.Version ||
		occurrence.ExecutionRuntimeRevisionID != previousSpec.RuntimeRevisionID ||
		occurrence.EffectiveInputSHA256 != previousSpec.EffectiveInputSHA256 ||
		run.CurrentTurnAttempt != previousAttempt ||
		run.CurrentProcessVersion != previousProcess.Version {
		return errs.ErrStateConflict
	}
	occurrence.ExecutionSessionID = queuedSession.ID
	occurrence.ExecutionSessionVersion = queuedSession.Version
	occurrence.ExecutionTurnID = continuationTurn.ID
	occurrence.ExecutionTurnVersion = continuationTurn.Version
	occurrence.ExecutionProcessRunID = runningProcess.ID
	occurrence.ExecutionProcessVersion = runningProcess.Version
	occurrence.ExecutionRuntimeRevisionID = revision.ID
	occurrence.ExecutionRuntimeRevisionVersion = revision.Version
	occurrence.EffectiveInputSHA256 = inputSHA256
	occurrence.Outcome = ""
	occurrence.ResultArtifactID = ""
	occurrence.UpdatedAt = now
	if err := tx.UpdateScheduleOccurrence(
		ctx, occurrence, occurrence.Attempt, occurrence.TokenHash,
	); err != nil {
		return err
	}
	run.CurrentSessionID = queuedSession.ID
	run.CurrentSessionVersion = queuedSession.Version
	run.CurrentTurnID = continuationTurn.ID
	run.CurrentTurnVersion = continuationTurn.Version
	run.CurrentTurnAttempt = continuationSpec.Attempt
	run.CurrentProcessRunID = runningProcess.ID
	run.CurrentProcessVersion = runningProcess.Version
	run.CurrentRuntimeRevisionID = revision.ID
	run.CurrentRuntimeRevisionVersion = revision.Version
	run.CurrentInputSHA256 = inputSHA256
	if err := tx.RebindScheduledRun(
		ctx, run, previousTurn.ID, previousAttempt,
	); err != nil {
		return err
	}
	return appendScheduleOccurrenceAudit(
		ctx, tx, principal, "materialize_integration_schedule", occurrence,
	)
}

func (service *Service) GetIntegrationContinuation(
	ctx context.Context,
	input GetIntegrationContinuationInput,
) (IntegrationContinuation, error) {
	if err := authorize(input.Principal, permissionIntegrationRead); err != nil {
		return IntegrationContinuation{}, err
	}
	if input.Principal.CallerWorkload != agentRunnerWorkload ||
		input.Principal.CallerSPIFFEID != agentRunnerSPIFFEID ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		value.ValidateID(input.Principal.AuthorityReference) != nil {
		return IntegrationContinuation{}, errs.ErrPermissionDenied
	}
	var result IntegrationContinuation
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID, ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		resolved, err := service.resolveBoundExecution(ctx, tx, input.Principal)
		if err != nil {
			return err
		}
		continuation, err := tx.GetIntegrationContinuationByContinuationTurn(
			ctx, resolved.Turn.ID,
		)
		if err != nil {
			return err
		}
		continuation, err = service.lockIntegrationContinuationAfterBindings(
			ctx, tx, input.Principal, continuation, false,
		)
		if err != nil {
			return err
		}
		if continuation.ContinuationState != "READY" ||
			continuation.ContinuationTurnID != input.Principal.AuthorityReference ||
			resolved.Turn.ID != continuation.ContinuationTurnID ||
			resolved.Process.ID != continuation.ProcessID ||
			resolved.Session.ID != continuation.SessionID ||
			resolved.Revision.ID != continuation.ContinuationRuntimeRevisionID ||
			resolved.Revision.Version != continuation.ContinuationRuntimeRevisionVersion ||
			continuation.ContinuationInputSHA256 != input.Principal.AuthorityDigest ||
			continuation.ContinuationAttempt != uint32(input.Principal.AuthorityRevision) ||
			resolved.TurnSpec.Attempt != continuation.ContinuationAttempt {
			return errs.ErrNotFound
		}
		result = continuation
		return nil
	})
	return result, err
}

func (service *Service) AcknowledgeIntegrationContinuation(
	ctx context.Context,
	input AcknowledgeIntegrationContinuationInput,
) (IntegrationContinuation, error) {
	if err := authorize(input.Principal, permissionIntegrationAcknowledge); err != nil {
		return IntegrationContinuation{}, err
	}
	if input.Principal.CallerWorkload != agentRunnerWorkload ||
		input.Principal.CallerSPIFFEID != agentRunnerSPIFFEID ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.ExpectedVersion == 0 || input.ExpectedFence == 0 ||
		!validSHA256Text(input.ExpectedInputSHA256) {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, acknowledgeIntegrationIntent{
		ExpectedVersion: input.ExpectedVersion, ExpectedFence: input.ExpectedFence,
		ExpectedInputSHA256: input.ExpectedInputSHA256,
	})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	const receiptScope = "acknowledge_integration_continuation"
	keyHash := hashString(input.IdempotencyKey)
	var result IntegrationContinuation
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		resolved, err := service.resolveBoundExecution(ctx, tx, input.Principal)
		if err != nil {
			return err
		}
		continuation, err := tx.GetIntegrationContinuationByContinuationTurn(
			ctx, resolved.Turn.ID,
		)
		if err != nil {
			return err
		}
		continuation, err = service.lockIntegrationContinuationAfterBindings(
			ctx, tx, input.Principal, continuation, false,
		)
		if err != nil {
			return err
		}
		if continuation.ContinuationTurnID != input.Principal.AuthorityReference ||
			continuation.ContinuationAttempt != uint32(input.Principal.AuthorityRevision) ||
			resolved.TurnSpec.Attempt != continuation.ContinuationAttempt ||
			continuation.ContinuationInputSHA256 != input.Principal.AuthorityDigest {
			return errs.ErrNotFound
		}
		receipt, receiptErr := tx.GetReceipt(
			ctx, input.Principal.OrganizationID, receiptScope, keyHash,
		)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || len(receipt.Payload) == 0 ||
				json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			if continuation.ContinuationState != "REJOINED" ||
				result.ID != continuation.ID || result.Version != continuation.Version ||
				result.Fence != continuation.Fence ||
				result.ContinuationAttempt != continuation.ContinuationAttempt ||
				result.ContinuationRuntimeRevisionID !=
					continuation.ContinuationRuntimeRevisionID ||
				result.ContinuationInputSHA256 != continuation.ContinuationInputSHA256 {
				return errs.ErrStateConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		if continuation.ContinuationState != "READY" ||
			continuation.Version != input.ExpectedVersion ||
			continuation.Fence != input.ExpectedFence ||
			resolved.Turn.ID != continuation.ContinuationTurnID ||
			resolved.Process.ID != continuation.ProcessID ||
			resolved.Session.ID != continuation.SessionID ||
			resolved.Revision.ID != continuation.ContinuationRuntimeRevisionID ||
			resolved.Revision.Version != continuation.ContinuationRuntimeRevisionVersion ||
			continuation.ContinuationInputSHA256 != input.ExpectedInputSHA256 {
			return errs.ErrStateConflict
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		expectedVersion, expectedFence := continuation.Version, continuation.Fence
		continuation.Version++
		continuation.Fence++
		continuation.ContinuationState = "REJOINED"
		continuation.UpdatedAt = now
		if err := tx.UpdateIntegrationContinuation(
			ctx, continuation, expectedVersion, expectedFence,
		); err != nil {
			return err
		}
		result = continuation
		if err := service.appendLifecycleAudit(
			ctx, tx, input.Principal, receiptScope,
			continuation.ID, "INTEGRATION_CONTINUATION", continuation.Version, now,
		); err != nil {
			return err
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			Scope:          receiptScope,
			KeyHash:        keyHash,
			RequestHash:    requestHash,
			Payload:        payload,
			CreatedAt:      now,
		})
	})
	return result, err
}

func (service *Service) requireRuntimeOwner(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
) error {
	turn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, execution.TurnID,
	)
	if err != nil {
		return err
	}
	if turn.OwnerActorID != principal.ActorID {
		return errs.ErrNotFound
	}
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || spec.Attempt != execution.Attempt ||
		spec.ProcessRunID != execution.ProcessID || spec.SessionID != execution.SessionID ||
		spec.RuntimeRevisionID != execution.RuntimeRevisionID ||
		spec.EffectiveInputSHA256 != execution.ImmutableInputSHA256 {
		return errs.ErrStateConflict
	}
	return nil
}
