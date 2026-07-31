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
)

// ManageSchedule реализует закрытый набор действий над schedule.
func (service *Service) ManageSchedule(
	ctx context.Context,
	input ManageScheduleInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionManageSchedule); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		(input.Action != "CREATE" && value.ValidateID(input.ScheduleID) != nil) ||
		(input.Action == "CREATE" && value.ValidateName(input.Name) != nil) ||
		(input.Action != "CREATE" && input.ExpectedVersion == 0) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	switch input.Action {
	case "CREATE", "UPDATE", "ACTIVATE", "PAUSE", "ARCHIVE", "DELETE":
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		Action          string
		ScheduleID      string
		ExpectedVersion uint64
		Name            string
		Spec            entity.ScheduleSpec
	}{
		identity(input.Principal),
		input.Action,
		input.ScheduleID,
		input.ExpectedVersion,
		input.Name,
		input.Spec,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"manage_schedule_"+input.Action,
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now := service.now().UTC().Truncate(time.Microsecond)
			if input.Action == "CREATE" || input.Action == "UPDATE" {
				target, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Spec.TargetResourceID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if target.State == enum.StateDeleted ||
					target.Kind == enum.KindSchedule {
					return entity.Resource{}, errs.ErrNotFound
				}
				input.Spec.TargetKind = target.Kind
				input.Spec.TargetVersion = target.Version
				input.Spec.EffectiveInputSHA, err = entity.ProjectionSHA256(target)
				if err != nil {
					return entity.Resource{}, errs.ErrInternal
				}
			}
			if input.Action == "CREATE" {
				created, err := entity.New(
					uuid.NewString(),
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					"",
					input.Principal.ActorID,
					enum.KindSchedule,
					input.Name,
					input.Spec,
					now,
				)
				if err != nil || validateTemporalCreation(input.Spec, now) != nil {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				if err := tx.Insert(ctx, created); err != nil {
					return entity.Resource{}, err
				}
				return created, service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"create_schedule",
					created,
				)
			}
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ScheduleID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != enum.KindSchedule ||
				current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			var updated entity.Resource
			switch input.Action {
			case "UPDATE":
				if current.State != enum.StateActive &&
					current.State != enum.StatePaused {
					return entity.Resource{}, errs.ErrStateConflict
				}
				updated, err = current.Update(input.Name, input.Spec, now)
			case "ACTIVATE":
				updated, err = current.Transition(enum.StateActive, now)
			case "PAUSE":
				updated, err = current.Transition(enum.StatePaused, now)
			case "ARCHIVE":
				updated, err = current.Transition(enum.StateArchived, now)
			case "DELETE":
				if current.State != enum.StateArchived {
					return entity.Resource{}, errs.ErrStateConflict
				}
				updated, err = current.Transition(enum.StateDeletionPending, now)
			}
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
				"manage_schedule_"+input.Action,
				updated,
			)
		},
	)
}

type scheduleOccurrenceReceipt struct {
	Occurrence domainrepo.ScheduleOccurrence `json:"occurrence"`
	LeaseToken string                        `json:"leaseToken,omitempty"`
}

