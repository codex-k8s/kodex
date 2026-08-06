package resource

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

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
		value.ValidateStableKey(input.ProviderPoolStableKey) != nil ||
		!validExternalRefText(input.RuntimeSelectionRef) ||
		input.RuntimeSelectionVersion == 0 || !validSHA256Text(input.RuntimeSelectionSHA256) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity                commandIdentity
		ScheduleID              string
		ExpectedVersion         uint64
		AgentStableKey          string
		InstructionSetStableKey string
		RuntimeSelectionRef     string
		RuntimeSelectionVersion uint64
		RuntimeSelectionSHA256  string
		ProviderPoolStableKey   string
	}{identity(input.Principal), input.ScheduleID, input.ExpectedVersion, input.AgentStableKey,
		input.InstructionSetStableKey, input.RuntimeSelectionRef, input.RuntimeSelectionVersion,
		input.RuntimeSelectionSHA256, input.ProviderPoolStableKey})
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
		if !agentOK || !instructionOK || instructionSpec.VersionState != "PUBLISHED" ||
			agentSpec.InstructionSetID != instruction.ID ||
			agentSpec.InstructionSetVersion != instruction.Version ||
			agentSpec.ProviderPoolID != pool.ID || agentSpec.ProviderPoolVersion != pool.Version ||
			agentSpec.RuntimeProfileRef != input.RuntimeSelectionRef ||
			agentSpec.RuntimeProfileVersion != input.RuntimeSelectionVersion ||
			agentSpec.RuntimeProfileSHA256 != input.RuntimeSelectionSHA256 {
			return entity.Resource{}, errs.ErrStateConflict
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
		spec.TargetResourceID, spec.TargetKind, spec.TargetVersion = agentID, enum.KindAgent, agentVersion
		spec.PromptProfileID, spec.PromptRevision, spec.RuntimeRevisionID = "", 0, ""
		spec.AgentID, spec.AgentVersion, spec.AgentSHA256 = agentID, agentVersion, agentSHA
		spec.InstructionSetID, spec.InstructionSetVersion, spec.InstructionSetSHA256 =
			instructionID, instructionVersion, instructionSHA
		spec.ProviderPoolID, spec.ProviderPoolVersion, spec.ProviderPoolSHA256 = poolID, poolVersion, poolSHA
		spec.RuntimeSelectionRef, spec.RuntimeSelectionVersion, spec.RuntimeSelectionSHA256 =
			input.RuntimeSelectionRef, input.RuntimeSelectionVersion, input.RuntimeSelectionSHA256
		promptArtifact, artifactErr := service.requireCleanArtifact(ctx, tx, input.Principal, spec.PromptArtifactID)
		if artifactErr != nil {
			return entity.Resource{}, artifactErr
		}
		spec.EffectiveInputSHA, err = targetScheduleEffectiveInput(spec, promptArtifact.SHA256)
		if err != nil || spec.Validate() != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		now := service.now().UTC().Truncate(time.Microsecond)
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
		PromptArtifactSHA256                        string
		TargetType, PlaybookRef                     string
		PlaybookVersion                             uint64
		SessionPolicy, RoomID                       string
	}{spec.AgentID, spec.AgentSHA256, spec.AgentVersion,
		spec.InstructionSetID, spec.InstructionSetSHA256, spec.InstructionSetVersion,
		spec.RuntimeSelectionRef, spec.RuntimeSelectionSHA256, spec.RuntimeSelectionVersion,
		spec.ProviderPoolID, spec.ProviderPoolSHA256, spec.ProviderPoolVersion,
		promptArtifactSHA256,
		spec.TargetType, spec.PlaybookRef, spec.PlaybookVersion, spec.SessionPolicy, spec.RoomID})
}

func validExternalRefText(reference string) bool {
	return len(reference) >= 3 && len(reference) <= 1024
}
