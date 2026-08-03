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

// requireLifecycleOwner повторно связывает lifecycle-команду с сохранённым
// владельцем. Project membership и caller-supplied ID этот барьер не заменяют.
func requireLifecycleOwner(principal value.Principal, resource entity.Resource) error {
	if resource.OwnerActorID != principal.ActorID {
		return errs.ErrNotFound
	}
	return nil
}

func (service *Service) revokeExecutionClaims(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	processRunID, turnID, reason string,
	now time.Time,
) error {
	return service.revokeExecutionClaimsForOwner(
		ctx, tx, principal, principal.ActorID, processRunID, turnID, reason, now,
	)
}

func (service *Service) revokeExecutionClaimsForOwner(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	ownerActorID, processRunID, turnID, reason string,
	now time.Time,
) error {
	claims, err := tx.ActiveWorkClaimsForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, processRunID, turnID,
	)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		if claim.OwnerActorID != ownerActorID {
			return errs.ErrNotFound
		}
		cancelled, transitionErr := claim.Transition(enum.StateCancelled, now)
		if transitionErr != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelled, claim.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(
			ctx, tx, principal, "revoke_work_claim_"+reason, cancelled,
		); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) cancelTurnExecution(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turn entity.Resource,
	reason string,
	now time.Time,
) (entity.Resource, error) {
	return service.cancelTurnExecutionForOwner(
		ctx, tx, principal, principal.ActorID, turn, reason, now,
	)
}

func (service *Service) cancelTurnExecutionForOwner(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	ownerActorID string,
	turn entity.Resource,
	reason string,
	now time.Time,
) (entity.Resource, error) {
	if turn.OwnerActorID != ownerActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	spec, ok := turn.Spec.(entity.TurnSpec)
	if !ok || turn.Kind != enum.KindTurn || turn.State.Terminal() {
		return entity.Resource{}, errs.ErrStateConflict
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, spec.Attempt)
	if err != nil || attempt.InputSHA256 != spec.EffectiveInputSHA256 ||
		attempt.FinishedAt.After(time.Unix(0, 0)) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	lease, leaseErr := tx.GetTurnLeaseForUpdate(ctx, turn.ID)
	if leaseErr != nil && !errors.Is(leaseErr, errs.ErrNotFound) {
		return entity.Resource{}, leaseErr
	}
	if leaseErr == nil {
		if lease.Attempt != spec.Attempt || lease.Fence != turn.Version ||
			lease.AuthorityGeneration != attempt.AuthorityGeneration {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := tx.DeleteTurnLease(ctx, turn.ID, lease.Fence); err != nil {
			return entity.Resource{}, err
		}
	}
	spec.Outcome = reason
	cancelled, err := turn.ReplaceAndTransition(spec, enum.StateCancelled, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, cancelled, turn.Version); err != nil {
		return entity.Resource{}, err
	}
	attempt.State = "CANCELLED"
	attempt.FinishedAt = now
	attempt.Outcome = reason
	if err := tx.FinishTurnAttempt(ctx, attempt); err != nil {
		return entity.Resource{}, err
	}
	if err := service.revokeExecutionClaimsForOwner(
		ctx, tx, principal, ownerActorID, spec.ProcessRunID, turn.ID, reason, now,
	); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "cancel_turn_graph", cancelled,
	); err != nil {
		return entity.Resource{}, err
	}
	return cancelled, nil
}

