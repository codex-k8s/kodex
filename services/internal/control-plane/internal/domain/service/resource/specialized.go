package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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
	// next_run_at является owner-side PostgreSQL watermark и не входит в
	// caller-owned intent create/update.
	if input.Action == "CREATE" || input.Action == "UPDATE" {
		input.Spec.NextRunAt = time.Time{}
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
	var lockedCurrent entity.Resource
	validate := func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
		current, err := tx.GetForUpdate(
			ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.ScheduleID,
		)
		if err != nil {
			return 0, err
		}
		if current.Kind != enum.KindSchedule {
			return 0, errs.ErrNotFound
		}
		if err := requireLifecycleOwner(input.Principal, current); err != nil {
			return 0, err
		}
		if input.Action == "DELETE" {
			if current.Version == input.ExpectedVersion &&
				(current.State == enum.StateActive || current.State == enum.StatePaused ||
					current.State == enum.StateArchived || current.State == enum.StateDeletionPending) {
				// apply ниже закрывает полный terminal graph одной транзакцией.
			} else if current.State != enum.StateDeleted || current.Version <= input.ExpectedVersion ||
				current.Version > input.ExpectedVersion+3 {
				return 0, errs.ErrVersionMismatch
			}
		} else if current.Version != input.ExpectedVersion &&
			current.Version != input.ExpectedVersion+1 {
			return 0, errs.ErrVersionMismatch
		}
		if scheduleMutationRequiresClosedGraph(input.Action) {
			open, err := tx.HasOpenScheduleOccurrence(
				ctx, current.OrganizationID, current.ProjectID, current.ID,
			)
			if err != nil {
				return 0, err
			}
			if open {
				return 0, errs.ErrStateConflict
			}
		}
		lockedCurrent = current
		if current.Version == input.ExpectedVersion {
			return lifecycleReceiptApplyOrReplay, nil
		}
		return lifecycleReceiptReplay, nil
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return entity.Resource{}, err
		}
		now = now.UTC().Truncate(time.Microsecond)
		current := lockedCurrent
		if input.Action == "CREATE" || input.Action == "UPDATE" {
			if input.Action == "UPDATE" {
				currentSpec, ok := current.Spec.(entity.ScheduleSpec)
				if !ok {
					return entity.Resource{}, errs.ErrStateConflict
				}
				input.Spec.NextRunAt = currentSpec.NextRunAt
			}
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
			if input.Action == "CREATE" {
				input.Spec.NextRunAt, err = firstScheduleRun(input.Spec, now)
				if err != nil {
					return entity.Resource{}, err
				}
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
		if current.ID == "" {
			current, err = tx.GetForUpdate(
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
		}
		if input.Action == "UPDATE" {
			currentSpec, currentOK := current.Spec.(entity.ScheduleSpec)
			if !currentOK {
				return entity.Resource{}, errs.ErrStateConflict
			}
			input.Spec.NextRunAt = currentSpec.NextRunAt
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
			if input.Spec.Cron != currentSpec.Cron ||
				input.Spec.Interval != currentSpec.Interval ||
				input.Spec.Timezone != currentSpec.Timezone {
				input.Spec.NextRunAt, err = firstScheduleRun(input.Spec, now)
				if err != nil {
					return entity.Resource{}, err
				}
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
		if input.Action == "DELETE" {
			deleted := current
			if deleted.State == enum.StateActive || deleted.State == enum.StatePaused {
				archived, transitionErr := deleted.Transition(enum.StateArchived, now)
				if transitionErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if transitionErr = tx.Update(ctx, archived, deleted.Version); transitionErr != nil {
					return entity.Resource{}, transitionErr
				}
				if transitionErr = service.appendMutationRecords(
					ctx, tx, input.Principal, "manage_schedule_DELETE_ARCHIVE", archived,
				); transitionErr != nil {
					return entity.Resource{}, transitionErr
				}
				deleted = archived
			}
			if deleted.State == enum.StateArchived {
				pending, transitionErr := deleted.Transition(enum.StateDeletionPending, now)
				if transitionErr != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if transitionErr = tx.Update(ctx, pending, deleted.Version); transitionErr != nil {
					return entity.Resource{}, transitionErr
				}
				if transitionErr = service.appendMutationRecords(
					ctx, tx, input.Principal, "manage_schedule_DELETE_PENDING", pending,
				); transitionErr != nil {
					return entity.Resource{}, transitionErr
				}
				deleted = pending
			}
			terminal, transitionErr := deleted.Transition(enum.StateDeleted, now)
			if transitionErr != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if transitionErr = tx.Update(ctx, terminal, deleted.Version); transitionErr != nil {
				return entity.Resource{}, transitionErr
			}
			return terminal, service.appendMutationRecords(
				ctx, tx, input.Principal, "manage_schedule_DELETE", terminal,
			)
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
	}
	if input.Action == "CREATE" {
		return service.withResourceReceipt(
			ctx, input.Principal, input.IdempotencyKey,
			"manage_schedule_"+input.Action, requestHash, apply,
		)
	}
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"manage_schedule_"+input.Action,
		requestHash,
		validate,
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(lockedCurrent, stored)
		},
		apply,
	)
}

func firstScheduleRun(spec entity.ScheduleSpec, now time.Time) (time.Time, error) {
	return nextScheduleRun(spec, now, now)
}

// RunScheduleNow создаёт отдельную немедленную occurrence и не меняет
// server-owned плановый watermark Schedule.NextRunAt.
func (service *Service) RunScheduleNow(
	ctx context.Context,
	input RunScheduleNowInput,
) (domainrepo.ScheduleOccurrence, error) {
	if err := authorize(input.Principal, permissionManageSchedule); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ScheduleID) != nil || input.ExpectedVersion == 0 {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		ScheduleID      string
		ExpectedVersion uint64
	}{input.ScheduleID, input.ExpectedVersion})
	if err != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.ScheduleOccurrence
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		schedule, err := tx.GetForUpdate(
			ctx, input.Principal.OrganizationID, input.Principal.ProjectID, input.ScheduleID,
		)
		if err != nil {
			return err
		}
		spec, ok := schedule.Spec.(entity.ScheduleSpec)
		if !ok || schedule.Kind != enum.KindSchedule || schedule.State != enum.StateActive {
			return errs.ErrStateConflict
		}
		if schedule.Version != input.ExpectedVersion {
			return errs.ErrVersionMismatch
		}
		if err := requireLifecycleOwner(input.Principal, schedule); err != nil {
			return err
		}
		receipt, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID, "run_schedule_now", keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		open, err := tx.HasOpenScheduleOccurrence(ctx, schedule.OrganizationID, schedule.ProjectID, schedule.ID)
		if err != nil {
			return err
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		now = now.UTC().Truncate(time.Microsecond)
		state, outcome := scheduleOccurrenceDisposition(spec, now, now, open)
		result = domainrepo.ScheduleOccurrence{
			ID:         uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:manual-schedule:"+schedule.ID+":"+keyHash)).String(),
			Version:    1,
			ScheduleID: schedule.ID, OrganizationID: schedule.OrganizationID, ProjectID: schedule.ProjectID,
			ScheduledFor: now, TargetResourceID: spec.TargetResourceID, TargetKind: spec.TargetKind,
			TargetVersion: spec.TargetVersion, EffectiveInputSHA256: spec.EffectiveInputSHA,
			PromptProfileID: spec.PromptProfileID, PromptRevision: spec.PromptRevision,
			RuntimeRevisionID: spec.RuntimeRevisionID, SessionPolicy: spec.SessionPolicy,
			RoomID: spec.RoomID, NotificationPolicy: spec.NotificationPolicy,
			MaximumExecution: spec.MaximumExecutionDuration, Coalesce: spec.Coalesce,
			OverlapPolicy: spec.OverlapPolicy, MaximumAttempts: spec.MaximumAttempts,
			InitialBackoff: spec.InitialBackoff, MaximumBackoff: spec.MaximumBackoff,
			DeadLetterAt: now.Add(spec.DeadLetterAfter), State: state, Attempt: 1,
			AvailableAt: now, Outcome: outcome, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.SaveScheduleOccurrence(ctx, result); err != nil {
			return err
		}
		if err := appendScheduleOccurrenceAudit(ctx, tx, input.Principal, "run_schedule_now", result); err != nil {
			return err
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID, Scope: "run_schedule_now", KeyHash: keyHash,
			RequestHash: requestHash, Payload: payload, CreatedAt: now,
		})
	})
	return result, err
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

func scheduledProcessRef(spec entity.ScheduleSpec) string {
	if spec.TargetType == "PLAYBOOK" {
		return spec.PlaybookRef
	}
	return "agent-role:" + spec.TargetResourceID
}

func scheduledProcessRevision(spec entity.ScheduleSpec) uint64 {
	if spec.TargetType == "PLAYBOOK" {
		return spec.PlaybookVersion
	}
	return spec.TargetVersion
}

type scheduleOccurrenceReceipt struct {
	Occurrence domainrepo.ScheduleOccurrence `json:"occurrence"`
}

const (
	materializeScheduleOccurrenceMethod = "/controlplane.v1.ControlPlaneService/MaterializeScheduleOccurrence"
	completeScheduleOccurrenceMethod    = "/controlplane.v1.ControlPlaneService/CompleteScheduleOccurrence"
)

func (service *Service) scheduleCapabilityToken(capability domainrepo.ScheduleOccurrenceCapability) string {
	return service.leaseToken(
		capability.ID+"\x00"+capability.OccurrenceID+"\x00"+capability.ImmutableInputSHA256,
		capability.AuthorityGeneration,
		capability.Attempt,
		capability.AuthorityGeneration,
		capability.WorkloadID,
		capability.FullMethod,
	)
}

func schedulerMaterializationKey(claimKey, occurrenceID string) string {
	return hashString("materialize\x00" + claimKey + "\x00" + occurrenceID + "\x00")
}

func scheduleMaterializationRequestHash(input MaterializeScheduleOccurrenceInput) (string, error) {
	return canonicalHash(struct {
		Identity                       commandIdentity
		OccurrenceID, CapabilitySHA256 string
		ExpectedAttempt                uint32
	}{
		identity(input.Principal), input.OccurrenceID,
		hashString(input.MaterializationCapability), input.ExpectedAttempt,
	})
}

// ClaimScheduleOccurrence резервирует одну due occurrence и выдаёт capability
// только на её materialization. Этот RPC не создаёт исполняемый owner graph.
func (service *Service) ClaimScheduleOccurrence(
	ctx context.Context,
	input ClaimScheduleOccurrenceInput,
) (ScheduleOccurrenceResult, error) {
	return service.claimScheduleOccurrence(ctx, input, service.maximumScheduleClaims)
}

