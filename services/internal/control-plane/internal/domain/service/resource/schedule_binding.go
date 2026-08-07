package resource

import (
	"context"
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

// CreateScheduleFromOwnerSelections атомарно создаёт Schedule из stable keys
// и отображаемого имени prompt artifact. Ни один target UUID/version/digest не
// принимается от browser.
func (service *Service) CreateScheduleFromOwnerSelections(
	ctx context.Context,
	input CreateScheduleFromOwnerSelectionsInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionScheduleCreateFromSelections); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateName(input.Name) != nil ||
		value.ValidateStableKey(input.AgentStableKey) != nil ||
		value.ValidateStableKey(input.InstructionSetStableKey) != nil ||
		value.ValidateStableKey(input.ProviderPoolStableKey) != nil ||
		(input.RoomStableKey != "" && value.ValidateStableKey(input.RoomStableKey) != nil) ||
		value.ValidateName(input.PromptArtifactName) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	// Owner выбирает только поведение Schedule. Весь authority-bearing tuple
	// очищается до semantic hash и затем назначается сервером под locks.
	input.Spec.TargetResourceID, input.Spec.TargetKind, input.Spec.TargetVersion = "", "", 0
	input.Spec.TargetType, input.Spec.PromptArtifactID, input.Spec.RoomID = "", "", ""
	input.Spec.AgentID, input.Spec.AgentVersion, input.Spec.AgentSHA256 = "", 0, ""
	input.Spec.InstructionSetID, input.Spec.InstructionSetVersion, input.Spec.InstructionSetSHA256 = "", 0, ""
	input.Spec.ProviderPoolID, input.Spec.ProviderPoolVersion, input.Spec.ProviderPoolSHA256 = "", 0, ""
	input.Spec.RuntimeSelectionRef, input.Spec.RuntimeSelectionVersion, input.Spec.RuntimeSelectionSHA256 = "", 0, ""
	input.Spec.AgentAssignmentID, input.Spec.AgentAssignmentVersion, input.Spec.AgentAssignmentSHA256 = "", 0, ""
	input.Spec.PromptProfileID, input.Spec.PromptRevision, input.Spec.RuntimeRevisionID = "", 0, ""
	input.Spec.ExecutionSessionID, input.Spec.EffectiveInputSHA = "", ""
	input.Spec.Ownership, input.Spec.NextRunAt = entity.ConfigurationOwnership{}, time.Time{}
	requestHash, err := canonicalHash(struct {
		Identity                                             commandIdentity
		Name, Agent, Instruction, Pool, Room, PromptArtifact string
		Spec                                                 entity.ScheduleSpec
	}{identity(input.Principal), input.Name, input.AgentStableKey, input.InstructionSetStableKey,
		input.ProviderPoolStableKey, input.RoomStableKey, input.PromptArtifactName, input.Spec})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"create_schedule_from_owner_selections", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			protected, ok := tx.(domainrepo.ProtectedTransaction)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			// Persistent/rolling create получает project fence до Workspace,
			// configuration и candidate locks: все Session writers соблюдают один
			// порядок и не образуют Schedule↔Session lock cycle.
			if input.Spec.SessionPolicy != "NEW" {
				if _, err := lockScheduleSessionProjectFence(ctx, tx, input.Principal); err != nil {
					return entity.Resource{}, err
				}
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			now = now.UTC().Truncate(time.Microsecond)
			workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
			if err != nil {
				return entity.Resource{}, err
			}
			roomID := ""
			if input.RoomStableKey != "" {
				room, roomErr := protected.GetByStableKeyForUpdate(ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, enum.KindChat, input.RoomStableKey)
				if roomErr != nil {
					return entity.Resource{}, roomErr
				}
				if room.Kind != enum.KindChat || room.State != enum.StateActive || room.OwnerActorID != input.Principal.ActorID {
					return entity.Resource{}, errs.ErrNotFound
				}
				roomID = room.ID
			}
			agent, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindAgent, input.AgentStableKey)
			if err != nil {
				return entity.Resource{}, err
			}
			instruction, err := requireProtectedStable(ctx, protected, input.Principal,
				enum.KindInstructionSet, input.InstructionSetStableKey)
			if err != nil {
				return entity.Resource{}, err
			}
			pool, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindProviderPool, input.ProviderPoolStableKey)
			if err != nil {
				return entity.Resource{}, err
			}
			agentSpec, agentOK := agent.Spec.(entity.AgentSpec)
			instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
			if !agentOK || !agentSpec.Enabled || agent.State != enum.StateActive || !instructionOK ||
				instructionSpec.VersionState != "PUBLISHED" || agentSpec.InstructionSetID != instruction.ID ||
				agentSpec.InstructionSetVersion != instruction.Version || agentSpec.ProviderPoolID != pool.ID ||
				agentSpec.ProviderPoolVersion != pool.Version {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if _, _, err := lockAgentRuntimeProfile(ctx, tx, input.Principal, agentSpec); err != nil {
				return entity.Resource{}, err
			}
			agentID, agentVersion, agentSHA, err := protectedTuple(agent)
			if err != nil {
				return entity.Resource{}, err
			}
			instructionID, instructionVersion, instructionSHA, err := protectedTuple(instruction)
			if err != nil {
				return entity.Resource{}, err
			}
			poolID, poolVersion, poolSHA, err := protectedTuple(pool)
			if err != nil {
				return entity.Resource{}, err
			}
			assignment, err := lockActiveAgentAssignment(ctx, tx, input.Principal, agent.ID, agent.Version,
				agentSHA, workspace.Version, workspaceSHA, roomID)
			if err != nil {
				return entity.Resource{}, err
			}
			assignmentSHA, err := entity.ProjectionSHA256(assignment)
			if err != nil {
				return entity.Resource{}, errs.ErrInternal
			}
			prompt, err := protected.GetByNameForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, enum.KindArtifact, input.PromptArtifactName)
			if err != nil {
				return entity.Resource{}, err
			}
			promptSpec, ok := prompt.Spec.(entity.ArtifactSpec)
			if !ok || prompt.Kind != enum.KindArtifact || prompt.State != enum.StateActive ||
				prompt.OwnerActorID != input.Principal.ActorID || promptSpec.Direction != "INPUT" ||
				promptSpec.MediaType != "text/markdown" || promptSpec.ScanStatus != "CLEAN" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec := input.Spec
			spec.TargetResourceID, spec.TargetKind, spec.TargetVersion = agentID, enum.KindAgent, agentVersion
			spec.TargetType, spec.PromptArtifactID, spec.RoomID = "AGENT", prompt.ID, roomID
			spec.AgentID, spec.AgentVersion, spec.AgentSHA256 = agentID, agentVersion, agentSHA
			spec.InstructionSetID, spec.InstructionSetVersion, spec.InstructionSetSHA256 =
				instructionID, instructionVersion, instructionSHA
			spec.ProviderPoolID, spec.ProviderPoolVersion, spec.ProviderPoolSHA256 = poolID, poolVersion, poolSHA
			spec.RuntimeSelectionRef, spec.RuntimeSelectionVersion, spec.RuntimeSelectionSHA256 =
				agentSpec.RuntimeProfileRef, agentSpec.RuntimeProfileVersion, agentSpec.RuntimeProfileSHA256
			spec.AgentAssignmentID, spec.AgentAssignmentVersion, spec.AgentAssignmentSHA256 =
				assignment.ID, assignment.Version, assignmentSHA
			spec.PromptProfileID, spec.PromptRevision, spec.RuntimeRevisionID = "", 0, ""
			spec.Ownership = entity.ConfigurationOwnership{ManagedBy: "UI"}
			if spec.SessionPolicy != "NEW" {
				session, sessionErr := uniqueScheduleSession(ctx, tx, input.Principal, spec)
				if sessionErr != nil {
					return entity.Resource{}, sessionErr
				}
				spec.ExecutionSessionID = session.ID
			} else {
				spec.ExecutionSessionID = ""
			}
			spec.EffectiveInputSHA, err = targetScheduleEffectiveInput(spec, promptSpec.SHA256)
			if err != nil {
				return entity.Resource{}, errs.ErrInternal
			}
			spec.NextRunAt, err = firstScheduleRun(spec, now)
			if err != nil || spec.Validate() != nil || validateConfigurationCreate(ctx, tx, input.Principal, spec) != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			created, err := entity.New(uuid.NewString(), input.Principal.OrganizationID, input.Principal.ProjectID,
				"", input.Principal.ActorID, enum.KindSchedule, input.Name, spec, now)
			if err != nil || validateTemporalCreation(spec, now) != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := tx.Insert(ctx, created); err != nil {
				return entity.Resource{}, err
			}
			return created, service.appendMutationRecords(ctx, tx, input.Principal,
				"create_schedule_from_owner_selections", created)
		})
}

