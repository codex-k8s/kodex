package resource

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	ownerSchedulePresetRevision   = uint64(1)
	ownerScheduleDefaultsRevision = uint64(1)
	ownerSchedulePromptLease      = 30 * time.Second
	ownerSchedulePromptRPCTimeout = 15 * time.Second
)

func ownerScheduleDefaults() (OwnerScheduleDefaults, error) {
	defaults := OwnerScheduleDefaults{
		Revision: ownerScheduleDefaultsRevision, Calendar: "GREGORIAN", OverlapPolicy: "FORBID",
		MisfirePolicy: "RUN_ONCE", DeliveryPolicy: "AT_LEAST_ONCE", MaximumAttempts: 3,
		InitialBackoff: 30 * time.Second, MaximumBackoff: 5 * time.Minute, DeadLetterAfter: 24 * time.Hour,
		SessionPolicy: "NEW", NotificationPolicy: "ON_ACTION_OR_FAILURE",
		MaximumExecutionDuration: time.Hour, Coalesce: true,
	}
	var err error
	defaults.SHA256, err = canonicalHash(struct {
		Revision                                       uint64
		Calendar, Overlap, Misfire, Delivery           string
		Attempts                                       uint32
		Initial, Maximum, DeadLetter, MaximumExecution time.Duration
		Session, Notification                          string
		Coalesce                                       bool
	}{defaults.Revision, defaults.Calendar, defaults.OverlapPolicy, defaults.MisfirePolicy,
		defaults.DeliveryPolicy, defaults.MaximumAttempts, defaults.InitialBackoff, defaults.MaximumBackoff,
		defaults.DeadLetterAfter, defaults.MaximumExecutionDuration, defaults.SessionPolicy,
		defaults.NotificationPolicy, defaults.Coalesce})
	return defaults, err
}

func ownerSchedulePresets() ([]OwnerSchedulePreset, error) {
	presets := []OwnerSchedulePreset{
		{Key: "hourly", DisplayName: "Каждый час", Description: "Запуск в начале каждого часа", Cron: "0 * * * *", Revision: ownerSchedulePresetRevision},
		{Key: "daily", DisplayName: "Каждый день", Description: "Ежедневный запуск в 09:00", Cron: "0 9 * * *", Revision: ownerSchedulePresetRevision},
		{Key: "weekly", DisplayName: "Каждую неделю", Description: "Запуск по понедельникам в 09:00", Cron: "0 9 * * 1", Revision: ownerSchedulePresetRevision},
	}
	for index := range presets {
		digest, err := canonicalHash(struct {
			Key, DisplayName, Description, Cron string
			Revision                            uint64
		}{presets[index].Key, presets[index].DisplayName, presets[index].Description,
			presets[index].Cron, presets[index].Revision})
		if err != nil {
			return nil, err
		}
		presets[index].SHA256 = digest
	}
	return presets, nil
}

func buildOwnerScheduleSpec(selection OwnerScheduleSelection) (entity.ScheduleSpec, error) {
	defaults, err := ownerScheduleDefaults()
	if err != nil {
		return entity.ScheduleSpec{}, errs.ErrInternal
	}
	presets, err := ownerSchedulePresets()
	if err != nil {
		return entity.ScheduleSpec{}, errs.ErrInternal
	}
	index := slices.IndexFunc(presets, func(item OwnerSchedulePreset) bool { return item.Key == selection.PresetKey })
	if index < 0 || selection.Timezone == "" {
		return entity.ScheduleSpec{}, errs.ErrInvalidInput
	}
	preset := presets[index]
	if selection.Overrides.Present["cron"] && selection.Overrides.Present["interval"] {
		return entity.ScheduleSpec{}, errs.ErrInvalidInput
	}
	spec := entity.ScheduleSpec{
		Cron: preset.Cron, Timezone: selection.Timezone, Calendar: defaults.Calendar,
		OverlapPolicy: defaults.OverlapPolicy, MisfirePolicy: defaults.MisfirePolicy,
		DeliveryPolicy: defaults.DeliveryPolicy, MaximumAttempts: defaults.MaximumAttempts,
		InitialBackoff: defaults.InitialBackoff, MaximumBackoff: defaults.MaximumBackoff,
		DeadLetterAfter: defaults.DeadLetterAfter, SessionPolicy: defaults.SessionPolicy,
		NotificationPolicy: defaults.NotificationPolicy, MaximumExecutionDuration: defaults.MaximumExecutionDuration,
		Coalesce: defaults.Coalesce, OwnerPresetKey: preset.Key, OwnerPresetRevision: preset.Revision,
		OwnerPresetSHA256: preset.SHA256, OwnerDefaultsRevision: defaults.Revision,
		OwnerDefaultsSHA256: defaults.SHA256,
	}
	applyOwnerScheduleOverrides(&spec, selection.Overrides)
	if validateOwnerScheduleBehavior(spec) != nil {
		return entity.ScheduleSpec{}, errs.ErrInvalidInput
	}
	return spec, nil
}

