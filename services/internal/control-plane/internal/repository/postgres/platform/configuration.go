package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// changeSchedule handles typed schedule create/update/enable/archive/delete inside the caller
// transaction. It normalizes timing/content, pins eligible project targets and immutable
// revisions, checks locked OCC/state and cancels claimed occurrences on
// update/archive/delete/disable. Delete requires ARCHIVED; the returned outcome carries
// SCHEDULE_CHANGED metadata without committing or launching a run.
// changeSchedule обрабатывает типизированные schedule create/update/enable/archive/delete в
// транзакции caller. Нормализует время/содержимое, закрепляет допустимые project targets и
// immutable revisions, проверяет locked OCC/state и отменяет claimed occurrences при
// update/archive/delete/disable. Delete требует ARCHIVED; возвращаемый outcome несёт
// SCHEDULE_CHANGED metadata без commit или запуска run.
func (repository *Repository) changeSchedule(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ScheduleInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	payload.Name = strings.TrimSpace(payload.Name)
	if input.Kind == command.CreateSchedule {
		normalized, err := normalizeScheduleInput(payload, time.Now().UTC())
		if err != nil {
			return commandOutcome{}, err
		}
		payload.CronExpression = normalized.CronExpression
		payload.TimeOfDay = normalized.TimeOfDay
		payload.DayOfWeek = normalized.DayOfWeek
		applyNormalizedSchedulePolicies(&payload, normalized)
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		if payload.TargetVersion, payload.TargetDigest, err = repository.validateScheduleTarget(ctx, tx, scope.organizationID, projectID, payload.Target); err != nil {
			return commandOutcome{}, err
		}
		ref, _ := newRef("sch")
		revisionRef, _ := newRef("srev")
		scheduleID, revisionID := uuid.NewString(), uuid.NewString()
		revisionDigest, err := scheduleRevisionDigest(payload)
		if err != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		var item entity.Schedule
		var next *time.Time
		var revisionCreatedAt time.Time
		err = tx.QueryRow(ctx, queryConfigurationChangescheduleInsertSchedulesRefProjectIdTargetType,
			scheduleID, ref, scope.organizationID, projectID, payload.Name, payload.Target.Type,
			payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input),
			payload.SessionPolicy, payload.NotificationPolicy, payload.DSTGapPolicy, payload.DSTFoldPolicy,
			payload.MisfirePolicy, payload.OverlapPolicy, payload.TargetVersion, payload.TargetDigest,
			payload.AutomationText, asJSON(payload.PromptInputs), normalized.Next, scope.actorID, revisionID,
		).Scan(&item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone,
			&item.SessionPolicy, &item.NotificationPolicy, &item.State, &item.Enabled, &item.Version,
			&next, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt, &item.DSTGapPolicy,
			&item.DSTFoldPolicy, &item.MisfirePolicy, &item.OverlapPolicy, &item.TargetVersion,
			&item.TargetDigest, &item.AutomationText, &item.PromptInputs)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if err = tx.QueryRow(ctx, queryConfigurationChangescheduleInsertScheduleRevision,
			revisionID, revisionRef, scope.organizationID, scheduleID, int64(1), payload.Name,
			payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone,
			asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, revisionDigest, scope.actorID,
			payload.DSTGapPolicy, payload.DSTFoldPolicy, payload.MisfirePolicy, payload.OverlapPolicy,
			payload.TargetVersion, payload.TargetDigest, payload.AutomationText, asJSON(payload.PromptInputs),
		).Scan(&revisionCreatedAt); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.ProjectRef = payload.ProjectRef
		item.Target = payload.Target
		item.Input = payload.Input
		item.PromptInputs = payload.PromptInputs
		item.TimeOfDay = payload.TimeOfDay
		item.DayOfWeek = payload.DayOfWeek
		item.NextRunAt = next
		item.CurrentRevision = entity.ScheduleRevision{
			Ref: revisionRef, Revision: 1, Digest: revisionDigest, Name: payload.Name,
			Target: payload.Target, Preset: payload.Preset, CronExpression: payload.CronExpression,
			Timezone: payload.Timezone, Input: payload.Input, SessionPolicy: payload.SessionPolicy,
			NotificationPolicy: payload.NotificationPolicy, CreatedAt: revisionCreatedAt,
			DSTGapPolicy: payload.DSTGapPolicy, DSTFoldPolicy: payload.DSTFoldPolicy,
			MisfirePolicy: payload.MisfirePolicy, OverlapPolicy: payload.OverlapPolicy,
			TargetVersion: payload.TargetVersion, TargetDigest: payload.TargetDigest,
			AutomationText: payload.AutomationText, PromptInputs: payload.PromptInputs,
		}
		item.NextActions = scheduleActions(item, true)
		return scheduleCommandOutcome(item, projectID, payload.ProjectRef, "i18n:SCHEDULE_CREATED"), nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var scheduleID, projectID, projectRef, storedPreset, storedCron, storedTimezone, storedState string
	var storedVersion int64
	var currentRevision entity.ScheduleRevision
	var currentRevisionInput, currentRevisionPromptInputs []byte
	if err := tx.QueryRow(ctx, queryConfigurationChangescheduleSelectScheduleForUpdate, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "schedule_ref": payload.Ref,
	}).Scan(
		&scheduleID, &projectID, &projectRef, &storedPreset, &storedCron, &storedTimezone, &storedState, &storedVersion,
		&currentRevision.Ref, &currentRevision.Revision, &currentRevision.Digest, &currentRevision.Name,
		&currentRevision.Target.Type, &currentRevision.Target.Ref, &currentRevision.Preset,
		&currentRevision.CronExpression, &currentRevision.Timezone, &currentRevisionInput,
		&currentRevision.SessionPolicy, &currentRevision.NotificationPolicy, &currentRevision.DSTGapPolicy,
		&currentRevision.DSTFoldPolicy, &currentRevision.MisfirePolicy, &currentRevision.OverlapPolicy,
		&currentRevision.TargetVersion, &currentRevision.TargetDigest, &currentRevision.AutomationText,
		&currentRevisionPromptInputs, &currentRevision.CreatedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if json.Unmarshal(currentRevisionInput, &currentRevision.Input) != nil ||
		json.Unmarshal(currentRevisionPromptInputs, &currentRevision.PromptInputs) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if storedVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	item := entity.Schedule{CurrentRevision: currentRevision}
	if input.Kind == command.DeleteSchedule {
		if storedState != "ARCHIVED" {
			return commandOutcome{}, errs.ErrConflict
		}
		changedProjectID, changedProjectRef, changeErr := finishScheduleLifecycleMutation(ctx, tx, scheduleID, tx.QueryRow(ctx, queryConfigurationChangescheduleDeleteSchedule, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID, "schedule_ref": payload.Ref,
			"expected_version": *input.Mutation.ExpectedVersion,
		}), &item)
		if changeErr != nil {
			return commandOutcome{}, changeErr
		}
		projectID, projectRef = changedProjectID, changedProjectRef
		return scheduleCommandOutcome(item, projectID, projectRef, "i18n:SCHEDULE_DELETED"), nil
	}
	if storedState == "ARCHIVED" {
		return commandOutcome{}, errs.ErrConflict
	}
	summary := "i18n:SCHEDULE_UPDATED"
	if input.Kind == command.ArchiveSchedule {
		changedProjectID, changedProjectRef, changeErr := finishScheduleLifecycleMutation(ctx, tx, scheduleID, tx.QueryRow(ctx, queryConfigurationChangescheduleArchiveSchedule, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion), &item)
		if changeErr != nil {
			return commandOutcome{}, changeErr
		}
		projectID, projectRef = changedProjectID, changedProjectRef
		item.NextActions = []string{"OPEN"}
		summary = "i18n:SCHEDULE_ARCHIVED"
	} else if input.Kind == command.UpdateSchedule {
		normalized, normalizeErr := normalizeScheduleInput(payload, time.Now().UTC())
		if normalizeErr != nil {
			return commandOutcome{}, normalizeErr
		}
		var targetErr error
		if payload.TargetVersion, payload.TargetDigest, targetErr = repository.validateScheduleTarget(ctx, tx, scope.organizationID, projectID, payload.Target); targetErr != nil {
			return commandOutcome{}, targetErr
		}
		payload.CronExpression = normalized.CronExpression
		payload.TimeOfDay = normalized.TimeOfDay
		payload.DayOfWeek = normalized.DayOfWeek
		applyNormalizedSchedulePolicies(&payload, normalized)
		revisionRef, _ := newRef("srev")
		revisionID := uuid.NewString()
		revisionDigest, digestErr := scheduleRevisionDigest(payload)
		if digestErr != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		var revisionCreatedAt time.Time
		if revisionErr := tx.QueryRow(ctx, queryConfigurationChangescheduleInsertScheduleRevision,
			revisionID, revisionRef, scope.organizationID, scheduleID, currentRevision.Revision+1, payload.Name,
			payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone,
			asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, revisionDigest, scope.actorID,
			payload.DSTGapPolicy, payload.DSTFoldPolicy, payload.MisfirePolicy, payload.OverlapPolicy,
			payload.TargetVersion, payload.TargetDigest, payload.AutomationText, asJSON(payload.PromptInputs),
		).Scan(&revisionCreatedAt); revisionErr != nil {
			return commandOutcome{}, mapWriteError(revisionErr)
		}
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleUpdateSchedulesNameTargetTypeTargetRef, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, payload.DSTGapPolicy, payload.DSTFoldPolicy, payload.MisfirePolicy, payload.OverlapPolicy, payload.TargetVersion, payload.TargetDigest, payload.AutomationText, asJSON(payload.PromptInputs), normalized.Next, revisionID).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.State, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt, &item.DSTGapPolicy, &item.DSTFoldPolicy, &item.MisfirePolicy, &item.OverlapPolicy, &item.TargetVersion, &item.TargetDigest, &item.AutomationText, &item.PromptInputs)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, cancelErr := tx.Exec(ctx, queryConfigurationChangescheduleCancelClaimedOccurrences,
			pgx.StrictNamedArgs{"schedule_id": scheduleID}); cancelErr != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		item.Target = payload.Target
		item.Input = payload.Input
		item.PromptInputs = payload.PromptInputs
		item.TimeOfDay = payload.TimeOfDay
		item.DayOfWeek = payload.DayOfWeek
		item.CurrentRevision = entity.ScheduleRevision{
			Ref: revisionRef, Revision: currentRevision.Revision + 1, Digest: revisionDigest, Name: payload.Name,
			Target: payload.Target, Preset: payload.Preset, CronExpression: payload.CronExpression,
			Timezone: payload.Timezone, Input: payload.Input, SessionPolicy: payload.SessionPolicy,
			NotificationPolicy: payload.NotificationPolicy, CreatedAt: revisionCreatedAt,
			DSTGapPolicy: payload.DSTGapPolicy, DSTFoldPolicy: payload.DSTFoldPolicy,
			MisfirePolicy: payload.MisfirePolicy, OverlapPolicy: payload.OverlapPolicy,
			TargetVersion: payload.TargetVersion, TargetDigest: payload.TargetDigest,
			AutomationText: payload.AutomationText, PromptInputs: payload.PromptInputs,
		}
	} else {
		next, nextErr := scheduleservice.NextWithPolicy(scheduleservice.Spec{
			Preset: storedPreset, CronExpression: storedCron, Timezone: storedTimezone,
			DSTGapPolicy: currentRevision.DSTGapPolicy, DSTFoldPolicy: currentRevision.DSTFoldPolicy,
			MisfirePolicy: currentRevision.MisfirePolicy, OverlapPolicy: currentRevision.OverlapPolicy,
		}, time.Now().UTC())
		if nextErr != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleUpdateSchedulesEnabledVersionUpdatedAt, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled, next).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.State, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !payload.Enabled {
			if _, cancelErr := tx.Exec(ctx, queryConfigurationChangescheduleCancelClaimedOccurrences,
				pgx.StrictNamedArgs{"schedule_id": scheduleID}); cancelErr != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	}
	if input.Kind != command.ArchiveSchedule {
		item.ProjectRef = projectRef
		if displayErr := attachScheduleDisplay(&item); displayErr != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		item.NextActions = scheduleActions(item, true)
	}
	return scheduleCommandOutcome(item, projectID, projectRef, summary), nil
}