func uniqueScheduleSession(ctx context.Context, tx domainrepo.Transaction, principal value.Principal,
	spec entity.ScheduleSpec,
) (entity.Resource, error) {
	result, found, err := lockUniqueScheduleSession(ctx, tx, principal, spec)
	if err != nil {
		return entity.Resource{}, err
	}
	if !found {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return result, nil
}

func lockUniqueScheduleSession(ctx context.Context, tx domainrepo.Transaction, principal value.Principal,
	spec entity.ScheduleSpec,
) (entity.Resource, bool, error) {
	scheduleTx, err := lockScheduleSessionProjectFence(ctx, tx, principal)
	if err != nil {
		return entity.Resource{}, false, err
	}
	return lockUniqueScheduleSessionAfterFence(ctx, scheduleTx, principal, spec)
}

func lockScheduleSessionProjectFence(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
) (domainrepo.ScheduleSessionTransaction, error) {
	scheduleTx, ok := tx.(domainrepo.ScheduleSessionTransaction)
	if !ok {
		return nil, errs.ErrInternal
	}
	if err := scheduleTx.LockScheduleSessionProjectFence(
		ctx, principal.OrganizationID, principal.ProjectID,
	); err != nil {
		return nil, err
	}
	return scheduleTx, nil
}

func lockUniqueScheduleSessionAfterFence(
	ctx context.Context,
	scheduleTx domainrepo.ScheduleSessionTransaction,
	principal value.Principal,
	spec entity.ScheduleSpec,
) (entity.Resource, bool, error) {
	resources, err := scheduleTx.ListScheduleSessionConversationForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.RoomID,
	)
	if err != nil {
		return entity.Resource{}, false, err
	}
	// Для непустой conversation uniqueness совпадает с admission index:
	// любой live объект другого owner/tuple является закрытым конфликтом.
	if spec.RoomID != "" && len(resources) > 1 {
		return entity.Resource{}, false, errs.ErrStateConflict
	}
	ids := scheduleSessionCandidateIDs(resources, principal.ActorID, spec)
	if len(ids) > 1 {
		return entity.Resource{}, false, errs.ErrStateConflict
	}
	if len(ids) == 0 {
		if spec.RoomID != "" && len(resources) != 0 {
			return entity.Resource{}, false, errs.ErrStateConflict
		}
		return entity.Resource{}, false, nil
	}
	for _, resource := range resources {
		if resource.ID == ids[0] {
			return resource, true, nil
		}
	}
	return entity.Resource{}, false, errs.ErrStateConflict
}