func validateOwnerScheduleBehavior(spec entity.ScheduleSpec) error {
	if (spec.Cron == "") == (spec.Interval == 0) || len(spec.Cron) > 128 ||
		(spec.Interval != 0 && (spec.Interval < time.Minute || spec.Interval > 365*24*time.Hour)) ||
		(spec.Calendar != "GREGORIAN" && spec.Calendar != "BUSINESS") ||
		(spec.OverlapPolicy != "FORBID" && spec.OverlapPolicy != "SKIP" && spec.OverlapPolicy != "QUEUE") ||
		(spec.MisfirePolicy != "SKIP" && spec.MisfirePolicy != "RUN_ONCE" &&
			spec.MisfirePolicy != "CATCH_UP" && spec.MisfirePolicy != "WITHIN_GRACE") ||
		(spec.DeliveryPolicy != "AT_LEAST_ONCE" && spec.DeliveryPolicy != "EXACTLY_ONCE_EFFECT") ||
		spec.MaximumAttempts == 0 || spec.MaximumAttempts > 100 || spec.InitialBackoff < time.Second ||
		spec.MaximumBackoff < spec.InitialBackoff || spec.MaximumBackoff > 24*time.Hour ||
		spec.DeadLetterAfter < spec.MaximumBackoff || spec.DeadLetterAfter > 30*24*time.Hour ||
		(spec.SessionPolicy != "NEW" && spec.SessionPolicy != "PERSISTENT" && spec.SessionPolicy != "ROLLING") ||
		(spec.NotificationPolicy != "ALWAYS" && spec.NotificationPolicy != "ON_ACTION" &&
			spec.NotificationPolicy != "ON_FAILURE" && spec.NotificationPolicy != "ON_ACTION_OR_FAILURE" &&
			spec.NotificationPolicy != "AUDIT_ONLY") ||
		spec.MaximumExecutionDuration < time.Minute || spec.MaximumExecutionDuration > 24*time.Hour ||
		(spec.OverlapPolicy == "QUEUE") == spec.Coalesce ||
		(spec.MisfirePolicy == "WITHIN_GRACE" &&
			(spec.MisfireGrace < time.Second || spec.MisfireGrace > 24*time.Hour)) ||
		(spec.MisfirePolicy != "WITHIN_GRACE" && spec.MisfireGrace != 0) {
		return errs.ErrInvalidInput
	}
	_, err := firstScheduleRun(spec, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return err
}

// BuildOwnerScheduleSpec материализует server-authored preset/defaults без transport defaults.
func BuildOwnerScheduleSpec(selection OwnerScheduleSelection) (entity.ScheduleSpec, error) {
	return buildOwnerScheduleSpec(selection)
}

func ensureOwnerScheduleMetadata(spec *entity.ScheduleSpec) error {
	if spec.OwnerPresetKey != "" {
		return nil
	}
	defaults, err := ownerScheduleDefaults()
	if err != nil {
		return errs.ErrInternal
	}
	digest, err := canonicalHash(struct {
		Key      string
		Revision uint64
		Cron     string
		Interval time.Duration
	}{"custom", ownerSchedulePresetRevision, spec.Cron, spec.Interval})
	if err != nil {
		return errs.ErrInternal
	}
	spec.OwnerPresetKey, spec.OwnerPresetRevision, spec.OwnerPresetSHA256 = "custom", ownerSchedulePresetRevision, digest
	spec.OwnerDefaultsRevision, spec.OwnerDefaultsSHA256 = defaults.Revision, defaults.SHA256
	spec.AdvancedOverrides = []string{"calendar", "coalesce", "dead_letter_after", "delivery_policy",
		"initial_backoff", "maximum_attempts", "maximum_backoff", "maximum_execution_duration",
		"misfire_policy", "notification_policy", "overlap_policy", "session_policy"}
	if spec.Cron != "" {
		spec.AdvancedOverrides = append(spec.AdvancedOverrides, "cron")
	} else {
		spec.AdvancedOverrides = append(spec.AdvancedOverrides, "interval")
	}
	if spec.MisfireGrace != 0 {
		spec.AdvancedOverrides = append(spec.AdvancedOverrides, "misfire_grace")
	}
	slices.Sort(spec.AdvancedOverrides)
	return nil
}

func applyOwnerScheduleOverrides(spec *entity.ScheduleSpec, overrides OwnerScheduleOverrides) {
	applyString := func(key string, target *string, source string) {
		if overrides.Present[key] {
			*target = source
			spec.AdvancedOverrides = append(spec.AdvancedOverrides, key)
		}
	}
	applyDuration := func(key string, target *time.Duration, source time.Duration) {
		if overrides.Present[key] {
			*target = source
			spec.AdvancedOverrides = append(spec.AdvancedOverrides, key)
		}
	}
	applyString("cron", &spec.Cron, overrides.Cron)
	if overrides.Present["interval"] {
		spec.Cron = ""
		applyDuration("interval", &spec.Interval, overrides.Interval)
	}
	applyString("calendar", &spec.Calendar, overrides.Calendar)
	applyString("overlap_policy", &spec.OverlapPolicy, overrides.OverlapPolicy)
	applyString("misfire_policy", &spec.MisfirePolicy, overrides.MisfirePolicy)
	applyDuration("misfire_grace", &spec.MisfireGrace, overrides.MisfireGrace)
	applyString("delivery_policy", &spec.DeliveryPolicy, overrides.DeliveryPolicy)
	if overrides.Present["maximum_attempts"] {
		spec.MaximumAttempts = overrides.MaximumAttempts
		spec.AdvancedOverrides = append(spec.AdvancedOverrides, "maximum_attempts")
	}
	applyDuration("initial_backoff", &spec.InitialBackoff, overrides.InitialBackoff)
	applyDuration("maximum_backoff", &spec.MaximumBackoff, overrides.MaximumBackoff)
	applyDuration("dead_letter_after", &spec.DeadLetterAfter, overrides.DeadLetterAfter)
	applyString("session_policy", &spec.SessionPolicy, overrides.SessionPolicy)
	applyString("notification_policy", &spec.NotificationPolicy, overrides.NotificationPolicy)
	applyDuration("maximum_execution_duration", &spec.MaximumExecutionDuration, overrides.MaximumExecutionDuration)
	if overrides.Present["coalesce"] {
		spec.Coalesce = overrides.Coalesce
		spec.AdvancedOverrides = append(spec.AdvancedOverrides, "coalesce")
	}
	slices.Sort(spec.AdvancedOverrides)
}

func (service *Service) prepareOwnerSchedulePrompt(
	ctx context.Context,
	input ownerSchedulePromptPreparationInput,
	prompt OwnerSchedulePromptInput,
) (OwnerSchedulePromptInput, error) {
	switch prompt.Kind {
	case "INLINE":
		if strings.TrimSpace(prompt.InlineMarkdown) == "" || len([]byte(prompt.InlineMarkdown)) > 262144 ||
			service.instructionObjects == nil {
			return OwnerSchedulePromptInput{}, errs.ErrInvalidInput
		}
		digest := hashString(prompt.InlineMarkdown)
		if input.RequestSHA256 == "" || input.Action == "" {
			return OwnerSchedulePromptInput{}, errs.ErrInvalidInput
		}
		var preparation domainrepo.SchedulePromptPreparation
		var claimed bool
		err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: input.Principal.OrganizationID,
			ProjectID: input.Principal.ProjectID, ActorID: input.Principal.ActorID}, func(tx domainrepo.Transaction) error {
			preparations, ok := tx.(domainrepo.SchedulePromptPreparationTransaction)
			if !ok {
				return errs.ErrInternal
			}
			keyHash := hashString(input.IdempotencyKey)
			now, timeErr := tx.CurrentTime(ctx)
			if timeErr != nil {
				return timeErr
			}
			now = now.UTC().Truncate(time.Microsecond)
			existing, existingErr := preparations.GetSchedulePromptPreparation(ctx, keyHash)
			if existingErr == nil {
				if existing.RequestSHA256 != input.RequestSHA256 || existing.Action != input.Action ||
					existing.TargetID != input.ScheduleID || existing.ExpectedVersion != input.ExpectedVersion ||
					existing.ObjectKey != "schedule-prompts/"+digest+".md" {
					return errs.ErrStateConflict
				}
				if existing.State == "READY" || existing.State == "CONSUMED" {
					preparation = existing
					claimed = false
					return nil
				}
				semanticSHA256, preflightErr := service.validateOwnerSchedulePromptPreparation(ctx, tx, input)
				if preflightErr != nil {
					return preflightErr
				}
				if semanticSHA256 != existing.SemanticSHA256 {
					return errs.ErrStateConflict
				}
				preparation, claimed, existingErr = preparations.ReserveSchedulePromptPreparation(ctx,
					domainrepo.SchedulePromptPreparation{
						OrganizationID: existing.OrganizationID, ProjectID: existing.ProjectID,
						OwnerActorID: existing.OwnerActorID, KeyHash: existing.KeyHash,
						RequestSHA256: existing.RequestSHA256, SemanticSHA256: existing.SemanticSHA256,
						Action: existing.Action, TargetID: existing.TargetID, ExpectedVersion: existing.ExpectedVersion,
						ObjectKey: existing.ObjectKey, State: "WRITING", Generation: existing.Generation,
						LeaseExpiresAt: now.Add(ownerSchedulePromptLease), CreatedAt: now, UpdatedAt: now,
					})
				return existingErr
			}
			if !errors.Is(existingErr, errs.ErrNotFound) {
				return existingErr
			}
			semanticSHA256, preflightErr := service.validateOwnerSchedulePromptPreparation(ctx, tx, input)
			if preflightErr != nil {
				return preflightErr
			}
			preparation, claimed, preflightErr = preparations.ReserveSchedulePromptPreparation(ctx,
				domainrepo.SchedulePromptPreparation{
					OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
					OwnerActorID: input.Principal.ActorID, KeyHash: keyHash,
					RequestSHA256: input.RequestSHA256, SemanticSHA256: semanticSHA256,
					Action: input.Action, TargetID: input.ScheduleID, ExpectedVersion: input.ExpectedVersion,
					ObjectKey: "schedule-prompts/" + digest + ".md", State: "WRITING", Generation: 1,
					LeaseExpiresAt: now.Add(ownerSchedulePromptLease), CreatedAt: now, UpdatedAt: now,
				})
			return preflightErr
		})
		if err != nil {
			return OwnerSchedulePromptInput{}, err
		}
		if !claimed {
			if preparation.State != "READY" && preparation.State != "CONSUMED" {
				return OwnerSchedulePromptInput{}, errs.ErrUnavailable
			}
			prompt.Object = domainobjectstore.Object{Reference: preparation.ObjectReference,
				VersionID: preparation.ObjectVersionID, SHA256: preparation.ObjectSHA256,
				Size: preparation.ObjectSize, MediaType: preparation.ObjectMediaType}
			prompt.PreparationKeyHash, prompt.PreparationGeneration = preparation.KeyHash, preparation.Generation
			return prompt, nil
		}
		objectContext, cancelObject := context.WithTimeout(ctx, ownerSchedulePromptRPCTimeout)
		object, err := service.instructionObjects.Put(objectContext, input.Principal.ProjectID,
			preparation.ObjectKey, []byte(prompt.InlineMarkdown), "text/markdown", digest)
		cancelObject()
		if err != nil || object.Reference == "" || object.VersionID == "" || object.SHA256 != digest ||
			object.Size != uint64(len([]byte(prompt.InlineMarkdown))) || object.MediaType != "text/markdown" {
			_ = service.finishOwnerSchedulePromptPreparation(ctx, input.Principal, preparation, object, true)
			return OwnerSchedulePromptInput{}, errs.ErrUnavailable
		}
		if err := service.finishOwnerSchedulePromptPreparation(ctx, input.Principal, preparation, object, false); err != nil {
			return OwnerSchedulePromptInput{}, err
		}
		prompt.Object = object
		prompt.PreparationKeyHash, prompt.PreparationGeneration = preparation.KeyHash, preparation.Generation
		return prompt, nil
	case "SELECTOR":
		if value.ValidateName(prompt.ArtifactName) != nil {
			return OwnerSchedulePromptInput{}, errs.ErrInvalidInput
		}
		return prompt, nil
	default:
		return OwnerSchedulePromptInput{}, errs.ErrInvalidInput
	}
}

