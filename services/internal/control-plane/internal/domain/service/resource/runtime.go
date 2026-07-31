package resource

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

var scheduleParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// EnqueueTurn атомарно увеличивает session sequence и создаёт exact attempt.
func (service *Service) EnqueueTurn(
	ctx context.Context,
	input EnqueueTurnInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionEnqueueTurn); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SessionID) != nil ||
		!validRuntimeReference(input.SourceRef) ||
		value.ValidateID(input.PromptArtifactID) != nil ||
		value.ValidateID(input.RuntimeRevisionID) != nil ||
		(input.ProcessRunID != "" && value.ValidateID(input.ProcessRunID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity          commandIdentity
		SessionID         string
		SourceRef         string
		PromptArtifactID  string
		ProcessRunID      string
		RuntimeRevisionID string
	}{
		identity(input.Principal),
		input.SessionID,
		input.SourceRef,
		input.PromptArtifactID,
		input.ProcessRunID,
		input.RuntimeRevisionID,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"enqueue_turn",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			session, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.SessionID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			sessionSpec, ok := session.Spec.(entity.SessionSpec)
			if !ok || session.Kind != enum.KindSession ||
				session.State != enum.StateActive {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if sessionSpec.LastTurnSequence == ^uint64(0) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			sessionSpec.LastTurnSequence++
			now := service.now().UTC().Truncate(time.Microsecond)
			updatedSession, err := session.Update(session.Name, sessionSpec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updatedSession, session.Version); err != nil {
				return entity.Resource{}, err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"enqueue_turn_session",
				updatedSession,
			); err != nil {
				return entity.Resource{}, err
			}
			turn, err := entity.New(
				uuid.NewString(),
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.SessionID,
				input.Principal.ActorID,
				enum.KindTurn,
				"Turn "+input.SessionID,
				entity.TurnSpec{
					SessionID:         input.SessionID,
					Sequence:          sessionSpec.LastTurnSequence,
					SourceRef:         input.SourceRef,
					PromptArtifactID:  input.PromptArtifactID,
					ProcessRunID:      input.ProcessRunID,
					RuntimeRevisionID: input.RuntimeRevisionID,
					Attempt:           1,
				},
				now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := service.validateReferences(ctx, tx, turn); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, turn); err != nil {
				return entity.Resource{}, err
			}
			return turn, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"enqueue_turn",
				turn,
			)
		},
	)
}

// ClaimTurn выдаёт exact workload одну bounded lease.
func (service *Service) ClaimTurn(
	ctx context.Context,
	input ClaimTurnInput,
) (ClaimTurnResult, error) {
	if err := authorize(input.Principal, permissionClaimTurn); err != nil {
		return ClaimTurnResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return ClaimTurnResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
	}{identity(input.Principal)})
	if err != nil {
		return ClaimTurnResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result ClaimTurnResult
	mutated := false
	err = service.repository.Transact(
		ctx,
		input.Principal.OrganizationID,
		input.Principal.ProjectID,
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"claim_turn",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload claimTurnReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil ||
					payload.LeaseExpiresAt.IsZero() {
					return errs.ErrInternal
				}
				result = ClaimTurnResult{
					Turn: receipt.Result,
					LeaseToken: service.leaseToken(
						receipt.Result.ID,
						receipt.Result.Version,
						input.IdempotencyKey,
					),
					LeaseExpiresAt: payload.LeaseExpiresAt,
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			expired, err := tx.ExpiredClaimedTurns(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				32,
				now,
			)
			if err != nil {
				return err
			}
			for _, stale := range expired {
				requeued, err := stale.Turn.Transition(enum.StateQueued, now)
				if err != nil {
					return errs.ErrStateConflict
				}
				if err := tx.Update(ctx, requeued, stale.Turn.Version); err != nil {
					return err
				}
				if err := tx.DeleteTurnLease(
					ctx,
					stale.Turn.ID,
					stale.Lease.Fence,
				); err != nil {
					return err
				}
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"requeue_expired_turn",
					requeued,
				); err != nil {
					return err
				}
			}
			turn, err := tx.NextQueuedTurn(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
			)
			if err != nil {
				return err
			}
			claimed, err := turn.Transition(enum.StateClaimed, now)
			if err != nil {
				return errs.ErrStateConflict
			}
			token := service.leaseToken(claimed.ID, claimed.Version, input.IdempotencyKey)
			expiresAt := now.Add(service.turnLeaseDuration)
			if err := tx.Update(ctx, claimed, turn.Version); err != nil {
				return err
			}
			if err := tx.SaveTurnLease(ctx, domainrepo.TurnLease{
				TurnID:     claimed.ID,
				TokenHash:  hashString(token),
				WorkloadID: input.Principal.CallerWorkload,
				ExpiresAt:  expiresAt,
				Fence:      claimed.Version,
			}); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"claim_turn",
				claimed,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(claimTurnReceipt{LeaseExpiresAt: expiresAt})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "claim_turn",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         claimed,
				Payload:        payload,
				CreatedAt:      service.now().UTC().Truncate(time.Microsecond),
			}); err != nil {
				return err
			}
			result = ClaimTurnResult{
				Turn:           claimed,
				LeaseToken:     token,
				LeaseExpiresAt: expiresAt,
			}
			mutated = true
			return nil
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(enum.KindTurn, "claim_turn")
	}
	return result, err
}