func scheduleSessionCandidateIDs(resources []entity.Resource, ownerActorID string,
	spec entity.ScheduleSpec,
) []string {
	ids := make([]string, 0, 1)
	for _, candidate := range resources {
		session, ok := candidate.Spec.(entity.SessionSpec)
		if ok && candidate.Kind == enum.KindSession && candidate.State == enum.StateActive &&
			candidate.OwnerActorID == ownerActorID && scheduleSessionCompatible(session, spec) {
			ids = append(ids, candidate.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func scheduleSessionCompatible(session entity.SessionSpec, schedule entity.ScheduleSpec) bool {
	return session.AgentID == schedule.AgentID &&
		session.AgentVersion == schedule.AgentVersion && session.AgentSHA256 == schedule.AgentSHA256 &&
		session.ProviderPoolID == schedule.ProviderPoolID &&
		session.ProviderPoolVersion == schedule.ProviderPoolVersion &&
		session.ProviderPoolSHA256 == schedule.ProviderPoolSHA256 &&
		session.AgentAssignmentID == schedule.AgentAssignmentID &&
		session.AgentAssignmentVersion == schedule.AgentAssignmentVersion &&
		session.AgentAssignmentSHA256 == schedule.AgentAssignmentSHA256 &&
		session.ConversationID == schedule.RoomID
}

func (service *Service) rebindScheduleSession(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	schedule entity.Resource,
	previousSessionID string,
	spec entity.ScheduleSpec,
	now time.Time,
) (entity.Resource, error) {
	if value.ValidateID(previousSessionID) != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	scheduleTx, err := lockScheduleSessionProjectFence(ctx, tx, principal)
	if err != nil {
		return entity.Resource{}, err
	}
	// Candidate read выполняется только после project graph fence и берёт row
	// locks. Ожидавший конкурент поэтому перечитывает уже committed tuple.
	boundary, err := scheduleTx.ListScheduleSessionConversationForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.RoomID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	previous, err := tx.GetForUpdateIncludingDeleted(
		ctx, principal.OrganizationID, principal.ProjectID, previousSessionID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	previousSpec, ok := previous.Spec.(entity.SessionSpec)
	if !ok || previous.Kind != enum.KindSession || previous.OwnerActorID != schedule.OwnerActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if previous.State == enum.StateActive && scheduleSessionCompatible(previousSpec, spec) {
		if len(boundary) != 1 || boundary[0].ID != previous.ID {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return previous, nil
	}
	if previous.State != enum.StateActive {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if spec.RoomID != "" && (len(boundary) != 1 || boundary[0].ID != previous.ID) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := lockAndRejectOtherScheduleSessionReferences(
		ctx, tx, principal, schedule.ID, previous.ID,
	); err != nil {
		return entity.Resource{}, err
	}
	blocked, err := tx.SessionBlocksRuntimeCleanup(
		ctx, principal.OrganizationID, principal.ProjectID, previous.ID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	if blocked {
		return entity.Resource{}, errs.ErrStateConflict
	}
	archived, err := previous.Transition(enum.StateArchived, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, archived, previous.Version); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "schedule_rebind_archive_session", archived,
	); err != nil {
		return entity.Resource{}, err
	}
	// Exact-compatible Session могла быть создана другим сериализованным
	// schedule lifecycle. Её переиспользуем только при доказанной
	// единственности; дубли закрыто отклоняются.
	compatible, found, err := lockUniqueScheduleSessionAfterFence(ctx, scheduleTx, principal, spec)
	if err != nil {
		return entity.Resource{}, err
	}
	if found {
		return compatible, nil
	}
	// Архивирование сохраняет immutable историю прежнего tuple, а Schedule
	// получает единственную новую admission-active Session.
	replacement, err := entity.New(uuid.NewString(), principal.OrganizationID, principal.ProjectID,
		schedule.ID, schedule.OwnerActorID, enum.KindSession, "Scheduled session "+schedule.ID,
		entity.SessionSpec{
			AgentID: spec.AgentID, AgentVersion: spec.AgentVersion, AgentSHA256: spec.AgentSHA256,
			ProviderPoolID: spec.ProviderPoolID, ProviderPoolVersion: spec.ProviderPoolVersion,
			ProviderPoolSHA256: spec.ProviderPoolSHA256, ConversationID: spec.RoomID,
			AgentAssignmentID: spec.AgentAssignmentID, AgentAssignmentVersion: spec.AgentAssignmentVersion,
			AgentAssignmentSHA256: spec.AgentAssignmentSHA256,
		}, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, replacement); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(ctx, tx, principal, "schedule_rebind_agent_session", replacement); err != nil {
		return entity.Resource{}, err
	}
	return replacement, nil
}

func lockAndRejectOtherScheduleSessionReferences(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	scheduleID, sessionID string,
) error {
	referenced, err := tx.OtherScheduleReferencesSessionForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, scheduleID, sessionID,
	)
	if err != nil {
		return err
	}
	if referenced {
		return errs.ErrStateConflict
	}
	return nil
}

// BindScheduleConfiguration разрешает человекочитаемые stable key внутри
// owner boundary и сохраняет только exact version/digest tuple.
func (service *Service) BindScheduleConfiguration(
	ctx context.Context,
	input BindScheduleConfigurationInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionScheduleBind); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ScheduleID) != nil || input.ExpectedVersion == 0 ||
		value.ValidateStableKey(input.AgentStableKey) != nil ||
		value.ValidateStableKey(input.InstructionSetStableKey) != nil ||
		value.ValidateStableKey(input.ProviderPoolStableKey) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                commandIdentity
		ScheduleID              string
		ExpectedVersion         uint64
		AgentStableKey          string
		InstructionSetStableKey string
		ProviderPoolStableKey   string
	}{identity(input.Principal), input.ScheduleID, input.ExpectedVersion, input.AgentStableKey,
		input.InstructionSetStableKey, input.ProviderPoolStableKey})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	apply := func(tx domainrepo.Transaction) (entity.Resource, error) {
		protected, ok := tx.(domainrepo.ProtectedTransaction)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		schedule, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.ScheduleID)
		if err != nil {
			return entity.Resource{}, err
		}
		if schedule.Kind != enum.KindSchedule || schedule.OwnerActorID != input.Principal.ActorID {
			return entity.Resource{}, errs.ErrNotFound
		}
		if schedule.Version != input.ExpectedVersion {
			return entity.Resource{}, errs.ErrVersionMismatch
		}
		if schedule.State != enum.StateActive && schedule.State != enum.StatePaused {
			return entity.Resource{}, errs.ErrStateConflict
		}
		workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
		if err != nil {
			return entity.Resource{}, err
		}
		open, err := tx.HasOpenScheduleOccurrence(ctx, schedule.OrganizationID, schedule.ProjectID, schedule.ID)
		if err != nil {
			return entity.Resource{}, err
		}
		if open {
			return entity.Resource{}, errs.ErrStateConflict
		}
		agent, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindAgent, input.AgentStableKey)
		if err != nil {
			return entity.Resource{}, err
		}
		instruction, err := requireProtectedStable(ctx, protected, input.Principal,
			enum.KindInstructionSet, input.InstructionSetStableKey)
		if err != nil {
			return entity.Resource{}, err
		}
		pool, err := requireProtectedStable(ctx, protected, input.Principal,
			enum.KindProviderPool, input.ProviderPoolStableKey)
		if err != nil {
			return entity.Resource{}, err
		}
		agentSpec, agentOK := agent.Spec.(entity.AgentSpec)
		instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
		if !agentOK || !agentSpec.Enabled || agent.State != enum.StateActive || !instructionOK || instructionSpec.VersionState != "PUBLISHED" ||
			agentSpec.InstructionSetID != instruction.ID ||
			agentSpec.InstructionSetVersion != instruction.Version ||
			agentSpec.ProviderPoolID != pool.ID || agentSpec.ProviderPoolVersion != pool.Version {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if _, _, err := lockAgentRuntimeProfile(ctx, tx, input.Principal, agentSpec); err != nil {
			return entity.Resource{}, err
		}
		agentID, agentVersion, agentSHA, err := protectedTuple(agent)
		if err != nil {
			return entity.Resource{}, err
		}
		instructionID, instructionVersion, instructionSHA, err := protectedTuple(instruction)
		if err != nil {
			return entity.Resource{}, err
		}
		poolID, poolVersion, poolSHA, err := protectedTuple(pool)
		if err != nil {
			return entity.Resource{}, err
		}
		spec, ok := schedule.Spec.(entity.ScheduleSpec)
		if !ok {
			return entity.Resource{}, errs.ErrStateConflict
		}
		assignment, err := lockActiveAgentAssignment(ctx, tx, input.Principal,
			agent.ID, agent.Version, agentSHA, workspace.Version, workspaceSHA, spec.RoomID)
		if err != nil {
			return entity.Resource{}, err
		}
		assignmentSHA, err := entity.ProjectionSHA256(assignment)
		if err != nil {
			return entity.Resource{}, errs.ErrInternal
		}
		previousExecutionSessionID := spec.ExecutionSessionID
		spec.TargetResourceID, spec.TargetKind, spec.TargetVersion = agentID, enum.KindAgent, agentVersion
		spec.PromptProfileID, spec.PromptRevision, spec.RuntimeRevisionID = "", 0, ""
		spec.AgentID, spec.AgentVersion, spec.AgentSHA256 = agentID, agentVersion, agentSHA
		spec.InstructionSetID, spec.InstructionSetVersion, spec.InstructionSetSHA256 =
			instructionID, instructionVersion, instructionSHA
		spec.ProviderPoolID, spec.ProviderPoolVersion, spec.ProviderPoolSHA256 = poolID, poolVersion, poolSHA
		spec.RuntimeSelectionRef, spec.RuntimeSelectionVersion, spec.RuntimeSelectionSHA256 =
			agentSpec.RuntimeProfileRef, agentSpec.RuntimeProfileVersion, agentSpec.RuntimeProfileSHA256
		spec.AgentAssignmentID, spec.AgentAssignmentVersion, spec.AgentAssignmentSHA256 =
			assignment.ID, assignment.Version, assignmentSHA
		// Rebind не сохраняет ссылку на Session со старым authority tuple.
		// Для persistent/rolling exact совместимая Session заново разрешается и
		// блокируется до commit; NEW всегда отвязывается и создаёт новую Session
		// при следующей materialization.
		now := service.now().UTC().Truncate(time.Microsecond)
		if spec.SessionPolicy == "NEW" {
			spec.ExecutionSessionID = ""
		} else {
			compatible, sessionErr := service.rebindScheduleSession(
				ctx, tx, input.Principal, schedule, previousExecutionSessionID, spec, now,
			)
			if sessionErr != nil {
				return entity.Resource{}, sessionErr
			}
			spec.ExecutionSessionID = compatible.ID
		}
		promptArtifact, artifactErr := service.requireCleanArtifact(ctx, tx, input.Principal, spec.PromptArtifactID)
		if artifactErr != nil {
			return entity.Resource{}, artifactErr
		}
		spec.EffectiveInputSHA, err = targetScheduleEffectiveInput(spec, promptArtifact.SHA256)
		if err != nil || spec.Validate() != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		updated, err := schedule.Update(schedule.Name, spec, now)
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		if err := tx.Update(ctx, updated, schedule.Version); err != nil {
			return entity.Resource{}, err
		}
		if err := appendOwnerStateAudit(ctx, tx, input.Principal, "bind_schedule_configuration",
			updated.OrganizationID, updated.ProjectID, updated.ID, string(updated.Kind), updated.Version, now); err != nil {
			return entity.Resource{}, err
		}
		return updated, nil
	}
	return service.withOwnerLockedResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"bind_schedule_configuration", requestHash, input.ScheduleID, enum.KindSchedule,
		input.ExpectedVersion, func(stored entity.Resource) error {
			if stored.ID != input.ScheduleID || stored.Kind != enum.KindSchedule ||
				stored.OwnerActorID != input.Principal.ActorID || stored.Version != input.ExpectedVersion+1 {
				return errs.ErrStateConflict
			}
			return nil
		}, apply)
}