func validateScheduledRunBinding(
	occurrence domainrepo.ScheduleOccurrence,
	run domainrepo.ScheduledRun,
) error {
	if !validSHA256Text(run.EffectiveInputSHA256) {
		return errs.ErrStateConflict
	}
	if run.CurrentSessionID == "" {
		run.CurrentSessionID = run.SessionID
		run.CurrentSessionVersion = run.SessionVersion
		run.CurrentTurnID = run.TurnID
		run.CurrentTurnVersion = run.TurnVersion
		run.CurrentTurnAttempt = 1
		run.CurrentProcessRunID = run.ProcessRunID
		run.CurrentProcessVersion = run.ProcessVersion
		run.CurrentRuntimeRevisionID = run.RuntimeRevisionID
		run.CurrentRuntimeRevisionVersion = run.RuntimeRevisionVersion
		run.CurrentInputSHA256 = run.EffectiveInputSHA256
		if run.ContinuationTurnID != "" {
			run.CurrentTurnID = run.ContinuationTurnID
			run.CurrentTurnVersion = run.ContinuationTurnVersion
			run.CurrentRuntimeRevisionID = run.ContinuationRuntimeRevisionID
			run.CurrentRuntimeRevisionVersion = run.ContinuationRuntimeRevisionVersion
			run.CurrentInputSHA256 = run.ContinuationInputSHA256
		}
	}
	if !validSHA256Text(run.CurrentInputSHA256) {
		return errs.ErrStateConflict
	}
	if run.OccurrenceID != occurrence.ID || run.Attempt != occurrence.Attempt ||
		run.CurrentSessionID != occurrence.ExecutionSessionID ||
		run.CurrentSessionVersion != occurrence.ExecutionSessionVersion ||
		run.CurrentTurnID != occurrence.ExecutionTurnID ||
		run.CurrentTurnVersion != occurrence.ExecutionTurnVersion ||
		run.CurrentProcessRunID != occurrence.ExecutionProcessRunID ||
		run.CurrentProcessVersion > occurrence.ExecutionProcessVersion ||
		run.CurrentRuntimeRevisionID != occurrence.ExecutionRuntimeRevisionID ||
		run.CurrentRuntimeRevisionVersion != occurrence.ExecutionRuntimeRevisionVersion ||
		run.CurrentInputSHA256 != occurrence.EffectiveInputSHA256 {
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) validateActiveWorkClaimGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graph lockedOwnerGraph,
	claim entity.Resource,
	spec entity.WorkClaimSpec,
) error {
	session, turn, process := graph.Session, graph.Turn, graph.Process
	turnSpec, turnOK := turn.Spec.(entity.TurnSpec)
	processSpec, processOK := process.Spec.(entity.ProcessRunSpec)
	if !turnOK || !processOK || claim.State != enum.StateActive ||
		session.Kind != enum.KindSession || session.State != enum.StateActive ||
		turn.Kind != enum.KindTurn || turn.State.Terminal() ||
		process.Kind != enum.KindProcessRun || process.State.Terminal() ||
		session.OwnerActorID != claim.OwnerActorID ||
		turn.OwnerActorID != claim.OwnerActorID ||
		process.OwnerActorID != claim.OwnerActorID ||
		turnSpec.SessionID != session.ID || turnSpec.ProcessRunID != process.ID ||
		turnSpec.Attempt != spec.Attempt ||
		turnSpec.EffectiveInputSHA256 != principal.AuthorityDigest ||
		processSpec.RootInitiatorActorID != claim.OwnerActorID {
		return errs.ErrStateConflict
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, spec.Attempt)
	if err != nil || (attempt.State != "QUEUED" && attempt.State != "CLAIMED") ||
		attempt.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
		attempt.AuthorityGeneration != spec.AuthorityGeneration {
		return errs.ErrStateConflict
	}
	execution, err := currentExecution(processSpec)
	if err != nil || !executionMatchesTurn(execution, turn, turnSpec) {
		return errs.ErrStateConflict
	}
	if processSpec.ContinuationTurnID != "" {
		return nil
	}
	if processSpec.ParentProcessRunID == "" {
		if processSpec.RootSessionID != session.ID ||
			processSpec.RootTurnID != turn.ID ||
			processSpec.RootAttempt != spec.Attempt {
			return errs.ErrStateConflict
		}
		return nil
	}
	edge, err := tx.GetDelegationEdgeByTargetTurn(
		ctx, principal.OrganizationID, principal.ProjectID, turn.ID,
	)
	if err != nil || edge.ID != processSpec.DelegationID ||
		edge.ParentProcessRunID != processSpec.ParentProcessRunID ||
		edge.TargetSessionID != session.ID || edge.TargetTurnID != turn.ID ||
		edge.TargetAttempt != spec.Attempt ||
		edge.TargetInputSHA256 != turnSpec.EffectiveInputSHA256 ||
		edge.GrantGeneration != spec.AuthorityGeneration {
		return errs.ErrStateConflict
	}
	return nil
}