func (service *Service) claimScheduleOccurrence(
	ctx context.Context,
	input ClaimScheduleOccurrenceInput,
	isolationRemaining int,
) (ScheduleOccurrenceResult, error) {
	if err := authorize(input.Principal, permissionClaimSchedule); err != nil {
		return ScheduleOccurrenceResult{}, err
	}
	if input.Principal.CallerWorkload != service.schedulerWorkload ||
		input.Principal.CallerSPIFFEID != service.schedulerSPIFFEID ||
		input.Principal.ProjectID != "" ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return ScheduleOccurrenceResult{}, errs.ErrPermissionDenied
	}
	partitionHash, err := semanticCommandHash(input.Principal, struct{ Operation string }{"CLAIM"})
	if err != nil {
		return ScheduleOccurrenceResult{}, errs.ErrInvalidInput
	}
	projectID, err := service.selectAutomationProject(
		ctx, input.Principal, "CLAIM", input.IdempotencyKey, partitionHash,
	)
	if err != nil {
		return ScheduleOccurrenceResult{}, err
	}
	input.Principal.ProjectID = projectID
	requestHash, err := semanticCommandHash(input.Principal, struct{ Operation string }{"CLAIM"})
	if err != nil {
		return ScheduleOccurrenceResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	scope := domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID, ActorID: input.Principal.ActorID,
	}

	var result ScheduleOccurrenceResult
	replay := func(tx domainrepo.Transaction) error {
		var found bool
		var replayErr error
		result, found, replayErr = service.replayScheduleOccurrenceClaim(
			ctx, tx, input, requestHash, keyHash,
		)
		if replayErr != nil || found {
			return replayErr
		}
		return nil
	}
	if err := service.repository.Transact(ctx, scope, replay); err != nil || result.Disposition != "" {
		return result, err
	}
	if err := service.recoverExpiredScheduleOccurrences(ctx, scope, input.Principal); err != nil {
		return ScheduleOccurrenceResult{}, err
	}
	if err := service.skipOverlappedScheduleOccurrences(ctx, scope, input.Principal); err != nil {
		return ScheduleOccurrenceResult{}, err
	}
	candidateForIsolation := domainrepo.ScheduleOccurrence{}
	err = service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		if err := replay(tx); err != nil || result.Disposition != "" {
			return err
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		occurrence, err := tx.NextScheduleOccurrence(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, now)
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				_, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID,
					"claim_schedule_occurrence", keyHash)
				if receiptErr == nil {
					return errs.ErrStateConflict
				}
				if !errors.Is(receiptErr, errs.ErrNotFound) {
					return receiptErr
				}
			}
			return err
		}
		candidateForIsolation = occurrence
		now, err = tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		schedule, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, occurrence.ScheduleID)
		if err != nil {
			return err
		}
		blocking, err := tx.HasBlockingScheduleExecution(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, schedule.ID, occurrence.ID)
		if err != nil {
			return err
		}
		spec, ok := schedule.Spec.(entity.ScheduleSpec)
		if blocking || !ok || schedule.State != enum.StateActive ||
			validateQueuedScheduledOccurrence(occurrence, schedule) != nil ||
			spec.TargetResourceID != occurrence.TargetResourceID ||
			spec.TargetKind != occurrence.TargetKind || spec.TargetVersion != occurrence.TargetVersion ||
			spec.PromptProfileID != occurrence.PromptProfileID ||
			spec.PromptRevision != occurrence.PromptRevision ||
			spec.RuntimeRevisionID != occurrence.RuntimeRevisionID ||
			spec.SessionPolicy != occurrence.SessionPolicy || spec.RoomID != occurrence.RoomID {
			return errs.ErrStateConflict
		}
		generation := occurrence.AuthorityGeneration + 1
		if generation == 0 {
			return errs.ErrStateConflict
		}
		capability := domainrepo.ScheduleOccurrenceCapability{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(occurrence.ID+"\x00"+
				strconv.FormatUint(uint64(occurrence.Attempt), 10)+"\x00"+
				strconv.FormatUint(generation, 10)+"\x00"+materializeScheduleOccurrenceMethod)).String(),
			OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
			OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
			AuthorityGeneration: generation, ImmutableInputSHA256: occurrence.EffectiveInputSHA256,
			FullMethod: materializeScheduleOccurrenceMethod, WorkloadID: input.Principal.CallerWorkload,
			CallerSPIFFEID: input.Principal.CallerSPIFFEID, State: "ISSUED", IssuedAt: now,
			ExpiresAt: now.Add(2 * time.Minute),
		}
		capability.TokenSHA256 = hashString(service.scheduleCapabilityToken(capability))
		if err := tx.InsertScheduleOccurrenceCapability(ctx, capability); err != nil {
			return err
		}
		expectedAttempt := occurrence.Attempt
		occurrence.State = "RESERVED"
		occurrence.Version++
		occurrence.ClaimantWorkloadID = input.Principal.CallerWorkload
		occurrence.AuthorityGeneration = generation
		occurrence.TokenHash = capability.TokenSHA256
		occurrence.ClaimKeySHA256 = keyHash
		occurrence.LeaseExpiresAt = capability.ExpiresAt
		occurrence.UpdatedAt = now
		if err := tx.UpdateScheduleOccurrence(ctx, occurrence, expectedAttempt, ""); err != nil {
			return err
		}
		if err := appendScheduleOccurrenceAudit(ctx, tx, input.Principal,
			"reserve_schedule_occurrence", occurrence); err != nil {
			return err
		}
		payload, err := json.Marshal(scheduleOccurrenceReceipt{Occurrence: occurrence})
		if err != nil {
			return errs.ErrInternal
		}
		if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID, Scope: "claim_schedule_occurrence",
			KeyHash: keyHash, RequestHash: requestHash, Payload: payload, CreatedAt: now,
		}); err != nil {
			return err
		}
		result = ScheduleOccurrenceResult{
			Occurrence:                    occurrence,
			MaterializationCapability:     service.scheduleCapabilityToken(capability),
			MaterializationIdempotencyKey: schedulerMaterializationKey(input.IdempotencyKey, occurrence.ID),
			CapabilityExpiresAt:           capability.ExpiresAt, ProjectID: projectID,
			Disposition: ScheduleOccurrenceClaimReserved,
		}
		return nil
	})
	if err == nil || candidateForIsolation.ID == "" || isolationRemaining < 1 ||
		(!errors.Is(err, errs.ErrStateConflict) && !errors.Is(err, errs.ErrNotFound) &&
			!errors.Is(err, errs.ErrInvalidInput)) {
		return result, err
	}
	isolationErr := service.deadLetterQueuedScheduleOccurrence(ctx, scope, input.Principal,
		candidateForIsolation, keyHash)
	if isolationErr != nil {
		return ScheduleOccurrenceResult{}, err
	}
	input.Principal.ProjectID = ""
	return service.claimScheduleOccurrence(ctx, input, isolationRemaining-1)
}