// scheduleRevisionDigest hashes the fixed typed schedule revision envelope as lowercase SHA-256,
// including target version/digest, policies and both input maps. It returns JSON marshal errors
// rather than silently hashing fallback bytes; identity and authority are checked separately.
// scheduleRevisionDigest хеширует фиксированный типизированный schedule revision envelope строчным
// SHA-256, включая target version/digest, policies и обе input maps. Возвращает ошибки JSON
// marshal вместо хеширования fallback bytes; identity и authority проверяются отдельно.
func scheduleRevisionDigest(payload command.ScheduleInput) (string, error) {
	encoded, err := json.Marshal(struct {
		Name, TargetType, TargetRef, Preset, CronExpression, Timezone             string
		DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy, AutomationText string
		TargetVersion                                                             int64
		TargetDigest                                                              string
		Input, PromptInputs                                                       map[string]any
		SessionPolicy, NotificationPolicy                                         string
	}{
		payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset,
		payload.CronExpression, payload.Timezone, payload.DSTGapPolicy, payload.DSTFoldPolicy,
		payload.MisfirePolicy, payload.OverlapPolicy, payload.AutomationText,
		payload.TargetVersion, payload.TargetDigest, payload.Input, payload.PromptInputs,
		payload.SessionPolicy, payload.NotificationPolicy,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// scheduleCommandOutcome projects current revision policies/content pins onto a copied schedule
// and builds audit/event routing. Empty/ACTIVE lifecycle state becomes ACTIVE or PAUSED according
// to Enabled; terminal lifecycle labels are preserved. It does not emit the event.
// scheduleCommandOutcome проецирует current revision policies/content pins в копию schedule и
// строит audit/event routing. Пустое/ACTIVE lifecycle state становится ACTIVE либо PAUSED по
// Enabled; terminal lifecycle labels сохраняются. Само событие не создаётся.
func scheduleCommandOutcome(item entity.Schedule, projectID, projectRef, summary string) commandOutcome {
	item.DSTGapPolicy, item.DSTFoldPolicy = item.CurrentRevision.DSTGapPolicy, item.CurrentRevision.DSTFoldPolicy
	item.MisfirePolicy, item.OverlapPolicy = item.CurrentRevision.MisfirePolicy, item.CurrentRevision.OverlapPolicy
	item.TargetVersion, item.TargetDigest = item.CurrentRevision.TargetVersion, item.CurrentRevision.TargetDigest
	item.AutomationText, item.PromptInputs = item.CurrentRevision.AutomationText, item.CurrentRevision.PromptInputs
	state := item.State
	if state == "" || state == "ACTIVE" {
		state = "PAUSED"
		if item.Enabled {
			state = "ACTIVE"
		}
	}
	return commandOutcome{
		result:                   command.Result{Schedule: &item},
		projectID:                projectID,
		projectRef:               projectRef,
		resourceKind:             "SCHEDULE",
		resourceRef:              item.Ref,
		summary:                  summary,
		platformEvent:            "SCHEDULE_CHANGED",
		platformAggregateVersion: item.Version,
		platformState:            state,
	}
}

// normalizeScheduleInput validates a local copy of schedule name, automation text, prompt inputs,
// target and session/notification policies, then normalizes timing after the supplied instant.
// Empty automation falls back to legacy input.task then name; only timing normalization is
// returned, so the caller separately applies content defaults.
// normalizeScheduleInput проверяет локальную копию schedule name, automation text, prompt inputs,
// target и session/notification policies, затем нормализует время после переданного момента.
// Пустой automation использует legacy input.task, затем name; возвращается только timing
// normalization, поэтому caller отдельно применяет content defaults.
func normalizeScheduleInput(payload command.ScheduleInput, after time.Time) (scheduleservice.Normalized, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	applyScheduleContentDefaults(&payload)
	if payload.Name == "" || len(payload.Name) > 160 || payload.AutomationText == "" || len(payload.AutomationText) > 32768 ||
		!validBoundedRunInput(payload.PromptInputs) || !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) || payload.Target.Ref == "" || !contains([]string{"NEW_EACH_RUN", "CONTINUE_ONE"}, payload.SessionPolicy) || !contains([]string{"CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"}, payload.NotificationPolicy) {
		return scheduleservice.Normalized{}, errs.ErrInvalid
	}
	normalized, err := scheduleservice.Normalize(scheduleservice.Spec{
		Preset: payload.Preset, CronExpression: payload.CronExpression, TimeOfDay: payload.TimeOfDay,
		DayOfWeek: payload.DayOfWeek, Timezone: payload.Timezone, DSTGapPolicy: payload.DSTGapPolicy,
		DSTFoldPolicy: payload.DSTFoldPolicy, MisfirePolicy: payload.MisfirePolicy,
		OverlapPolicy: payload.OverlapPolicy,
	}, after)
	if err != nil {
		return scheduleservice.Normalized{}, errs.ErrInvalid
	}
	return normalized, nil
}

// applyNormalizedSchedulePolicies copies normalized preset/timezone/DST/misfire/overlap policies
// into the payload and applies the same automation/prompt defaults used for validation.
// Cron/display fields are assigned by callers; no target eligibility or persistence occurs.
// applyNormalizedSchedulePolicies копирует нормализованные preset/timezone/DST/misfire/overlap
// policies в payload и применяет те же automation/prompt defaults, что при validation.
// Cron/display fields назначают callers; target eligibility или сохранение не выполняются.
func applyNormalizedSchedulePolicies(payload *command.ScheduleInput, normalized scheduleservice.Normalized) {
	payload.Preset = normalized.Preset
	payload.Timezone = normalized.Timezone
	payload.DSTGapPolicy = normalized.DSTGapPolicy
	payload.DSTFoldPolicy = normalized.DSTFoldPolicy
	payload.MisfirePolicy = normalized.MisfirePolicy
	payload.OverlapPolicy = normalized.OverlapPolicy
	applyScheduleContentDefaults(payload)
}

// validateScheduleTarget reads only a ready enabled nonsystem project agent or a published project
// workflow revision, returning the server version/digest. Unsupported kinds are Invalid, absent
// targets NotFound and read failure Unavailable; this does not launch or grant target authority.
// validateScheduleTarget читает только ready enabled nonsystem project agent либо published
// project workflow revision, возвращая серверные version/digest. Неподдерживаемый kind даёт
// Invalid, отсутствие target NotFound, ошибка чтения Unavailable; не запускает target и не выдаёт
// authority.
func (repository *Repository) validateScheduleTarget(ctx context.Context, tx pgx.Tx, organizationID, projectID string, target entity.RunTarget) (int64, string, error) {
	query := queryConfigurationChangescheduleSelectAgentTarget
	if target.Type == "WORKFLOW" {
		query = queryConfigurationChangescheduleSelectWorkflowTarget
	} else if target.Type != "AGENT" {
		return 0, "", errs.ErrInvalid
	}
	var id string
	var version int64
	var digest string
	if err := tx.QueryRow(ctx, query, organizationID, projectID, target.Ref).Scan(&id, &version, &digest); errors.Is(err, pgx.ErrNoRows) {
		return 0, "", errs.ErrNotFound
	} else if err != nil {
		return 0, "", errs.ErrUnavailable
	}
	return version, digest, nil
}

// attachScheduleDisplay derives display time/day from persisted preset/cron and mutates those
// fields only after successful parsing, returning the original display error.
// attachScheduleDisplay выводит отображаемые time/day из сохранённых preset/cron и меняет эти поля
// лишь после успешного разбора, возвращая исходную display error.
func attachScheduleDisplay(item *entity.Schedule) error {
	timeOfDay, dayOfWeek, err := scheduleservice.Display(item.Preset, item.CronExpression)
	if err != nil {
		return err
	}
	item.TimeOfDay = timeOfDay
	item.DayOfWeek = dayOfWeek
	return nil
}

// changeConnection routes specialized grant commands and otherwise handles typed connection
// creation, update/delete, credential projection, asynchronous test admission or enablement.
// Existing-connection paths first cancel/revoke write-back claims; disable also cancels
// interaction intents, grants and tests. Package pins/OCC/credential descriptors are validated
// without reading secret values or performing a provider health call here.
// changeConnection направляет специализированные grant commands и иначе обрабатывает
// типизированные connection creation, update/delete, credential projection, допуск асинхронного
// test либо enablement. Пути существующего connection сначала отменяют/отзывают write-back claims;
// disable также отменяет interaction intents, grants и tests. Package pins/OCC/credential
// descriptors проверяются без чтения secret values или health-вызова провайдера здесь.
func (repository *Repository) changeConnection(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	if input.Kind == command.ChangeIntegrationGrant || input.Kind == command.ChangeSystemAssistantIntegrationGrant || input.Kind == command.ChangeProjectAssistantIntegrationGrant {
		return repository.changeIntegrationGrant(ctx, tx, scope, input)
	}
	payload, ok := input.Payload.(command.ConnectionInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateConnection {
		payload.Name = strings.TrimSpace(payload.Name)
		if payload.Name == "" || len(payload.Name) > 160 {
			return commandOutcome{}, errs.ErrInvalid
		}
		definition, exists := repository.integrationDefinitions[payload.DefinitionKey]
		if !exists {
			return commandOutcome{}, errs.ErrNotFound
		}
		var storedVersion, storedDigest string
		if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled, payload.DefinitionKey).Scan(&storedVersion, &storedDigest); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if storedVersion != definition.Metadata.Version || storedDigest != definition.Digest {
			return commandOutcome{}, errs.ErrConflict
		}
		configuration, valid := integrationStringConfiguration(payload.PublicConfiguration)
		if !valid || definition.ValidateConnectionBootstrapConfiguration(configuration) != nil || payload.CredentialRevision != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, _ := newRef("int")
		maskedCredentials := "CONFIGURED"
		if definition.Spec.Credential != nil || payload.DefinitionKey == "openapi-mcp" {
			maskedCredentials = "NOT_CONFIGURED"
		}
		var item entity.IntegrationConnection
		var connectionID string
		var config []byte
		err := tx.QueryRow(ctx, queryConfigurationChangeconnectionInsertIntegrationConnectionsRefDefinitionKeyState,
			ref, scope.organizationID, payload.Name, maskedCredentials, asJSON(configuration), scope.actorID,
			definition.Metadata.Version, definition.Digest, payload.DefinitionKey,
		).Scan(&connectionID, &item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.Enabled, &item.Version, &config, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if json.Unmarshal(config, &item.PublicConfiguration) != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		item, err = readConnection(ctx, tx, scope, ref)
		if err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: ref, summary: "i18n:INTEGRATION_CONNECTION_CREATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if err := repository.cancelConfigurationWriteBacks(ctx, tx, scope, "", payload.Ref); err != nil {
		return commandOutcome{}, err
	}
	if input.Kind == command.UpdateConnection {
		return repository.updateIntegrationConnection(ctx, tx, scope, input.Mutation, payload)
	}
	if input.Kind == command.DeleteConnection {
		return repository.deleteIntegrationConnection(ctx, tx, scope, input.Mutation, payload)
	}
	var item entity.IntegrationConnection
	if input.Kind == command.ConfigureConnectionCredential {
		credential := payload.CredentialRevision
		var connectionID string
		if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionSelectCredentialTarget,
			scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion,
		).Scan(&connectionID); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		locked, err := repository.lockIntegrationConnection(ctx, tx, scope.organizationID, payload.Ref)
		if err != nil {
			return commandOutcome{}, err
		}
		definition, err := repository.integrationPackage(ctx, tx, scope.organizationID, payload.Ref, locked.definitionKey, locked.definitionVersion, locked.definitionDigest)
		if err != nil {
			return commandOutcome{}, err
		}
		if !definition.RequiresConnectionCredential() || payload.MaterializationRef == "" || len(payload.MaterializationRef) > 128 ||
			!validIntegrationCredentialInput(true, credential) {
			return commandOutcome{}, errs.ErrInvalid
		}
		credentialRef, _ := newRef("icr")
		var credentialID string
		stored := &entity.IntegrationCredentialRevision{}
		if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionInsertCredentialRevision,
			credentialRef, scope.organizationID, connectionID, credential.SecretRef, credential.SecretUID,
			credential.SecretResourceVersion, credential.ContentSHA256, scope.actorID,
		).Scan(&credentialID, &stored.Ref, &stored.Revision, &stored.SecretRef, &stored.SecretUID,
			&stored.SecretResourceVersion, &stored.ContentSHA256, &stored.CreatedAt); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		var updatedRef string
		if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionActivateCredentialRevision,
			connectionID, credentialID, payload.MaterializationRef, *input.Mutation.ExpectedVersion,
		).Scan(&updatedRef); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		item, readErr := readConnection(ctx, tx, scope, updatedRef)
		if readErr != nil {
			return commandOutcome{}, readErr
		}
		return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref, summary: "i18n:INTEGRATION_CREDENTIAL_CONFIGURED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
	}
	if input.Kind == command.TestConnection {
		locked, err := repository.lockIntegrationConnection(ctx, tx, scope.organizationID, payload.Ref)
		if err != nil {
			return commandOutcome{}, err
		}
		definition, err := repository.integrationPackage(ctx, tx, scope.organizationID, payload.Ref, locked.definitionKey, locked.definitionVersion, locked.definitionDigest)
		if err != nil {
			return commandOutcome{}, err
		}
		health, found := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
		if !found || health.Risk != "READ" || health.ApprovalPolicy != "NONE" {
			return commandOutcome{}, errs.ErrForbidden
		}
		var connectionID string
		err = tx.QueryRow(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionsStateLastTestSummaryVersion, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion).Scan(&connectionID, &item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := advanceMailboxObservation(ctx, tx, scope.organizationID, connectionID, item.Version-1, item.Version); err != nil {
			return commandOutcome{}, err
		}
		testRef, _ := newRef("tst")
		if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionInsertIntegrationConnectionTestsRefConnectionIdCreatedBy, testRef, scope.organizationID, connectionID, scope.actorID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		state := "DISABLED"
		if payload.Enabled {
			state = "NOT_CONNECTED"
		}
		err := tx.QueryRow(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionsEnabledStateVersion, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled, state).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !payload.Enabled {
			args := pgx.StrictNamedArgs{"organization_id": scope.organizationID, "connection_ref": payload.Ref}
			if err := repository.cancelInteractionIntents(ctx, tx, scope, payload.Ref); err != nil {
				return commandOutcome{}, err
			}
			if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt, args); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest, args); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	}
	item, err := readConnection(ctx, tx, scope, item.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref, summary: "i18n:INTEGRATION_CONNECTION_UPDATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
}

// lockedIntegrationConnection holds the locked connection identity, lifecycle/runtime state,
// enablement, OCC version and exact definition pins needed for update/delete/credential/test
// decisions; no credential value is stored.
// lockedIntegrationConnection хранит заблокированные connection identity, lifecycle/runtime state,
// enablement, OCC version и точные definition pins для update/delete/credential/test решений;
// credential value не хранится.
type lockedIntegrationConnection struct {
	id, definitionKey, lifecycleState, state, definitionVersion, definitionDigest string
	enabled                                                                       bool
	version                                                                       int64
}

// lockIntegrationConnection locks the exact active tenant/ref connection and reads eight
// lifecycle/definition fields. Missing rows return zero/NotFound and scan failures
// zero/Unavailable; the caller retains transaction ownership.
// lockIntegrationConnection блокирует точный active tenant/ref connection и читает восемь
// lifecycle/definition fields. Отсутствие строки возвращает zero/NotFound, scan failure
// zero/Unavailable; транзакцией владеет caller.
func (repository *Repository) lockIntegrationConnection(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, ref string,
) (lockedIntegrationConnection, error) {
	var item lockedIntegrationConnection
	err := tx.QueryRow(ctx, queryConfigurationChangeconnectionLock, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_ref": ref,
	}).Scan(&item.id, &item.definitionKey, &item.lifecycleState, &item.state, &item.enabled,
		&item.version, &item.definitionVersion, &item.definitionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedIntegrationConnection{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedIntegrationConnection{}, errs.ErrUnavailable
	}
	return item, nil
}

// integrationConnectionDependencies counts active tests/invocations/deliveries and enabled grants
// for an already resolved connection ID. Query failure returns zero counts with Unavailable; zero
// counts are not themselves authorization.
// integrationConnectionDependencies считает активные tests/invocations/deliveries и enabled grants
// уже разрешённого connection ID. Ошибка запроса возвращает нулевые счётчики с Unavailable;
// нулевые счётчики сами не являются authorization.
func (repository *Repository) integrationConnectionDependencies(
	ctx context.Context,
	tx pgx.Tx,
	connectionID string,
) (int64, int64, error) {
	var effects, grants int64
	if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionCountActiveDependencies,
		pgx.StrictNamedArgs{"connection_id": connectionID}).Scan(&effects, &grants); err != nil {
		return 0, 0, errs.ErrUnavailable
	}
	return effects, grants, nil
}