// CompleteTurn принимает terminal outcome только для current unexpired lease.
func (service *Service) CompleteTurn(
	ctx context.Context,
	input CompleteTurnInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionCompleteTurn); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		len(input.LeaseToken) != 64 ||
		input.ExpectedVersion == 0 ||
		(input.TerminalState != enum.StateSucceeded &&
			input.TerminalState != enum.StateFailed &&
			input.TerminalState != enum.StateCancelled) ||
		len(input.Outcome) < 1 || len(input.Outcome) > 256 ||
		(input.ResultArtifactID != "" &&
			value.ValidateID(input.ResultArtifactID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity         commandIdentity
		TurnID           string
		LeaseTokenHash   string
		ExpectedVersion  uint64
		TerminalState    enum.State
		Outcome          string
		ResultArtifactID string
	}{
		identity(input.Principal),
		input.TurnID,
		hashString(input.LeaseToken),
		input.ExpectedVersion,
		input.TerminalState,
		input.Outcome,
		input.ResultArtifactID,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"complete_turn",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.TurnID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != enum.KindTurn ||
				current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			lease, err := tx.ValidateTurnLease(
				ctx,
				input.TurnID,
				hashString(input.LeaseToken),
				input.Principal.CallerWorkload,
				service.now(),
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if lease.Fence != current.Version {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec, ok := current.Spec.(entity.TurnSpec)
			if !ok {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.Outcome = input.Outcome
			spec.ResultArtifactID = input.ResultArtifactID
			updated, err := current.ReplaceAndTransition(
				spec,
				input.TerminalState,
				service.now(),
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := service.validateReferences(ctx, tx, updated); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.DeleteTurnLease(ctx, input.TurnID, lease.Fence); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"complete_turn",
				updated,
			)
		},
	)
}

// ClaimDueSchedules фиксирует immutable occurrences и сдвигает high watermark.
func (service *Service) ClaimDueSchedules(
	ctx context.Context,
	input ClaimDueSchedulesInput,
) (ClaimDueSchedulesResult, error) {
	if err := authorize(input.Principal, permissionClaimSchedule); err != nil {
		return ClaimDueSchedulesResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.Limit < 1 || input.Limit > service.maximumScheduleClaims {
		return ClaimDueSchedulesResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Limit    int
	}{identity(input.Principal), input.Limit})
	if err != nil {
		return ClaimDueSchedulesResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result ClaimDueSchedulesResult
	mutated := false
	err = service.repository.Transact(
		ctx,
		input.Principal.OrganizationID,
		input.Principal.ProjectID,
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"claim_due_schedules",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					json.Unmarshal(receipt.Payload, &result) != nil {
					return errs.ErrIdempotencyConflict
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			schedules, err := tx.DueSchedules(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Limit,
				now,
			)
			if err != nil {
				return err
			}
			result.Occurrences = make([]ScheduleOccurrence, 0, len(schedules))
			for _, schedule := range schedules {
				spec, ok := schedule.Spec.(entity.ScheduleSpec)
				if !ok || schedule.Kind != enum.KindSchedule ||
					schedule.State != enum.StateActive {
					return errs.ErrStateConflict
				}
				scheduledFor := spec.NextRunAt.UTC().Truncate(time.Microsecond)
				emit := shouldEmitOccurrence(spec, scheduledFor, now)
				nextRun, err := nextScheduleRun(spec, scheduledFor, now)
				if err != nil {
					return err
				}
				spec.NextRunAt = nextRun
				updated, err := schedule.Update(schedule.Name, spec, now)
				if err != nil {
					return errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, schedule.Version); err != nil {
					return err
				}
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"claim_due_schedule",
					updated,
				); err != nil {
					return err
				}
				if !emit {
					continue
				}
				occurrence := ScheduleOccurrence{
					ScheduleID:   schedule.ID,
					ScheduledFor: scheduledFor,
					OccurrenceID: uuid.NewSHA1(
						uuid.NameSpaceOID,
						[]byte(schedule.ID+"\x00"+scheduledFor.Format(time.RFC3339Nano)),
					).String(),
					TargetResourceID: spec.TargetResourceID,
				}
				if err := tx.SaveScheduleOccurrence(ctx, domainrepo.ScheduleOccurrence{
					ID:               occurrence.OccurrenceID,
					ScheduleID:       occurrence.ScheduleID,
					OrganizationID:   schedule.OrganizationID,
					ProjectID:        schedule.ProjectID,
					ScheduledFor:     occurrence.ScheduledFor,
					TargetResourceID: occurrence.TargetResourceID,
				}); err != nil {
					return err
				}
				result.Occurrences = append(result.Occurrences, occurrence)
			}
			payload, err := json.Marshal(result)
			if err != nil {
				return errs.ErrInternal
			}
			mutated = len(schedules) > 0
			return tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "claim_due_schedules",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      now,
			})
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(enum.KindSchedule, "claim_schedules")
	}
	return result, err
}

// ResolveOwnerGate связывает решение владельца с exact gate version.
func (service *Service) ResolveOwnerGate(
	ctx context.Context,
	input ResolveOwnerGateInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionResolveGate); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OwnerGateID) != nil ||
		input.ExpectedVersion == 0 ||
		(input.Decision != "APPROVED" && input.Decision != "REJECTED" &&
			input.Decision != "CHANGES_REQUESTED" && input.Decision != "CANCELLED") ||
		len(input.Reason) < 1 || len(input.Reason) > 2048 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		OwnerGateID     string
		ExpectedVersion uint64
		Decision        string
		Reason          string
	}{
		identity(input.Principal),
		input.OwnerGateID,
		input.ExpectedVersion,
		input.Decision,
		input.Reason,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"resolve_owner_gate",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OwnerGateID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != enum.KindOwnerGate ||
				current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			spec, ok := current.Spec.(entity.OwnerGateSpec)
			if !ok {
				return entity.Resource{}, errs.ErrStateConflict
			}
			target := ownerGateTarget(input.Decision)
			if !spec.ExpiresAt.After(service.now()) {
				target = enum.StateExpired
				spec.Decision = ""
				spec.DecisionReason = ""
			} else {
				spec.Decision = input.Decision
				spec.DecisionReason = input.Reason
			}
			updated, err := current.ReplaceAndTransition(spec, target, service.now())
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"resolve_owner_gate",
				updated,
			)
		},
	)
}