// ClaimScheduleOccurrence выдаёт exact scheduler bounded single-attempt lease.
func (service *Service) ClaimScheduleOccurrence(
	ctx context.Context,
	input ClaimScheduleOccurrenceInput,
) (ScheduleOccurrenceResult, error) {
	if err := authorize(input.Principal, permissionExecuteSchedule); err != nil {
		return ScheduleOccurrenceResult{}, err
	}
	if input.Principal.CallerWorkload != service.schedulerWorkload ||
		input.Principal.CallerSPIFFEID != service.schedulerSPIFFEID {
		return ScheduleOccurrenceResult{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return ScheduleOccurrenceResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
	}{identity(input.Principal)})
	if err != nil {
		return ScheduleOccurrenceResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result ScheduleOccurrenceResult
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"claim_schedule_occurrence",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload scheduleOccurrenceReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil ||
					payload.Occurrence.ID == "" ||
					payload.LeaseToken == "" {
					return errs.ErrInternal
				}
				result = ScheduleOccurrenceResult{
					Occurrence: payload.Occurrence,
					LeaseToken: payload.LeaseToken,
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			recovered, err := tx.RecoverExpiredScheduleOccurrences(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				now,
			)
			if err != nil {
				return err
			}
			for _, occurrence := range recovered {
				schedule, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					occurrence.ScheduleID,
				)
				if err != nil {
					return err
				}
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"recover_schedule_occurrence",
					schedule,
				); err != nil {
					return err
				}
			}
			skipped, err := tx.SkipOverlappedScheduleOccurrences(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				now,
			)
			if err != nil {
				return err
			}
			for _, occurrence := range skipped {
				schedule, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					occurrence.ScheduleID,
				)
				if err != nil {
					return err
				}
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"skip_schedule_occurrence",
					schedule,
				); err != nil {
					return err
				}
			}
			occurrence, err := tx.NextScheduleOccurrence(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				now,
			)
			if err != nil {
				return err
			}
			target, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				occurrence.TargetResourceID,
			)
			if err != nil {
				return err
			}
			targetDigest, err := entity.ProjectionSHA256(target)
			if err != nil {
				return errs.ErrInternal
			}
			if target.Kind != occurrence.TargetKind ||
				target.Version != occurrence.TargetVersion ||
				targetDigest != occurrence.EffectiveInputSHA256 ||
				target.State == enum.StateDeleted {
				return errs.ErrStateConflict
			}
			token := service.leaseToken(
				occurrence.ID,
				uint64(occurrence.Attempt),
				occurrence.Attempt,
				input.Principal.AuthorityGeneration,
				input.Principal.CallerWorkload,
				input.IdempotencyKey,
			)
			expectedAttempt := occurrence.Attempt
			occurrence.State = "CLAIMED"
			occurrence.ClaimantWorkloadID = input.Principal.CallerWorkload
			occurrence.AuthorityGeneration = input.Principal.AuthorityGeneration
			occurrence.TokenHash = hashString(token)
			occurrence.LeaseExpiresAt = now.Add(service.turnLeaseDuration)
			occurrence.UpdatedAt = now
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				expectedAttempt,
				"",
			); err != nil {
				return err
			}
			schedule, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				occurrence.ScheduleID,
			)
			if err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"claim_schedule_occurrence",
				schedule,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(scheduleOccurrenceReceipt{
				Occurrence: occurrence,
				LeaseToken: token,
			})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "claim_schedule_occurrence",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      now,
			}); err != nil {
				return err
			}
			result = ScheduleOccurrenceResult{
				Occurrence: occurrence,
				LeaseToken: token,
			}
			return nil
		},
	)
	return result, err
}