// updateIntegrationConnection locks tenant connection/OCC, validates the pinned actual package and
// string configuration and rejects TESTING or active effects. It updates metadata, revokes
// grants/tests and rereads the canonical connection in the same transaction; package/configuration
// errors retain the existing Invalid mapping.
// updateIntegrationConnection блокирует tenant connection/OCC, проверяет закреплённый фактический
// package и string configuration, отклоняет TESTING либо active effects. Обновляет metadata,
// отзывает grants/tests и перечитывает канонический connection в той же транзакции;
// package/configuration errors сохраняют прежнее отображение в Invalid.
func (repository *Repository) updateIntegrationConnection(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	mutation value.Mutation,
	payload command.ConnectionInput,
) (commandOutcome, error) {
	locked, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	if locked.version != *mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	payload.Name = strings.TrimSpace(payload.Name)
	definition, packageErr := repository.integrationPackage(ctx, tx, current.organizationID, payload.Ref, locked.definitionKey, locked.definitionVersion, locked.definitionDigest)
	configuration, valid := integrationStringConfiguration(payload.PublicConfiguration)
	if payload.Name == "" || len(payload.Name) > 160 || packageErr != nil || !valid ||
		definition.Metadata.Version != locked.definitionVersion || definition.Digest != locked.definitionDigest ||
		definition.ValidateConfiguration(configuration) != nil || payload.CredentialRevision != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if locked.state == "TESTING" {
		return commandOutcome{}, errs.ErrConflict
	}
	activeEffects, _, err := repository.integrationConnectionDependencies(ctx, tx, locked.id)
	if err != nil {
		return commandOutcome{}, err
	}
	if activeEffects != 0 {
		return commandOutcome{}, errs.ErrConflict
	}
	var updatedRef string
	if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionUpdate, pgx.StrictNamedArgs{
		"connection_id": locked.id, "expected_version": locked.version,
		"name": payload.Name, "public_configuration": asJSON(configuration),
	}).Scan(&updatedRef); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	} else if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	dependencyArgs := pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_ref": payload.Ref}
	if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt, dependencyArgs); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest, dependencyArgs); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item, err := readConnection(ctx, tx, current, updatedRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref,
		summary: "i18n:INTEGRATION_CONNECTION_UPDATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED",
		platformAggregateVersion: item.Version, platformState: item.State,
	}, nil
}