type claimTurnReceipt struct {
	LeaseExpiresAt time.Time `json:"leaseExpiresAt"`
}

func ownerGateTarget(decision string) enum.State {
	switch decision {
	case "APPROVED":
		return enum.StateSucceeded
	case "REJECTED":
		return enum.StateFailed
	case "CHANGES_REQUESTED":
		return enum.StateBlocked
	default:
		return enum.StateCancelled
	}
}

func shouldEmitOccurrence(
	spec entity.ScheduleSpec,
	scheduledFor, now time.Time,
) bool {
	if !scheduledFor.Before(now) {
		return true
	}
	switch spec.MisfirePolicy {
	case "SKIP":
		return false
	case "WITHIN_GRACE":
		return now.Sub(scheduledFor) <= spec.MisfireGrace
	default:
		return true
	}
}

func nextScheduleRun(
	spec entity.ScheduleSpec,
	scheduledFor, now time.Time,
) (time.Time, error) {
	var next func(time.Time) time.Time
	if spec.Interval > 0 {
		next = func(current time.Time) time.Time {
			return current.Add(spec.Interval)
		}
	} else {
		schedule, err := scheduleParser.Parse(spec.Cron)
		if err != nil {
			return time.Time{}, errs.ErrInvalidInput
		}
		location, err := time.LoadLocation(spec.Timezone)
		if err != nil {
			return time.Time{}, errs.ErrInvalidInput
		}
		next = func(current time.Time) time.Time {
			return schedule.Next(current.In(location)).UTC()
		}
	}
	nextRun := next(scheduledFor)
	if spec.MisfirePolicy == "CATCH_UP" {
		return nextRun.UTC().Truncate(time.Microsecond), nil
	}
	for !nextRun.After(now) {
		nextRun = next(nextRun)
	}
	return nextRun.UTC().Truncate(time.Microsecond), nil
}

func validRuntimeReference(reference string) bool {
	if len(reference) < 1 || len(reference) > 512 {
		return false
	}
	for _, symbol := range reference {
		if symbol < 0x20 || symbol == 0x7f {
			return false
		}
	}
	return true
}