// CompleteScheduleOccurrence завершает/retries только current scheduler lease.
func (service *Service) CompleteScheduleOccurrence(
	ctx context.Context,
	input CompleteScheduleOccurrenceInput,
) (domainrepo.ScheduleOccurrence, error) {
	if err := authorize(input.Principal, permissionExecuteSchedule); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	if input.Principal.CallerWorkload != service.schedulerWorkload ||
		input.Principal.CallerSPIFFEID != service.schedulerSPIFFEID {
		return domainrepo.ScheduleOccurrence{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OccurrenceID) != nil ||
		len(input.LeaseToken) != 64 ||
		input.ExpectedAttempt == 0 ||
		(input.TerminalState != "SUCCEEDED" && input.TerminalState != "FAILED") ||
		len(input.Outcome) < 1 || len(input.Outcome) > 256 ||
		(input.ResultArtifactID != "" &&
			value.ValidateID(input.ResultArtifactID) != nil) {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity         commandIdentity
		OccurrenceID     string
		TokenHash        string
		ExpectedAttempt  uint32
		TerminalState    string
		Outcome          string
		ResultArtifactID string
	}{
		identity(input.Principal),
		input.OccurrenceID,
		hashString(input.LeaseToken),
		input.ExpectedAttempt,
		input.TerminalState,
		input.Outcome,
		input.ResultArtifactID,
	})
	if err != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.ScheduleOccurrence
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"complete_schedule_occurrence",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload scheduleOccurrenceReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil ||
					payload.Occurrence.ID == "" {
					return errs.ErrInternal
				}
				result = payload.Occurrence
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			occurrence, err := tx.GetScheduleOccurrenceForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OccurrenceID,
			)
			if err != nil {
				return err
			}
			if occurrence.State != "CLAIMED" ||
				occurrence.Attempt != input.ExpectedAttempt ||
				occurrence.TokenHash != hashString(input.LeaseToken) ||
				occurrence.ClaimantWorkloadID != input.Principal.CallerWorkload ||
				occurrence.AuthorityGeneration != input.Principal.AuthorityGeneration ||
				!occurrence.LeaseExpiresAt.After(service.now()) {
				return errs.ErrStateConflict
			}
			if input.ResultArtifactID != "" {
				if _, err := service.requireCleanArtifact(
					ctx,
					tx,
					input.Principal,
					input.ResultArtifactID,
				); err != nil {
					return err
				}
			}
			schedule, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				occurrence.ScheduleID,
			)
			if err != nil {
				return err
			}
			scheduleSpec, ok := schedule.Spec.(entity.ScheduleSpec)
			if !ok || schedule.Kind != enum.KindSchedule {
				return errs.ErrStateConflict
			}
			expectedToken := occurrence.TokenHash
			now := service.now().UTC().Truncate(time.Microsecond)
			occurrence.State = input.TerminalState
			occurrence.Outcome = input.Outcome
			occurrence.ResultArtifactID = input.ResultArtifactID
			occurrence.ClaimantWorkloadID = ""
			occurrence.AuthorityGeneration = 0
			occurrence.TokenHash = ""
			occurrence.LeaseExpiresAt = time.Time{}
			occurrence.UpdatedAt = now
			if input.TerminalState == "FAILED" {
				if occurrence.Attempt < scheduleSpec.MaximumAttempts &&
					now.Sub(occurrence.ScheduledFor) < scheduleSpec.DeadLetterAfter {
					occurrence.State = "QUEUED"
					occurrence.Attempt++
					occurrence.AvailableAt = now.Add(
						scheduleBackoff(scheduleSpec, occurrence.Attempt),
					)
				} else {
					occurrence.State = "DEAD_LETTER"
				}
			}
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				input.ExpectedAttempt,
				expectedToken,
			); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"complete_schedule_occurrence",
				schedule,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(
				scheduleOccurrenceReceipt{Occurrence: occurrence},
			)
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "complete_schedule_occurrence",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      now,
			}); err != nil {
				return err
			}
			result = occurrence
			return nil
		},
	)
	return result, err
}

// CancelScheduleOccurrence отзывает queued/current occurrence без запуска.
func (service *Service) CancelScheduleOccurrence(
	ctx context.Context,
	input CancelScheduleOccurrenceInput,
) (domainrepo.ScheduleOccurrence, error) {
	if err := authorize(input.Principal, permissionManageSchedule); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OccurrenceID) != nil ||
		input.ExpectedAttempt == 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		OccurrenceID    string
		ExpectedAttempt uint32
		ReasonCode      string
	}{
		identity(input.Principal),
		input.OccurrenceID,
		input.ExpectedAttempt,
		input.ReasonCode,
	})
	if err != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.ScheduleOccurrence
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"cancel_schedule_occurrence",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload scheduleOccurrenceReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil {
					return errs.ErrInternal
				}
				result = payload.Occurrence
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			occurrence, err := tx.GetScheduleOccurrenceForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OccurrenceID,
			)
			if err != nil {
				return err
			}
			if occurrence.Attempt != input.ExpectedAttempt ||
				(occurrence.State != "QUEUED" && occurrence.State != "CLAIMED") {
				return errs.ErrStateConflict
			}
			expectedToken := occurrence.TokenHash
			occurrence.State = "CANCELLED"
			occurrence.Outcome = input.ReasonCode
			occurrence.ClaimantWorkloadID = ""
			occurrence.AuthorityGeneration = 0
			occurrence.TokenHash = ""
			occurrence.LeaseExpiresAt = time.Time{}
			occurrence.UpdatedAt = service.now().UTC().Truncate(time.Microsecond)
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				input.ExpectedAttempt,
				expectedToken,
			); err != nil {
				return err
			}
			schedule, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				occurrence.ScheduleID,
			)
			if err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"cancel_schedule_occurrence",
				schedule,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(
				scheduleOccurrenceReceipt{Occurrence: occurrence},
			)
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "cancel_schedule_occurrence",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      occurrence.UpdatedAt,
			}); err != nil {
				return err
			}
			result = occurrence
			return nil
		},
	)
	return result, err
}