// replayScheduleOccurrenceClaim возвращает только доказанный authoritative
// stage того же semantic key. RETIRED не является пустым успехом: receipt,
// owner occurrence и монотонный tuple проверяются до disposition.
func (service *Service) replayScheduleOccurrenceClaim(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ClaimScheduleOccurrenceInput,
	requestHash, keyHash string,
) (ScheduleOccurrenceResult, bool, error) {
	receipt, err := tx.GetReceipt(ctx, input.Principal.OrganizationID,
		"claim_schedule_occurrence", keyHash)
	if errors.Is(err, errs.ErrNotFound) {
		_, boundErr := tx.GetScheduleOccurrenceByClaimKey(ctx,
			input.Principal.OrganizationID, input.Principal.ProjectID, keyHash)
		if errors.Is(boundErr, errs.ErrNotFound) {
			return ScheduleOccurrenceResult{}, false, nil
		}
		if boundErr != nil {
			return ScheduleOccurrenceResult{}, false, boundErr
		}
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	if err != nil {
		return ScheduleOccurrenceResult{}, false, err
	}
	if receipt.RequestHash != requestHash {
		return ScheduleOccurrenceResult{}, false, errs.ErrIdempotencyConflict
	}
	var stored scheduleOccurrenceReceipt
	if json.Unmarshal(receipt.Payload, &stored) != nil ||
		stored.Occurrence.ID == "" || stored.Occurrence.State != "RESERVED" ||
		stored.Occurrence.OrganizationID != input.Principal.OrganizationID ||
		stored.Occurrence.ProjectID != input.Principal.ProjectID ||
		stored.Occurrence.ClaimKeySHA256 != keyHash ||
		stored.Occurrence.AuthorityGeneration == 0 || stored.Occurrence.Attempt == 0 {
		return ScheduleOccurrenceResult{}, false, errs.ErrInternal
	}
	current, err := tx.GetScheduleOccurrenceForUpdate(ctx,
		input.Principal.OrganizationID, input.Principal.ProjectID, stored.Occurrence.ID)
	if errors.Is(err, errs.ErrNotFound) {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	if err != nil {
		return ScheduleOccurrenceResult{}, false, err
	}
	if current.Version < stored.Occurrence.Version || current.Attempt < stored.Occurrence.Attempt {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	retired := ScheduleOccurrenceResult{
		ProjectID: input.Principal.ProjectID, Disposition: ScheduleOccurrenceClaimRetired,
	}
	if current.ClaimKeySHA256 != keyHash || current.Attempt != stored.Occurrence.Attempt ||
		(current.State != "RESERVED" && current.State != "CLAIMED") {
		return retired, true, nil
	}
	capability, err := tx.GetScheduleOccurrenceCapabilityByOccurrenceForUpdate(
		ctx, current.ID, current.Attempt, materializeScheduleOccurrenceMethod,
		stored.Occurrence.AuthorityGeneration,
	)
	if errors.Is(err, errs.ErrNotFound) {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	if err != nil {
		return ScheduleOccurrenceResult{}, false, err
	}
	if capability.OrganizationID != input.Principal.OrganizationID ||
		capability.ProjectID != input.Principal.ProjectID || capability.OccurrenceID != current.ID ||
		capability.Attempt != current.Attempt ||
		capability.AuthorityGeneration != stored.Occurrence.AuthorityGeneration ||
		capability.ImmutableInputSHA256 != stored.Occurrence.EffectiveInputSHA256 ||
		capability.FullMethod != materializeScheduleOccurrenceMethod ||
		capability.WorkloadID != input.Principal.CallerWorkload ||
		capability.CallerSPIFFEID != input.Principal.CallerSPIFFEID {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	token := service.scheduleCapabilityToken(capability)
	if hashString(token) != capability.TokenSHA256 {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	if current.State == "RESERVED" {
		if capability.State != "ISSUED" || current.AuthorityGeneration != capability.AuthorityGeneration ||
			current.TokenHash != capability.TokenSHA256 ||
			current.ClaimantWorkloadID != capability.WorkloadID ||
			!current.LeaseExpiresAt.Equal(capability.ExpiresAt) {
			return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return ScheduleOccurrenceResult{}, false, err
		}
		if !capability.ExpiresAt.After(now) {
			return retired, true, nil
		}
		return ScheduleOccurrenceResult{
			Occurrence:                    current,
			MaterializationCapability:     token,
			MaterializationIdempotencyKey: schedulerMaterializationKey(input.IdempotencyKey, current.ID),
			CapabilityExpiresAt:           capability.ExpiresAt,
			ProjectID:                     input.Principal.ProjectID, Disposition: ScheduleOccurrenceClaimReserved,
		}, true, nil
	}
	if capability.State != "CONSUMED" ||
		current.AuthorityGeneration != capability.AuthorityGeneration {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	materializePrincipal := input.Principal
	materializePrincipal.Permission = permissionUseScheduleCapability
	materializeInput := MaterializeScheduleOccurrenceInput{
		Principal:      materializePrincipal,
		IdempotencyKey: schedulerMaterializationKey(input.IdempotencyKey, current.ID),
		OccurrenceID:   current.ID, ProjectID: current.ProjectID,
		ExpectedAttempt: current.Attempt, MaterializationCapability: token,
	}
	materializeHash, err := scheduleMaterializationRequestHash(materializeInput)
	if err != nil {
		return ScheduleOccurrenceResult{}, false, errs.ErrInternal
	}
	materialized, found, err := service.replayScheduleOccurrenceMaterialization(
		ctx, tx, materializeInput, materializeHash, hashString(materializeInput.IdempotencyKey),
	)
	if errors.Is(err, errs.ErrNotFound) {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	if err != nil {
		return ScheduleOccurrenceResult{}, false, err
	}
	if !found || materialized.Occurrence.ID != current.ID ||
		materialized.Occurrence.Attempt != current.Attempt {
		return ScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return ScheduleOccurrenceResult{}, false, err
	}
	if !current.LeaseExpiresAt.After(now) {
		return retired, true, nil
	}
	return ScheduleOccurrenceResult{
		Occurrence:                    current,
		MaterializationCapability:     token,
		MaterializationIdempotencyKey: materializeInput.IdempotencyKey,
		CapabilityExpiresAt:           capability.ExpiresAt,
		ProjectID:                     input.Principal.ProjectID, Disposition: ScheduleOccurrenceClaimMaterialized,
	}, true, nil
}

// MaterializeScheduleOccurrence атомарно создаёт owner graph только по exact
// одноразовой server-issued capability зарезервированной occurrence.
func (service *Service) MaterializeScheduleOccurrence(
	ctx context.Context,
	input MaterializeScheduleOccurrenceInput,
) (MaterializeScheduleOccurrenceResult, error) {
	if err := authorize(input.Principal, permissionUseScheduleCapability); err != nil {
		return MaterializeScheduleOccurrenceResult{}, err
	}
	if input.Principal.CallerWorkload != service.schedulerWorkload ||
		input.Principal.CallerSPIFFEID != service.schedulerSPIFFEID ||
		input.Principal.ProjectID != "" {
		return MaterializeScheduleOccurrenceResult{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OccurrenceID) != nil || value.ValidateID(input.ProjectID) != nil ||
		input.ExpectedAttempt == 0 || len(input.MaterializationCapability) != 64 {
		return MaterializeScheduleOccurrenceResult{}, errs.ErrInvalidInput
	}
	input.Principal.ProjectID = input.ProjectID
	requestHash, err := scheduleMaterializationRequestHash(input)
	if err != nil {
		return MaterializeScheduleOccurrenceResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	scope := domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}
	var result MaterializeScheduleOccurrenceResult
	err = service.repository.Transact(
		ctx,
		scope,
		func(tx domainrepo.Transaction) error {
			result = MaterializeScheduleOccurrenceResult{}
			replayed, found, replayErr := service.replayScheduleOccurrenceMaterialization(
				ctx, tx, input, requestHash, keyHash,
			)
			if replayErr != nil {
				return replayErr
			}
			if found {
				result = replayed
				return nil
			}
			candidateNow, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			capability, err := tx.GetScheduleOccurrenceCapabilityForUpdate(
				ctx, hashString(input.MaterializationCapability),
			)
			if err != nil {
				return err
			}
			if capability.State != "ISSUED" || !capability.ExpiresAt.After(candidateNow) ||
				capability.OrganizationID != input.Principal.OrganizationID ||
				capability.ProjectID != input.ProjectID || capability.OccurrenceID != input.OccurrenceID ||
				capability.Attempt != input.ExpectedAttempt ||
				capability.FullMethod != materializeScheduleOccurrenceMethod ||
				capability.WorkloadID != input.Principal.CallerWorkload ||
				capability.CallerSPIFFEID != input.Principal.CallerSPIFFEID ||
				capability.TokenSHA256 != hashString(input.MaterializationCapability) {
				return errs.ErrPermissionDenied
			}
			occurrence, err := tx.GetScheduleOccurrenceForUpdate(ctx,
				input.Principal.OrganizationID, input.Principal.ProjectID, input.OccurrenceID)
			if err != nil {
				return err
			}
			if occurrence.State != "RESERVED" || occurrence.Attempt != input.ExpectedAttempt ||
				occurrence.AuthorityGeneration != capability.AuthorityGeneration ||
				occurrence.EffectiveInputSHA256 != capability.ImmutableInputSHA256 ||
				occurrence.TokenHash != capability.TokenSHA256 ||
				occurrence.ClaimantWorkloadID != capability.WorkloadID ||
				!occurrence.LeaseExpiresAt.Equal(capability.ExpiresAt) {
				return errs.ErrStateConflict
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
			blockingExecution, err := tx.HasBlockingScheduleExecution(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				schedule.ID,
				occurrence.ID,
			)
			if err != nil {
				return err
			}
			if blockingExecution {
				return errs.ErrStateConflict
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
			if occurrence.ScheduleID != schedule.ID || occurrence.State != "RESERVED" ||
				occurrenceHasExecutionBinding(occurrence) ||
				occurrence.EffectiveInputSHA256 != scheduleSpec.EffectiveInputSHA {
				return errs.ErrStateConflict
			}
			scheduleSnapshotInputSHA256 := pinnedInputSHA256
			claimNow, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if occurrence.AvailableAt.After(claimNow) || !capability.ExpiresAt.After(claimNow) {
				return errs.ErrStateConflict
			}
			_, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"materialize_schedule_occurrence",
				keyHash,
			)
			switch {
			case receiptErr == nil:
				// Receipt без exact server-owned claim binding не разрешает
				// повтор и отклоняется после authoritative candidate locks.
				return errs.ErrStateConflict
			case errors.Is(receiptErr, errs.ErrNotFound):
			default:
				return receiptErr
			}
			session, sessionSpec, err := service.prepareScheduleSession(
				ctx,
				tx,
				input.Principal,
				schedule,
				scheduleSpec,
				revisionSpec,
				claimNow,
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
				entity.NewScheduledResultContractRef(),
			)
			if err != nil {
				return err
			}
			processID := uuid.NewString()
			var previousProcess entity.Resource
			if occurrence.Attempt > 1 {
				previousRun, err := tx.GetScheduledRunForUpdate(
					ctx, occurrence.ID, occurrence.Attempt-1,
				)
				if err != nil || value.ValidateID(previousRun.ProcessRunID) != nil {
					return errs.ErrStateConflict
				}
				previousProcess, err = tx.GetForUpdate(ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, previousRun.ProcessRunID)
				previousSpec, ok := previousProcess.Spec.(entity.ProcessRunSpec)
				if err != nil || !ok || previousProcess.Kind != enum.KindProcessRun ||
					previousSpec.ScheduleID != schedule.ID || previousSpec.OccurrenceID != occurrence.ID ||
					!previousProcess.State.Terminal() {
					return errs.ErrStateConflict
				}
				processID = previousProcess.ID
			}
			sessionSpec.LastTurnSequence++
			updatedSession, err := session.Update(
				session.Name,
				sessionSpec,
				claimNow,
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
					SessionID:               session.ID,
					Sequence:                sessionSpec.LastTurnSequence,
					SourceRef:               sourceRef,
					PromptArtifactID:        scheduleSpec.PromptArtifactID,
					RuntimeRevisionID:       runtimeRevision.ID,
					ProcessRunID:            processID,
					Attempt:                 occurrence.Attempt,
					ScheduledResultContract: entity.NewScheduledResultContractRef(),
					EffectiveInputSHA256: hashRuntimeInput(
						sourceRef,
						promptArtifact.SHA256,
						runtimeSpec.ManifestSHA256,
						processID,
					),
				},
				claimNow,
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
				AuthorityGeneration: capability.AuthorityGeneration,
				State:               "QUEUED",
				InputSHA256:         turnSpec.EffectiveInputSHA256,
				LeaseFence:          turn.Version,
				StartedAt:           claimNow,
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
				process := previousProcess
				if process.ID == "" {
					process, err = entity.New(
						processID,
						input.Principal.OrganizationID,
						input.Principal.ProjectID,
						schedule.ID,
						schedule.OwnerActorID,
						enum.KindProcessRun,
						"Scheduled process "+occurrence.ID,
						entity.ProcessRunSpec{
							PlaybookRef:                   scheduledProcessRef(scheduleSpec),
							PolicyRevision:                scheduledProcessRevision(scheduleSpec),
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
							CurrentAttempt:                occurrence.Attempt,
							CurrentRuntimeRevisionID:      runtimeRevision.ID,
							CurrentRuntimeRevisionVersion: runtimeRevision.Version,
							CurrentInputSHA256:            turnSpec.EffectiveInputSHA256,
						},
						claimNow,
					)
					if err != nil {
						return errs.ErrInternal
					}
					if err := tx.Insert(ctx, process); err != nil {
						return err
					}
				} else {
					processSpec := process.Spec.(entity.ProcessRunSpec)
					setCurrentExecution(&processSpec, executionTuple{
						SessionID: session.ID, SessionVersion: updatedSession.Version,
						TurnID: turn.ID, TurnVersion: turn.Version, Attempt: turnSpec.Attempt,
						RuntimeRevisionID:      runtimeRevision.ID,
						RuntimeRevisionVersion: runtimeRevision.Version,
						InputSHA256:            turnSpec.EffectiveInputSHA256,
					})
					processSpec.Outcome, processSpec.ResultArtifactID = "", ""
					process, err = process.ReplaceAndTransition(processSpec, enum.StateRunning, claimNow)
					if err != nil {
						return errs.ErrStateConflict
					}
					if err := tx.Update(ctx, process, previousProcess.Version); err != nil {
						return err
					}
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
			completionCapability := domainrepo.ScheduleOccurrenceCapability{
				ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(occurrence.ID+"\x00"+
					strconv.FormatUint(uint64(occurrence.Attempt), 10)+"\x00"+
					strconv.FormatUint(capability.AuthorityGeneration, 10)+"\x00"+
					completeScheduleOccurrenceMethod)).String(),
				OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
				OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
				AuthorityGeneration:  capability.AuthorityGeneration,
				ImmutableInputSHA256: turnSpec.EffectiveInputSHA256,
				FullMethod:           completeScheduleOccurrenceMethod,
				WorkloadID:           input.Principal.CallerWorkload,
				CallerSPIFFEID:       input.Principal.CallerSPIFFEID,
				State:                "ISSUED", IssuedAt: claimNow,
				ExpiresAt: claimNow.Add(occurrence.MaximumExecution),
			}
			token := service.scheduleCapabilityToken(completionCapability)
			completionCapability.TokenSHA256 = hashString(token)
			if err := tx.InsertScheduleOccurrenceCapability(ctx, completionCapability); err != nil {
				return err
			}
			expectedAttempt := occurrence.Attempt
			if err := materializeScheduledOccurrence(
				&occurrence,
				scheduleSnapshotInputSHA256,
				scheduledOccurrenceExecutionBinding{
					SessionID: updatedSession.ID, SessionVersion: updatedSession.Version,
					TurnID: turn.ID, TurnVersion: turn.Version,
					ProcessRunID: scheduledProcess.ID, ProcessVersion: scheduledProcess.Version,
					RuntimeRevisionID:      runtimeRevision.ID,
					RuntimeRevisionVersion: runtimeRevision.Version,
					InputSHA256:            turnSpec.EffectiveInputSHA256,
				},
				scheduledOccurrenceClaimBinding{
					WorkloadID:          input.Principal.CallerWorkload,
					AuthorityGeneration: capability.AuthorityGeneration,
					TokenSHA256:         completionCapability.TokenSHA256,
					ClaimKeySHA256:      occurrence.ClaimKeySHA256,
					LeaseExpiresAt:      completionCapability.ExpiresAt,
				},
				claimNow,
			); err != nil {
				return err
			}
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				expectedAttempt,
				"",
			); err != nil {
				return err
			}
			capability.State = "CONSUMED"
			capability.ConsumedAt = claimNow
			if err := tx.UpdateScheduleOccurrenceCapability(ctx, capability, "ISSUED"); err != nil {
				return err
			}
			if err := tx.SaveScheduledRun(ctx, domainrepo.ScheduledRun{
				OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
				SessionID: updatedSession.ID, SessionVersion: updatedSession.Version,
				TurnID: turn.ID, TurnVersion: turn.Version,
				ProcessRunID: scheduledProcess.ID, ProcessVersion: scheduledProcess.Version,
				RuntimeRevisionID:             runtimeRevision.ID,
				RuntimeRevisionVersion:        runtimeRevision.Version,
				EffectiveInputSHA256:          scheduleSnapshotInputSHA256,
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
				State:                         "CLAIMED", CreatedAt: claimNow,
			}); err != nil {
				return err
			}
			if err := appendScheduleOccurrenceAudit(
				ctx, tx, input.Principal, "materialize_schedule_occurrence", occurrence,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(scheduleOccurrenceReceipt{Occurrence: occurrence})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "materialize_schedule_occurrence",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      claimNow,
			}); err != nil {
				return err
			}
			result = MaterializeScheduleOccurrenceResult{
				Occurrence: occurrence, CompletionCapability: token,
			}
			return nil
		},
	)
	return result, err
}

func (service *Service) replayScheduleOccurrenceMaterialization(
	ctx context.Context,
	tx domainrepo.Transaction,
	input MaterializeScheduleOccurrenceInput,
	requestHash, keyHash string,
) (MaterializeScheduleOccurrenceResult, bool, error) {
	capability, err := tx.GetScheduleOccurrenceCapabilityForUpdate(
		ctx, hashString(input.MaterializationCapability),
	)
	if errors.Is(err, errs.ErrNotFound) {
		return MaterializeScheduleOccurrenceResult{}, false, nil
	}
	if err != nil {
		return MaterializeScheduleOccurrenceResult{}, false, err
	}
	if capability.OrganizationID != input.Principal.OrganizationID ||
		capability.ProjectID != input.ProjectID || capability.OccurrenceID != input.OccurrenceID ||
		capability.Attempt != input.ExpectedAttempt ||
		capability.FullMethod != materializeScheduleOccurrenceMethod ||
		capability.WorkloadID != input.Principal.CallerWorkload ||
		capability.CallerSPIFFEID != input.Principal.CallerSPIFFEID ||
		capability.TokenSHA256 != hashString(input.MaterializationCapability) {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrPermissionDenied
	}
	if capability.State == "ISSUED" {
		return MaterializeScheduleOccurrenceResult{}, false, nil
	}
	if capability.State != "CONSUMED" {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	receipt, err := tx.GetReceipt(ctx, input.Principal.OrganizationID,
		"materialize_schedule_occurrence", keyHash)
	if err != nil {
		return MaterializeScheduleOccurrenceResult{}, false, err
	}
	if receipt.RequestHash != requestHash {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrIdempotencyConflict
	}
	current, err := tx.GetScheduleOccurrenceForUpdate(ctx, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.OccurrenceID)
	if err != nil {
		return MaterializeScheduleOccurrenceResult{}, false, err
	}
	if current.State != "CLAIMED" || current.Attempt != input.ExpectedAttempt ||
		current.AuthorityGeneration != capability.AuthorityGeneration {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	var payload scheduleOccurrenceReceipt
	if json.Unmarshal(receipt.Payload, &payload) != nil ||
		payload.Occurrence.ID != current.ID || payload.Occurrence.Attempt != current.Attempt {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrInternal
	}
	completionCapability, err := tx.GetScheduleOccurrenceCapabilityByOccurrenceForUpdate(
		ctx, current.ID, current.Attempt, completeScheduleOccurrenceMethod,
		current.AuthorityGeneration,
	)
	if err != nil || completionCapability.State != "ISSUED" {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	expectedToken := service.scheduleCapabilityToken(completionCapability)
	if current.TokenHash != hashString(expectedToken) {
		return MaterializeScheduleOccurrenceResult{}, false, errs.ErrStateConflict
	}
	return MaterializeScheduleOccurrenceResult{
		Occurrence: current, CompletionCapability: expectedToken,
	}, true, nil
}

type automationProjectReceipt struct {
	ProjectID string `json:"projectId"`
}

func (service *Service) selectAutomationProject(
	ctx context.Context,
	principal value.Principal,
	operation, idempotencyKey, requestHash string,
) (string, error) {
	if principal.ProjectID != "" || (operation != "DUE" && operation != "CLAIM") {
		return "", errs.ErrPermissionDenied
	}
	keyHash := hashString(idempotencyKey)
	scopeName := "automation_scheduler_partition_" + strings.ToLower(operation)
	var result automationProjectReceipt
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID, ActorID: principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, principal.OrganizationID, scopeName, keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil ||
				value.ValidateID(result.ProjectID) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		projectID, err := tx.NextAutomationProject(ctx, principal.OrganizationID, operation)
		if err != nil {
			return err
		}
		result.ProjectID = projectID
		payload, err := json.Marshal(result)
		if err != nil {
			return errs.ErrInternal
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: principal.OrganizationID,
			Scope:          scopeName, KeyHash: keyHash, RequestHash: requestHash,
			Payload: payload, CreatedAt: now,
		})
	})
	if err != nil {
		return "", err
	}
	return result.ProjectID, nil
}

// recoverExpiredScheduleOccurrences фиксирует каждую независимую watchdog
// disposition до выбора следующего claimable occurrence. Поэтому штатный
// ErrNotFound следующего poll не способен откатить уже закрытую authority.
func (service *Service) recoverExpiredScheduleOccurrences(
	ctx context.Context,
	scope domainrepo.Scope,
	principal value.Principal,
) error {
	var candidates []domainrepo.ScheduleOccurrence
	if err := service.repository.Transact(
		ctx,
		scope,
		func(tx domainrepo.Transaction) error {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			candidates, err = tx.ExpiredScheduleOccurrenceCandidates(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				now,
			)
			return err
		},
	); err != nil {
		return err
	}
	for _, candidate := range candidates {
		candidate := candidate
		if candidate.State == "RESERVED" {
			if err := service.releaseExpiredScheduleReservation(ctx, scope, principal, candidate); err != nil &&
				!errors.Is(err, errs.ErrStateConflict) && !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			continue
		}
		if candidate.ExecutionTurnID == "" {
			repaired, repairErr := service.repairBrokenScheduleRecovery(
				ctx, scope, principal, candidate,
			)
			if repairErr != nil && !errors.Is(repairErr, errs.ErrStateConflict) &&
				!errors.Is(repairErr, errs.ErrNotFound) {
				return repairErr
			}
			if repaired {
				continue
			}
			if err := service.recordBlockedScheduleRecovery(
				ctx, scope, principal, candidate,
			); err != nil && !errors.Is(err, errs.ErrStateConflict) &&
				!errors.Is(err, errs.ErrNotFound) {
				return err
			}
			continue
		}
		maintenanceEffect := ""
		if err := service.repository.Transact(
			ctx,
			scope,
			func(tx domainrepo.Transaction) error {
				graph, err := service.lockOwnerGraphByTurn(
					ctx, tx, principal, candidate.ExecutionTurnID,
				)
				if err != nil {
					return err
				}
				if graph.Occurrence.ID != candidate.ID {
					return errs.ErrStateConflict
				}
				now, err := tx.CurrentTime(ctx)
				if err != nil {
					return err
				}
				if err := service.recoverExpiredScheduleOccurrence(
					ctx, tx, principal, graph, now,
				); err != nil {
					return err
				}
				recovered, err := tx.GetScheduleOccurrenceForUpdate(
					ctx,
					principal.OrganizationID,
					principal.ProjectID,
					candidate.ID,
				)
				if err != nil {
					return err
				}
				switch recovered.State {
				case "QUEUED":
					maintenanceEffect = "requeue"
				case "DEAD_LETTER":
					maintenanceEffect = "dead_letter"
				}
				return nil
			},
		); err != nil {
			if !errors.Is(err, errs.ErrStateConflict) &&
				!errors.Is(err, errs.ErrNotFound) &&
				!errors.Is(err, errs.ErrInvalidInput) &&
				!errors.Is(err, errs.ErrVersionMismatch) {
				return err
			}
			if incidentErr := service.recordBlockedScheduleRecovery(
				ctx, scope, principal, candidate,
			); incidentErr != nil &&
				!errors.Is(incidentErr, errs.ErrStateConflict) &&
				!errors.Is(incidentErr, errs.ErrNotFound) {
				return incidentErr
			}
		} else if maintenanceEffect != "" {
			service.observer.ObserveScheduleMaintenance(maintenanceEffect)
		}
	}
	return nil
}

func (service *Service) releaseExpiredScheduleReservation(
	ctx context.Context, scope domainrepo.Scope, principal value.Principal,
	candidate domainrepo.ScheduleOccurrence,
) error {
	committed := false
	err := service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		occurrence, err := tx.GetScheduleOccurrenceForUpdate(ctx, principal.OrganizationID,
			principal.ProjectID, candidate.ID)
		if err != nil {
			return err
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		if occurrence.State != "RESERVED" || occurrence.Attempt != candidate.Attempt ||
			occurrenceHasExecutionBinding(occurrence) || occurrence.LeaseExpiresAt.After(now) {
			return errs.ErrStateConflict
		}
		expectedToken := occurrence.TokenHash
		if err := service.revokeIssuedScheduleCapability(ctx, tx, expectedToken, now); err != nil {
			return err
		}
		occurrence.State = "QUEUED"
		occurrence.Version++
		occurrence.ClaimantWorkloadID, occurrence.TokenHash, occurrence.ClaimKeySHA256 = "", "", ""
		// Поколение является durable high-watermark этой attempt. Сброс здесь
		// повторно выдал бы тот же deterministic capability JTI после expiry.
		occurrence.LeaseExpiresAt = time.Time{}
		occurrence.AvailableAt = now.UTC().Truncate(time.Microsecond)
		occurrence.UpdatedAt = occurrence.AvailableAt
		if err := tx.UpdateScheduleOccurrence(ctx, occurrence, candidate.Attempt, expectedToken); err != nil {
			return err
		}
		if err := appendScheduleOccurrenceAudit(ctx, tx, principal,
			"release_expired_schedule_reservation", occurrence); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err == nil && committed {
		service.observer.ObserveScheduleMaintenance("reservation_expired")
	}
	return err
}

// repairBrokenScheduleRecovery восстанавливает binding только из coherent
// ScheduledRun либо dead-letter-ит доказанно не материализованную строку.
func (service *Service) repairBrokenScheduleRecovery(
	ctx context.Context,
	scope domainrepo.Scope,
	principal value.Principal,
	candidate domainrepo.ScheduleOccurrence,
) (bool, error) {
	repaired := false
	err := service.repository.Transact(ctx, scope, func(tx domainrepo.Transaction) error {
		occurrence, err := tx.GetScheduleOccurrenceForUpdate(ctx, principal.OrganizationID,
			principal.ProjectID, candidate.ID)
		if err != nil {
			return err
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		if occurrence.State != "CLAIMED" || occurrence.Attempt != candidate.Attempt ||
			occurrence.ExecutionTurnID != "" || occurrence.LeaseExpiresAt.After(now) {
			return errs.ErrStateConflict
		}
		run, runErr := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
		if errors.Is(runErr, errs.ErrNotFound) {
			blocking, err := tx.HasBlockingScheduleExecution(ctx, principal.OrganizationID,
				principal.ProjectID, occurrence.ScheduleID, occurrence.ID)
			if err != nil {
				return err
			}
			if blocking {
				return errs.ErrStateConflict
			}
			expectedToken := occurrence.TokenHash
			occurrence.State, occurrence.Outcome = "DEAD_LETTER", "recovery_graph_absent"
			occurrence.ResultArtifactID = ""
			occurrence.ClaimantWorkloadID, occurrence.TokenHash, occurrence.ClaimKeySHA256 = "", "", ""
			occurrence.AuthorityGeneration = 0
			occurrence.LeaseExpiresAt = time.Time{}
			clearScheduledExecutionBinding(&occurrence)
			occurrence.UpdatedAt = now.UTC().Truncate(time.Microsecond)
			if err := tx.UpdateScheduleOccurrence(ctx, occurrence, candidate.Attempt, expectedToken); err != nil {
				return err
			}
			if err := appendScheduleOccurrenceAudit(ctx, tx, principal,
				"dead_letter_absent_schedule_graph", occurrence); err != nil {
				return err
			}
			repaired = true
			return nil
		}
		if runErr != nil {
			return runErr
		}
		binding := scheduledOccurrenceExecutionBinding{
			SessionID: run.CurrentSessionID, SessionVersion: run.CurrentSessionVersion,
			TurnID: run.CurrentTurnID, TurnVersion: run.CurrentTurnVersion,
			ProcessRunID: run.CurrentProcessRunID, ProcessVersion: run.CurrentProcessVersion,
			RuntimeRevisionID:      run.CurrentRuntimeRevisionID,
			RuntimeRevisionVersion: run.CurrentRuntimeRevisionVersion,
			InputSHA256:            run.CurrentInputSHA256,
		}
		if run.State != "CLAIMED" || !validScheduledExecutionBinding(binding) ||
			run.OccurrenceID != occurrence.ID || run.Attempt != occurrence.Attempt {
			return errs.ErrStateConflict
		}
		setScheduledExecutionBinding(&occurrence, binding)
		occurrence.UpdatedAt = now.UTC().Truncate(time.Microsecond)
		if err := tx.UpdateScheduleOccurrence(ctx, occurrence, candidate.Attempt, candidate.TokenHash); err != nil {
			return err
		}
		if err := appendScheduleOccurrenceAudit(ctx, tx, principal,
			"repair_schedule_occurrence_binding", occurrence); err != nil {
			return err
		}
		repaired = true
		return nil
	})
	if err == nil && repaired {
		service.observer.ObserveScheduleMaintenance("repair")
	}
	return repaired, err
}

type blockedScheduleRecoveryReceipt struct {
	OccurrenceID string `json:"occurrenceId"`
	Attempt      uint32 `json:"attempt"`
}

// recordBlockedScheduleRecovery сохраняет один audit для повреждённого
// expired graph, не пытаясь частично терминализировать возможно исполняемую
// Session/Turn/ProcessRun связку. Receipt делает повторные watchdog polls
// идемпотентными, а остальные schedule rows продолжают обрабатываться.
func (service *Service) recordBlockedScheduleRecovery(
	ctx context.Context,
	scope domainrepo.Scope,
	principal value.Principal,
	candidate domainrepo.ScheduleOccurrence,
) error {
	keyHash := hashString("schedule_recovery_blocked\x00" + candidate.ID + "\x00" +
		strconv.FormatUint(uint64(candidate.Attempt), 10))
	committed := false
	err := service.repository.Transact(
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
			if occurrence.State != "CLAIMED" || occurrence.Attempt != candidate.Attempt {
				return errs.ErrStateConflict
			}
			if _, err := tx.GetForUpdate(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				occurrence.ScheduleID,
			); err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if occurrence.LeaseExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			var run *domainrepo.ScheduledRun
			lockedRun, runErr := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
			if runErr == nil {
				run = &lockedRun
			} else if !errors.Is(runErr, errs.ErrNotFound) {
				return runErr
			}
			semanticHash, err := canonicalHash(struct {
				OccurrenceID, ScheduleID string
				Version                  uint64
				Attempt                  uint32
				Binding                  scheduledOccurrenceExecutionBinding
				Run                      *domainrepo.ScheduledRun
			}{
				occurrence.ID, occurrence.ScheduleID, occurrence.Version, occurrence.Attempt,
				scheduledExecutionBinding(occurrence), run,
			})
			if err != nil {
				return errs.ErrInternal
			}
			_, receiptErr := tx.GetReceipt(
				ctx,
				principal.OrganizationID,
				"schedule_recovery_blocked",
				keyHash,
			)
			switch {
			case receiptErr == nil:
				return nil
			case errors.Is(receiptErr, errs.ErrNotFound):
			default:
				return receiptErr
			}
			expectedToken := occurrence.TokenHash
			if err := service.revokeIssuedScheduleCapability(ctx, tx, expectedToken, now); err != nil {
				return err
			}
			occurrence.State, occurrence.Outcome = "RECOVERY_BLOCKED", "invalid_owner_graph"
			occurrence.RecoveryEvidenceSHA256 = semanticHash
			occurrence.RecoveryBlockedAt = now.UTC().Truncate(time.Microsecond)
			occurrence.Version++
			occurrence.ClaimantWorkloadID, occurrence.TokenHash, occurrence.ClaimKeySHA256 = "", "", ""
			occurrence.AuthorityGeneration = 0
			occurrence.LeaseExpiresAt = time.Time{}
			occurrence.UpdatedAt = now.UTC().Truncate(time.Microsecond)
			if err := tx.UpdateScheduleOccurrence(ctx, occurrence, candidate.Attempt, expectedToken); err != nil {
				return err
			}
			if err := appendScheduleOccurrenceAudit(
				ctx,
				tx,
				principal,
				"block_invalid_schedule_occurrence_recovery",
				occurrence,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(blockedScheduleRecoveryReceipt{
				OccurrenceID: occurrence.ID,
				Attempt:      occurrence.Attempt,
			})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: principal.OrganizationID,
				ProjectID:      principal.ProjectID,
				Scope:          "schedule_recovery_blocked",
				KeyHash:        keyHash,
				RequestHash:    semanticHash,
				Payload:        payload,
				CreatedAt:      now,
			}); err != nil {
				return err
			}
			committed = true
			return nil
		},
	)
	if err == nil && committed {
		service.observer.ObserveScheduleMaintenance("blocked_recovery")
	}
	return err
}

// skipOverlappedScheduleOccurrences коммитит terminal SKIPPED и его audit как
// самостоятельный owner fact до следующей попытки выбора scheduler work.
func (service *Service) skipOverlappedScheduleOccurrences(
	ctx context.Context,
	scope domainrepo.Scope,
	principal value.Principal,
) error {
	skippedCount := 0
	err := service.repository.Transact(
		ctx,
		scope,
		func(tx domainrepo.Transaction) error {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			skipped, err := tx.SkipOverlappedScheduleOccurrences(
				ctx,
				principal.OrganizationID,
				principal.ProjectID,
				now,
				16,
			)
			if err != nil {
				return err
			}
			for _, occurrence := range skipped {
				if err := appendScheduleOccurrenceAudit(
					ctx, tx, principal, "skip_schedule_occurrence", occurrence,
				); err != nil {
					return err
				}
				skippedCount++
			}
			return nil
		},
	)
	if err == nil {
		for range skippedCount {
			service.observer.ObserveScheduleMaintenance("overlap_skip")
		}
	}
	return err
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
	if err := authorize(input.Principal, permissionUseScheduleCapability); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	if input.Principal.CallerWorkload != service.schedulerWorkload ||
		input.Principal.CallerSPIFFEID != service.schedulerSPIFFEID ||
		input.Principal.ProjectID != "" {
		return domainrepo.ScheduleOccurrence{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OccurrenceID) != nil ||
		value.ValidateID(input.ProjectID) != nil ||
		len(input.CompletionCapability) != 64 ||
		input.ExpectedAttempt == 0 ||
		input.TerminalState != "" || input.Outcome != "" ||
		input.ResultArtifactID != "" {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	// ProjectID используется только как locator. Доступ к эффекту даёт exact
	// lease, чей hash и occurrence tuple проверяются после RLS lookup.
	input.Principal.ProjectID = input.ProjectID
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		OccurrenceID    string
		TokenHash       string
		ExpectedAttempt uint32
	}{
		identity(input.Principal),
		input.OccurrenceID,
		hashString(input.CompletionCapability),
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
			capability, err := tx.GetScheduleOccurrenceCapabilityForUpdate(
				ctx, hashString(input.CompletionCapability),
			)
			if err != nil {
				return err
			}
			if capability.OrganizationID != input.Principal.OrganizationID ||
				capability.ProjectID != input.ProjectID ||
				capability.OccurrenceID != input.OccurrenceID ||
				capability.Attempt != input.ExpectedAttempt ||
				capability.FullMethod != completeScheduleOccurrenceMethod ||
				capability.WorkloadID != input.Principal.CallerWorkload ||
				capability.CallerSPIFFEID != input.Principal.CallerSPIFFEID ||
				capability.TokenSHA256 != hashString(input.CompletionCapability) ||
				(capability.State != "ISSUED" && capability.State != "CONSUMED") {
				return errs.ErrPermissionDenied
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
			if candidate.State == "QUEUED" {
				if capability.State != "CONSUMED" {
					return errs.ErrStateConflict
				}
				occurrence, lockErr := tx.GetScheduleOccurrenceForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					candidate.ID,
				)
				if lockErr != nil {
					return lockErr
				}
				schedule, scheduleErr := tx.GetForUpdate(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					occurrence.ScheduleID,
				)
				if scheduleErr != nil {
					return scheduleErr
				}
				replayed, replayErr := replayRequeuedScheduleCompletion(
					ctx, tx, input.Principal.OrganizationID, occurrence, schedule,
					input.ExpectedAttempt, keyHash, requestHash,
				)
				if replayErr != nil {
					return replayErr
				}
				result = replayed
				return nil
			}
			if candidate.ExecutionTurnID == "" || candidate.State == "SKIPPED" {
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
			if err := requireClosedRuntimeConsistentWithTurn(graph); err != nil {
				return err
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx, input.Principal.OrganizationID,
				"complete_schedule_occurrence", keyHash,
			)
			if receiptErr == nil {
				if capability.State != "CONSUMED" {
					return errs.ErrStateConflict
				}
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload scheduleOccurrenceReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil ||
					payload.Occurrence.ID == "" {
					return errs.ErrInternal
				}
				if payload.Occurrence != occurrence {
					return errs.ErrStateConflict
				}
				result = occurrence
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			decisionNow, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if occurrence.State != "CLAIMED" ||
				occurrence.Attempt != input.ExpectedAttempt ||
				occurrence.TokenHash != hashString(input.CompletionCapability) ||
				occurrence.ClaimantWorkloadID != input.Principal.CallerWorkload ||
				occurrence.AuthorityGeneration != capability.AuthorityGeneration ||
				capability.ImmutableInputSHA256 != graph.Run.CurrentInputSHA256 ||
				capability.State != "ISSUED" || !capability.ExpiresAt.After(decisionNow) ||
				!occurrence.LeaseExpiresAt.Equal(capability.ExpiresAt) {
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
			if _, ok := schedule.Spec.(entity.ScheduleSpec); !ok ||
				schedule.Kind != enum.KindSchedule {
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
			now := decisionNow.UTC().Truncate(time.Microsecond)
			capability.State = "CONSUMED"
			capability.ConsumedAt = decisionNow
			if err := tx.UpdateScheduleOccurrenceCapability(ctx, capability, "ISSUED"); err != nil {
				return err
			}
			occurrence, err = service.applyScheduledTerminalDisposition(
				ctx, tx, input.Principal, schedule, occurrence, run,
				terminalState, outcome, resultArtifactID, now,
				"complete_schedule_occurrence",
			)
			if err != nil {
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

func replayRequeuedScheduleCompletion(
	ctx context.Context,
	tx domainrepo.Transaction,
	organizationID string,
	occurrence domainrepo.ScheduleOccurrence,
	schedule entity.Resource,
	expectedAttempt uint32,
	keyHash string,
	requestHash string,
) (domainrepo.ScheduleOccurrence, error) {
	if occurrence.Attempt != expectedAttempt+1 ||
		validateQueuedScheduledOccurrence(occurrence, schedule) != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrStateConflict
	}
	receipt, err := tx.GetReceipt(
		ctx, organizationID, "complete_schedule_occurrence", keyHash,
	)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return domainrepo.ScheduleOccurrence{}, errs.ErrStateConflict
		}
		return domainrepo.ScheduleOccurrence{}, err
	}
	if receipt.RequestHash != requestHash {
		return domainrepo.ScheduleOccurrence{}, errs.ErrIdempotencyConflict
	}
	var payload scheduleOccurrenceReceipt
	if json.Unmarshal(receipt.Payload, &payload) != nil || payload.Occurrence != occurrence {
		return domainrepo.ScheduleOccurrence{}, errs.ErrStateConflict
	}
	return occurrence, nil
}

// ResolveScheduleRecovery — отдельный owner-only путь для bounded incident
// state. Он либо восстанавливает точный binding из неизменяемого ScheduledRun,
// либо закрывает доказанно отсутствующий graph; потенциально живой graph не
// терминализируется частично.
func (service *Service) ResolveScheduleRecovery(
	ctx context.Context,
	input ResolveScheduleRecoveryInput,
) (domainrepo.ScheduleOccurrence, error) {
	if err := authorize(input.Principal, permissionRecoverSchedule); err != nil {
		return domainrepo.ScheduleOccurrence{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ScheduleID) != nil || value.ValidateID(input.OccurrenceID) != nil || input.ExpectedVersion == 0 ||
		input.ExpectedAttempt == 0 || !validSHA256Text(input.EvidenceSHA256) ||
		(input.Action != "REPAIR" && input.Action != "CANCEL" && input.Action != "SKIP") ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                 commandIdentity
		ScheduleID, OccurrenceID string
		Version                  uint64
		Attempt                  uint32
		Action, Evidence, Reason string
	}{
		identity(input.Principal), input.ScheduleID, input.OccurrenceID, input.ExpectedVersion,
		input.ExpectedAttempt, input.Action, input.EvidenceSHA256, input.ReasonCode,
	})
	if err != nil {
		return domainrepo.ScheduleOccurrence{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.ScheduleOccurrence
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		occurrence, err := tx.GetScheduleOccurrenceForUpdate(ctx,
			input.Principal.OrganizationID, input.Principal.ProjectID, input.OccurrenceID)
		if err != nil {
			return err
		}
		receipt, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID,
			"resolve_schedule_recovery", keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash {
				return errs.ErrIdempotencyConflict
			}
			var payload scheduleOccurrenceReceipt
			if json.Unmarshal(receipt.Payload, &payload) != nil || payload.Occurrence != occurrence {
				return errs.ErrStateConflict
			}
			result = occurrence
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		schedule, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, occurrence.ScheduleID)
		if err != nil {
			return err
		}
		if requireLifecycleOwner(input.Principal, schedule) != nil ||
			occurrence.ScheduleID != input.ScheduleID ||
			occurrence.State != "RECOVERY_BLOCKED" ||
			occurrence.Version != input.ExpectedVersion || occurrence.Attempt != input.ExpectedAttempt ||
			occurrence.RecoveryEvidenceSHA256 != input.EvidenceSHA256 {
			return errs.ErrStateConflict
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		run, runErr := tx.GetScheduledRunForUpdate(ctx, occurrence.ID, occurrence.Attempt)
		switch input.Action {
		case "REPAIR":
			if runErr != nil || run.State != "CLAIMED" {
				return errs.ErrStateConflict
			}
			binding := scheduledOccurrenceExecutionBinding{
				SessionID: run.CurrentSessionID, SessionVersion: run.CurrentSessionVersion,
				TurnID: run.CurrentTurnID, TurnVersion: run.CurrentTurnVersion,
				ProcessRunID: run.CurrentProcessRunID, ProcessVersion: run.CurrentProcessVersion,
				RuntimeRevisionID:      run.CurrentRuntimeRevisionID,
				RuntimeRevisionVersion: run.CurrentRuntimeRevisionVersion,
				InputSHA256:            run.CurrentInputSHA256,
			}
			if !validScheduledExecutionBinding(binding) {
				return errs.ErrStateConflict
			}
			session, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, binding.SessionID)
			if err != nil || session.Kind != enum.KindSession || session.Version != binding.SessionVersion ||
				session.OwnerActorID != schedule.OwnerActorID {
				return errs.ErrStateConflict
			}
			turn, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, binding.TurnID)
			turnSpec, ok := turn.Spec.(entity.TurnSpec)
			if err != nil || !ok || turn.Kind != enum.KindTurn || turn.Version != binding.TurnVersion ||
				turnSpec.SessionID != binding.SessionID || turnSpec.Attempt != run.CurrentTurnAttempt ||
				turnSpec.RuntimeRevisionID != binding.RuntimeRevisionID ||
				turnSpec.EffectiveInputSHA256 != binding.InputSHA256 {
				return errs.ErrStateConflict
			}
			revision, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, binding.RuntimeRevisionID)
			if err != nil || revision.Kind != enum.KindRuntimeRevision ||
				revision.Version != binding.RuntimeRevisionVersion {
				return errs.ErrStateConflict
			}
			if binding.ProcessRunID != "" {
				process, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, binding.ProcessRunID)
				if err != nil || process.Kind != enum.KindProcessRun ||
					process.Version != binding.ProcessVersion || process.OwnerActorID != schedule.OwnerActorID {
					return errs.ErrStateConflict
				}
			}
			// REPAIR возвращает строку в watchdog lifecycle, а не выдаёт
			// scheduler новый секрет через owner HTTP path. Поэтому owner
			// фиксирует краткоживущую exact completion capability только как
			// durable fence; следующий bounded poll истечёт её и завершит уже
			// повторно разрешённый canonical graph.
			generation := occurrence.Version + 1
			expiresAt := now.Add(time.Millisecond)
			capability := domainrepo.ScheduleOccurrenceCapability{
				ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(occurrence.ID+"\x00"+
					strconv.FormatUint(uint64(occurrence.Attempt), 10)+"\x00"+
					strconv.FormatUint(generation, 10)+"\x00"+completeScheduleOccurrenceMethod)).String(),
				OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
				OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
				AuthorityGeneration: generation, ImmutableInputSHA256: binding.InputSHA256,
				FullMethod: completeScheduleOccurrenceMethod, WorkloadID: service.schedulerWorkload,
				CallerSPIFFEID: service.schedulerSPIFFEID, State: "ISSUED", IssuedAt: now,
				ExpiresAt: expiresAt,
			}
			capability.TokenSHA256 = hashString(service.scheduleCapabilityToken(capability))
			if err := tx.InsertScheduleOccurrenceCapability(ctx, capability); err != nil {
				return err
			}
			occurrence.State = "CLAIMED"
			setScheduledExecutionBinding(&occurrence, binding)
			occurrence.ClaimantWorkloadID = service.schedulerWorkload
			occurrence.AuthorityGeneration = generation
			occurrence.TokenHash = capability.TokenSHA256
			occurrence.ClaimKeySHA256 = hashString(input.IdempotencyKey + "\x00watchdog")
			occurrence.LeaseExpiresAt = expiresAt
		case "CANCEL", "SKIP":
			if runErr == nil {
				if err := service.cancelBlockedScheduleRecoveryGraph(
					ctx, tx, input.Principal, schedule, occurrence, run, input.ReasonCode, now,
				); err != nil {
					return err
				}
			} else {
				if !errors.Is(runErr, errs.ErrNotFound) {
					return runErr
				}
				blocking, err := tx.HasBlockingScheduleExecution(ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, occurrence.ScheduleID, occurrence.ID)
				if err != nil || blocking {
					return errs.ErrStateConflict
				}
			}
			if input.Action == "CANCEL" {
				occurrence.State, occurrence.Outcome = "CANCELLED", "failed"
			} else {
				occurrence.State, occurrence.Outcome = "SKIPPED", "recovery_operator_skip"
			}
			clearScheduledExecutionBinding(&occurrence)
			occurrence.ClaimantWorkloadID, occurrence.TokenHash, occurrence.ClaimKeySHA256 = "", "", ""
			occurrence.AuthorityGeneration = 0
			occurrence.LeaseExpiresAt = time.Time{}
		}
		occurrence.RecoveryEvidenceSHA256 = ""
		occurrence.RecoveryBlockedAt = time.Time{}
		occurrence.Version++
		occurrence.UpdatedAt = now.UTC().Truncate(time.Microsecond)
		if err := tx.UpdateScheduleOccurrence(ctx, occurrence, input.ExpectedAttempt, ""); err != nil {
			return err
		}
		if err := appendScheduleOccurrenceAudit(ctx, tx, input.Principal,
			"resolve_schedule_recovery_"+strings.ToLower(input.Action), occurrence); err != nil {
			return err
		}
		payload, err := json.Marshal(scheduleOccurrenceReceipt{Occurrence: occurrence})
		if err != nil {
			return errs.ErrInternal
		}
		if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
			Scope: "resolve_schedule_recovery", KeyHash: keyHash, RequestHash: requestHash,
			Payload: payload, CreatedAt: now,
		}); err != nil {
			return err
		}
		result = occurrence
		return nil
	})
	return result, err
}

func (service *Service) cancelBlockedScheduleRecoveryGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	schedule entity.Resource,
	occurrence domainrepo.ScheduleOccurrence,
	run domainrepo.ScheduledRun,
	reason string,
	now time.Time,
) error {
	if run.OccurrenceID != occurrence.ID || run.Attempt != occurrence.Attempt ||
		(run.State != "CLAIMED" && run.State != "WAITING_OWNER" && run.State != "CONTINUATION") ||
		value.ValidateID(run.CurrentTurnID) != nil || value.ValidateID(run.CurrentSessionID) != nil ||
		value.ValidateID(run.CurrentRuntimeRevisionID) != nil ||
		value.ValidateID(run.CurrentProcessRunID) != nil || !validSHA256Text(run.CurrentInputSHA256) {
		return errs.ErrStateConflict
	}
	var runtime *RuntimeExecution
	lockedRuntime, runtimeErr := tx.GetRuntimeExecutionByTurnForUpdate(
		ctx, run.CurrentTurnID, run.CurrentTurnAttempt,
	)
	if runtimeErr == nil {
		runtime = &lockedRuntime
	} else if !errors.Is(runtimeErr, errs.ErrNotFound) {
		return runtimeErr
	}
	session, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, run.CurrentSessionID)
	if err != nil {
		return err
	}
	turn, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, run.CurrentTurnID)
	if err != nil {
		return err
	}
	process, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, run.CurrentProcessRunID)
	if err != nil {
		return err
	}
	revision, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, run.CurrentRuntimeRevisionID,
	)
	if err != nil {
		return err
	}
	turnSpec, turnOK := turn.Spec.(entity.TurnSpec)
	processSpec, processOK := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	if session.ID != run.CurrentSessionID || session.Kind != enum.KindSession ||
		session.OwnerActorID != schedule.OwnerActorID || turn.ID != run.CurrentTurnID ||
		!turnOK || turn.Kind != enum.KindTurn || turn.OwnerActorID != schedule.OwnerActorID ||
		turnSpec.SessionID != session.ID || turnSpec.ProcessRunID != run.CurrentProcessRunID ||
		turnSpec.RuntimeRevisionID != run.CurrentRuntimeRevisionID ||
		turnSpec.Attempt != run.CurrentTurnAttempt ||
		turnSpec.EffectiveInputSHA256 != run.CurrentInputSHA256 ||
		revision.ID != run.CurrentRuntimeRevisionID || revision.Kind != enum.KindRuntimeRevision ||
		process.ID != run.CurrentProcessRunID || !processOK || process.Kind != enum.KindProcessRun ||
		process.OwnerActorID != schedule.OwnerActorID || processSpec.ScheduleID != schedule.ID ||
		processSpec.OccurrenceID != occurrence.ID || currentErr != nil ||
		!executionMatchesTurn(current, turn, turnSpec) || turn.State.Terminal() || process.State.Terminal() {
		return errs.ErrStateConflict
	}
	if runtime != nil && (runtime.OrganizationID != principal.OrganizationID ||
		runtime.ProjectID != principal.ProjectID || runtime.SessionID != session.ID ||
		runtime.TurnID != turn.ID || runtime.ProcessID != process.ID ||
		runtime.Attempt != turnSpec.Attempt || runtime.ImmutableInputSHA256 != turnSpec.EffectiveInputSHA256) {
		return errs.ErrStateConflict
	}
	if runtime == nil {
		if _, err := tx.GetRuntimeExecutionByTurn(ctx, turn.ID, turnSpec.Attempt); err == nil {
			return errs.ErrStateConflict
		} else if !errors.Is(err, errs.ErrNotFound) {
			return err
		}
	}
	children, err := tx.HasActiveChildProcesses(ctx, process.OrganizationID, process.ProjectID, process.ID)
	if err != nil || children {
		return errs.ErrStateConflict
	}
	if _, err := service.cancelTurnExecution(ctx, tx, principal, turn, reason, now); err != nil {
		return err
	}
	if turn.State == enum.StateWaitingOwner || process.State == enum.StateWaitingOwner {
		gate, err := tx.ActiveOwnerGateForProcess(ctx, process.OrganizationID, process.ProjectID, process.ID)
		if err != nil {
			return err
		}
		cancelledGate, err := gate.Transition(enum.StateCancelled, now)
		if err != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelledGate, gate.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(ctx, tx, principal,
			"resolve_schedule_recovery_cancel_gate", cancelledGate); err != nil {
			return err
		}
	}
	cancelledProcess, err := process.Transition(enum.StateCancelled, now)
	if err != nil {
		return errs.ErrStateConflict
	}
	if err := tx.Update(ctx, cancelledProcess, process.Version); err != nil {
		return err
	}
	if err := service.revokeExecutionClaims(
		ctx, tx, principal, process.ID, turn.ID, reason, now,
	); err != nil {
		return err
	}
	if err := service.appendMutationRecords(ctx, tx, principal,
		"resolve_schedule_recovery_cancel_process", cancelledProcess); err != nil {
		return err
	}
	if session.ParentID == schedule.ID && !session.State.Terminal() {
		cancelledSession, err := session.Transition(enum.StateCancelled, now)
		if err != nil {
			return errs.ErrStateConflict
		}
		if err := tx.Update(ctx, cancelledSession, session.Version); err != nil {
			return err
		}
		if err := service.appendMutationRecords(ctx, tx, principal,
			"resolve_schedule_recovery_cancel_session", cancelledSession); err != nil {
			return err
		}
	}
	return tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
		OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
		State: "CANCELLED", Outcome: "failed", FinishedAt: now,
	})
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
			queuedSubset := candidate.State == "QUEUED" || candidate.State == "RESERVED" ||
				(candidate.State == "CANCELLED" && candidate.ExecutionTurnID == "")
			if queuedSubset {
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
				(occurrence.State != "QUEUED" && occurrence.State != "RESERVED" && occurrence.State != "CLAIMED" &&
					occurrence.State != "WAITING_OWNER" &&
					occurrence.State != "CONTINUATION" &&
					occurrence.State != "CANCELLED") {
				return errs.ErrStateConflict
			}
			var schedule entity.Resource
			if queuedSubset {
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
			if occurrence.State == "QUEUED" {
				if err := validateQueuedScheduledOccurrence(occurrence, schedule); err != nil {
					return err
				}
			}
			if !queuedSubset {
				if err := requireClosedRuntimeConsistentWithTurn(graph); err != nil {
					return err
				}
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx, input.Principal.OrganizationID,
				"cancel_schedule_occurrence", keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				var payload scheduleOccurrenceReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil {
					return errs.ErrInternal
				}
				if occurrence.State != "CANCELLED" ||
					payload.Occurrence != occurrence {
					return errs.ErrStateConflict
				}
				result = occurrence
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			if occurrence.State != "QUEUED" && occurrence.State != "RESERVED" && occurrence.State != "CLAIMED" &&
				occurrence.State != "WAITING_OWNER" &&
				occurrence.State != "CONTINUATION" {
				return errs.ErrStateConflict
			}
			expectedToken := occurrence.TokenHash
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			now = now.UTC().Truncate(time.Microsecond)
			if occurrence.State != "QUEUED" && occurrence.State != "RESERVED" {
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
					State: "CANCELLED", Outcome: "failed", FinishedAt: now,
				}); err != nil {
					return err
				}
			}
			if occurrence.State == "RESERVED" && expectedToken != "" {
				capability, err := tx.GetScheduleOccurrenceCapabilityForUpdate(ctx, expectedToken)
				if err != nil || capability.State != "ISSUED" ||
					capability.FullMethod != materializeScheduleOccurrenceMethod {
					return errs.ErrStateConflict
				}
				capability.State, capability.RevokedAt = "REVOKED", now
				if err := tx.UpdateScheduleOccurrenceCapability(ctx, capability, "ISSUED"); err != nil {
					return err
				}
			}
			occurrence.State = "CANCELLED"
			occurrence.Outcome = "failed"
			occurrence.ClaimantWorkloadID = ""
			occurrence.AuthorityGeneration = 0
			occurrence.TokenHash = ""
			occurrence.ClaimKeySHA256 = ""
			occurrence.LeaseExpiresAt = time.Time{}
			occurrence.Version++
			occurrence.UpdatedAt = now
			if err := tx.UpdateScheduleOccurrence(
				ctx,
				occurrence,
				input.ExpectedAttempt,
				expectedToken,
			); err != nil {
				return err
			}
			if err := appendScheduleOccurrenceAudit(
				ctx, tx, input.Principal, "cancel_schedule_occurrence", occurrence,
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
	var startTargetGraph lockedOwnerGraph
	var startParentGraph lockedOwnerGraph
	var startDelegation domainrepo.DelegationEdge
	var receiptProcess entity.Resource
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"start_process",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			targetTurnID := input.RootTurnID
			turnIDs := []string{targetTurnID}
			var parentCandidate entity.Resource
			var parentCurrent executionTuple
			if input.ParentProcessID != "" {
				targetTurnID = input.Principal.AuthorityReference
				turnIDs[0] = targetTurnID
				var err error
				parentCandidate, err = tx.Get(
					ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, input.ParentProcessID,
				)
				if err != nil {
					return 0, err
				}
				parentSpec, ok := parentCandidate.Spec.(entity.ProcessRunSpec)
				if !ok || parentCandidate.Kind != enum.KindProcessRun ||
					parentCandidate.State.Terminal() {
					return 0, errs.ErrStateConflict
				}
				parentCurrent, err = currentExecution(parentSpec)
				if err != nil {
					return 0, err
				}
				turnIDs = append(turnIDs, parentCurrent.TurnID)
			}
			locked, err := service.lockOwnerGraphSet(
				ctx, tx, input.Principal, turnIDs, nil,
			)
			if err != nil {
				return 0, err
			}
			targetGraph, ok := locked.ByTurn[targetTurnID]
			if !ok {
				return 0, errs.ErrStateConflict
			}
			targetSpec, ok := targetGraph.Turn.Spec.(entity.TurnSpec)
			if !ok || targetGraph.Turn.Kind != enum.KindTurn ||
				targetGraph.Turn.State.Terminal() ||
				targetGraph.Session.Kind != enum.KindSession ||
				targetGraph.Session.State != enum.StateActive ||
				targetGraph.Turn.OwnerActorID != input.Principal.ActorID ||
				targetGraph.Session.OwnerActorID != input.Principal.ActorID {
				return 0, errs.ErrStateConflict
			}
			startTargetGraph = targetGraph
			if input.ParentProcessID == "" {
				if targetSpec.SessionID != input.RootSessionID ||
					targetSpec.Attempt != input.RootAttempt {
					return 0, errs.ErrStateConflict
				}
				if targetSpec.ProcessRunID == "" {
					if err := requireOwnerGraphRuntimeDisposition(
						targetGraph, runtimeDispositionAbsent,
					); err != nil {
						return 0, err
					}
					return lifecycleReceiptApply, nil
				}
				receiptProcess = targetGraph.Process
				if err := requireOwnerGraphRuntimeDisposition(
					targetGraph, runtimeDispositionAbsent,
					runtimeDispositionNonterminal, runtimeDispositionTerminal,
				); err != nil {
					return 0, err
				}
				processSpec, ok := receiptProcess.Spec.(entity.ProcessRunSpec)
				if !ok || processSpec.ParentProcessRunID != "" ||
					processSpec.RootSessionID != input.RootSessionID ||
					processSpec.RootTurnID != input.RootTurnID ||
					processSpec.RootAttempt != input.RootAttempt {
					return 0, errs.ErrStateConflict
				}
				return lifecycleReceiptReplay, nil
			}
			parentGraph, ok := locked.ByTurn[parentCurrent.TurnID]
			if !ok || parentGraph.Process.ID != input.ParentProcessID ||
				parentGraph.Process.Version != parentCandidate.Version {
				return 0, errs.ErrStateConflict
			}
			parentSpec, ok := parentGraph.Process.Spec.(entity.ProcessRunSpec)
			lockedParentCurrent, currentErr := currentExecution(parentSpec)
			if !ok || currentErr != nil || lockedParentCurrent != parentCurrent ||
				parentGraph.Process.State.Terminal() ||
				parentSpec.RootInitiatorActorID != input.Principal.ActorID {
				return 0, errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				parentGraph, runtimeDispositionAbsent, runtimeDispositionNonterminal,
			); err != nil {
				return 0, err
			}
			delegation, err := tx.GetDelegationEdgeByTargetTurn(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID, targetTurnID,
			)
			if err != nil || delegation.ParentProcessRunID != parentGraph.Process.ID ||
				delegation.TargetTurnID != targetTurnID ||
				delegation.TargetAttempt != uint32(input.Principal.AuthorityRevision) ||
				delegation.TargetInputSHA256 != input.Principal.AuthorityDigest ||
				delegation.RootInitiatorActorID != parentSpec.RootInitiatorActorID ||
				delegation.GrantGeneration != input.Principal.AuthorityGrantGeneration ||
				targetSpec.SessionID != delegation.TargetSessionID ||
				targetSpec.Attempt != delegation.TargetAttempt ||
				targetSpec.EffectiveInputSHA256 != delegation.TargetInputSHA256 {
				return 0, errs.ErrStateConflict
			}
			startParentGraph = parentGraph
			startDelegation = delegation
			if targetSpec.ProcessRunID == parentGraph.Process.ID {
				if err := requireOwnerGraphRuntimeDisposition(
					targetGraph, runtimeDispositionAbsent,
				); err != nil {
					return 0, err
				}
				return lifecycleReceiptApply, nil
			}
			receiptProcess = targetGraph.Process
			if err := requireOwnerGraphRuntimeDisposition(
				targetGraph, runtimeDispositionAbsent,
				runtimeDispositionNonterminal, runtimeDispositionTerminal,
			); err != nil {
				return 0, err
			}
			childSpec, ok := receiptProcess.Spec.(entity.ProcessRunSpec)
			if !ok || childSpec.ParentProcessRunID != parentGraph.Process.ID ||
				childSpec.DelegationID != delegation.ID ||
				childSpec.TargetTurnID != targetTurnID ||
				childSpec.TargetAttempt != delegation.TargetAttempt {
				return 0, errs.ErrStateConflict
			}
			return lifecycleReceiptReplay, nil
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(receiptProcess, stored)
		},
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
				parent := startParentGraph.Process
				parentSpec, ok := parent.Spec.(entity.ProcessRunSpec)
				if !ok || parent.Kind != enum.KindProcessRun ||
					parent.State.Terminal() ||
					parentSpec.RootInitiatorActorID != input.Principal.ActorID {
					return entity.Resource{}, errs.ErrStateConflict
				}
				delegation := startDelegation
				if delegation.ParentProcessRunID != parent.ID ||
					delegation.TargetTurnID != input.Principal.AuthorityReference ||
					delegation.TargetAttempt != uint32(input.Principal.AuthorityRevision) ||
					delegation.TargetInputSHA256 != input.Principal.AuthorityDigest ||
					delegation.RootInitiatorActorID != parentSpec.RootInitiatorActorID ||
					delegation.GrantGeneration != input.Principal.AuthorityGrantGeneration {
					return entity.Resource{}, errs.ErrStateConflict
				}
				targetTurn := startTargetGraph.Turn
				targetSpec, ok := targetTurn.Spec.(entity.TurnSpec)
				if !ok || targetTurn.Kind != enum.KindTurn || targetTurn.State.Terminal() ||
					targetTurn.OwnerActorID != parentSpec.RootInitiatorActorID ||
					targetSpec.SessionID != delegation.TargetSessionID ||
					targetSpec.ProcessRunID != parent.ID ||
					targetSpec.Attempt != delegation.TargetAttempt ||
					targetSpec.EffectiveInputSHA256 != delegation.TargetInputSHA256 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				targetSession := startTargetGraph.Session
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
				session := startTargetGraph.Session
				turn := startTargetGraph.Turn
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
				boundTurn = startTargetGraph.Turn
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
	requestHash, err := semanticCommandHash(input.Principal, struct {
		ProcessRunID     string
		ExpectedVersion  uint64
		TerminalState    enum.State
		Outcome          string
		ResultArtifactID string
	}{
		input.ProcessRunID, input.ExpectedVersion, input.TerminalState,
		input.Outcome, input.ResultArtifactID,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var receiptProcess entity.Resource
	var lockedProcessGraph lockedOwnerGraph
	return service.withValidatedResourceReceipt(
		ctx, input.Principal, input.IdempotencyKey, "complete_process", requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			graph, err := service.lockOwnerGraphByProcess(
				ctx, tx, input.Principal, input.ProcessRunID,
			)
			if err != nil {
				return 0, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return 0, err
			}
			lockedProcessGraph = graph
			process := graph.Process
			spec, ok := process.Spec.(entity.ProcessRunSpec)
			turnSpec, turnOK := graph.Turn.Spec.(entity.TurnSpec)
			if !ok || !turnOK || requireLifecycleOwner(input.Principal, process) != nil ||
				spec.RootInitiatorActorID != input.Principal.ActorID ||
				graph.Turn.State != input.TerminalState ||
				turnSpec.Outcome != input.Outcome ||
				turnSpec.ResultArtifactID != input.ResultArtifactID {
				return 0, errs.ErrStateConflict
			}
			if process.Version == input.ExpectedVersion && !process.State.Terminal() {
				return lifecycleReceiptApply, nil
			}
			if process.Version == input.ExpectedVersion+1 &&
				process.State == input.TerminalState && spec.Outcome == input.Outcome &&
				spec.ResultArtifactID == input.ResultArtifactID {
				receiptProcess = process
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(receiptProcess, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			process := lockedProcessGraph.Process
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
			turn := lockedProcessGraph.Turn
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
	var receiptProcess entity.Resource
	var lockedProcessGraph lockedOwnerGraph
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"cancel_process",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			graph, err := service.lockOwnerGraphByProcess(
				ctx, tx, input.Principal, input.ProcessRunID,
			)
			if err != nil {
				return 0, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return 0, err
			}
			lockedProcessGraph = graph
			process := graph.Process
			if requireLifecycleOwner(input.Principal, process) != nil {
				return 0, errs.ErrNotFound
			}
			if process.Version == input.ExpectedVersion && !process.State.Terminal() {
				return lifecycleReceiptApply, nil
			}
			if process.Version == input.ExpectedVersion+1 &&
				process.State == enum.StateCancelled {
				receiptProcess = process
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(receiptProcess, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			process := lockedProcessGraph.Process
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
			turns, err := tx.ActiveProcessTurnCandidates(
				ctx, process.OrganizationID, process.ProjectID, process.ID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if len(turns) != 1 || turns[0].ID != lockedProcessGraph.Turn.ID ||
				turns[0].Version != lockedProcessGraph.Turn.Version {
				return entity.Resource{}, errs.ErrStateConflict
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			if _, err := service.cancelTurnExecution(
				ctx, tx, input.Principal, lockedProcessGraph.Turn, input.ReasonCode, now,
			); err != nil {
				return entity.Resource{}, err
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
				processSpec.RootInitiatorActorID != input.Principal.ActorID {
				return errs.ErrStateConflict
			}
			gateTurn := graph.Turn
			gateTurnSpec, ok := gateTurn.Spec.(entity.TurnSpec)
			if !ok {
				return errs.ErrStateConflict
			}
			execution, err := currentExecution(processSpec)
			if err != nil {
				return err
			}
			if !executionMatchesTurn(execution, gateTurn, gateTurnSpec) ||
				execution.SessionID != graph.Session.ID {
				return errs.ErrStateConflict
			}
			executionSessionID := execution.SessionID
			executionTurnID := execution.TurnID
			executionAttempt := execution.Attempt
			if executionSessionID != input.SessionID ||
				executionTurnID != input.TurnID || executionAttempt != input.Attempt {
				return errs.ErrStateConflict
			}
			if gateTurn.Kind != enum.KindTurn ||
				gateTurn.OwnerActorID != process.OwnerActorID ||
				gateTurnSpec.SessionID != input.SessionID ||
				gateTurnSpec.ProcessRunID != process.ID ||
				gateTurnSpec.Attempt != input.Attempt ||
				input.Principal.AuthoritySource != "AGENT_SESSION" ||
				input.Principal.AuthorityReference != gateTurn.ID ||
				input.Principal.AuthorityRevision != uint64(gateTurnSpec.Attempt) ||
				input.Principal.AuthorityDigest != gateTurnSpec.EffectiveInputSHA256 {
				return errs.ErrStateConflict
			}
			var sourceAttempt domainrepo.TurnAttempt
			var sourceLease domainrepo.TurnLease
			if process.State == enum.StateRunning {
				sourceAttempt, err = tx.GetTurnAttemptForUpdate(
					ctx, gateTurn.ID, gateTurnSpec.Attempt,
				)
				if err != nil || sourceAttempt.State != "CLAIMED" ||
					sourceAttempt.InputSHA256 != gateTurnSpec.EffectiveInputSHA256 ||
					sourceAttempt.AuthorityGeneration !=
						input.Principal.AuthorityGrantGeneration {
					return errs.ErrStateConflict
				}
				sourceLease, err = tx.GetTurnLeaseForUpdate(ctx, gateTurn.ID)
				if err != nil || sourceLease.Attempt != gateTurnSpec.Attempt ||
					sourceLease.AuthorityGeneration != sourceAttempt.AuthorityGeneration ||
					sourceLease.Fence != gateTurn.Version {
					return errs.ErrStateConflict
				}
			}
			var replayGate entity.Resource
			if process.State == enum.StateWaitingOwner {
				replayGate, err = tx.ActiveOwnerGateForProcess(
					ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
					process.ID,
				)
				if err != nil {
					return err
				}
				replaySpec, replayOK := replayGate.Spec.(entity.OwnerGateSpec)
				if !replayOK || replayGate.OwnerActorID != process.OwnerActorID ||
					replaySpec.ProcessRunID != process.ID ||
					replaySpec.SessionID != gateTurnSpec.SessionID ||
					replaySpec.TurnID != gateTurn.ID ||
					replaySpec.Attempt != gateTurnSpec.Attempt {
					return errs.ErrStateConflict
				}
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if !input.ExpiresAt.After(now) || input.ExpiresAt.After(now.Add(30*24*time.Hour)) {
				return errs.ErrStateConflict
			}
			if process.State == enum.StateRunning {
				if err := requireOwnerGateSuspensionLease(
					graph.Runtime, sourceLease, now,
				); err != nil {
					return errs.ErrStateConflict
				}
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx, input.Principal.OrganizationID, "request_owner_gate", keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					process.State != enum.StateWaitingOwner ||
					process.Version != input.ProcessExpectedVersion+1 ||
					gateTurn.State != enum.StateWaitingOwner ||
					gateTurnSpec.Outcome != "owner_gate_pending" ||
					gateTurnSpec.ResultArtifactID != input.ResultArtifactID {
					return errs.ErrIdempotencyConflict
				}
				var payload ownerGateReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil {
					return errs.ErrInternal
				}
				gate := replayGate
				if gate.ID == "" || gate.ID != receipt.Result.ID ||
					resourceReceiptMatchesCurrent(gate, receipt.Result) != nil ||
					resourceReceiptMatchesCurrent(process, payload.Process) != nil {
					return errs.ErrStateConflict
				}
				if graph.Runtime != nil &&
					(graph.Runtime.State != "SUSPENDED" ||
						graph.Runtime.TerminalReference != gate.ID ||
						graph.Runtime.TerminalSHA256 != requestHash) {
					return errs.ErrStateConflict
				}
				if err := requireOwnerGraphRuntimeDisposition(
					graph, runtimeDispositionAbsent, runtimeDispositionTerminal,
				); err != nil {
					return err
				}
				result = OwnerGateResult{OwnerGate: gate, Process: process}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			if process.State != enum.StateRunning ||
				process.Version != input.ProcessExpectedVersion ||
				gateTurnSpec.ResultArtifactID != "" ||
				(gateTurn.State != enum.StateClaimed &&
					gateTurn.State != enum.StateRunning) {
				return errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent, runtimeDispositionNonterminal,
			); err != nil {
				return err
			}
			artifactResource, artifact, err := service.requireCleanArtifactResource(
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
			if sourceAttempt.TurnID == "" || sourceLease.TurnID == "" {
				return errs.ErrStateConflict
			}
			gateID := uuid.NewString()
			if err := service.suspendRuntimeExecutionForOwnerGate(
				ctx, tx, input.Principal, graph, gateID, requestHash, now,
			); err != nil {
				return err
			}
			sourceAttempt.State = "WAITING_OWNER"
			sourceAttempt.FinishedAt = now
			sourceAttempt.Outcome = "owner_gate_pending"
			if err := tx.FinishTurnAttempt(ctx, sourceAttempt); err != nil {
				return err
			}
			gateTurnSpec.ResultArtifactID = input.ResultArtifactID
			gateTurnSpec.ResultArtifactVersion = artifactResource.Version
			gateTurnSpec.ResultArtifactSHA256 = artifact.SHA256
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
			if err := tx.DeleteTurnLease(ctx, gateTurn.ID, sourceLease.Fence); err != nil {
				return err
			}
			deliveryID := uuid.NewString()
			notificationRoomID := ""
			if processSpec.OccurrenceID != "" {
				if graph.Occurrence.ID != processSpec.OccurrenceID || graph.Occurrence.RoomID == "" {
					return errs.ErrStateConflict
				}
				notificationRoomID = graph.Occurrence.RoomID
			}
			deliveryPayloadSHA256, err := canonicalHash(struct {
				Version            int
				DeliveryID         string
				OwnerGateID        string
				ProcessRunID       string
				SessionID          string
				TurnID             string
				Attempt            uint32
				ResultSHA256       string
				ImmutableInput     string
				RecipientActorID   string
				ExpiresAt          time.Time
				ScheduleID         string
				OccurrenceID       string
				NotificationRoomID string
			}{
				1,
				deliveryID,
				gateID,
				process.ID,
				executionSessionID,
				executionTurnID,
				executionAttempt,
				artifact.SHA256,
				gateTurnSpec.EffectiveInputSHA256,
				processSpec.RootInitiatorActorID,
				input.ExpiresAt.UTC().Truncate(time.Microsecond),
				processSpec.ScheduleID,
				processSpec.OccurrenceID,
				notificationRoomID,
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
					ImmutableInputSHA256:  gateTurnSpec.EffectiveInputSHA256,
					RecipientActorID:      processSpec.RootInitiatorActorID,
					DeliveryWorkloadID:    service.ownerGateDeliveryWorkload,
					DeliverySPIFFEID:      service.ownerGateDeliverySPIFFEID,
					DeliveryID:            deliveryID,
					DeliveryPayloadSHA256: deliveryPayloadSHA256,
					ScheduleID:            processSpec.ScheduleID,
					OccurrenceID:          processSpec.OccurrenceID,
					NotificationRoomID:    notificationRoomID,
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
				occurrence.Outcome = "requires_human"
				occurrence.ResultArtifactID = input.ResultArtifactID
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
				if err := tx.WaitScheduledRun(ctx, domainrepo.ScheduledRun{
					OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
					Outcome: "requires_human", ResultArtifactID: input.ResultArtifactID,
				}); err != nil {
					return err
				}
				schedule := graph.Schedule
				if schedule.Kind != enum.KindSchedule ||
					schedule.OwnerActorID != process.OwnerActorID {
					return errs.ErrStateConflict
				}
				if err := appendScheduleOccurrenceAudit(
					ctx, tx, input.Principal, "owner_gate_wait_schedule", occurrence,
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