func targetScheduleEffectiveInput(spec entity.ScheduleSpec, promptArtifactSHA256 string) (string, error) {
	return canonicalHash(struct {
		AgentID, AgentSHA256                        string
		AgentVersion                                uint64
		InstructionSetID, InstructionSetSHA256      string
		InstructionSetVersion                       uint64
		RuntimeSelectionRef, RuntimeSelectionSHA256 string
		RuntimeSelectionVersion                     uint64
		ProviderPoolID, ProviderPoolSHA256          string
		ProviderPoolVersion                         uint64
		AgentAssignmentID, AgentAssignmentSHA256    string
		AgentAssignmentVersion                      uint64
		PromptArtifactSHA256                        string
		TargetType, PlaybookRef                     string
		PlaybookVersion                             uint64
		SessionPolicy, RoomID                       string
	}{spec.AgentID, spec.AgentSHA256, spec.AgentVersion,
		spec.InstructionSetID, spec.InstructionSetSHA256, spec.InstructionSetVersion,
		spec.RuntimeSelectionRef, spec.RuntimeSelectionSHA256, spec.RuntimeSelectionVersion,
		spec.ProviderPoolID, spec.ProviderPoolSHA256, spec.ProviderPoolVersion,
		spec.AgentAssignmentID, spec.AgentAssignmentSHA256, spec.AgentAssignmentVersion,
		promptArtifactSHA256,
		spec.TargetType, spec.PlaybookRef, spec.PlaybookVersion, spec.SessionPolicy, spec.RoomID})
}