func scheduleBackoff(spec entity.ScheduleSpec, attempt uint32) time.Duration {
	delay := spec.InitialBackoff
	for current := uint32(2); current < attempt && delay < spec.MaximumBackoff; current++ {
		if delay > spec.MaximumBackoff/2 {
			return spec.MaximumBackoff
		}
		delay *= 2
	}
	if delay > spec.MaximumBackoff {
		return spec.MaximumBackoff
	}
	return delay
}

// StartProcess связывает server-derived root actor с immutable turn input.
func (service *Service) StartProcess(
	ctx context.Context,
	input StartProcessInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionStartProcess); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateName(input.Name) != nil ||
		!validRuntimeReference(input.PlaybookRef) ||
		!validRuntimeReference(input.RootTriggerRef) ||
		input.PolicyRevision == 0 ||
		value.ValidateID(input.RootSessionID) != nil ||
		value.ValidateID(input.RootTurnID) != nil ||
		input.RootAttempt == 0 ||
		value.ValidateID(input.InputArtifactID) != nil ||
		(input.ParentProcessID != "" &&
			value.ValidateID(input.ParentProcessID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		Name            string
		ParentProcessID string
		PlaybookRef     string
		PolicyRevision  uint64
		RootTriggerRef  string
		RootSessionID   string
		RootTurnID      string
		RootAttempt     uint32
		InputArtifactID string
	}{
		identity(input.Principal),
		input.Name,
		input.ParentProcessID,
		input.PlaybookRef,
		input.PolicyRevision,
		input.RootTriggerRef,
		input.RootSessionID,
		input.RootTurnID,
		input.RootAttempt,
		input.InputArtifactID,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"start_process",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			session, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.RootSessionID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			turn, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.RootTurnID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			turnSpec, ok := turn.Spec.(entity.TurnSpec)
			if !ok || session.Kind != enum.KindSession ||
				session.State != enum.StateActive ||
				turn.Kind != enum.KindTurn ||
				turnSpec.SessionID != session.ID ||
				turnSpec.Attempt != input.RootAttempt ||
				turn.State.Terminal() {
				return entity.Resource{}, errs.ErrStateConflict
			}
			artifact, err := service.requireCleanArtifact(
				ctx,
				tx,
				input.Principal,
				input.InputArtifactID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if input.ParentProcessID != "" {
				parent, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.ParentProcessID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if parent.Kind != enum.KindProcessRun ||
					parent.State.Terminal() {
					return entity.Resource{}, errs.ErrStateConflict
				}
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			spec := entity.ProcessRunSpec{
				ParentProcessRunID:   input.ParentProcessID,
				PlaybookRef:          input.PlaybookRef,
				PolicyRevision:       input.PolicyRevision,
				RootTriggerRef:       input.RootTriggerRef,
				RootInitiatorActorID: input.Principal.ActorID,
				RootSessionID:        session.ID,
				RootTurnID:           turn.ID,
				RootAttempt:          input.RootAttempt,
				ImmutableInputSHA256: hashRuntimeInput(
					input.RootTriggerRef,
					artifact.SHA256,
					turnSpec.EffectiveInputSHA256,
					input.PlaybookRef,
				),
			}
			process, err := entity.New(
				uuid.NewString(),
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ParentProcessID,
				input.Principal.ActorID,
				enum.KindProcessRun,
				input.Name,
				spec,
				now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := tx.Insert(ctx, process); err != nil {
				return entity.Resource{}, err
			}
			return process, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"start_process",
				process,
			)
		},
	)
}

// CancelProcess завершает exact process version специализированной командой.
func (service *Service) CancelProcess(
	ctx context.Context,
	input CancelProcessInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionCancelProcess); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProcessRunID) != nil ||
		input.ExpectedVersion == 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ProcessRunID    string
		ExpectedVersion uint64
		ReasonCode      string
	}{
		identity(input.Principal),
		input.ProcessRunID,
		input.ExpectedVersion,
		input.ReasonCode,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"cancel_process",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			process, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ProcessRunID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if process.Kind != enum.KindProcessRun ||
				process.Version != input.ExpectedVersion ||
				process.State.Terminal() {
				return entity.Resource{}, errs.ErrStateConflict
			}
			cancelled, err := process.Transition(
				enum.StateCancelled,
				service.now().UTC().Truncate(time.Microsecond),
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, cancelled, process.Version); err != nil {
				return entity.Resource{}, err
			}
			return cancelled, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"cancel_process",
				cancelled,
			)
		},
	)
}