// deleteIntegrationConnection requires exact locked OCC, disabled DISABLED state and no active
// effects or enabled grants before the logical delete. It preserves the readable prior connection
// metadata, replaces lifecycle/version/time, clears next actions and returns a DELETED event
// outcome without committing.
// deleteIntegrationConnection требует точные locked OCC, disabled DISABLED state и отсутствие
// active effects либо enabled grants перед логическим delete. Сохраняет прежние доступные
// connection metadata, заменяет lifecycle/version/time, очищает next actions и возвращает DELETED
// event outcome без commit.
func (repository *Repository) deleteIntegrationConnection(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	mutation value.Mutation,
	payload command.ConnectionInput,
) (commandOutcome, error) {
	locked, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	if locked.version != *mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if locked.enabled || locked.state != "DISABLED" {
		return commandOutcome{}, errs.ErrConflict
	}
	activeEffects, activeGrants, err := repository.integrationConnectionDependencies(ctx, tx, locked.id)
	if err != nil {
		return commandOutcome{}, err
	}
	if activeEffects != 0 || activeGrants != 0 {
		return commandOutcome{}, errs.ErrConflict
	}
	item, err := readConnection(ctx, tx, current, payload.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionDelete, pgx.StrictNamedArgs{
		"connection_id": locked.id, "expected_version": locked.version,
	}).Scan(&item.LifecycleState, &item.Version, &item.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.State = item.LifecycleState
	item.Enabled = false
	item.NextActions = nil
	return commandOutcome{
		result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref,
		summary: "i18n:INTEGRATION_CONNECTION_DELETED", platformEvent: "INTEGRATION_CONNECTION_CHANGED",
		platformAggregateVersion: item.Version, platformState: "DELETED",
	}, nil
}

// changeIntegrationGrant resolves ordinary or specialized assistant grant input, rechecks
// authority after connection locking and before OCC, and validates exact
// capability/approval/resource-scope pins and idle policy changes. It enables/revokes the grant,
// cancels interaction/write-back work, increments connection version and advances mailbox
// observation atomically in the caller transaction.
// changeIntegrationGrant разрешает обычный либо специализированный assistant grant input, повторно
// проверяет authority после connection lock до OCC и сверяет точные
// capability/approval/resource-scope pins и idle policy changes. Включает/отзывает grant, отменяет
// interaction/write-back work, увеличивает connection version и продвигает mailbox observation
// атомарно в транзакции caller.
func (repository *Repository) changeIntegrationGrant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationGrantInput)
	var systemTarget systemAssistantIntegrationGrantTarget
	if input.Kind == command.ChangeProjectAssistantIntegrationGrant {
		var err error
		payload, _, err = repository.projectAssistantIntegrationGrantInput(ctx, tx, scope, input.Payload)
		if err != nil {
			return commandOutcome{}, err
		}
		ok = true
	}
	if input.Kind == command.ChangeSystemAssistantIntegrationGrant {
		var err error
		payload, systemTarget, err = repository.systemAssistantIntegrationGrantInput(ctx, tx, scope, input.Payload)
		if err != nil {
			return commandOutcome{}, err
		}
		ok = true
	}
	if !ok || payload.ConnectionRef == "" || payload.CapabilityKey == "" || !validIntegrationApprovalPolicy(payload.ApprovalPolicy) || input.Mutation.ExpectedVersion == nil || (payload.AgentRef == "") == (payload.WorkflowRef == "") {
		return commandOutcome{}, errs.ErrInvalid
	}
	targetType, targetRef := "AGENT", payload.AgentRef
	if payload.WorkflowRef != "" {
		targetType, targetRef = "WORKFLOW", payload.WorkflowRef
	}
	var connectionID, definitionKey, connectionState, definitionVersion, definitionDigest string
	var connectionEnabled bool
	var connectionVersion int64
	var encodedConfiguration []byte
	if err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantSelectIntegrationConnectionsOrganizationIdRefEnabled, scope.organizationID, payload.ConnectionRef).Scan(
		&connectionID, &definitionKey, &connectionEnabled, &connectionState, &connectionVersion,
		&encodedConfiguration, &definitionVersion, &definitionDigest,
	); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	// Повтор после ожидания connection lock предшествует OCC и новому effect.
	if input.Kind == command.ChangeProjectAssistantIntegrationGrant {
		if _, _, err := repository.projectAssistantIntegrationGrantInput(ctx, tx, scope, input.Payload); err != nil {
			return commandOutcome{}, err
		}
	} else if input.Kind == command.ChangeSystemAssistantIntegrationGrant {
		if _, _, err := repository.systemAssistantIntegrationGrantInput(ctx, tx, scope, input.Payload); err != nil {
			return commandOutcome{}, err
		}
	} else {
		if err := repository.authorizeIntegrationGrant(ctx, tx, scope, payload); err != nil {
			return commandOutcome{}, err
		}
	}
	if connectionVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if payload.Enabled && (!connectionEnabled || connectionState != "CONNECTED") {
		return commandOutcome{}, errs.ErrConflict
	}
	var projectID, projectRef, targetName string
	targetQuery := queryConfigurationChangeintegrationgrantSelectAgentOrganizationIdRef
	if targetType == "WORKFLOW" {
		targetQuery = queryConfigurationChangeintegrationgrantSelectWorkflowOrganizationIdRef
	}
	if input.Kind == command.ChangeSystemAssistantIntegrationGrant {
		targetName = systemTarget.name
	} else if err := tx.QueryRow(ctx, targetQuery, scope.organizationID, targetRef).Scan(&projectID, &projectRef, &targetName); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	definition, packageErr := repository.integrationPackage(ctx, tx, scope.organizationID, payload.ConnectionRef, definitionKey, definitionVersion, definitionDigest)
	capability, valid := definition.Capability(payload.CapabilityKey)
	if packageErr != nil || !valid || definition.Metadata.Version != definitionVersion || definition.Digest != definitionDigest {
		return commandOutcome{}, errs.ErrInvalid
	}
	if !capability.AllowsApprovalPolicy(payload.ApprovalPolicy) {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Enabled {
		if payload.ApprovalPolicy == "HUMAN_SCOPED" {
			if capability.ValidateApprovalScopePaths(payload.ApprovalScopePaths) != nil {
				return commandOutcome{}, errs.ErrInvalid
			}
		} else if len(payload.ApprovalScopePaths) != 0 {
			return commandOutcome{}, errs.ErrInvalid
		}
	} else if len(payload.ApprovalScopePaths) != 0 {
		return commandOutcome{}, errs.ErrInvalid
	}
	if err := requireIntegrationGrantPolicyChangeIdle(ctx, tx, scope.organizationID, connectionID, payload); err != nil {
		return commandOutcome{}, err
	}
	configuration := map[string]string{}
	if json.Unmarshal(encodedConfiguration, &configuration) != nil || definition.ValidateConfiguration(configuration) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	resourceScope, err := capability.ResourceScopeValues(configuration)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	encodedScope, err := json.Marshal(resourceScope)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	scopeDigest := sha256.Sum256(encodedScope)
	var grantRef string
	if payload.Enabled {
		grantRef, _ = newRef("grt")
		err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantInsertIntegrationGrantsRefConnectionIdTargetKind,
			grantRef, scope.organizationID, connectionID, payload.CapabilityKey, targetType, targetRef,
			payload.ApprovalPolicy, scope.actorID, capability.Risk, capability.ResourceScope.Kind,
			encodedScope, hex.EncodeToString(scopeDigest[:]), definition.Metadata.Version, definition.Digest,
			sortedApprovalScopePaths(payload.ApprovalScopePaths),
		).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantUpdateIntegrationGrantsEnabledVersionUpdatedAt, scope.organizationID, connectionID, payload.CapabilityKey, targetType, targetRef).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
	}
	if err := repository.cancelInteractionIntents(ctx, tx, scope, payload.ConnectionRef); err != nil {
		return commandOutcome{}, err
	}
	if err := repository.cancelConfigurationWriteBacks(ctx, tx, scope, "", payload.ConnectionRef); err != nil {
		return commandOutcome{}, err
	}
	tag, err := tx.Exec(ctx, queryConfigurationChangeintegrationgrantUpdateIntegrationConnectionsVersion, connectionID, connectionVersion)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err := advanceMailboxObservation(ctx, tx, scope.organizationID, connectionID, connectionVersion, connectionVersion+1); err != nil {
		return commandOutcome{}, err
	}
	connection, err := readConnection(ctx, tx, scope, payload.ConnectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &connection}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_GRANT", resourceRef: grantRef, summary: "i18n:INTEGRATION_GRANT_UPDATED", platformEvent: "INTEGRATION_GRANT_CHANGED"}, nil
}

// integrationStringConfiguration copies public configuration into a new string map, trimming
// values and rejecting nonstrings or empty results; keys are unchanged and nil input yields an
// empty nonnil map.
// integrationStringConfiguration копирует public configuration в новую string map, обрезая пробелы
// значений и отклоняя нестроковые либо пустые результаты; ключи неизменны, nil input даёт пустую
// ненулевую map.
func integrationStringConfiguration(input map[string]any) (map[string]string, bool) {
	result := make(map[string]string, len(input))
	for key, raw := range input {
		value, ok := raw.(string)
		value = strings.TrimSpace(value)
		if !ok || value == "" {
			return nil, false
		}
		result[key] = value
	}
	return result, true
}