type ownerSchedulePromptPreparationInput struct {
	Principal                                                      value.Principal
	IdempotencyKey, RequestSHA256, Action, ScheduleID              string
	AgentStableKey, InstructionSetStableKey, ProviderPoolStableKey string
	RoomStableKey, SessionPolicy                                   string
	ExpectedVersion                                                uint64
}

func (service *Service) validateOwnerSchedulePromptPreparation(
	ctx context.Context,
	tx domainrepo.Transaction,
	input ownerSchedulePromptPreparationInput,
) (string, error) {
	protected, ok := tx.(domainrepo.ProtectedTransaction)
	if !ok {
		return "", errs.ErrInternal
	}
	if input.Action == "update" {
		schedule, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.ScheduleID)
		if err != nil {
			return "", err
		}
		spec, specOK := schedule.Spec.(entity.ScheduleSpec)
		if !specOK || schedule.Kind != enum.KindSchedule || schedule.OwnerActorID != input.Principal.ActorID {
			return "", errs.ErrNotFound
		}
		if schedule.Version != input.ExpectedVersion {
			return "", errs.ErrVersionMismatch
		}
		if (schedule.State != enum.StateActive && schedule.State != enum.StatePaused) ||
			spec.SessionPolicy != input.SessionPolicy {
			return "", errs.ErrStateConflict
		}
		open, err := tx.HasOpenScheduleOccurrence(ctx, schedule.OrganizationID, schedule.ProjectID, schedule.ID)
		if err != nil || open {
			if err != nil {
				return "", err
			}
			return "", errs.ErrStateConflict
		}
	}
	workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
	if err != nil {
		return "", err
	}
	roomID := ""
	if input.RoomStableKey != "" {
		room, roomErr := protected.GetByStableKeyForUpdate(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, enum.KindChat, input.RoomStableKey)
		if roomErr != nil {
			return "", roomErr
		}
		if room.State != enum.StateActive || room.OwnerActorID != input.Principal.ActorID {
			return "", errs.ErrNotFound
		}
		roomID = room.ID
	}
	agent, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindAgent, input.AgentStableKey)
	if err != nil {
		return "", err
	}
	instruction, err := requireProtectedStable(ctx, protected, input.Principal,
		enum.KindInstructionSet, input.InstructionSetStableKey)
	if err != nil {
		return "", err
	}
	pool, err := requireProtectedStable(ctx, protected, input.Principal, enum.KindProviderPool, input.ProviderPoolStableKey)
	if err != nil {
		return "", err
	}
	agentSpec, agentOK := agent.Spec.(entity.AgentSpec)
	instructionSpec, instructionOK := instruction.Spec.(entity.InstructionSetSpec)
	agentSHA, agentSHAErr := entity.ProjectionSHA256(agent)
	if !agentOK || !instructionOK || agentSHAErr != nil || !agentSpec.Enabled ||
		agent.State != enum.StateActive || instructionSpec.VersionState != "PUBLISHED" ||
		agentSpec.InstructionSetID != instruction.ID || agentSpec.InstructionSetVersion != instruction.Version ||
		agentSpec.ProviderPoolID != pool.ID || agentSpec.ProviderPoolVersion != pool.Version {
		return "", errs.ErrStateConflict
	}
	if _, _, err := lockAgentRuntimeProfile(ctx, tx, input.Principal, agentSpec); err != nil {
		return "", err
	}
	assignment, err := lockActiveAgentAssignment(ctx, tx, input.Principal, agent.ID, agent.Version,
		agentSHA, workspace.Version, workspaceSHA, roomID)
	if err != nil {
		return "", err
	}
	tuples := make([]string, 0, 5)
	for _, item := range []entity.Resource{workspace, agent, instruction, pool, assignment} {
		digest, digestErr := entity.ProjectionSHA256(item)
		if digestErr != nil {
			return "", errs.ErrInternal
		}
		tuples = append(tuples, fmt.Sprintf("%s:%d:%s", item.ID, item.Version, digest))
	}
	return canonicalHash(struct {
		RequestSHA256 string
		Tuples        []string
	}{input.RequestSHA256, tuples})
}