// RegisterArtifact принимает только immutable metadata и server-owned PENDING.
func (service *Service) RegisterArtifact(
	ctx context.Context,
	input RegisterArtifactInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionRegisterArtifact); err != nil {
		return entity.Resource{}, err
	}
	input.Spec.ScanStatus = "PENDING"
	input.Spec.ScanPolicyRevision = 0
	input.Spec.ScanEvidenceSHA256 = ""
	input.Spec.ScannerWorkloadID = ""
	input.Spec.ScannedAt = time.Time{}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateName(input.Name) != nil ||
		(input.ParentID != "" && value.ValidateID(input.ParentID) != nil) ||
		input.Spec.Validate() != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Name     string
		ParentID string
		Spec     entity.ArtifactSpec
	}{
		identity(input.Principal),
		input.Name,
		input.ParentID,
		input.Spec,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"register_artifact",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now := service.now().UTC().Truncate(time.Microsecond)
			artifact, err := entity.New(
				uuid.NewString(),
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ParentID,
				input.Principal.ActorID,
				enum.KindArtifact,
				input.Name,
				input.Spec,
				now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := service.validateReferences(ctx, tx, artifact); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, artifact); err != nil {
				return entity.Resource{}, err
			}
			return artifact, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"register_artifact",
				artifact,
			)
		},
	)
}

// RecordArtifactScan принимает result только от exact configured scanner.
func (service *Service) RecordArtifactScan(
	ctx context.Context,
	input RecordArtifactScanInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionScanArtifact); err != nil {
		return entity.Resource{}, err
	}
	if input.Principal.CallerWorkload != service.scannerWorkload ||
		input.Principal.CallerSPIFFEID != service.scannerSPIFFEID {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ArtifactID) != nil ||
		input.ExpectedVersion == 0 ||
		input.ScanPolicyRevision == 0 ||
		(input.TargetState != "SCANNING" &&
			input.TargetState != "CLEAN" &&
			input.TargetState != "QUARANTINED" &&
			input.TargetState != "FAILED") ||
		(input.TargetState != "SCANNING" &&
			!validSHA256Text(input.EvidenceSHA256)) ||
		(input.TargetState == "SCANNING" && input.EvidenceSHA256 != "") {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity           commandIdentity
		ArtifactID         string
		ExpectedVersion    uint64
		TargetState        string
		ScanPolicyRevision uint64
		EvidenceSHA256     string
	}{
		identity(input.Principal),
		input.ArtifactID,
		input.ExpectedVersion,
		input.TargetState,
		input.ScanPolicyRevision,
		input.EvidenceSHA256,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"record_artifact_scan",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ArtifactID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.ArtifactSpec)
			if !ok || current.Kind != enum.KindArtifact ||
				current.Version != input.ExpectedVersion ||
				!((spec.ScanStatus == "PENDING" && input.TargetState == "SCANNING") ||
					(spec.ScanStatus == "SCANNING" &&
						input.TargetState != "SCANNING")) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.ScanStatus = input.TargetState
			spec.ScanPolicyRevision = input.ScanPolicyRevision
			spec.ScannerWorkloadID = input.Principal.CallerWorkload
			if input.TargetState != "SCANNING" {
				spec.ScanEvidenceSHA256 = input.EvidenceSHA256
				spec.ScannedAt = service.now().UTC().Truncate(time.Microsecond)
			}
			updated, err := current.Update(
				current.Name,
				spec,
				service.now().UTC().Truncate(time.Microsecond),
			)
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
				"artifact_scan_"+input.TargetState,
				updated,
			)
		},
	)
}