func lockActiveAgentAssignment(ctx context.Context, tx domainrepo.Transaction, principal value.Principal,
	agentID string, agentVersion uint64, agentSHA256 string,
	workspaceVersion uint64, workspaceSHA256, roomID string,
) (entity.Resource, error) {
	resources, err := tx.ListSnapshotResources(ctx, principal.OrganizationID, principal.ProjectID)
	if err != nil {
		return entity.Resource{}, err
	}
	ids := make([]string, 0)
	for _, candidate := range resources {
		spec, ok := candidate.Spec.(entity.AgentAssignmentSpec)
		if ok && candidate.Kind == enum.KindAgentAssignment && candidate.State == enum.StateActive &&
			candidate.OwnerActorID == principal.ActorID && spec.RootActorID == principal.ActorID &&
			spec.AgentID == agentID && spec.AgentVersion == agentVersion && spec.AgentSHA256 == agentSHA256 &&
			spec.WorkspaceID == principal.ProjectID && spec.WorkspaceVersion == workspaceVersion &&
			spec.WorkspaceSHA256 == workspaceSHA256 && spec.RoomID == roomID {
			ids = append(ids, candidate.ID)
		}
	}
	if len(ids) != 1 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	slices.Sort(ids)
	assignment, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, ids[0])
	if err != nil {
		return entity.Resource{}, err
	}
	spec, ok := assignment.Spec.(entity.AgentAssignmentSpec)
	if !ok || assignment.Kind != enum.KindAgentAssignment || assignment.State != enum.StateActive ||
		assignment.OwnerActorID != principal.ActorID || spec.RootActorID != principal.ActorID ||
		spec.AgentID != agentID || spec.AgentVersion != agentVersion || spec.AgentSHA256 != agentSHA256 ||
		spec.WorkspaceID != principal.ProjectID || spec.WorkspaceVersion != workspaceVersion ||
		spec.WorkspaceSHA256 != workspaceSHA256 || spec.RoomID != roomID {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return assignment, nil
}

