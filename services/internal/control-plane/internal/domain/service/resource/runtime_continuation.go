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
	minimumApprovalLifetime      = time.Minute
	maximumApprovalLifetime      = 7 * 24 * time.Hour
	cleanupAuthorizationLifetime = 15 * time.Minute
)

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
}

func (service *Service) withLifecycleReceipt(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey, scope, requestHash string,
	result any,
	apply func(domainrepo.Transaction) error,
) error {
	keyHash := hashString(idempotencyKey)
	return service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID,
		ProjectID:      principal.ProjectID,
		ActorID:        principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, err := tx.GetReceipt(ctx, principal.OrganizationID, scope, keyHash)
		if err == nil {
			if receipt.RequestHash != requestHash ||
				len(receipt.Payload) == 0 {
				return errs.ErrIdempotencyConflict
			}
			if json.Unmarshal(receipt.Payload, result) != nil {
				return errs.ErrInternal
			}
			return nil
		}
		if !errors.Is(err, errs.ErrNotFound) {
			return err
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
	turn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, principal.AuthorityReference,
	)
	if err != nil {
		return resolvedExecution{}, err
	}
	turnSpec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		turnSpec.Attempt != uint32(principal.AuthorityRevision) ||
		turnSpec.EffectiveInputSHA256 != principal.AuthorityDigest ||
		turnSpec.ProcessRunID == "" {
		return resolvedExecution{}, errs.ErrNotFound
	}
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
		lease.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		!lease.ExpiresAt.After(now) ||
		attempt.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		attempt.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
		!attempt.FinishedAt.IsZero() {
		return resolvedExecution{}, errs.ErrNotFound
	}
	session, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, turnSpec.SessionID,
	)
	if err != nil {
		return resolvedExecution{}, err
	}
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.Kind != enum.KindSession || session.OwnerActorID != turn.OwnerActorID {
		return resolvedExecution{}, errs.ErrStateConflict
	}
	process, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, turnSpec.ProcessRunID,
	)
	if err != nil {
		return resolvedExecution{}, err
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if !ok || currentErr != nil || process.Kind != enum.KindProcessRun ||
		process.OwnerActorID != turn.OwnerActorID || process.State.Terminal() ||
		current.SessionID != session.ID || current.TurnID != turn.ID ||
		current.Attempt != turnSpec.Attempt ||
		current.RuntimeRevisionID != turnSpec.RuntimeRevisionID ||
		current.InputSHA256 != turnSpec.EffectiveInputSHA256 {
		return resolvedExecution{}, errs.ErrStateConflict
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
	return resolvedExecution{
		Turn: turn, TurnSpec: turnSpec, Session: session, SessionSpec: sessionSpec,
		Process: process, ProcessSpec: processSpec, Revision: revision,
		RevisionSpec: revisionSpec,
		Role:         role, RoleSpec: roleSpec, RevisionHash: revisionHash,
	}, nil
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
	requestHash, err := canonicalHash(struct{ Identity commandIdentity }{identity(principal)})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, principal, idempotencyKey, "claim_runtime_execution", requestHash, &result,
		func(tx domainrepo.Transaction) error {
			resolved, err := service.resolveBoundExecution(ctx, tx, principal)
			if err != nil {
				return err
			}
			existing, err := tx.GetRuntimeExecutionByTurnForUpdate(
				ctx, resolved.Turn.ID, resolved.TurnSpec.Attempt,
			)
			if err == nil {
				if existing.GrantGeneration != principal.AuthorityGrantGeneration ||
					existing.RuntimeRevisionSHA256 != resolved.RevisionHash ||
					existing.ImmutableInputSHA256 != principal.AuthorityDigest {
					return errs.ErrStateConflict
				}
				result = existing
				return nil
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return err
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
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    RuntimeExecutionInput
	}{identity(input.Principal), input})
	if err != nil {
		return AdmitRuntimeExecutionResult{}, errs.ErrInvalidInput
	}
	var result AdmitRuntimeExecutionResult
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "admit_runtime_execution",
		requestHash, &result, func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input, "PENDING"); err != nil {
				return err
			}
			now, err := service.requireActiveRuntimeGraph(
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
	requestHash, err := canonicalHash(struct {
		Identity  commandIdentity
		Execution string
		Version   uint64
		Fence     uint64
		TokenHash string
	}{identity(input.Principal), input.ExecutionID, input.ExpectedVersion,
		input.ExpectedFence, hashString(input.LeaseToken)})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "heartbeat_runtime_execution",
		requestHash, &result, func(tx domainrepo.Transaction) error {
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "record_runtime_incident",
		requestHash, &result, func(tx domainrepo.Transaction) error {
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
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    CompleteRuntimeExecutionInput
		Token    string
	}{identity(input.Principal), input, hashString(input.LeaseToken)})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "complete_runtime_execution",
		requestHash, &result, func(tx domainrepo.Transaction) error {
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "cancel_runtime_execution",
		requestHash, &result, func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RetryRuntimeExecutionResult{}, errs.ErrInvalidInput
	}
	var result RetryRuntimeExecutionResult
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "retry_runtime_execution",
		requestHash, &result, func(tx domainrepo.Transaction) error {
			execution, err := tx.GetRuntimeExecutionForUpdate(ctx, input.ExecutionID)
			if err != nil {
				return err
			}
			if err := service.requireRuntimeOwner(ctx, tx, input.Principal, execution); err != nil {
				return err
			}
			if err := matchRuntimeMutation(execution, input); err != nil {
				return errs.ErrStateConflict
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			var turn entity.Resource
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
			turnSpec, ok := turn.Spec.(entity.TurnSpec)
			if !ok {
				return errs.ErrInternal
			}
			retried, _, err := service.prepareRetriedExecution(
				ctx, tx, input.Principal, turn, turnSpec, now,
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
	requestHash, err := canonicalHash(struct{ Identity commandIdentity }{identity(principal)})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, principal, idempotencyKey, "expire_runtime_execution", requestHash,
		&result, func(tx domainrepo.Transaction) error {
			execution, err := tx.NextExpiredRuntimeExecution(
				ctx, principal.OrganizationID, principal.ProjectID,
				principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err != nil {
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "record_runtime_archive",
		requestHash, &result, func(tx domainrepo.Transaction) error {
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "verify_runtime_restore",
		requestHash, &result, func(tx domainrepo.Transaction) error {
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "authorize_runtime_cleanup",
		requestHash, &result, func(tx domainrepo.Transaction) error {
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
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, execution.TurnID, execution.Attempt,
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey,
		"consume_runtime_cleanup_authorization", requestHash, &result,
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
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, execution.TurnID, execution.Attempt,
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
	requestHash, err := canonicalHash(input)
	if err != nil {
		return RuntimeExecution{}, errs.ErrInvalidInput
	}
	var result RuntimeExecution
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey,
		"expire_runtime_cleanup_authorization", requestHash, &result,
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
	turn, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, execution.TurnID,
	)
	if err != nil {
		return time.Time{}, err
	}
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.OwnerActorID != principal.ActorID ||
		(turn.State != enum.StateClaimed && turn.State != enum.StateRunning) ||
		spec.Attempt != execution.Attempt ||
		spec.ProcessRunID != execution.ProcessID || spec.SessionID != execution.SessionID ||
		spec.RuntimeRevisionID != execution.RuntimeRevisionID ||
		spec.EffectiveInputSHA256 != execution.ImmutableInputSHA256 {
		return time.Time{}, errs.ErrStateConflict
	}
	lease, err := tx.GetTurnLeaseForUpdate(ctx, turn.ID)
	if err != nil {
		return time.Time{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, execution.Attempt)
	if err != nil {
		return time.Time{}, err
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if lease.Attempt != execution.Attempt ||
		lease.AuthorityGeneration != execution.GrantGeneration ||
		!lease.ExpiresAt.After(now) ||
		attempt.AuthorityGeneration != execution.GrantGeneration ||
		attempt.InputSHA256 != execution.ImmutableInputSHA256 ||
		!attempt.FinishedAt.IsZero() {
		return time.Time{}, errs.ErrStateConflict
	}
	return now, nil
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
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return IntegrationSessionContext{}, err
	}
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
				credential.State != enum.StateActive || !bound || !matches ||
				(!credentialSpec.ExpiresAt.IsZero() &&
					!credentialSpec.ExpiresAt.After(now)) {
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
	now time.Time,
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
			credentialComponent.ProjectionSHA256 != selected.ProjectionSHA256 ||
			(!credentialSpec.ExpiresAt.IsZero() && !credentialSpec.ExpiresAt.After(now)) {
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
	invocationID string,
	requestSHA256 string,
	now time.Time,
) error {
	execution, err := tx.GetRuntimeExecutionByTurnForUpdate(
		ctx, resolved.Turn.ID, resolved.TurnSpec.Attempt,
	)
	if err != nil {
		return err
	}
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
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return err
	}
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
		if matchErr != nil || !ok || !matches || credential.State != enum.StateActive ||
			(!credentialSpec.ExpiresAt.IsZero() && !credentialSpec.ExpiresAt.After(now)) {
			return errs.ErrStateConflict
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
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    SuspendIntegrationInput
	}{identity(input.Principal), input})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	var result IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, "suspend_integration_approval",
		requestHash, &result, func(tx domainrepo.Transaction) error {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if input.ApprovalExpiresAt.Before(now.Add(minimumApprovalLifetime)) ||
				input.ApprovalExpiresAt.After(now.Add(maximumApprovalLifetime)) {
				return errs.ErrInvalidInput
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
				ctx, tx, input.Principal, resolved, input, now,
			)
			if err != nil {
				return err
			}
			if err := service.suspendRuntimeExecutionForIntegration(
				ctx, tx, input.Principal, resolved, input.InvocationID,
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
				ID: uuid.NewString(), OrganizationID: input.Principal.OrganizationID,
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
		lease.AuthorityGeneration != principal.AuthorityGrantGeneration {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.DeleteTurnLease(ctx, resolved.Turn.ID, lease.Fence); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	attempt, err := tx.GetTurnAttemptForUpdate(
		ctx, resolved.Turn.ID, resolved.TurnSpec.Attempt,
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	if attempt.InputSHA256 != resolved.TurnSpec.EffectiveInputSHA256 ||
		attempt.AuthorityGeneration != principal.AuthorityGrantGeneration ||
		!attempt.FinishedAt.IsZero() {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	attempt.State = "WAITING_EXTERNAL"
	attempt.FinishedAt = now
	attempt.Outcome = "integration_approval"
	if err := tx.FinishTurnAttempt(ctx, attempt); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	suspendedTurn, err := resolved.Turn.Transition(enum.StateWaitingExternal, now)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, suspendedTurn, resolved.Turn.Version); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	suspendedSession, err := resolved.Session.ReplaceAndTransition(
		resolved.SessionSpec, enum.StateWaitingExternal, now,
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, suspendedSession, resolved.Session.Version); err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, err
	}
	suspendedProcess, err := resolved.Process.ReplaceAndTransition(
		resolved.ProcessSpec, enum.StateWaitingExternal, now,
	)
	if err != nil {
		return entity.Resource{}, entity.Resource{}, entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, suspendedProcess, resolved.Process.Version); err != nil {
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
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Decision string
		Input    IntegrationDecisionInput
	}{identity(input.Principal), decision, input})
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
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, scope, requestHash, &result,
		func(tx domainrepo.Transaction) error {
			continuation, err := tx.GetIntegrationContinuationForUpdate(
				ctx, input.ContinuationID,
			)
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
	requestHash, err := canonicalHash(struct{ Identity commandIdentity }{identity(principal)})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	var result IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, principal, idempotencyKey, "expire_integration_invocation",
		requestHash, &result, func(tx domainrepo.Transaction) error {
			continuation, err := tx.NextExpiredIntegrationContinuation(
				ctx, principal.OrganizationID, principal.ProjectID,
				principal.AuthorityReference, uint32(principal.AuthorityRevision),
			)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
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
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Target   string
		Input    IntegrationExecutionInput
	}{identity(input.Principal), target, input})
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
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey, scope, requestHash, &result,
		func(tx domainrepo.Transaction) error {
			continuation, err := tx.GetIntegrationContinuationForUpdate(
				ctx, input.ContinuationID,
			)
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
	session, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, continuation.SessionID,
	)
	if err != nil {
		return err
	}
	sessionSpec, ok := session.Spec.(entity.SessionSpec)
	if !ok || session.Kind != enum.KindSession || session.State != enum.StateWaitingExternal ||
		session.OwnerActorID != principal.ActorID {
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
		current.Attempt != continuation.Attempt ||
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
	outcomeDigest := continuation.DecisionSHA256
	if continuation.ExecutionState == "SUCCEEDED" {
		outcomeDigest = continuation.ResultSHA256
	} else if continuation.ExecutionState == "FAILED" {
		outcomeDigest = continuation.ErrorSHA256
	}
	if !validSHA256Text(outcomeDigest) {
		return errs.ErrStateConflict
	}
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
	processSpec.ContinuationTurnID = turn.ID
	processSpec.ContinuationTurnVersion = turn.Version
	processSpec.ContinuationAttempt = 1
	processSpec.ContinuationRuntimeRevisionID = revision.ID
	processSpec.ContinuationRuntimeRevisionVersion = revision.Version
	processSpec.ContinuationInputSHA256 = inputDigest
	runningProcess, err := process.ReplaceAndTransition(processSpec, enum.StateRunning, now)
	if err != nil {
		return errs.ErrStateConflict
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
	for action, changed := range map[string]entity.Resource{
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
	continuation.ContinuationRuntimeRevisionID = revision.ID
	continuation.ContinuationRuntimeRevisionVersion = revision.Version
	continuation.ContinuationInputSHA256 = inputDigest
	return nil
}

func (service *Service) GetIntegrationContinuation(
	ctx context.Context,
	input GetIntegrationContinuationInput,
) (IntegrationContinuation, error) {
	if err := authorize(input.Principal, permissionIntegrationRead); err != nil {
		return IntegrationContinuation{}, err
	}
	if input.Principal.CallerWorkload != "agent-runner" ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		value.ValidateID(input.Principal.AuthorityReference) != nil {
		return IntegrationContinuation{}, errs.ErrPermissionDenied
	}
	var result IntegrationContinuation
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID, ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		continuation, err := tx.GetIntegrationContinuationByContinuationTurn(
			ctx, input.Principal.AuthorityReference,
		)
		if err != nil {
			return err
		}
		resolved, err := service.resolveBoundExecution(ctx, tx, input.Principal)
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
			input.Principal.AuthorityRevision != 1 {
			return errs.ErrNotFound
		}
		if err := service.validatePinnedIntegrationContinuation(
			ctx, tx, input.Principal, continuation, false,
		); err != nil {
			return err
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
	if input.Principal.CallerWorkload != "agent-runner" ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.ExpectedVersion == 0 || input.ExpectedFence == 0 ||
		!validSHA256Text(input.ExpectedInputSHA256) {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    AcknowledgeIntegrationContinuationInput
	}{identity(input.Principal), input})
	if err != nil {
		return IntegrationContinuation{}, errs.ErrInvalidInput
	}
	var result IntegrationContinuation
	err = service.withLifecycleReceipt(
		ctx, input.Principal, input.IdempotencyKey,
		"acknowledge_integration_continuation", requestHash, &result,
		func(tx domainrepo.Transaction) error {
			continuation, err := tx.GetIntegrationContinuationByContinuationTurn(
				ctx, input.Principal.AuthorityReference,
			)
			if err != nil {
				return err
			}
			resolved, err := service.resolveBoundExecution(ctx, tx, input.Principal)
			if err != nil {
				return err
			}
			if continuation.ContinuationState != "READY" ||
				continuation.Version != input.ExpectedVersion ||
				continuation.Fence != input.ExpectedFence ||
				resolved.Turn.ID != continuation.ContinuationTurnID ||
				resolved.Process.ID != continuation.ProcessID ||
				resolved.Session.ID != continuation.SessionID ||
				resolved.Revision.ID != continuation.ContinuationRuntimeRevisionID ||
				resolved.Revision.Version != continuation.ContinuationRuntimeRevisionVersion ||
				continuation.ContinuationTurnID != input.Principal.AuthorityReference ||
				continuation.ContinuationInputSHA256 != input.ExpectedInputSHA256 ||
				continuation.ContinuationInputSHA256 != input.Principal.AuthorityDigest {
				return errs.ErrStateConflict
			}
			if err := service.validatePinnedIntegrationContinuation(
				ctx, tx, input.Principal, continuation, false,
			); err != nil {
				return err
			}
			now := service.now().UTC().Truncate(time.Microsecond)
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
			return service.appendLifecycleAudit(
				ctx, tx, input.Principal, "acknowledge_integration_continuation",
				continuation.ID, "INTEGRATION_CONTINUATION", continuation.Version, now,
			)
		},
	)
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
