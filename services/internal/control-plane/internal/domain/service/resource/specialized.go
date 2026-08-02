package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// ManageSchedule реализует закрытый набор действий над расписанием.
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
		DetachGit       bool
	}{
		identity(input.Principal),
		input.Action,
		input.ScheduleID,
		input.ExpectedVersion,
		input.Name,
		input.Spec,
		input.DetachGitManagement,
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
				if input.Action == "CREATE" {
					if input.DetachGitManagement {
						return entity.Resource{}, errs.ErrInvalidInput
					}
					if err := validateConfigurationCreate(
						ctx,
						tx,
						input.Principal,
						input.Spec,
					); err != nil {
						return entity.Resource{}, err
					}
				}
				target, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Spec.TargetResourceID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if target.State != enum.StateActive ||
					target.Kind != enum.KindRole {
					return entity.Resource{}, errs.ErrNotFound
				}
				input.Spec.TargetKind = target.Kind
				input.Spec.TargetVersion = target.Version
				targetProjectionSHA256, err := entity.ProjectionSHA256(target)
				if err != nil {
					return entity.Resource{}, errs.ErrInternal
				}
				promptArtifact, err := service.requireCleanArtifact(
					ctx,
					tx,
					input.Principal,
					input.Spec.PromptArtifactID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				prompt, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Spec.PromptProfileID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				promptSpec, ok := prompt.Spec.(entity.PromptProfileSpec)
				if !ok || prompt.Kind != enum.KindPromptProfile ||
					prompt.State != enum.StateActive ||
					promptSpec.Revision != input.Spec.PromptRevision {
					return entity.Resource{}, errs.ErrStateConflict
				}
				revision, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Spec.RuntimeRevisionID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
				if !ok || revision.Kind != enum.KindRuntimeRevision ||
					revision.State != enum.StateActive ||
					revisionSpec.PromptProfileID != prompt.ID ||
					revisionSpec.PromptRevision != promptSpec.Revision ||
					revisionSpec.RoleID != target.ID {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if input.Spec.RoomID != "" {
					room, err := tx.GetForUpdate(
						ctx,
						input.Principal.OrganizationID,
						input.Principal.ProjectID,
						input.Spec.RoomID,
					)
					if err != nil {
						return entity.Resource{}, err
					}
					if room.Kind != enum.KindChat || room.State != enum.StateActive {
						return entity.Resource{}, errs.ErrNotFound
					}
					if revisionSpec.ChatID != room.ID {
						return entity.Resource{}, errs.ErrStateConflict
					}
				} else if revisionSpec.ChatID != "" {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if input.Spec.SessionPolicy != "NEW" {
					session, err := tx.GetForUpdate(
						ctx,
						input.Principal.OrganizationID,
						input.Principal.ProjectID,
						input.Spec.ExecutionSessionID,
					)
					if err != nil {
						return entity.Resource{}, err
					}
					sessionSpec, ok := session.Spec.(entity.SessionSpec)
					if !ok || session.Kind != enum.KindSession ||
						session.State != enum.StateActive ||
						session.OwnerActorID != input.Principal.ActorID ||
						sessionSpec.AgentID != target.ID ||
						sessionSpec.ProviderAccountBindingID !=
							revisionSpec.ProviderCredentialBindingID ||
						sessionSpec.ConversationID != input.Spec.RoomID {
						return entity.Resource{}, errs.ErrStateConflict
					}
				}
				input.Spec.EffectiveInputSHA, err = scheduleEffectiveInput(
					input.Spec,
					targetProjectionSHA256,
					promptArtifact.SHA256,
					revision.Version,
					revisionSpec.ManifestSHA256,
				)
				if err != nil {
					return entity.Resource{}, errs.ErrInternal
				}
				if input.Spec.Validate() != nil {
					return entity.Resource{}, errs.ErrInvalidInput
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
			if err := requireLifecycleOwner(input.Principal, current); err != nil {
				return entity.Resource{}, err
			}
			if input.Action == "UPDATE" {
				nextSpec, err := configurationUpdateSpec(
					ctx,
					tx,
					input.Principal,
					current.Spec,
					input.Spec,
					input.DetachGitManagement,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				var ok bool
				input.Spec, ok = nextSpec.(entity.ScheduleSpec)
				if !ok {
					return entity.Resource{}, errs.ErrStateConflict
				}
			} else {
				if input.DetachGitManagement {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				if err := authorizeGitManagedMutation(
					ctx,
					tx,
					input.Principal,
					current.Spec,
				); err != nil {
					return entity.Resource{}, err
				}
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
				open, openErr := tx.HasOpenScheduleOccurrence(
					ctx, current.OrganizationID, current.ProjectID, current.ID,
				)
				if openErr != nil {
					return entity.Resource{}, openErr
				}
				if open {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if current.State == enum.StateArchived {
					updated, err = current.Transition(enum.StateDeletionPending, now)
				} else if current.State == enum.StateDeletionPending {
					updated, err = current.Transition(enum.StateDeleted, now)
				} else {
					return entity.Resource{}, errs.ErrStateConflict
				}
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

func scheduleEffectiveInput(
	spec entity.ScheduleSpec,
	targetProjectionSHA256 string,
	promptArtifactSHA256 string,
	runtimeVersion uint64,
	runtimeManifestSHA256 string,
) (string, error) {
	return canonicalHash(struct {
		TargetProjectionSHA256 string
		TargetType             string
		PlaybookRef            string
		PlaybookVersion        uint64
		PromptProfileID        string
		PromptRevision         uint64
		PromptArtifactSHA256   string
		RuntimeRevisionID      string
		RuntimeVersion         uint64
		RuntimeManifestSHA256  string
		SessionPolicy          string
		ExecutionSessionID     string
		RoomID                 string
	}{
		targetProjectionSHA256,
		spec.TargetType,
		spec.PlaybookRef,
		spec.PlaybookVersion,
		spec.PromptProfileID,
		spec.PromptRevision,
		promptArtifactSHA256,
		spec.RuntimeRevisionID,
		runtimeVersion,
		runtimeManifestSHA256,
		spec.SessionPolicy,
		spec.ExecutionSessionID,
		spec.RoomID,
	})
}

type scheduleOccurrenceReceipt struct {
	Occurrence domainrepo.ScheduleOccurrence `json:"occurrence"`
	LeaseToken string                        `json:"leaseToken,omitempty"`
}

// ClaimScheduleOccurrence выдаёт точному планировщику ограниченную аренду одной попытки.
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
				result = ScheduleOccurrenceResult(payload)
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			recovered, err := tx.ExpiredScheduleOccurrenceCandidates(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				now,
			)
			if err != nil {
				return err
			}
			for _, occurrence := range recovered {
				if occurrence.ExecutionTurnID == "" {
					return errs.ErrStateConflict
				}
				graph, err := service.lockOwnerGraphByTurn(
					ctx, tx, input.Principal, occurrence.ExecutionTurnID,
				)
				if err != nil {
					return err
				}
				if graph.Occurrence.ID != occurrence.ID {
					return errs.ErrStateConflict
				}
				schedule := graph.Schedule
				if err := service.recoverExpiredScheduleOccurrence(
					ctx, tx, input.Principal, graph, now,
				); err != nil {
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
			if !ok || schedule.Kind != enum.KindSchedule ||
				schedule.State != enum.StateActive ||
				scheduleSpec.EffectiveInputSHA != occurrence.EffectiveInputSHA256 ||
				scheduleSpec.TargetResourceID != occurrence.TargetResourceID ||
				scheduleSpec.TargetKind != occurrence.TargetKind ||
				scheduleSpec.TargetVersion != occurrence.TargetVersion ||
				scheduleSpec.PromptProfileID != occurrence.PromptProfileID ||
				scheduleSpec.PromptRevision != occurrence.PromptRevision ||
				scheduleSpec.RuntimeRevisionID != occurrence.RuntimeRevisionID ||
				scheduleSpec.SessionPolicy != occurrence.SessionPolicy ||
				scheduleSpec.RoomID != occurrence.RoomID {
				return errs.ErrStateConflict
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
				target.State == enum.StateDeleted {
				return errs.ErrStateConflict
			}
			prompt, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				occurrence.PromptProfileID,
			)
			if err != nil {
				return err
			}
			promptSpec, ok := prompt.Spec.(entity.PromptProfileSpec)
			if !ok || prompt.Kind != enum.KindPromptProfile ||
				prompt.State != enum.StateActive ||
				promptSpec.Revision != occurrence.PromptRevision {
				return errs.ErrStateConflict
			}
			revision, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				occurrence.RuntimeRevisionID,
			)
			if err != nil {
				return err
			}
			revisionSpec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
			if !ok || revision.Kind != enum.KindRuntimeRevision ||
				revision.State != enum.StateActive ||
				revisionSpec.PromptProfileID != occurrence.PromptProfileID ||
				revisionSpec.PromptRevision != occurrence.PromptRevision {
				return errs.ErrStateConflict
			}
			if occurrence.RoomID != "" {
				room, roomErr := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					occurrence.RoomID,
				)
				if roomErr != nil {
					return roomErr
				}
				if room.Kind != enum.KindChat || room.State != enum.StateActive {
					return errs.ErrStateConflict
				}
			}
			promptArtifact, err := service.requireCleanArtifact(
				ctx,
				tx,
				input.Principal,
				scheduleSpec.PromptArtifactID,
			)
			if err != nil {
				return err
			}
			pinnedInputSHA256, err := scheduleEffectiveInput(
				scheduleSpec,
				targetDigest,
				promptArtifact.SHA256,
				revision.Version,
				revisionSpec.ManifestSHA256,
			)
			if err != nil {
				return errs.ErrInternal
			}
			if pinnedInputSHA256 != occurrence.EffectiveInputSHA256 {
				return errs.ErrStateConflict
			}
			session, sessionSpec, err := service.prepareScheduleSession(
				ctx,
				tx,
				input.Principal,
				schedule,
				scheduleSpec,
				revisionSpec,
				now,
			)
			if err != nil {
				return err
			}
			runtimeRevision, err := service.createRuntimeRevision(
				ctx,
				tx,
				input.Principal,
				session,
				sessionSpec,
			)
			if err != nil {
				return err
			}
			processID := ""
			if scheduleSpec.TargetType == "PLAYBOOK" {
				processID = uuid.NewString()
			}
			sessionSpec.LastTurnSequence++
			updatedSession, err := session.Update(
				session.Name,
				sessionSpec,
				now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updatedSession, session.Version); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"schedule_enqueue_session",
				updatedSession,
			); err != nil {
				return err
			}
			sourceRef := "schedule-occurrence:" + occurrence.ID
			runtimeSpec := runtimeRevision.Spec.(entity.RuntimeRevisionSpec)
			turnID := uuid.NewString()
			turn, err := entity.New(
				turnID,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				session.ID,
				schedule.OwnerActorID,
				enum.KindTurn,
				"Scheduled turn "+occurrence.ID,
				entity.TurnSpec{
					SessionID:         session.ID,
					Sequence:          sessionSpec.LastTurnSequence,
					SourceRef:         sourceRef,
					PromptArtifactID:  scheduleSpec.PromptArtifactID,
					RuntimeRevisionID: runtimeRevision.ID,
					ProcessRunID:      processID,
					Attempt:           1,
					EffectiveInputSHA256: hashRuntimeInput(
						sourceRef,
						promptArtifact.SHA256,
						runtimeSpec.ManifestSHA256,
						processID,
					),
				},
				now,
			)
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.Insert(ctx, turn); err != nil {
				return err
			}
			turnSpec := turn.Spec.(entity.TurnSpec)
			if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
				TurnID:              turn.ID,
				Attempt:             turnSpec.Attempt,
				WorkloadID:          "unassigned",
				AuthorityGeneration: input.Principal.AuthorityGeneration,
				State:               "QUEUED",
				InputSHA256:         turnSpec.EffectiveInputSHA256,
				LeaseFence:          turn.Version,
				StartedAt:           now,
			}); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"schedule_enqueue_turn",
				turn,
			); err != nil {
				return err
			}
			var scheduledProcess entity.Resource
			if processID != "" {
				process, err := entity.New(
					processID,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					schedule.ID,
					schedule.OwnerActorID,
					enum.KindProcessRun,
					"Scheduled process "+occurrence.ID,
					entity.ProcessRunSpec{
						PlaybookRef:                   scheduleSpec.PlaybookRef,
						PolicyRevision:                scheduleSpec.PlaybookVersion,
						RootTriggerRef:                sourceRef,
						RootInitiatorActorID:          schedule.OwnerActorID,
						RootSessionID:                 session.ID,
						RootSessionVersion:            updatedSession.Version,
						RootTurnID:                    turn.ID,
						RootTurnVersion:               turn.Version,
						RootAttempt:                   1,
						ImmutableInputSHA256:          turnSpec.EffectiveInputSHA256,
						RuntimeRevisionID:             runtimeRevision.ID,
						ScheduleID:                    schedule.ID,
						OccurrenceID:                  occurrence.ID,
						CurrentSessionID:              session.ID,
						CurrentSessionVersion:         updatedSession.Version,
						CurrentTurnID:                 turn.ID,
						CurrentTurnVersion:            turn.Version,
						CurrentAttempt:                1,
						CurrentRuntimeRevisionID:      runtimeRevision.ID,
						CurrentRuntimeRevisionVersion: runtimeRevision.Version,
						CurrentInputSHA256:            turnSpec.EffectiveInputSHA256,
					},
					now,
				)
				if err != nil {
					return errs.ErrInternal
				}
				if err := tx.Insert(ctx, process); err != nil {
					return err
				}
				scheduledProcess = process
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"schedule_start_process",
					process,
				); err != nil {
					return err
				}
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
			occurrence.LeaseExpiresAt = now.Add(occurrence.MaximumExecution)
			occurrence.ExecutionSessionID = updatedSession.ID
			occurrence.ExecutionSessionVersion = updatedSession.Version
			occurrence.ExecutionTurnID = turn.ID
			occurrence.ExecutionTurnVersion = turn.Version
			occurrence.ExecutionProcessRunID = scheduledProcess.ID
			occurrence.ExecutionProcessVersion = scheduledProcess.Version
			occurrence.ExecutionRuntimeRevisionID = runtimeRevision.ID
			occurrence.ExecutionRuntimeRevisionVersion = runtimeRevision.Version
			occurrence.UpdatedAt = now
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				expectedAttempt,
				"",
			); err != nil {
				return err
			}
			if err := tx.SaveScheduledRun(ctx, domainrepo.ScheduledRun{
				OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
				SessionID: updatedSession.ID, SessionVersion: updatedSession.Version,
				TurnID: turn.ID, TurnVersion: turn.Version,
				ProcessRunID: scheduledProcess.ID, ProcessVersion: scheduledProcess.Version,
				RuntimeRevisionID:             runtimeRevision.ID,
				RuntimeRevisionVersion:        runtimeRevision.Version,
				EffectiveInputSHA256:          turnSpec.EffectiveInputSHA256,
				CurrentSessionID:              updatedSession.ID,
				CurrentSessionVersion:         updatedSession.Version,
				CurrentTurnID:                 turn.ID,
				CurrentTurnVersion:            turn.Version,
				CurrentTurnAttempt:            turnSpec.Attempt,
				CurrentProcessRunID:           scheduledProcess.ID,
				CurrentProcessVersion:         scheduledProcess.Version,
				CurrentRuntimeRevisionID:      runtimeRevision.ID,
				CurrentRuntimeRevisionVersion: runtimeRevision.Version,
				CurrentInputSHA256:            turnSpec.EffectiveInputSHA256,
				State:                         "CLAIMED", CreatedAt: now,
			}); err != nil {
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

func (service *Service) prepareScheduleSession(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	schedule entity.Resource,
	spec entity.ScheduleSpec,
	revisionSpec entity.RuntimeRevisionSpec,
	now time.Time,
) (entity.Resource, entity.SessionSpec, error) {
	if spec.SessionPolicy != "NEW" {
		session, err := tx.GetForUpdate(
			ctx,
			principal.OrganizationID,
			principal.ProjectID,
			spec.ExecutionSessionID,
		)
		if err != nil {
			return entity.Resource{}, entity.SessionSpec{}, err
		}
		sessionSpec, ok := session.Spec.(entity.SessionSpec)
		if !ok || session.Kind != enum.KindSession ||
			session.State != enum.StateActive ||
			session.OwnerActorID != schedule.OwnerActorID ||
			sessionSpec.AgentID != spec.TargetResourceID ||
			sessionSpec.ProviderAccountBindingID !=
				revisionSpec.ProviderCredentialBindingID ||
			sessionSpec.ConversationID != spec.RoomID {
			return entity.Resource{}, entity.SessionSpec{}, errs.ErrStateConflict
		}
		return session, sessionSpec, nil
	}
	role, err := tx.GetForUpdate(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
		spec.TargetResourceID,
	)
	if err != nil {
		return entity.Resource{}, entity.SessionSpec{}, err
	}
	roleSpec, ok := role.Spec.(entity.RoleSpec)
	if !ok || role.Kind != enum.KindRole || role.State != enum.StateActive {
		return entity.Resource{}, entity.SessionSpec{}, errs.ErrStateConflict
	}
	binding, err := service.selectProviderBinding(
		ctx, tx, principal, role.ID, roleSpec, "", now,
	)
	if err != nil {
		return entity.Resource{}, entity.SessionSpec{}, err
	}
	sessionSpec := entity.SessionSpec{
		AgentID:                  role.ID,
		ProviderAccountBindingID: binding.ID,
		ConversationID:           spec.RoomID,
	}
	session, err := entity.New(
		uuid.NewString(),
		principal.OrganizationID,
		principal.ProjectID,
		schedule.ID,
		schedule.OwnerActorID,
		enum.KindSession,
		"Scheduled session "+schedule.ID,
		sessionSpec,
		now,
	)
	if err != nil {
		return entity.Resource{}, entity.SessionSpec{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, session); err != nil {
		return entity.Resource{}, entity.SessionSpec{}, err
	}
	if err := service.appendMutationRecords(
		ctx,
		tx,
		principal,
		"schedule_create_session",
		session,
	); err != nil {
		return entity.Resource{}, entity.SessionSpec{}, err
	}
	return session, sessionSpec, nil
}

// CompleteScheduleOccurrence завершает или повторяет только текущую аренду планировщика.
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
		input.TerminalState != "" || input.Outcome != "" ||
		input.ResultArtifactID != "" {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		OccurrenceID    string
		TokenHash       string
		ExpectedAttempt uint32
	}{
		identity(input.Principal),
		input.OccurrenceID,
		hashString(input.LeaseToken),
		input.ExpectedAttempt,
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
			candidate, err := tx.GetScheduleOccurrence(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OccurrenceID,
			)
			if err != nil {
				return err
			}
			if candidate.State != "CLAIMED" || candidate.ExecutionTurnID == "" {
				return errs.ErrStateConflict
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, candidate.ExecutionTurnID,
			)
			if err != nil {
				return err
			}
			occurrence := graph.Occurrence
			if occurrence.ID != candidate.ID {
				return errs.ErrStateConflict
			}
			if occurrence.State != "CLAIMED" ||
				occurrence.Attempt != input.ExpectedAttempt ||
				occurrence.TokenHash != hashString(input.LeaseToken) ||
				occurrence.ClaimantWorkloadID != input.Principal.CallerWorkload ||
				occurrence.AuthorityGeneration != input.Principal.AuthorityGeneration ||
				!occurrence.LeaseExpiresAt.After(service.now()) {
				return errs.ErrStateConflict
			}
			if value.ValidateID(occurrence.ExecutionSessionID) != nil ||
				value.ValidateID(occurrence.ExecutionTurnID) != nil ||
				value.ValidateID(occurrence.ExecutionRuntimeRevisionID) != nil ||
				occurrence.ExecutionSessionVersion == 0 ||
				occurrence.ExecutionTurnVersion == 0 ||
				occurrence.ExecutionRuntimeRevisionVersion == 0 {
				return errs.ErrStateConflict
			}
			schedule := graph.Schedule
			scheduleSpec, ok := schedule.Spec.(entity.ScheduleSpec)
			if !ok || schedule.Kind != enum.KindSchedule {
				return errs.ErrStateConflict
			}
			run := graph.Run
			if validateScheduledRunBinding(occurrence, run) != nil ||
				run.State != "CLAIMED" {
				return errs.ErrStateConflict
			}
			session := graph.Session
			if session.Kind != enum.KindSession ||
				session.OwnerActorID != schedule.OwnerActorID {
				return errs.ErrStateConflict
			}
			turn := graph.Turn
			turnSpec, ok := turn.Spec.(entity.TurnSpec)
			if !ok || turn.Kind != enum.KindTurn || !turn.State.Terminal() ||
				(turn.State != enum.StateSucceeded && turn.State != enum.StateFailed &&
					turn.State != enum.StateCancelled && turn.State != enum.StateExpired) ||
				turnSpec.SessionID != occurrence.ExecutionSessionID ||
				turnSpec.RuntimeRevisionID != occurrence.ExecutionRuntimeRevisionID ||
				turnSpec.Attempt != run.CurrentTurnAttempt ||
				turnSpec.Outcome == "" {
				return errs.ErrStateConflict
			}
			terminalState := scheduledTerminalState(turn.State)
			outcome := turnSpec.Outcome
			resultArtifactID := turnSpec.ResultArtifactID
			if occurrence.ExecutionProcessRunID != "" {
				process := graph.Process
				processSpec, ok := process.Spec.(entity.ProcessRunSpec)
				current, currentErr := currentExecution(processSpec)
				if !ok || process.Kind != enum.KindProcessRun || !process.State.Terminal() ||
					process.State != turn.State || processSpec.OccurrenceID != occurrence.ID ||
					currentErr != nil || !executionMatchesTurn(current, turn, turnSpec) ||
					processSpec.Outcome != outcome ||
					processSpec.ResultArtifactID != resultArtifactID {
					return errs.ErrStateConflict
				}
			}
			expectedToken := occurrence.TokenHash
			now := service.now().UTC().Truncate(time.Microsecond)
			occurrence.State = terminalState
			occurrence.Outcome = outcome
			occurrence.ResultArtifactID = resultArtifactID
			occurrence.ClaimantWorkloadID = ""
			occurrence.AuthorityGeneration = 0
			occurrence.TokenHash = ""
			occurrence.LeaseExpiresAt = time.Time{}
			occurrence.UpdatedAt = now
			if terminalState == "FAILED" {
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
			if err := tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
				OccurrenceID: occurrence.ID, Attempt: input.ExpectedAttempt,
				State: terminalState, Outcome: outcome,
				ResultArtifactID: resultArtifactID, FinishedAt: now,
			}); err != nil {
				return err
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

// CancelScheduleOccurrence отзывает ожидающий или текущий запуск без исполнения.
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
			candidate, err := tx.GetScheduleOccurrence(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OccurrenceID,
			)
			if err != nil {
				return err
			}
			var graph lockedOwnerGraph
			var occurrence domainrepo.ScheduleOccurrence
			if candidate.State == "QUEUED" {
				occurrence, err = tx.GetScheduleOccurrenceForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					candidate.ID,
				)
			} else {
				if candidate.ExecutionTurnID == "" {
					return errs.ErrStateConflict
				}
				graph, err = service.lockOwnerGraphByTurn(
					ctx, tx, input.Principal, candidate.ExecutionTurnID,
				)
				occurrence = graph.Occurrence
			}
			if err != nil {
				return err
			}
			if occurrence.ID != candidate.ID {
				return errs.ErrStateConflict
			}
			if occurrence.Attempt != input.ExpectedAttempt ||
				(occurrence.State != "QUEUED" && occurrence.State != "CLAIMED" &&
					occurrence.State != "WAITING_OWNER" &&
					occurrence.State != "CONTINUATION") {
				return errs.ErrStateConflict
			}
			var schedule entity.Resource
			if occurrence.State == "QUEUED" {
				schedule, err = tx.GetForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					occurrence.ScheduleID,
				)
				if err != nil {
					return err
				}
			} else {
				schedule = graph.Schedule
			}
			if schedule.Kind != enum.KindSchedule {
				return errs.ErrStateConflict
			}
			if err := requireLifecycleOwner(input.Principal, schedule); err != nil {
				return err
			}
			expectedToken := occurrence.TokenHash
			now := service.now().UTC().Truncate(time.Microsecond)
			if occurrence.State == "QUEUED" {
				if occurrence.ExecutionSessionID != "" ||
					occurrence.ExecutionTurnID != "" ||
					occurrence.ExecutionProcessRunID != "" {
					return errs.ErrStateConflict
				}
			} else {
				run := graph.Run
				if validateScheduledRunBinding(occurrence, run) != nil ||
					run.State != occurrence.State {
					return errs.ErrStateConflict
				}
				session := graph.Session
				turn := graph.Turn
				turnSpec, ok := turn.Spec.(entity.TurnSpec)
				if !ok || session.Kind != enum.KindSession ||
					session.OwnerActorID != schedule.OwnerActorID ||
					turn.Kind != enum.KindTurn ||
					turn.OwnerActorID != schedule.OwnerActorID ||
					turnSpec.SessionID != session.ID ||
					turnSpec.Attempt != run.CurrentTurnAttempt ||
					turnSpec.RuntimeRevisionID != run.CurrentRuntimeRevisionID ||
					turnSpec.EffectiveInputSHA256 != run.CurrentInputSHA256 {
					return errs.ErrStateConflict
				}
				if !turn.State.Terminal() {
					if _, err := service.cancelTurnExecution(
						ctx, tx, input.Principal, turn, input.ReasonCode, now,
					); err != nil {
						return err
					}
				} else if turn.State != enum.StateSucceeded &&
					turn.State != enum.StateCancelled {
					return errs.ErrStateConflict
				}
				if run.CurrentProcessRunID != "" {
					process := graph.Process
					processSpec, ok := process.Spec.(entity.ProcessRunSpec)
					current, currentErr := currentExecution(processSpec)
					if !ok || process.Kind != enum.KindProcessRun ||
						process.State.Terminal() ||
						process.OwnerActorID != schedule.OwnerActorID ||
						processSpec.OccurrenceID != occurrence.ID ||
						processSpec.ScheduleID != schedule.ID || currentErr != nil ||
						!executionMatchesTurn(current, turn, turnSpec) {
						return errs.ErrStateConflict
					}
					children, err := tx.HasActiveChildProcesses(
						ctx, process.OrganizationID, process.ProjectID, process.ID,
					)
					if err != nil || children {
						return errs.ErrStateConflict
					}
					if occurrence.State == "WAITING_OWNER" {
						gate, gateErr := tx.ActiveOwnerGateForProcess(
							ctx, process.OrganizationID, process.ProjectID, process.ID,
						)
						if gateErr != nil {
							return gateErr
						}
						cancelledGate, transitionErr := gate.Transition(
							enum.StateCancelled, now,
						)
						if transitionErr != nil {
							return errs.ErrStateConflict
						}
						if err := tx.Update(ctx, cancelledGate, gate.Version); err != nil {
							return err
						}
						if err := service.appendMutationRecords(
							ctx, tx, input.Principal,
							"cancel_occurrence_owner_gate", cancelledGate,
						); err != nil {
							return err
						}
					}
					cancelledProcess, transitionErr := process.Transition(
						enum.StateCancelled, now,
					)
					if transitionErr != nil {
						return errs.ErrStateConflict
					}
					if err := tx.Update(ctx, cancelledProcess, process.Version); err != nil {
						return err
					}
					if err := service.revokeExecutionClaims(
						ctx, tx, input.Principal, process.ID, turn.ID,
						input.ReasonCode, now,
					); err != nil {
						return err
					}
					if err := service.appendMutationRecords(
						ctx, tx, input.Principal,
						"cancel_occurrence_process", cancelledProcess,
					); err != nil {
						return err
					}
				}
				if session.ParentID == schedule.ID && !session.State.Terminal() {
					cancelledSession, transitionErr := session.Transition(
						enum.StateCancelled, now,
					)
					if transitionErr != nil {
						return errs.ErrStateConflict
					}
					if err := tx.Update(ctx, cancelledSession, session.Version); err != nil {
						return err
					}
					if err := service.appendMutationRecords(
						ctx, tx, input.Principal,
						"cancel_occurrence_session", cancelledSession,
					); err != nil {
						return err
					}
				}
				if err := tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
					OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
					State: "CANCELLED", Outcome: input.ReasonCode, FinishedAt: now,
				}); err != nil {
					return err
				}
			}
			occurrence.State = "CANCELLED"
			occurrence.Outcome = input.ReasonCode
			occurrence.ClaimantWorkloadID = ""
			occurrence.AuthorityGeneration = 0
			occurrence.TokenHash = ""
			occurrence.LeaseExpiresAt = time.Time{}
			occurrence.UpdatedAt = now
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

// StartProcess связывает определённого сервером корневого инициатора с
// неизменяемым входом хода.
func (service *Service) StartProcess(
	ctx context.Context,
	input StartProcessInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionStartProcess); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateName(input.Name) != nil ||
		value.ValidateID(input.InputArtifactID) != nil ||
		(input.ParentProcessID != "" &&
			value.ValidateID(input.ParentProcessID) != nil) ||
		(input.ParentProcessID == "" &&
			(!validRuntimeReference(input.PlaybookRef) ||
				!validRuntimeReference(input.RootTriggerRef) ||
				strings.HasPrefix(
					input.RootTriggerRef,
					"schedule-occurrence:",
				) ||
				input.PolicyRevision == 0 ||
				value.ValidateID(input.RootSessionID) != nil ||
				value.ValidateID(input.RootTurnID) != nil ||
				input.RootAttempt == 0 ||
				input.LaunchingTurnID != "" ||
				input.LaunchingAttempt != 0)) ||
		(input.ParentProcessID != "" &&
			(value.ValidateID(input.LaunchingTurnID) != nil ||
				input.LaunchingAttempt == 0 ||
				input.Principal.AuthoritySource != "AGENT_SESSION" ||
				value.ValidateID(input.Principal.AuthorityReference) != nil ||
				input.LaunchingTurnID != input.Principal.AuthorityReference ||
				input.LaunchingAttempt != uint32(input.Principal.AuthorityRevision) ||
				input.Principal.AuthorityGrantGeneration == 0 ||
				input.PlaybookRef != "" ||
				input.PolicyRevision != 0 ||
				input.RootTriggerRef != "" ||
				input.RootSessionID != "" ||
				input.RootTurnID != "" ||
				input.RootAttempt != 0)) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity         commandIdentity
		Name             string
		ParentProcessID  string
		PlaybookRef      string
		PolicyRevision   uint64
		RootTriggerRef   string
		RootSessionID    string
		RootTurnID       string
		RootAttempt      uint32
		InputArtifactID  string
		LaunchingTurnID  string
		LaunchingAttempt uint32
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
		input.LaunchingTurnID,
		input.LaunchingAttempt,
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
			var delegatedTargetTurn entity.Resource
			rootActorID := input.Principal.ActorID
			rootSessionID := input.RootSessionID
			rootSessionVersion := uint64(0)
			rootTurnID := input.RootTurnID
			rootTurnVersion := uint64(0)
			rootAttempt := input.RootAttempt
			launchingTurnID := ""
			launchingAttempt := uint32(0)
			delegationID := ""
			targetSessionID := ""
			targetSessionVersion := uint64(0)
			targetTurnID := ""
			targetTurnVersion := uint64(0)
			targetAttempt := uint32(0)
			runtimeRevisionID := ""
			runtimeRevisionVersion := uint64(0)
			currentInputSHA256 := ""
			rootImmutableInput := ""
			playbookRef := input.PlaybookRef
			policyRevision := input.PolicyRevision
			rootTriggerRef := input.RootTriggerRef
			scheduleID := ""
			occurrenceID := ""
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
				parentSpec, ok := parent.Spec.(entity.ProcessRunSpec)
				if !ok || parent.Kind != enum.KindProcessRun ||
					parent.State.Terminal() ||
					parentSpec.RootInitiatorActorID != input.Principal.ActorID {
					return entity.Resource{}, errs.ErrStateConflict
				}
				delegation, err := tx.GetDelegationEdgeByTargetTurn(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Principal.AuthorityReference,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if delegation.ParentProcessRunID != parent.ID ||
					delegation.TargetTurnID != input.Principal.AuthorityReference ||
					delegation.TargetAttempt != uint32(input.Principal.AuthorityRevision) ||
					delegation.TargetInputSHA256 != input.Principal.AuthorityDigest ||
					delegation.RootInitiatorActorID != parentSpec.RootInitiatorActorID ||
					delegation.GrantGeneration != input.Principal.AuthorityGrantGeneration {
					return entity.Resource{}, errs.ErrStateConflict
				}
				targetTurn, err := tx.GetForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					delegation.TargetTurnID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				targetSpec, ok := targetTurn.Spec.(entity.TurnSpec)
				if !ok || targetTurn.Kind != enum.KindTurn || targetTurn.State.Terminal() ||
					targetTurn.OwnerActorID != parentSpec.RootInitiatorActorID ||
					targetSpec.SessionID != delegation.TargetSessionID ||
					targetSpec.ProcessRunID != parent.ID ||
					targetSpec.Attempt != delegation.TargetAttempt ||
					targetSpec.EffectiveInputSHA256 != delegation.TargetInputSHA256 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				targetSession, err := tx.GetForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					delegation.TargetSessionID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if targetSession.Kind != enum.KindSession ||
					targetSession.State != enum.StateActive ||
					targetSession.OwnerActorID != parentSpec.RootInitiatorActorID {
					return entity.Resource{}, errs.ErrStateConflict
				}
				delegatedTargetTurn = targetTurn
				rootActorID = parentSpec.RootInitiatorActorID
				rootSessionID = parentSpec.RootSessionID
				rootSessionVersion = parentSpec.RootSessionVersion
				rootTurnID = parentSpec.RootTurnID
				rootTurnVersion = parentSpec.RootTurnVersion
				rootAttempt = parentSpec.RootAttempt
				runtimeRevisionID = targetSpec.RuntimeRevisionID
				currentInputSHA256 = targetSpec.EffectiveInputSHA256
				rootImmutableInput = parentSpec.ImmutableInputSHA256
				playbookRef = parentSpec.PlaybookRef
				policyRevision = parentSpec.PolicyRevision
				rootTriggerRef = parentSpec.RootTriggerRef
				scheduleID = parentSpec.ScheduleID
				occurrenceID = parentSpec.OccurrenceID
				launchingTurnID = delegation.TargetTurnID
				launchingAttempt = input.LaunchingAttempt
				delegationID = delegation.ID
				targetSessionID = delegation.TargetSessionID
				targetSessionVersion = targetSession.Version
				targetTurnID = delegation.TargetTurnID
				targetTurnVersion = targetTurn.Version + 1
				targetAttempt = delegation.TargetAttempt
			} else {
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
					session.OwnerActorID != input.Principal.ActorID ||
					turn.Kind != enum.KindTurn ||
					turn.OwnerActorID != input.Principal.ActorID ||
					turnSpec.SessionID != session.ID ||
					turnSpec.Attempt != input.RootAttempt ||
					turnSpec.ProcessRunID != "" || turn.State.Terminal() {
					return entity.Resource{}, errs.ErrStateConflict
				}
				runtimeRevisionID = turnSpec.RuntimeRevisionID
				currentInputSHA256 = turnSpec.EffectiveInputSHA256
				rootImmutableInput = turnSpec.EffectiveInputSHA256
				rootSessionVersion = session.Version
				rootTurnVersion = turn.Version + 1
			}
			runtimeRevision, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				runtimeRevisionID,
			)
			if err != nil || runtimeRevision.Kind != enum.KindRuntimeRevision ||
				runtimeRevision.State != enum.StateActive {
				return entity.Resource{}, errs.ErrStateConflict
			}
			runtimeRevisionVersion = runtimeRevision.Version
			artifact, err := service.requireCleanArtifact(
				ctx,
				tx,
				input.Principal,
				input.InputArtifactID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			spec := entity.ProcessRunSpec{
				ParentProcessRunID:    input.ParentProcessID,
				PlaybookRef:           playbookRef,
				PolicyRevision:        policyRevision,
				RootTriggerRef:        rootTriggerRef,
				RootInitiatorActorID:  rootActorID,
				RootSessionID:         rootSessionID,
				RootSessionVersion:    rootSessionVersion,
				RootTurnID:            rootTurnID,
				RootTurnVersion:       rootTurnVersion,
				RootAttempt:           rootAttempt,
				RuntimeRevisionID:     runtimeRevisionID,
				LaunchingProcessRunID: input.ParentProcessID,
				LaunchingTurnID:       launchingTurnID,
				LaunchingAttempt:      launchingAttempt,
				DelegationID:          delegationID,
				TargetSessionID:       targetSessionID,
				TargetSessionVersion:  targetSessionVersion,
				TargetTurnID:          targetTurnID,
				TargetTurnVersion:     targetTurnVersion,
				TargetAttempt:         targetAttempt,
				ImmutableInputSHA256: hashRuntimeInput(
					rootTriggerRef,
					artifact.SHA256,
					rootImmutableInput,
					playbookRef,
				),
				ScheduleID:   scheduleID,
				OccurrenceID: occurrenceID,
			}
			currentTurnID := rootTurnID
			currentTurnVersion := rootTurnVersion
			currentSessionID := rootSessionID
			currentSessionVersion := rootSessionVersion
			currentAttempt := rootAttempt
			if input.ParentProcessID != "" {
				currentTurnID = targetTurnID
				currentTurnVersion = targetTurnVersion
				currentSessionID = targetSessionID
				currentSessionVersion = targetSessionVersion
				currentAttempt = targetAttempt
			}
			setCurrentExecution(&spec, executionTuple{
				SessionID:              currentSessionID,
				SessionVersion:         currentSessionVersion,
				TurnID:                 currentTurnID,
				TurnVersion:            currentTurnVersion,
				Attempt:                currentAttempt,
				RuntimeRevisionID:      runtimeRevisionID,
				RuntimeRevisionVersion: runtimeRevisionVersion,
				InputSHA256:            currentInputSHA256,
			})
			processID := uuid.NewString()
			process, err := entity.New(
				processID,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ParentProcessID,
				rootActorID,
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
			boundTurn := delegatedTargetTurn
			if boundTurn.ID == "" {
				boundTurn, err = tx.GetForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					rootTurnID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
			}
			boundSpec, ok := boundTurn.Spec.(entity.TurnSpec)
			if !ok || boundTurn.Kind != enum.KindTurn || boundTurn.State.Terminal() ||
				(boundSpec.ProcessRunID != "" &&
					boundSpec.ProcessRunID != input.ParentProcessID) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			boundSpec.ProcessRunID = process.ID
			updatedTarget, err := boundTurn.Update(
				boundTurn.Name, boundSpec, now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if updatedTarget.Version != rootTurnVersion &&
				updatedTarget.Version != targetTurnVersion {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updatedTarget, boundTurn.Version); err != nil {
				return entity.Resource{}, err
			}
			if err := service.appendMutationRecords(
				ctx, tx, input.Principal, "bind_process_turn", updatedTarget,
			); err != nil {
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

// CompleteProcess завершает процесс только по авторитетному terminal-ходу.
func (service *Service) CompleteProcess(
	ctx context.Context,
	input CompleteProcessInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionCompleteProcess); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProcessRunID) != nil || input.ExpectedVersion == 0 ||
		(input.TerminalState != enum.StateSucceeded &&
			input.TerminalState != enum.StateFailed &&
			input.TerminalState != enum.StateCancelled) ||
		len(input.Outcome) < 1 || len(input.Outcome) > 256 ||
		(input.ResultArtifactID != "" && value.ValidateID(input.ResultArtifactID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    CompleteProcessInput
	}{identity(input.Principal), input})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx, input.Principal, input.IdempotencyKey, "complete_process", requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			process, err := tx.GetForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				input.ProcessRunID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := process.Spec.(entity.ProcessRunSpec)
			if err := requireLifecycleOwner(input.Principal, process); err != nil {
				return entity.Resource{}, err
			}
			if !ok || process.Kind != enum.KindProcessRun ||
				process.Version != input.ExpectedVersion ||
				spec.RootInitiatorActorID != input.Principal.ActorID {
				return entity.Resource{}, errs.ErrStateConflict
			}
			execution, err := currentExecution(spec)
			if err != nil {
				return entity.Resource{}, err
			}
			turn, err := tx.GetForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				execution.TurnID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			turnSpec, ok := turn.Spec.(entity.TurnSpec)
			if !ok || turn.Kind != enum.KindTurn || turnSpec.ProcessRunID != process.ID ||
				!executionMatchesTurn(execution, turn, turnSpec) ||
				turn.State != input.TerminalState || turnSpec.Outcome != input.Outcome ||
				turnSpec.ResultArtifactID != input.ResultArtifactID {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if process.State.Terminal() {
				if process.State != input.TerminalState ||
					spec.Outcome != turnSpec.Outcome ||
					spec.ResultArtifactID != turnSpec.ResultArtifactID {
					return entity.Resource{}, errs.ErrStateConflict
				}
				return process, nil
			}
			open, err := tx.ProcessHasOpenWork(
				ctx, process.OrganizationID, process.ProjectID, process.ID,
				turn.ID, "",
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if open {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := service.revokeExecutionClaims(
				ctx, tx, input.Principal, process.ID, turn.ID,
				"complete_process", service.now().UTC().Truncate(time.Microsecond),
			); err != nil {
				return entity.Resource{}, err
			}
			spec.Outcome = turnSpec.Outcome
			spec.ResultArtifactID = turnSpec.ResultArtifactID
			updated, err := process.ReplaceAndTransition(
				spec, input.TerminalState,
				service.now().UTC().Truncate(time.Microsecond),
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, process.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx, tx, input.Principal, "complete_process", updated,
			)
		},
	)
}

// CancelProcess завершает точную версию процесса специализированной командой.
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
			if err := requireLifecycleOwner(input.Principal, process); err != nil {
				return entity.Resource{}, err
			}
			activeChildren, err := tx.HasActiveChildProcesses(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				process.ID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if activeChildren {
				return entity.Resource{}, errs.ErrStateConflict
			}
			turns, err := tx.ActiveProcessTurnsForUpdate(
				ctx, process.OrganizationID, process.ProjectID, process.ID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			for _, turn := range turns {
				if _, err := service.cancelTurnExecution(
					ctx, tx, input.Principal, turn, input.ReasonCode, now,
				); err != nil {
					return entity.Resource{}, err
				}
			}
			if err := service.revokeExecutionClaims(
				ctx, tx, input.Principal, process.ID, "",
				input.ReasonCode, now,
			); err != nil {
				return entity.Resource{}, err
			}
			gate, gateErr := tx.ActiveOwnerGateForProcess(
				ctx, process.OrganizationID, process.ProjectID, process.ID,
			)
			if gateErr == nil {
				cancelledGate, transitionErr := gate.Transition(enum.StateCancelled, now)
				if transitionErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, cancelledGate, gate.Version); err != nil {
					return entity.Resource{}, err
				}
				if err := service.appendMutationRecords(
					ctx, tx, input.Principal, "cancel_process_owner_gate", cancelledGate,
				); err != nil {
					return entity.Resource{}, err
				}
			} else if !errors.Is(gateErr, errs.ErrNotFound) {
				return entity.Resource{}, gateErr
			}
			cancelled, err := process.Transition(
				enum.StateCancelled,
				now,
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

// RegisterArtifact принимает только неизменяемые метаданные и назначенное
// сервером состояние PENDING.
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

// RecordArtifactScan принимает результат только от точно настроенного сканера.
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

// RequestOwnerGate создаёт шлюз и переводит связанный процесс одной фиксацией.
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
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return err
			}
			process := graph.Process
			processSpec, ok := process.Spec.(entity.ProcessRunSpec)
			if err := requireLifecycleOwner(input.Principal, process); err != nil {
				return err
			}
			if !ok || process.Kind != enum.KindProcessRun ||
				process.State != enum.StateRunning ||
				process.Version != input.ProcessExpectedVersion ||
				processSpec.RootInitiatorActorID != input.Principal.ActorID {
				return errs.ErrStateConflict
			}
			execution, err := service.resolveCurrentExecution(
				ctx, tx, input.Principal, process, processSpec,
			)
			if err != nil {
				return err
			}
			executionSessionID := execution.SessionID
			executionTurnID := execution.TurnID
			executionAttempt := execution.Attempt
			if executionSessionID != input.SessionID ||
				executionTurnID != input.TurnID || executionAttempt != input.Attempt {
				return errs.ErrStateConflict
			}
			gateTurn := graph.Turn
			gateTurnSpec, ok := gateTurn.Spec.(entity.TurnSpec)
			if !ok || gateTurn.Kind != enum.KindTurn ||
				(gateTurn.State != enum.StateClaimed &&
					gateTurn.State != enum.StateRunning) ||
				gateTurn.OwnerActorID != process.OwnerActorID ||
				gateTurnSpec.SessionID != input.SessionID ||
				gateTurnSpec.ProcessRunID != process.ID ||
				gateTurnSpec.Attempt != input.Attempt ||
				gateTurnSpec.ResultArtifactID != "" ||
				input.Principal.AuthoritySource != "AGENT_SESSION" ||
				input.Principal.AuthorityReference != gateTurn.ID ||
				input.Principal.AuthorityRevision != uint64(gateTurnSpec.Attempt) ||
				input.Principal.AuthorityDigest != gateTurnSpec.EffectiveInputSHA256 {
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
			open, err := tx.ProcessHasOpenWork(
				ctx, process.OrganizationID, process.ProjectID, process.ID,
				gateTurn.ID, "",
			)
			if err != nil {
				return err
			}
			if open {
				return errs.ErrStateConflict
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			attempt, err := tx.GetTurnAttemptForUpdate(
				ctx, gateTurn.ID, gateTurnSpec.Attempt,
			)
			if err != nil || attempt.State != "CLAIMED" ||
				attempt.InputSHA256 != gateTurnSpec.EffectiveInputSHA256 ||
				attempt.AuthorityGeneration !=
					input.Principal.AuthorityGrantGeneration {
				return errs.ErrStateConflict
			}
			lease, err := tx.GetTurnLeaseForUpdate(ctx, gateTurn.ID)
			if err != nil || lease.Attempt != gateTurnSpec.Attempt ||
				lease.AuthorityGeneration != attempt.AuthorityGeneration ||
				lease.Fence != gateTurn.Version {
				return errs.ErrStateConflict
			}
			gateTurnSpec.ResultArtifactID = input.ResultArtifactID
			gateTurnSpec.Outcome = "owner_gate_pending"
			waitingTurn, err := gateTurn.ReplaceAndTransition(
				gateTurnSpec, enum.StateWaitingOwner, now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Update(ctx, waitingTurn, gateTurn.Version); err != nil {
				return err
			}
			if err := tx.DeleteTurnLease(ctx, gateTurn.ID, lease.Fence); err != nil {
				return err
			}
			gateID := uuid.NewString()
			deliveryID := uuid.NewString()
			deliveryPayloadSHA256, err := canonicalHash(struct {
				Version          int
				DeliveryID       string
				OwnerGateID      string
				ProcessRunID     string
				SessionID        string
				TurnID           string
				Attempt          uint32
				ResultSHA256     string
				ImmutableInput   string
				RecipientActorID string
				ExpiresAt        time.Time
				ScheduleID       string
				OccurrenceID     string
			}{
				1,
				deliveryID,
				gateID,
				process.ID,
				executionSessionID,
				executionTurnID,
				executionAttempt,
				artifact.SHA256,
				processSpec.ImmutableInputSHA256,
				processSpec.RootInitiatorActorID,
				input.ExpiresAt.UTC().Truncate(time.Microsecond),
				processSpec.ScheduleID,
				processSpec.OccurrenceID,
			})
			if err != nil {
				return errs.ErrInternal
			}
			gate, err := entity.New(
				gateID,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				process.ID,
				process.OwnerActorID,
				enum.KindOwnerGate,
				"Owner gate "+process.ID,
				entity.OwnerGateSpec{
					ProcessRunID:          process.ID,
					ResultRef:             artifact.StorageRef,
					ResultSHA256:          artifact.SHA256,
					ExpiresAt:             input.ExpiresAt.UTC().Truncate(time.Microsecond),
					RootInitiatorActorID:  processSpec.RootInitiatorActorID,
					SessionID:             executionSessionID,
					TurnID:                executionTurnID,
					Attempt:               executionAttempt,
					ImmutableInputSHA256:  processSpec.ImmutableInputSHA256,
					RecipientActorID:      processSpec.RootInitiatorActorID,
					DeliveryWorkloadID:    service.ownerGateDeliveryWorkload,
					DeliverySPIFFEID:      service.ownerGateDeliverySPIFFEID,
					DeliveryID:            deliveryID,
					DeliveryPayloadSHA256: deliveryPayloadSHA256,
					ScheduleID:            processSpec.ScheduleID,
					OccurrenceID:          processSpec.OccurrenceID,
				},
				now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			// Открытие нового OwnerGate завершает прежний delivery arm. Его
			// provenance остаётся в owner aggregate/audit, а active union до
			// CHANGES_REQUESTED не содержит ни старый INTEGRATION, ни фиктивный
			// OWNER_GATE binding.
			processSpec.ClearContinuation()
			waiting, err := process.ReplaceAndTransition(
				processSpec, enum.StateWaitingOwner, now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Insert(ctx, gate); err != nil {
				return err
			}
			if processSpec.OccurrenceID != "" {
				occurrence := graph.Occurrence
				run := graph.Run
				if occurrence.ID != processSpec.OccurrenceID ||
					validateScheduledRunBinding(occurrence, run) != nil ||
					occurrence.ScheduleID != processSpec.ScheduleID ||
					!scheduledExecutionMayWaitOwner(occurrence.State, run.State) ||
					occurrence.ExecutionSessionID != executionSessionID ||
					occurrence.ExecutionTurnID != waitingTurn.ID ||
					occurrence.ExecutionProcessRunID != process.ID {
					return errs.ErrStateConflict
				}
				expectedToken := occurrence.TokenHash
				occurrence.State = "WAITING_OWNER"
				occurrence.ClaimantWorkloadID = ""
				occurrence.AuthorityGeneration = 0
				occurrence.TokenHash = ""
				occurrence.LeaseExpiresAt = time.Time{}
				occurrence.UpdatedAt = now
				if err := tx.UpdateScheduleOccurrence(
					ctx, occurrence, occurrence.Attempt, expectedToken,
				); err != nil {
					return err
				}
				if err := tx.WaitScheduledRun(
					ctx, occurrence.ID, occurrence.Attempt,
				); err != nil {
					return err
				}
				schedule := graph.Schedule
				if schedule.Kind != enum.KindSchedule ||
					schedule.OwnerActorID != process.OwnerActorID {
					return errs.ErrStateConflict
				}
				if err := service.appendMutationRecords(
					ctx, tx, input.Principal,
					"owner_gate_wait_schedule", schedule,
				); err != nil {
					return err
				}
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
				ctx, tx, input.Principal, "wait_owner_gate_turn", waitingTurn,
			); err != nil {
				return err
			}
			if err := service.revokeExecutionClaims(
				ctx, tx, input.Principal, process.ID, gateTurn.ID,
				"owner_gate_wait", now,
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

func scheduledExecutionMayWaitOwner(occurrenceState, runState string) bool {
	eligible := func(state string) bool {
		return state == "CLAIMED" || state == "CONTINUATION"
	}
	return occurrenceState == runState && eligible(occurrenceState)
}