// validIntegrationCredentialInput checks a new required credential descriptor has no assigned
// revision identity, a UUID SecretUID, bounded resource version, lowercase SHA-256 and an exact
// allowed SecretRef prefix/key shape. Optional credentials must be nil; this validates metadata
// syntax without reading a Kubernetes Secret or proving materialization.
// validIntegrationCredentialInput проверяет, что новый обязательный credential descriptor не имеет
// назначенной revision identity и содержит UUID SecretUID, ограниченную resource version, строчный
// SHA-256 и точные разрешённые SecretRef prefix/key shape. Необязательные credentials должны быть
// nil; проверяет синтаксис metadata без чтения Kubernetes Secret или доказательства
// materialization.
func validIntegrationCredentialInput(required bool, credential *entity.IntegrationCredentialRevision) bool {
	if !required {
		return credential == nil
	}
	if credential == nil || credential.Ref != "" || credential.Revision != 0 ||
		uuid.Validate(credential.SecretUID) != nil || credential.SecretResourceVersion == "" ||
		len(credential.SecretResourceVersion) > 128 || len(credential.ContentSHA256) != sha256.Size*2 ||
		!strings.HasPrefix(credential.SecretRef, "kodex-system/kodex-integration-credentials#") {
		return false
	}
	key := strings.TrimPrefix(credential.SecretRef, "kodex-system/kodex-integration-credentials#")
	if key == "" || len(key) > 253 || strings.ContainsAny(key, "\x00/\\\r\n") {
		return false
	}
	for _, character := range credential.ContentSHA256 {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// validateIntegrationConfiguration normalizes at most fifty declared fields with a closed key set:
// bounded trimmed TEXT, HTTPS authority-only URL and deduplicated ordered STRING_LIST. Missing
// required fields, unsupported types or malformed values fail with nil/false; optional absent
// fields remain absent and no network request occurs.
// validateIntegrationConfiguration нормализует до пятидесяти объявленных fields с закрытым набором
// ключей: ограниченный обрезанный TEXT, HTTPS URL только с authority и STRING_LIST с удалением
// повторов при сохранении порядка. Отсутствие обязательного field, неподдерживаемый type либо
// ошибочное значение дают nil/false; отсутствующие optional fields остаются отсутствующими,
// сетевых запросов нет.
func validateIntegrationConfiguration(fields []entity.IntegrationConfigurationField, input map[string]any) (map[string]any, bool) {
	if len(fields) > 50 || len(input) > len(fields) {
		return nil, false
	}
	allowed := make(map[string]entity.IntegrationConfigurationField, len(fields))
	for _, field := range fields {
		if field.Key == "" || len(field.Key) > 64 {
			return nil, false
		}
		allowed[field.Key] = field
	}
	for key := range input {
		if _, ok := allowed[key]; !ok {
			return nil, false
		}
	}
	normalized := make(map[string]any, len(fields))
	for _, field := range fields {
		raw, present := input[field.Key]
		if !present {
			if field.Required {
				return nil, false
			}
			continue
		}
		switch field.ValueType {
		case "TEXT":
			value, ok := raw.(string)
			value = strings.TrimSpace(value)
			if !ok || value == "" || len(value) > 500 {
				return nil, false
			}
			normalized[field.Key] = value
		case "URL":
			value, ok := raw.(string)
			value = strings.TrimSpace(value)
			parsed, err := url.Parse(value)
			if !ok || err != nil || len(value) > 2048 || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
				return nil, false
			}
			normalized[field.Key] = strings.TrimSuffix(value, "/")
		case "STRING_LIST":
			values, ok := raw.([]any)
			if !ok || len(values) == 0 || len(values) > 64 {
				return nil, false
			}
			clean := make([]string, 0, len(values))
			seen := make(map[string]struct{}, len(values))
			for _, item := range values {
				value, ok := item.(string)
				value = strings.TrimSpace(value)
				if !ok || value == "" || len(value) > 100 {
					return nil, false
				}
				if _, duplicate := seen[value]; duplicate {
					continue
				}
				seen[value] = struct{}{}
				clean = append(clean, value)
			}
			if len(clean) == 0 {
				return nil, false
			}
			normalized[field.Key] = clean
		default:
			return nil, false
		}
	}
	return normalized, true
}

// changeAssistant dispatches only the closed set of conversation, turn, plan, owner-instruction
// and recovery commands to their specialized handlers in the caller transaction; unknown kinds
// return Invalid.
// changeAssistant направляет только закрытый набор conversation, turn, plan, owner-instruction и
// recovery commands специализированным handlers в транзакции caller; неизвестные kinds возвращают
// Invalid.
func (repository *Repository) changeAssistant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.CreateAssistantConversation:
		return repository.createAssistantConversation(ctx, tx, scope, input)
	case command.UpdateAssistantConversation:
		return repository.updateAssistantConversationTitle(ctx, tx, scope, input)
	case command.ArchiveAssistantConversation, command.RestoreAssistantConversation, command.PurgeAssistantConversation:
		return repository.archiveAssistantConversation(ctx, tx, scope, input)
	case command.MoveAssistantConversationToProject:
		return repository.moveAssistantConversationToProject(ctx, tx, scope, input)
	case command.AddAssistantTurn:
		return repository.addAssistantTurnCommand(ctx, tx, scope, input)
	case command.CancelAssistantTurn:
		return repository.cancelAssistantTurnCommand(ctx, tx, scope, input)
	case command.UpdateAssistantPlan:
		return repository.updateAssistantPlanDraft(ctx, tx, scope, input)
	case command.ValidateAssistantPlan:
		return repository.validateAssistantPlan(ctx, tx, scope, input)
	case command.ApplyAssistantPlan:
		return repository.applyAssistantPlanCommand(ctx, tx, scope, input)
	case command.RejectAssistantPlan:
		return repository.rejectAssistantPlan(ctx, tx, scope, input)
	case command.UpdateAssistantInstructions:
		return repository.updateAssistantInstructions(ctx, tx, scope, input)
	case command.RecoverAssistant:
		return repository.recoverAssistant(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

// createAssistantConversation resolves project and canonical assistant identity, selects its
// provider account, creates a model-catalog-bound session and authoritatively expands context
// before inserting the conversation. Scope/profile are projected from server identity; no turn or
// provider execution is started.
// createAssistantConversation разрешает project и каноническую assistant identity, выбирает
// provider account, создаёт связанную с model catalog session и авторитетно расширяет context до
// вставки conversation. Scope/profile проецируются из серверной identity; turn или provider
// execution не запускаются.
func (repository *Repository) createAssistantConversation(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantConversationInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID any
	if payload.ProjectRef != "" {
		id := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if id == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		projectID = id
	} else {
		projectID = nil
	}
	sessionRef, _ := newRef("ses")
	assistant, err := repository.assistantForConversationCreation(ctx, tx, scope, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	providerAccountID, err := repository.selectProviderAccountForAgent(ctx, tx, scope.organizationID, assistant.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	var sessionID string
	if err := tx.QueryRow(ctx, queryConfigurationCreateassistantconversationInsertSessionsRefProjectIdTargetRef, sessionRef, scope.organizationID, projectID, providerAccountID, scope.actorID, assistant.Ref).Scan(&sessionID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := bindSessionModelCatalog(ctx, tx, scope.organizationID, sessionID, assistant.Ref); err != nil {
		return commandOutcome{}, err
	}
	ref, _ := newRef("cnv")
	resolvedContext, err := repository.resolveAssistantContext(ctx, tx, scope, payload.Context, payload.ProjectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.expandAssistantContext(ctx, tx, scope, assistant, payload.ProjectRef, &resolvedContext); err != nil {
		return commandOutcome{}, err
	}
	var item entity.AssistantConversation
	if err := tx.QueryRow(ctx, queryConfigurationCreateassistantconversationInsertAssistantConversationsRefProjectIdTitle,
		ref, scope.organizationID, projectID, sessionID, scope.actorID,
		resolvedContext.Route, resolvedContext.EntityKind, resolvedContext.EntityRef,
		resolvedContext.EntityName, resolvedContext.EntityVersion, resolvedContext.AllowedOperations,
		assistant.Scope, assistant.ID, assistant.ProfileRef,
	).Scan(&item.Ref, &item.Title, &item.TitleSource, &item.TitleRevision, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.ProjectRef = payload.ProjectRef
	item.SessionRef = sessionRef
	item.Context = resolvedContext
	assistant.projectConversation(&item)
	return commandOutcome{result: command.Result{Conversation: &item}, projectID: stringValue(projectID), projectRef: payload.ProjectRef, resourceKind: "ASSISTANT_CONVERSATION", resourceRef: ref, summary: "i18n:ASSISTANT_CONVERSATION_CREATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// resolveAssistantContext validates bounded paired context selectors, requires project.view when a
// project is supplied and resolves name/version/allowed operations through the
// tenant/actor/authority-project SQL projection. A conflicting resolved project is hidden as
// NotFound; payload route/ref do not establish authority.
// resolveAssistantContext проверяет ограниченные парные context selectors, требует project.view
// при переданном project и разрешает name/version/allowed operations через
// tenant/actor/authority-project SQL projection. Несовпавший resolved project скрывается как
// NotFound; payload route/ref не устанавливают authority.
func (repository *Repository) resolveAssistantContext(ctx context.Context, tx pgx.Tx, current scope, descriptor entity.AssistantContextDescriptor, projectRef string) (entity.AssistantContextDescriptor, error) {
	if len(descriptor.Route) > 500 || len(descriptor.EntityKind) > 80 || len(descriptor.EntityRef) > 96 ||
		(descriptor.EntityKind == "") != (descriptor.EntityRef == "") ||
		!contains([]string{"", "PROJECT", "AGENT", "WORKFLOW", "RUN", "FILE", "ENVIRONMENT", "ROLE_IMAGE_RECIPE", "INTEGRATION_CONNECTION", "SCHEDULE"}, descriptor.EntityKind) {
		return entity.AssistantContextDescriptor{}, errs.ErrInvalid
	}
	if projectRef != "" {
		if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: projectRef}); err != nil {
			return entity.AssistantContextDescriptor{}, err
		}
	}
	var projectID, resolvedProjectRef string
	err := tx.QueryRow(ctx, queryAssistantContextProjection, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID,
		"context_kind": descriptor.EntityKind, "context_ref": descriptor.EntityRef,
		"project_ref": projectRef,
	}).Scan(&projectID, &resolvedProjectRef, &descriptor.EntityName, &descriptor.EntityVersion, &descriptor.AllowedOperations)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantContextDescriptor{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.AssistantContextDescriptor{}, errs.ErrUnavailable
	}
	if projectRef != "" && resolvedProjectRef != "" && resolvedProjectRef != projectRef {
		return entity.AssistantContextDescriptor{}, errs.ErrNotFound
	}
	return descriptor, nil
}

// addAssistantTurnCommand delegates ordinary turn creation with attachment snapshot reuse
// disabled; canonical identity, readiness, context and lifecycle checks remain in the shared
// specialized implementation.
// addAssistantTurnCommand передаёт обычное создание turn с отключённым attachment snapshot reuse;
// проверки канонических identity, readiness, context и lifecycle остаются в общей
// специализированной реализации.
func (repository *Repository) addAssistantTurnCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	return repository.addAssistantTurnWithAttachmentPolicy(ctx, tx, scope, input, false)
}

// addAssistantTurnWithAttachmentPolicy validates content/delivery and rechecks conversation
// assistant identity, environment readiness, optional OCC and session catalogs. INTERRUPT_ACTIVE
// cancels the previous run before resolving fresh context and finalized attachment snapshots. It
// creates linked run/turn/node state, advances session/conversation and emits TURN_QUEUED
// atomically; the reuse flag only controls attachment snapshot policy, not authority.
// addAssistantTurnWithAttachmentPolicy проверяет content/delivery и повторно сверяет conversation
// assistant identity, environment readiness, optional OCC и session catalogs. INTERRUPT_ACTIVE
// отменяет прежний run до разрешения свежего context и finalized attachment snapshots. Атомарно
// создаёт связанные run/turn/node state, продвигает session/conversation и создаёт TURN_QUEUED;
// reuse flag управляет лишь attachment snapshot policy, не authority.
func (repository *Repository) addAssistantTurnWithAttachmentPolicy(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, reuseAttachmentSnapshot bool) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantTurnInput)
	if !ok || payload.ConversationRef == "" || !runtimecontract.ValidAssistantTurnContent(payload.Content) ||
		!contains([]string{"QUEUE", "INTERRUPT_ACTIVE"}, payload.DeliveryMode) {
		return commandOutcome{}, errs.ErrInvalid
	}
	identity, err := repository.conversationAssistantTx(ctx, tx, scope, payload.ConversationRef)
	if err != nil {
		return commandOutcome{}, err
	}
	candidate, err := repository.assistantForConversationCreation(ctx, tx, scope, command.AssistantConversationInput{
		AssistantScope: identity.Scope, ProjectRef: identity.ProjectRef,
	})
	if err != nil {
		return commandOutcome{}, err
	}
	if candidate.ID != identity.ID {
		return commandOutcome{}, errs.ErrConflict
	}
	configuration, err := repository.getRuntimeConfigurationViewTx(ctx, tx, scope, identity.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	if !configuration.Environment.Ready {
		return commandOutcome{}, fmt.Errorf("assistant runtime environment is not ready: %w", errs.ErrConflict)
	}
	if identity.Scope == "SYSTEM" {
		var runtimeReady bool
		if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(&runtimeReady); err != nil || !runtimeReady {
			return commandOutcome{}, fmt.Errorf("read system assistant runtime readiness: %w", errs.ErrUnavailable)
		}
	}
	var conversationID, sessionID, sessionRef string
	var projectID, projectRef string
	var version int64
	storedContext := entity.AssistantContextDescriptor{}
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState, scope.organizationID, payload.ConversationRef).Scan(
		&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &version,
		&storedContext.Route, &storedContext.EntityKind, &storedContext.EntityRef,
	); err != nil {
		return commandOutcome{}, fmt.Errorf("lock system assistant conversation: %w", errs.ErrNotFound)
	}
	if input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	dispatchPriority := int16(0)
	if payload.DeliveryMode == "INTERRUPT_ACTIVE" {
		dispatchPriority = 1
		if _, _, err := repository.cancelActiveAssistantRun(ctx, tx, scope, payload.ConversationRef, "Superseded by an immediate system assistant turn"); err != nil {
			return commandOutcome{}, fmt.Errorf("interrupt active system assistant run: %w", err)
		}
	}
	if err := validateSessionRuntimeCatalog(ctx, tx, scope.organizationID, sessionID, identity.Ref); err != nil {
		return commandOutcome{}, err
	}
	requestedContext := storedContext
	if payload.Context != nil {
		requestedContext = *payload.Context
	}
	resolvedContext, err := repository.resolveAssistantContext(ctx, tx, scope, requestedContext, projectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.expandAssistantContext(ctx, tx, scope, identity, projectRef, &resolvedContext); err != nil {
		return commandOutcome{}, err
	}
	turnRef, _ := newRef("trn")
	attachmentSet, err := repository.resolveFinalizedAttachmentSet(ctx, tx, scope, projectID, payload.AttachmentSetRef, "ASSISTANT_MESSAGE", reuseAttachmentSnapshot)
	if err != nil {
		return commandOutcome{}, err
	}
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectSessionsId, sessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, fmt.Errorf("lock system assistant session: %w", errs.ErrUnavailable)
	}
	runRef, _ := newRef("run")
	var runID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType,
		runRef, scope.organizationID, projectID, sessionID, payload.Content, scope.actorID,
		resolvedContext.Route, resolvedContext.EntityKind, resolvedContext.EntityRef,
		dispatchPriority, identity.Ref,
	).Scan(&runID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant run: %w", errs.ErrUnavailable)
	}
	if err := repository.attachSetToRun(ctx, tx, scope, projectID, attachmentSet, runID, "RUN_INPUT"); err != nil {
		return commandOutcome{}, fmt.Errorf("bind system assistant run attachment set: %w", err)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt, runID); err != nil {
		return commandOutcome{}, fmt.Errorf("start system assistant root run: %w", errs.ErrUnavailable)
	}
	var turnID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind,
		turnRef, scope.organizationID, sessionID, runID, turnNumber, scope.actorRef, payload.Content,
	).Scan(&turnID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant user turn: %w", errs.ErrUnavailable)
	}
	if err := repository.attachSetToTurn(ctx, tx, scope, projectID, attachmentSet, turnID, "ASSISTANT_MESSAGE"); err != nil {
		return commandOutcome{}, fmt.Errorf("bind system assistant message attachment set: %w", err)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return commandOutcome{}, fmt.Errorf("advance system assistant session: %w", errs.ErrUnavailable)
	}
	nodeRef, _ := newRef("nod")
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType, nodeRef, scope.organizationID, runID, turnID, truncate(payload.Content, 1000), identity.ID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant execution node: %w", errs.ErrUnavailable)
	}
	conversation := entity.AssistantConversation{
		Ref: payload.ConversationRef, ProjectRef: projectRef, SessionRef: sessionRef,
	}
	identity.projectConversation(&conversation)
	if err := tx.QueryRow(
		ctx,
		queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt,
		conversationID, resolvedContext.Route, resolvedContext.EntityKind, resolvedContext.EntityRef,
		resolvedContext.EntityName, resolvedContext.EntityVersion, resolvedContext.AllowedOperations,
		assistantUserMessageTitle(payload.Content),
	).Scan(
		&conversation.Title,
		&conversation.TitleSource,
		&conversation.TitleRevision,
		&conversation.State,
		&conversation.Version,
		&conversation.Context.Route,
		&conversation.Context.EntityKind,
		&conversation.Context.EntityRef,
		&conversation.Context.EntityName,
		&conversation.Context.EntityVersion,
		&conversation.Context.AllowedOperations,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
	); err != nil {
		return commandOutcome{}, fmt.Errorf("advance system assistant conversation: %w", errs.ErrUnavailable)
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, runID, runRef, "TURN_QUEUED", nodeRef, "", "", "", "i18n:ASSISTANT_TURN_QUEUED", "RUNNING", "QUEUED"); err != nil {
		return commandOutcome{}, err
	}
	conversation.Turns = []entity.AssistantTurn{{Ref: turnRef, Sequence: turnNumber, Actor: "USER", ActorName: scope.actorName, Content: payload.Content, AttachmentSetRef: payload.AttachmentSetRef, State: "QUEUED", RunRef: runRef, RunVersion: 1, CreatedAt: time.Now().UTC(), Source: entity.MessageSource{Origin: "ORDINARY"}}}
	result := command.Result{Conversation: &conversation}
	if identity.Scope == "SYSTEM" {
		assistant, err := repository.getAssistantTx(ctx, tx, scope)
		if err != nil {
			return commandOutcome{}, err
		}
		result.Assistant = &assistant
	}
	return commandOutcome{result: result, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_TURN", resourceRef: turnRef, summary: "i18n:ASSISTANT_TURN_ACCEPTED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// cancelAssistantTurnCommand locks the conversation and requires exact OCC before cancelling its
// active run through the canonical run command. It returns the run/cancelled readback and emits
// assistant-change metadata only if a run was actually cancelled.
// cancelAssistantTurnCommand блокирует conversation и требует точный OCC до отмены активного run
// через каноническую run command. Возвращает run/cancelled readback и добавляет assistant-change
// metadata только при фактической отмене run.
func (repository *Repository) cancelAssistantTurnCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantTurnCancellationInput)
	if !ok || payload.ConversationRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var conversationID, sessionID, sessionRef, projectID, projectRef string
	var version int64
	var context entity.AssistantContextDescriptor
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState, scope.organizationID, payload.ConversationRef).Scan(
		&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &version,
		&context.Route, &context.EntityKind, &context.EntityRef,
	); err != nil {
		return commandOutcome{}, fmt.Errorf("lock system assistant conversation: %w", errs.ErrNotFound)
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	runRef, cancelled, err := repository.cancelActiveAssistantRun(ctx, tx, scope, payload.ConversationRef, "Cancelled by the owner")
	if err != nil {
		return commandOutcome{}, err
	}
	result := command.Result{Runtime: map[string]any{"conversationRef": payload.ConversationRef, "runRef": runRef, "cancelled": cancelled}}
	outcome := commandOutcome{result: result, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_TURN", resourceRef: payload.ConversationRef, summary: "i18n:ASSISTANT_TURN_CANCELLED"}
	if cancelled {
		outcome.platformEvent = "SYSTEM_ASSISTANT_CHANGED"
	}
	return outcome, nil
}

// cancelActiveAssistantRun resolves the current active run/version and delegates CancelRun with
// that OCC and reason. No active run is a successful empty/false result; lookup and lifecycle
// failures propagate without inventing a cancellation.
// cancelActiveAssistantRun разрешает текущий active run/version и передаёт CancelRun с этими OCC и
// reason. Отсутствие active run успешно возвращает пустое значение/false; lookup и lifecycle
// failures передаются без вымышленной отмены.
func (repository *Repository) cancelActiveAssistantRun(ctx context.Context, tx pgx.Tx, scope scope, conversationRef, reason string) (string, bool, error) {
	var runRef string
	var runVersion int64
	err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectActiveRun, scope.organizationID, conversationRef).Scan(&runRef, &runVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolve active system assistant run: %w", errs.ErrUnavailable)
	}
	if _, err := repository.changeRun(ctx, tx, scope, command.Command{
		Kind: command.CancelRun, Mutation: value.Mutation{ExpectedVersion: &runVersion},
		Payload: command.RunCommandInput{RunRef: runRef, Reason: reason},
	}); err != nil {
		return "", false, err
	}
	return runRef, true, nil
}