func lockActiveWorkspace(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
) (entity.Resource, string, error) {
	workspace, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, principal.ProjectID)
	if err != nil {
		return entity.Resource{}, "", err
	}
	if workspace.ID != principal.ProjectID || workspace.ProjectID != principal.ProjectID ||
		workspace.OrganizationID != principal.OrganizationID || workspace.OwnerActorID != principal.ActorID ||
		workspace.Kind != enum.KindProject || workspace.State != enum.StateActive {
		return entity.Resource{}, "", errs.ErrNotFound
	}
	digest, err := entity.ProjectionSHA256(workspace)
	if err != nil {
		return entity.Resource{}, "", errs.ErrInternal
	}
	return workspace, digest, nil
}

func lockAgentRuntimeProfile(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	agentSpec entity.AgentSpec,
) (entity.Resource, string, error) {
	const prefix = "control-plane://runtime-profile/"
	if !strings.HasPrefix(agentSpec.RuntimeProfileRef, prefix) {
		return entity.Resource{}, "", errs.ErrStateConflict
	}
	profileID := strings.TrimPrefix(agentSpec.RuntimeProfileRef, prefix)
	if value.ValidateID(profileID) != nil || agentSpec.RuntimeProfileVersion == 0 ||
		!validSHA256Text(agentSpec.RuntimeProfileSHA256) {
		return entity.Resource{}, "", errs.ErrStateConflict
	}
	profile, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, profileID)
	if err != nil {
		return entity.Resource{}, "", err
	}
	digest, err := entity.ProjectionSHA256(profile)
	if err != nil {
		return entity.Resource{}, "", errs.ErrInternal
	}
	if profile.Kind != enum.KindRoleImageRecipe || profile.State != enum.StateActive ||
		profile.OwnerActorID != principal.ActorID || profile.Version != agentSpec.RuntimeProfileVersion ||
		digest != agentSpec.RuntimeProfileSHA256 {
		return entity.Resource{}, "", errs.ErrStateConflict
	}
	return profile, digest, nil
}