func (service *Service) finishOwnerSchedulePromptPreparation(
	ctx context.Context,
	principal value.Principal,
	preparation domainrepo.SchedulePromptPreparation,
	object domainobjectstore.Object,
	ambiguous bool,
) error {
	return service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		preparations, ok := tx.(domainrepo.SchedulePromptPreparationTransaction)
		if !ok {
			return errs.ErrInternal
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		preparation.UpdatedAt = now.UTC().Truncate(time.Microsecond)
		if ambiguous {
			return preparations.MarkSchedulePromptPreparationAmbiguous(ctx, preparation)
		}
		preparation.ObjectReference, preparation.ObjectVersionID = object.Reference, object.VersionID
		preparation.ObjectSHA256, preparation.ObjectSize = object.SHA256, object.Size
		preparation.ObjectMediaType = object.MediaType
		return preparations.CompleteSchedulePromptPreparation(ctx, preparation)
	})
}

func schedulePromptArtifactID(projectID, digest string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:schedule-prompt:"+projectID+":"+digest)).String()
}

func (service *Service) resolveOwnerSchedulePrompt(
	ctx context.Context,
	tx domainrepo.Transaction,
	protected domainrepo.ProtectedTransaction,
	principal value.Principal,
	prompt OwnerSchedulePromptInput,
	now time.Time,
) (entity.Resource, entity.ArtifactSpec, error) {
	if prompt.Kind == "SELECTOR" {
		artifact, err := protected.GetByNameForUpdate(ctx, principal.OrganizationID, principal.ProjectID,
			enum.KindArtifact, prompt.ArtifactName)
		if err != nil {
			return entity.Resource{}, entity.ArtifactSpec{}, err
		}
		spec, ok := artifact.Spec.(entity.ArtifactSpec)
		if !ok || artifact.OwnerActorID != principal.ActorID || artifact.State != enum.StateActive ||
			spec.Direction != "INPUT" || spec.MediaType != "text/markdown" || spec.ScanStatus != "CLEAN" {
			return entity.Resource{}, entity.ArtifactSpec{}, errs.ErrStateConflict
		}
		return artifact, spec, nil
	}
	if prompt.Kind != "INLINE" || prompt.Object.Reference == "" ||
		prompt.PreparationKeyHash == "" || prompt.PreparationGeneration == 0 {
		return entity.Resource{}, entity.ArtifactSpec{}, errs.ErrInvalidInput
	}
	id := schedulePromptArtifactID(principal.ProjectID, prompt.Object.SHA256)
	existing, err := tx.GetForUpdate(ctx, principal.OrganizationID, principal.ProjectID, id)
	if err == nil {
		spec, ok := existing.Spec.(entity.ArtifactSpec)
		if !ok || existing.OwnerActorID != principal.ActorID || existing.State != enum.StateActive ||
			spec.StorageRef != prompt.Object.Reference || spec.SHA256 != prompt.Object.SHA256 ||
			spec.SizeBytes != prompt.Object.Size || spec.MediaType != prompt.Object.MediaType || spec.ScanStatus != "CLEAN" {
			return entity.Resource{}, entity.ArtifactSpec{}, errs.ErrStateConflict
		}
		return existing, spec, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return entity.Resource{}, entity.ArtifactSpec{}, err
	}
	evidence, err := canonicalHash(struct {
		Reference, VersionID, SHA256, Validator string
		Size                                    uint64
	}{prompt.Object.Reference, prompt.Object.VersionID, prompt.Object.SHA256,
		"control-plane-schedule-prompt-validator-v1", prompt.Object.Size})
	if err != nil {
		return entity.Resource{}, entity.ArtifactSpec{}, errs.ErrInternal
	}
	spec := entity.ArtifactSpec{ArtifactKind: "schedule-prompt", Direction: "INPUT",
		StorageRef: prompt.Object.Reference, SizeBytes: prompt.Object.Size, MediaType: prompt.Object.MediaType,
		SHA256: prompt.Object.SHA256, ScanStatus: "CLEAN",
		RetentionPolicyRef: "control-plane://retention/schedule-prompt", ScanPolicyRevision: 1,
		ScanEvidenceSHA256: evidence, ScannerWorkloadID: "control-plane-schedule-prompt-validator", ScannedAt: now}
	artifact, err := entity.New(id, principal.OrganizationID, principal.ProjectID, principal.ProjectID,
		principal.ActorID, enum.KindArtifact, "Schedule prompt "+prompt.Object.SHA256[:12], spec, now)
	if err != nil {
		return entity.Resource{}, entity.ArtifactSpec{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, artifact); err != nil {
		return entity.Resource{}, entity.ArtifactSpec{}, err
	}
	if err := service.appendMutationRecords(ctx, tx, principal, "materialize_schedule_prompt", artifact); err != nil {
		return entity.Resource{}, entity.ArtifactSpec{}, err
	}
	return artifact, spec, nil
}