// applyAssistantPlanCommand requires a locked VALID plan with exact version/current validated
// revision, constrains the verified owner scope and applies selected specialized commands inside
// nested savepoints. It carries only server-observed connection/agent/binding versions, rechecks
// immutable snapshots and rolls back all operation effects on conflict before writing
// STALE/conflict receipt in the outer savepoint. Success records per-operation audits/events and
// one APPLIED receipt; single-project creation may promote conversation lineage. Savepoint commits
// do not commit the caller transaction, and historical ignored rollback failures outside the
// explicit conflict branch remain unchanged.
// applyAssistantPlanCommand требует заблокированный VALID plan с точными version/current validated
// revision, ограничивает проверенный owner scope и применяет выбранные specialized commands во
// вложенных savepoints. Переносит лишь наблюдённые сервером connection/agent/binding versions,
// перепроверяет immutable snapshots и откатывает все operation effects при конфликте до записи
// STALE/conflict receipt во внешнем savepoint. Успех сохраняет per-operation audits/events и одну
// APPLIED receipt; создание единственного project может повысить conversation lineage. Savepoint
// commits не фиксируют транзакцию caller, а прежние игнорируемые rollback failures вне явной
// conflict branch сохранены.
func (repository *Repository) applyAssistantPlanCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	// Исходный verified browser scope хранится отдельно. Только новая specialty
	// использует его после owner gate; проектная граница остальных effects не меняется.
	ownerEffectScope := scope
	payload, ok := input.Payload.(command.AssistantPlanInput)
	if !ok || payload.PlanRef == "" || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, conversationRef, summary, conversationProjectRef, digest string
	var raw []byte
	var version, revision int64
	var validatedRevision *int64
	if err := tx.QueryRow(ctx, queryConfigurationApplyassistantplancommandSelectAssistantPlansOrganizationIdRefState, scope.organizationID, payload.PlanRef).Scan(
		&planID, &conversationRef, &summary, &raw, &version, &revision, &validatedRevision, &digest, &conversationProjectRef,
	); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if version != *input.Mutation.ExpectedVersion || revision != payload.Revision || validatedRevision == nil || *validatedRevision != revision {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var stored []entity.AssistantPlanOperation
	if json.Unmarshal(raw, &stored) != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	operations, err := normalizeAssistantOperations(stored, conversationProjectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.constrainAssistantPlanScope(ctx, tx, &scope, conversationRef, operations); err != nil {
		return commandOutcome{}, err
	}
	effectTx, err := tx.Begin(ctx)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	operationEffectsTx, err := effectTx.Begin(ctx)
	if err != nil {
		_ = effectTx.Rollback(ctx)
		return commandOutcome{}, errs.ErrUnavailable
	}
	created := []string{}
	operationReceipts := []entity.AssistantPlanOperationReceipt{}
	grantPreflightConflict, err := repository.preflightAssistantGrantPlan(ctx, operationEffectsTx, scope, conversationProjectRef, operations)
	if err != nil {
		_ = operationEffectsTx.Rollback(ctx)
		_ = effectTx.Rollback(ctx)
		return commandOutcome{}, err
	}
	appliedGrantConnectionVersions := map[string]int64{}
	appliedAgentVersions := map[string]int64{}
	appliedBindingPins := map[string]map[string]any{}
	var projectID, projectRef string
	for _, operation := range operations {
		if !operation.Selected {
			continue
		}
		operationScope := scope
		if operation.Type == prepareProjectAssistantConnection {
			operationScope = ownerEffectScope
		}
		if grantPreflightConflict != nil {
			operation = *grantPreflightConflict
		}
		grantConnectionRef := assistantGrantConnection(operation)
		if version, carried := appliedGrantConnectionVersions[grantConnectionRef]; carried {
			operation = carryAssistantGrantConnectionVersion(operation, version)
		}
		agentVersionKey := assistantPlanAgentVersionKey(operation)
		if version, carried := appliedAgentVersions[agentVersionKey]; carried {
			operation = rebaseAssistantPlanAgentVersion(operation, version)
		}
		if pins, carried := appliedBindingPins[agentVersionKey]; carried && assistantProjectConfigurationOperation(operation) {
			operation = carryAssistantPlanBindingPins(operation, pins)
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			_ = operationEffectsTx.Rollback(ctx)
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, err
		}
		if err := repository.authorizeCommand(ctx, operationEffectsTx, operationScope, planned); err != nil {
			_ = operationEffectsTx.Rollback(ctx)
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, err
		}
		if assistantFileOperation(operation.Type) {
			planned, err = repository.resolveAssistantFileOperationTx(ctx, operationEffectsTx, operationScope, planID, revision, operation, planned)
			if err != nil {
				_ = operationEffectsTx.Rollback(ctx)
				_ = effectTx.Rollback(ctx)
				return commandOutcome{}, err
			}
		}
		var outcome commandOutcome
		if operation.Type == prepareProjectAssistantConnection {
			matching, matchErr := repository.projectAssistantConnectionSnapshotMatches(ctx, operationEffectsTx, operationScope, operation)
			if matchErr != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if grantPreflightConflict != nil {
			err = errs.ErrConflict
		}
		if operation.Type == "CHANGE_INTEGRATION_GRANT" {
			matching, matchErr := repository.assistantIntegrationGrantSnapshotMatches(ctx, operationEffectsTx, scope, conversationProjectRef, operation)
			if matchErr != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == changeProjectAssistantIntegrationGrant {
			matching, matchErr := repository.projectAssistantIntegrationGrantSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if matchErr != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			matching, matchErr := repository.systemAssistantIntegrationGrantSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if matchErr != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
			matching, matchErr := repository.systemAssistantImageSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if matchErr != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
			matching, matchErr := repository.assistantRuntimeConfigurationSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if matchErr != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "UPDATE_WORKFLOW" {
			var matching bool
			matching, err = repository.assistantWorkflowUpdateSnapshotMatches(ctx, operationEffectsTx, scope, conversationProjectRef, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "CREATE_INSTRUCTION_DRAFT" {
			var matching bool
			matching, err = repository.assistantInstructionDraftSnapshotMatches(ctx, operationEffectsTx, scope, conversationProjectRef, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			var matching bool
			matching, err = repository.assistantEnvironmentSnapshotMatches(ctx, operationEffectsTx, scope, conversationProjectRef, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
			var matching bool
			matching, err = repository.assistantSystemInstructionsSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT" {
			bindingProjectRef := conversationProjectRef
			if assistantProjectConfigurationOperation(operation) {
				bindingProjectRef = assistantString(operation.Parameters, "projectRef")
			}
			err = repository.lockAssistantBindingEnvironment(ctx, operationEffectsTx, scope, bindingProjectRef,
				assistantString(operation.Parameters, "environmentRef"))
			if err == nil {
				var matching bool
				matching, err = repository.assistantAgentBindingSnapshotMatches(ctx, operationEffectsTx, scope, conversationProjectRef, operation)
				if err != nil || !matching {
					err = errs.ErrConflict
				}
			}
		}
		if operation.Type == "UPDATE_SCHEDULE" {
			var matching bool
			matching, err = repository.assistantScheduleUpdateSnapshotMatches(ctx, operationEffectsTx, scope, conversationProjectRef, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "UPDATE_ROLE_IMAGE_RECIPE" {
			var matching bool
			matching, err = repository.assistantRoleImageUpdateSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if operation.Type == "PUBLISH_INTEGRATION_DEFINITION" {
			var matching bool
			matching, err = repository.assistantIntegrationDefinitionPublicationSnapshotMatches(ctx, operationEffectsTx, scope, operation)
			if err != nil || !matching {
				err = errs.ErrConflict
			}
		}
		if err == nil {
			outcome, err = repository.applyCommand(ctx, operationEffectsTx, operationScope, planned)
		}
		if err != nil {
			if errors.Is(err, errs.ErrVersionMismatch) || errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) {
				if rollbackErr := operationEffectsTx.Rollback(ctx); rollbackErr != nil {
					_ = effectTx.Rollback(ctx)
					return commandOutcome{}, fmt.Errorf("rollback assistant plan operation effects: %w", errs.ErrUnavailable)
				}
				conflict, problem := assistantOperationConflict(operation)
				conflicts := []entity.AssistantPlanConflict{conflict}
				if _, updateErr := effectTx.Exec(ctx, queryConfigurationMarkAssistantPlanStale, planID, []string{problem}); updateErr != nil {
					_ = effectTx.Rollback(ctx)
					return commandOutcome{}, errs.ErrUnavailable
				}
				receipt, receiptErr := repository.insertAssistantPlanReceipt(ctx, effectTx, scope, planID, payload.PlanRef,
					revision, "CONFLICT", nil, conflicts, nil)
				if receiptErr != nil {
					_ = effectTx.Rollback(ctx)
					return commandOutcome{}, receiptErr
				}
				if commitErr := effectTx.Commit(ctx); commitErr != nil {
					return commandOutcome{}, fmt.Errorf("commit assistant plan conflict receipt: %w", errs.ErrConflict)
				}
				plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: conversationProjectRef,
					Summary: summary, State: "STALE", Version: version + 1, Revision: revision, ValidatedRevision: validatedRevision,
					ContentDigest: digest, ValidationProblems: []string{problem}, Operations: operations}
				conversation := entity.AssistantConversation{Ref: conversationRef}
				return commandOutcome{result: command.Result{Conversation: &conversation, Plan: &plan, PlanReceipt: &receipt},
					resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_CONFLICT",
					platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
			}
			_ = operationEffectsTx.Rollback(ctx)
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, fmt.Errorf("apply assistant plan operation: %w", err)
		}
		if operation.Type != createProjectFileRevision {
			created = append(created, outcome.resourceRef)
		}
		if connection := outcome.result.Connection; grantConnectionRef != "" && connection != nil && connection.Ref == grantConnectionRef && connection.Version > 0 {
			appliedGrantConnectionVersions[grantConnectionRef] = connection.Version
		}
		if agentVersionKey != "" && outcome.result.Agent != nil && outcome.result.Agent.Ref == agentVersionKey && outcome.result.Agent.Version > 0 {
			appliedAgentVersions[agentVersionKey] = outcome.result.Agent.Version
		}
		if view := outcome.result.RuntimeConfiguration; agentVersionKey != "" && view != nil && view.Configuration.AgentRef == agentVersionKey && view.AgentVersion > 0 {
			appliedAgentVersions[agentVersionKey] = view.AgentVersion
			if planned.Kind == command.BindAgentRuntimeEnvironment {
				appliedBindingPins[agentVersionKey] = map[string]any{"runtimeEnvironmentBindingRef": view.EnvironmentBinding.Ref, "runtimeEnvironmentVersionRef": view.EnvironmentBinding.VersionRef, "runtimeEnvironmentDigest": view.Environment.CurrentVersion.Digest}
			}
		}
		if outcome.projectID != "" {
			projectID, projectRef = outcome.projectID, outcome.projectRef
		}
		auditRef, err := repository.auditAssistantOperation(ctx, operationEffectsTx, scope, conversationRef, outcome, operation.Type)
		if err != nil {
			_ = operationEffectsTx.Rollback(ctx)
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, fmt.Errorf("audit assistant plan operation: %w", err)
		}
		operationReceipt := entity.AssistantPlanOperationReceipt{OperationRef: operation.Key, ResourceRef: outcome.resourceRef, Outcome: "APPLIED", AuditRef: auditRef}
		if operation.Type == createProjectFileRevision && outcome.result.Artifact != nil {
			item := outcome.result.Artifact
			immutable, readErr := artifactRevisionRow(operationEffectsTx.QueryRow(ctx, queryArtifactRevisionGet, pgx.StrictNamedArgs{
				"organization_id": scope.organizationID, "artifact_ref": item.Ref, "revision_ref": item.CurrentRevisionRef}))
			if readErr != nil {
				_ = operationEffectsTx.Rollback(ctx)
				_ = effectTx.Rollback(ctx)
				return commandOutcome{}, readErr
			}
			operationReceipt.ArtifactRevision = &immutable
		}
		operationReceipts = append(operationReceipts, operationReceipt)
		if outcome.platformEvent != "" {
			if err := repository.emitCommandOutcomePlatformEvent(ctx, operationEffectsTx, scope, outcome); err != nil {
				_ = operationEffectsTx.Rollback(ctx)
				_ = effectTx.Rollback(ctx)
				return commandOutcome{}, fmt.Errorf("emit assistant plan operation event: %w", err)
			}
		}
	}
	if err := operationEffectsTx.Commit(ctx); err != nil {
		_ = effectTx.Rollback(ctx)
		return commandOutcome{}, fmt.Errorf("commit assistant plan operation effects: %w", errs.ErrConflict)
	}
	responseConversationProjectRef := conversationProjectRef
	if conversationProjectRef == "" && assistantPlanCreatesSingleProject(operations) && projectID != "" && projectRef != "" {
		if err := repository.promoteAssistantConversationProject(ctx, effectTx, scope, conversationRef, projectID, projectRef); err != nil {
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, err
		}
		responseConversationProjectRef = projectRef
	}
	if _, err := effectTx.Exec(ctx, queryConfigurationApplyassistantplancommandUpdateAssistantPlansStateVersionAppliedAt, planID); err != nil {
		_ = effectTx.Rollback(ctx)
		return commandOutcome{}, fmt.Errorf("mark assistant plan applied: %w", errs.ErrUnavailable)
	}
	receipt, err := repository.insertAssistantPlanReceipt(ctx, effectTx, scope, planID, payload.PlanRef, revision,
		"APPLIED", operationReceipts, nil, created)
	if err != nil {
		_ = effectTx.Rollback(ctx)
		return commandOutcome{}, fmt.Errorf("insert assistant plan receipt: %w", err)
	}
	if err := effectTx.Commit(ctx); err != nil {
		return commandOutcome{}, fmt.Errorf("commit assistant plan effects: %w", errs.ErrConflict)
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: conversationProjectRef,
		Summary: summary, State: "APPLIED", Version: version + 1, Revision: revision, ValidatedRevision: validatedRevision,
		ContentDigest: digest, Operations: operations, AppliedAt: timePointer(time.Now().UTC())}
	conversation := entity.AssistantConversation{Ref: conversationRef, ProjectRef: responseConversationProjectRef}
	return commandOutcome{result: command.Result{Conversation: &conversation, Plan: &plan, PlanReceipt: &receipt, CreatedRefs: created}, projectID: mustProjectID(ctx, tx, scope.organizationID, responseConversationProjectRef), projectRef: responseConversationProjectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_APPLIED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// assistantPlanCreatesSingleProject returns true only when exactly one selected operation creates
// a project; unselected operations are ignored and any other selected kind prevents conversation
// promotion.
// assistantPlanCreatesSingleProject возвращает true лишь когда ровно одна выбранная операция
// создаёт project; невыбранные операции игнорируются, любой другой выбранный kind запрещает
// conversation promotion.
func assistantPlanCreatesSingleProject(operations []entity.AssistantPlanOperation) bool {
	selected := 0
	for _, operation := range operations {
		if !operation.Selected {
			continue
		}
		selected++
		if operation.Type != "CREATE_PROJECT" {
			return false
		}
	}
	return selected == 1
}

// promoteAssistantConversationProject promotes the owned session, durable storage lineage and
// conversation to the newly created project in order, checking returned identity/lineage proof
// before emitting session-storage run changes. Caller transaction rollback retains atomicity
// across all steps.
// promoteAssistantConversationProject последовательно переводит собственные session, durable
// storage lineage и conversation в новый project, проверяя возвращённые identity/lineage proof до
// события session-storage run changes. Откат транзакции caller сохраняет атомарность всех шагов.
func (repository *Repository) promoteAssistantConversationProject(ctx context.Context, tx pgx.Tx, scope scope,
	conversationRef, projectID, projectRef string,
) error {
	var sessionID string
	if err := tx.QueryRow(ctx, queryConfigurationApplyassistantplancommandPromoteSessionProject,
		projectID, projectRef, scope.organizationID, scope.actorID, conversationRef,
	).Scan(&sessionID); err != nil {
		return fmt.Errorf("promote assistant session project: %w", errs.ErrConflict)
	}
	var lineagePromoted bool
	if err := tx.QueryRow(ctx, queryConfigurationApplyassistantplancommandPromoteSessionStorageProject,
		projectID, sessionID, scope.organizationID,
	).Scan(&lineagePromoted); err != nil {
		return fmt.Errorf("promote assistant session storage project: %w", errs.ErrUnavailable)
	}
	if !lineagePromoted {
		return fmt.Errorf("promote assistant session lineage project: %w", errs.ErrConflict)
	}
	var promotedRef string
	if err := tx.QueryRow(ctx, queryConfigurationApplyassistantplancommandPromoteConversationProject,
		scope.organizationID, scope.actorID, projectID, projectRef, conversationRef,
	).Scan(&promotedRef); err != nil || promotedRef != conversationRef {
		return fmt.Errorf("promote assistant conversation project: %w", errs.ErrConflict)
	}
	return repository.emitSessionStorageRunChanged(ctx, tx, scope, sessionID)
}

// valueOrNil projects an optional int64 pointer to its value or a nil interface for conflict
// metadata without allocating authority or modifying the source.
// valueOrNil проецирует необязательный int64 pointer в его значение либо nil interface для
// conflict metadata без назначения authority или изменения исходника.
func valueOrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// assistantOperationConflict projects LAUNCH_RUN conflicts as runtime READY/UNAVAILABLE and other
// kinds as expected-version/CHANGED, preserving operation/target locators and a fixed safe problem
// code. It does not inspect a provider or prove the underlying cause.
// assistantOperationConflict проецирует LAUNCH_RUN conflicts как runtime READY/UNAVAILABLE,
// остальные kinds как expected-version/CHANGED, сохраняя operation/target locators и фиксированный
// безопасный problem code. Не проверяет провайдера и не доказывает исходную причину.
func assistantOperationConflict(operation entity.AssistantPlanOperation) (entity.AssistantPlanConflict, string) {
	if operation.Type == "LAUNCH_RUN" {
		return entity.AssistantPlanConflict{OperationRef: operation.Key, TargetRef: operation.Target.Ref,
			Field: "runtime", Expected: "READY", Actual: "UNAVAILABLE"}, "operation-runtime-unavailable"
	}
	return entity.AssistantPlanConflict{OperationRef: operation.Key, TargetRef: operation.Target.Ref,
		Field: "version", Expected: valueOrNil(operation.ExpectedVersion), Actual: "CHANGED"}, "operation-version-conflict"
}

// auditAssistantOperation allocates an audit reference and inserts one assistant-plan operation
// record using resolved actor/project/resource metadata and conversation lineage. Entropy failure
// propagates; SQL failure or a non-single affected row maps to Unavailable, without committing.
// auditAssistantOperation назначает audit reference и вставляет одну assistant-plan operation
// запись с разрешёнными actor/project/resource metadata и conversation lineage. Ошибка entropy
// передаётся; SQL failure либо не одна затронутая строка дают Unavailable без commit.
func (repository *Repository) auditAssistantOperation(ctx context.Context, tx pgx.Tx, scope scope, conversationRef string, outcome commandOutcome, action string) (string, error) {
	ref, err := newRef("aud")
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, queryConfigurationAuditassistantoperationInsertAuditEventsRefProjectIdAssistantAgentId, ref, scope.organizationID, nullUUID(outcome.projectID), scope.actorID, "assistant."+strings.ToLower(action), outcome.resourceKind, outcome.resourceRef, outcome.summary, "assistant-plan", conversationRef)
	if err != nil || tag.RowsAffected() != 1 {
		return "", errs.ErrUnavailable
	}
	return ref, nil
}

// nullUUID returns nil for an empty internal UUID string and otherwise preserves the string
// unchanged for nullable SQL arguments; it does not validate UUID syntax.
// nullUUID возвращает nil для пустой внутренней UUID string, иначе сохраняет строку для nullable
// SQL arguments; синтаксис UUID не проверяет.
func nullUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// timePointer returns a pointer to an independent copy of the supplied timestamp, preserving its
// instant and location without reading the clock.
// timePointer возвращает указатель на независимую копию переданного timestamp, сохраняя момент и
// location без чтения часов.
func timePointer(value time.Time) *time.Time { return &value }

// stringValue returns a string dynamic value unchanged and maps nil or other types to empty; it
// performs no formatting or validation.
// stringValue возвращает строковое dynamic value без изменений, nil либо другие types отображает в
// пустую строку; не форматирует и не проверяет значение.
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

// updateAssistantInstructions checks typed owner instructions, expected version and the pre-trim
// byte limit, updates organization runtime instructions with OCC and returns assistant-change
// metadata. Resource-limit JSON decode errors remain ignored; system/nondeletable flags are set
// without starting a runtime.
// updateAssistantInstructions проверяет typed owner instructions, expected version и byte limit до
// trim, обновляет organization runtime instructions с OCC и возвращает assistant-change metadata.
// Ошибки resource-limit JSON decode по-прежнему игнорируются; system/nondeletable flags
// назначаются без запуска runtime.
func (repository *Repository) updateAssistantInstructions(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantInstructionsInput)
	if !ok || input.Mutation.ExpectedVersion == nil || len(payload.Instructions) > 20000 {
		return commandOutcome{}, errs.ErrInvalid
	}
	var assistant entity.SystemAssistant
	var limits []byte
	err := tx.QueryRow(ctx, queryConfigurationUpdateassistantinstructionsUpdateAssistantRuntimeOwnerInstructionsVersionUpdatedAt, scope.organizationID, *input.Mutation.ExpectedVersion, strings.TrimSpace(payload.Instructions)).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.System = true
	assistant.Deletable = false
	return commandOutcome{result: command.Result{Assistant: &assistant}, resourceKind: "SYSTEM_ASSISTANT", resourceRef: assistant.StableKey, summary: "i18n:ASSISTANT_INSTRUCTIONS_UPDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// recoverAssistant requires expected version, executes the canonical recovery-state reset and
// rereads assistant metadata. Any reset SQL error or affected-row mismatch retains the existing
// VersionMismatch result; this requests recovery rather than proving a restarted runtime.
// recoverAssistant требует expected version, выполняет канонический recovery-state reset и
// перечитывает assistant metadata. Любая reset SQL error либо affected-row mismatch сохраняет
// прежний VersionMismatch; запрашивает восстановление, не доказывая перезапуск runtime.
func (repository *Repository) recoverAssistant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	if input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	tag, err := tx.Exec(ctx, queryConfigurationRecoverassistantUpdateAssistantRuntimeRuntimeStateWarmInstanceRefLastHeartbeatAt, scope.organizationID, *input.Mutation.ExpectedVersion)
	if err != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	assistant, err := repository.getAssistantTx(ctx, tx, scope)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Assistant: &assistant}, resourceKind: "SYSTEM_ASSISTANT", resourceRef: assistant.StableKey, summary: "i18n:ASSISTANT_RECOVERY_REQUESTED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// getAssistantTx reads organization system-assistant metadata and derives Ready only from
// READY/BUSY plus a heartbeat less than45 seconds old. Resource-limit JSON errors remain ignored;
// system/nondeletable flags and role-based action hints do not prove provider or environment
// readiness.
// getAssistantTx читает organization system-assistant metadata и выводит Ready лишь из READY/BUSY
// и heartbeat моложе45 секунд. Ошибки resource-limit JSON игнорируются; system/nondeletable flags
// и role-based action hints не доказывают provider либо environment readiness.
func (repository *Repository) getAssistantTx(ctx context.Context, tx pgx.Tx, scope scope) (entity.SystemAssistant, error) {
	var item entity.SystemAssistant
	var limits []byte
	err := tx.QueryRow(ctx, queryConfigurationGetassistanttxSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &item.ResourceLimits)
	item.Ready = contains([]string{"READY", "BUSY"}, item.RuntimeState) && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) < 45*time.Second
	item.System = true
	item.Deletable = false
	item.NextActions = assistantActions(scope.role, item.Ready)
	return item, nil
}

// applyScheduleContentDefaults trims explicit automation text, falls back to the unchanged legacy
// task string or name, and allocates prompt inputs only when nil. It intentionally retains legacy
// task whitespace and caller map identity; validation and timing policies remain separate.
// applyScheduleContentDefaults обрезает пробелы явного automation text, использует неизменённую
// legacy task string либо name и создаёт prompt inputs только при nil. Намеренно сохраняет
// пробелы legacy task и identity map caller; validation и timing policies остаются отдельными.
func applyScheduleContentDefaults(payload *command.ScheduleInput) {
	payload.AutomationText = strings.TrimSpace(payload.AutomationText)
	if payload.AutomationText == "" {
		payload.AutomationText, _ = payload.Input["task"].(string)
		if strings.TrimSpace(payload.AutomationText) == "" {
			payload.AutomationText = payload.Name
		}
	}
	if payload.PromptInputs == nil {
		payload.PromptInputs = map[string]any{}
	}
}

// finishScheduleLifecycleMutation reads the shared nineteen-column archive/delete projection,
// maps missing rows to VersionMismatch and other scan failures to Unavailable, then cancels
// claimed occurrences and decodes input/display in the original order. Callers keep distinct
// SQL/state/OCC/response policies; this helper neither checks authority nor commits. Errors return
// zero project locators, and callers discard any partially populated internal schedule.
// finishScheduleLifecycleMutation читает общую проекцию девятнадцати колонок archive/delete,
// отображает отсутствие строки в VersionMismatch, прочие scan failures в Unavailable, затем
// отменяет claimed occurrences и декодирует input/display в прежнем порядке. Callers сохраняют
// разные SQL/state/OCC/response policies; helper не проверяет authority и не делает commit.
// Ошибки возвращают пустые project locators; callers отбрасывают частично заполненный schedule.
func finishScheduleLifecycleMutation(ctx context.Context, tx pgx.Tx, scheduleID string, row pgx.Row, item *entity.Schedule) (string, string, error) {
	var projectID, projectRef string
	var raw []byte
	err := row.Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Target.Type, &item.Target.Ref,
		&item.Preset, &item.CronExpression, &item.Timezone, &raw,
		&item.SessionPolicy, &item.NotificationPolicy, &item.State, &item.Enabled,
		&item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errs.ErrVersionMismatch
	}
	if err != nil {
		return "", "", errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationChangescheduleCancelClaimedOccurrences, pgx.StrictNamedArgs{"schedule_id": scheduleID}); err != nil {
		return "", "", errs.ErrUnavailable
	}
	item.ProjectRef = projectRef
	if json.Unmarshal(raw, &item.Input) != nil || attachScheduleDisplay(item) != nil {
		return "", "", errs.ErrUnavailable
	}
	return projectID, projectRef, nil
}