type ownerGateReceipt struct {
	Process entity.Resource `json:"process"`
}

// RequestOwnerGate создаёт gate и переводит связанный process одним commit.
func (service *Service) RequestOwnerGate(
	ctx context.Context,
	input RequestOwnerGateInput,
) (OwnerGateResult, error) {
	if err := authorize(input.Principal, permissionRequestGate); err != nil {
		return OwnerGateResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProcessRunID) != nil ||
		input.ProcessExpectedVersion == 0 ||
		value.ValidateID(input.SessionID) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		input.Attempt == 0 ||
		value.ValidateID(input.ResultArtifactID) != nil ||
		!input.ExpiresAt.After(service.now()) ||
		input.ExpiresAt.After(service.now().Add(30*24*time.Hour)) {
		return OwnerGateResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity               commandIdentity
		ProcessRunID           string
		ProcessExpectedVersion uint64
		SessionID              string
		TurnID                 string
		Attempt                uint32
		ResultArtifactID       string
		ExpiresAt              time.Time
	}{
		identity(input.Principal),
		input.ProcessRunID,
		input.ProcessExpectedVersion,
		input.SessionID,
		input.TurnID,
		input.Attempt,
		input.ResultArtifactID,
		input.ExpiresAt.UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		return OwnerGateResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result OwnerGateResult
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"request_owner_gate",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload ownerGateReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil {
					return errs.ErrInternal
				}
				result = OwnerGateResult{
					OwnerGate: receipt.Result,
					Process:   payload.Process,
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			process, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ProcessRunID,
			)
			if err != nil {
				return err
			}
			processSpec, ok := process.Spec.(entity.ProcessRunSpec)
			if !ok || process.Kind != enum.KindProcessRun ||
				process.State != enum.StateRunning ||
				process.Version != input.ProcessExpectedVersion ||
				processSpec.RootInitiatorActorID != input.Principal.ActorID ||
				processSpec.RootSessionID != input.SessionID ||
				processSpec.RootTurnID != input.TurnID ||
				processSpec.RootAttempt != input.Attempt {
				return errs.ErrStateConflict
			}
			artifact, err := service.requireCleanArtifact(
				ctx,
				tx,
				input.Principal,
				input.ResultArtifactID,
			)
			if err != nil {
				return err
			}
			project, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Principal.ProjectID,
			)
			if err != nil {
				return err
			}
			if project.Kind != enum.KindProject || project.OwnerActorID == "" {
				return errs.ErrStateConflict
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			gate, err := entity.New(
				uuid.NewString(),
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				process.ID,
				process.OwnerActorID,
				enum.KindOwnerGate,
				"Owner gate "+process.ID,
				entity.OwnerGateSpec{
					ProcessRunID:         process.ID,
					ResultRef:            artifact.StorageRef,
					ResultSHA256:         artifact.SHA256,
					ExpiresAt:            input.ExpiresAt.UTC().Truncate(time.Microsecond),
					RootInitiatorActorID: processSpec.RootInitiatorActorID,
					SessionID:            processSpec.RootSessionID,
					TurnID:               processSpec.RootTurnID,
					Attempt:              processSpec.RootAttempt,
					ImmutableInputSHA256: processSpec.ImmutableInputSHA256,
					RecipientActorID:     project.OwnerActorID,
					DeliveryWorkloadID:   service.ownerGateDeliveryWorkload,
					DeliverySPIFFEID:     service.ownerGateDeliverySPIFFEID,
				},
				now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			waiting, err := process.Transition(enum.StateWaitingOwner, now)
			if err != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Insert(ctx, gate); err != nil {
				return err
			}
			if err := tx.Update(ctx, waiting, process.Version); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"request_owner_gate",
				gate,
			); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"wait_owner_gate",
				waiting,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(ownerGateReceipt{Process: waiting})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "request_owner_gate",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         gate,
				Payload:        payload,
				CreatedAt:      now,
			}); err != nil {
				return err
			}
			result = OwnerGateResult{OwnerGate: gate, Process: waiting}
			return nil
		},
	)
	return result, err
}
