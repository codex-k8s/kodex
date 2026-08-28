package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

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
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err := repository.validateScheduleTarget(ctx, tx, scope.organizationID, projectID, payload.Target); err != nil {
			return commandOutcome{}, err
		}
		ref, _ := newRef("sch")
		var item entity.Schedule
		var next *time.Time
		err = tx.QueryRow(ctx, queryConfigurationChangescheduleInsertSchedulesRefProjectIdTargetType, ref, scope.organizationID, projectID, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, normalized.Next, scope.actorID).Scan(&item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &next, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.ProjectRef = payload.ProjectRef
		item.Target = payload.Target
		item.Input = payload.Input
		item.TimeOfDay = payload.TimeOfDay
		item.DayOfWeek = payload.DayOfWeek
		item.NextRunAt = next
		item.NextActions = scheduleActions(item, true)
		return commandOutcome{result: command.Result{Schedule: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "SCHEDULE", resourceRef: ref, summary: "i18n:SCHEDULE_CREATED", platformEvent: "SCHEDULE_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var scheduleID, projectID, projectRef, storedPreset, storedCron, storedTimezone string
	var storedVersion int64
	if err := tx.QueryRow(ctx, queryConfigurationChangescheduleSelectScheduleForUpdate, scope.organizationID, payload.Ref).Scan(&scheduleID, &projectID, &projectRef, &storedPreset, &storedCron, &storedTimezone, &storedVersion); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if storedVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var item entity.Schedule
	if input.Kind == command.UpdateSchedule {
		normalized, normalizeErr := normalizeScheduleInput(payload, time.Now().UTC())
		if normalizeErr != nil {
			return commandOutcome{}, normalizeErr
		}
		if targetErr := repository.validateScheduleTarget(ctx, tx, scope.organizationID, projectID, payload.Target); targetErr != nil {
			return commandOutcome{}, targetErr
		}
		payload.CronExpression = normalized.CronExpression
		payload.TimeOfDay = normalized.TimeOfDay
		payload.DayOfWeek = normalized.DayOfWeek
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleUpdateSchedulesNameTargetTypeTargetRef, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, normalized.Next).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.Target = payload.Target
		item.Input = payload.Input
		item.TimeOfDay = payload.TimeOfDay
		item.DayOfWeek = payload.DayOfWeek
	} else {
		next, nextErr := scheduleservice.Next(storedPreset, storedCron, storedTimezone, time.Now().UTC())
		if nextErr != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleUpdateSchedulesEnabledVersionUpdatedAt, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled, next).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !payload.Enabled {
			if _, cancelErr := tx.Exec(ctx, queryConfigurationChangescheduleCancelClaimedOccurrences, scheduleID); cancelErr != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	}
	item.ProjectRef = projectRef
	if displayErr := attachScheduleDisplay(&item); displayErr != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.NextActions = scheduleActions(item, true)
	return commandOutcome{result: command.Result{Schedule: &item}, projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE", resourceRef: item.Ref, summary: "i18n:SCHEDULE_UPDATED", platformEvent: "SCHEDULE_CHANGED"}, nil
}

func normalizeScheduleInput(payload command.ScheduleInput, after time.Time) (scheduleservice.Normalized, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" || len(payload.Name) > 160 || !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) || payload.Target.Ref == "" || !contains([]string{"NEW_EACH_RUN", "CONTINUE_ONE"}, payload.SessionPolicy) || !contains([]string{"CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"}, payload.NotificationPolicy) {
		return scheduleservice.Normalized{}, errs.ErrInvalid
	}
	normalized, err := scheduleservice.Normalize(scheduleservice.Spec{Preset: payload.Preset, TimeOfDay: payload.TimeOfDay, DayOfWeek: payload.DayOfWeek, Timezone: payload.Timezone}, after)
	if err != nil {
		return scheduleservice.Normalized{}, errs.ErrInvalid
	}
	return normalized, nil
}

func (repository *Repository) validateScheduleTarget(ctx context.Context, tx pgx.Tx, organizationID, projectID string, target entity.RunTarget) error {
	query := queryConfigurationChangescheduleSelectAgentTarget
	if target.Type == "WORKFLOW" {
		query = queryConfigurationChangescheduleSelectWorkflowTarget
	} else if target.Type != "AGENT" {
		return errs.ErrInvalid
	}
	var id string
	if err := tx.QueryRow(ctx, query, organizationID, projectID, target.Ref).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func attachScheduleDisplay(item *entity.Schedule) error {
	timeOfDay, dayOfWeek, err := scheduleservice.Display(item.Preset, item.CronExpression)
	if err != nil {
		return err
	}
	item.TimeOfDay = timeOfDay
	item.DayOfWeek = dayOfWeek
	return nil
}

func (repository *Repository) changeConnection(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	if input.Kind == command.ChangeIntegrationGrant {
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
		var schema []byte
		if err := tx.QueryRow(ctx, queryConfigurationChangeconnectionSelectIntegrationDefinitionsStableKeyEnabled, payload.DefinitionKey).Scan(&schema); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var fields []entity.IntegrationConfigurationField
		if json.Unmarshal(schema, &fields) != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		configuration, valid := validateIntegrationConfiguration(fields, payload.PublicConfiguration)
		if !valid {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, _ := newRef("int")
		credentialRef := "icr_" + ref
		var item entity.IntegrationConnection
		var config []byte
		err := tx.QueryRow(ctx, queryConfigurationChangeconnectionInsertIntegrationConnectionsRefDefinitionKeyState, ref, scope.organizationID, payload.Name, credentialRef, asJSON(configuration), scope.actorID, payload.DefinitionKey).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.Enabled, &item.Version, &config, &item.CreatedAt, &item.UpdatedAt)
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
	var item entity.IntegrationConnection
	if input.Kind == command.TestConnection {
		var connectionID string
		err := tx.QueryRow(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionsStateLastTestSummaryVersion, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion).Scan(&connectionID, &item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
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
			_, _ = tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationGrantsEnabledVersionUpdatedAt, payload.Ref)
			_, _ = tx.Exec(ctx, queryConfigurationChangeconnectionUpdateIntegrationConnectionTestsStateLeaseRefFenceDigest, payload.Ref)
		}
	}
	item, err := readConnection(ctx, tx, scope, item.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref, summary: "i18n:INTEGRATION_CONNECTION_UPDATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
}

func (repository *Repository) changeIntegrationGrant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationGrantInput)
	if !ok || payload.ConnectionRef == "" || payload.CapabilityKey == "" || input.Mutation.ExpectedVersion == nil || (payload.AgentRef == "") == (payload.WorkflowRef == "") {
		return commandOutcome{}, errs.ErrInvalid
	}
	targetType, targetRef := "AGENT", payload.AgentRef
	if payload.WorkflowRef != "" {
		targetType, targetRef = "WORKFLOW", payload.WorkflowRef
	}
	var connectionID, definitionKey, connectionState string
	var connectionEnabled bool
	var connectionVersion int64
	if err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantSelectIntegrationConnectionsOrganizationIdRefEnabled, scope.organizationID, payload.ConnectionRef).Scan(&connectionID, &definitionKey, &connectionEnabled, &connectionState, &connectionVersion); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
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
	if err := tx.QueryRow(ctx, targetQuery, scope.organizationID, targetRef).Scan(&projectID, &projectRef, &targetName); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var capabilities []byte
	if err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantSelectIntegrationDefinitionsStableKey, definitionKey).Scan(&capabilities); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var catalog []entity.IntegrationCapability
	if json.Unmarshal(capabilities, &catalog) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	valid := false
	risk := "READ"
	for _, capability := range catalog {
		if !contains([]string{"READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"}, capability.Risk) {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if capability.Key == payload.CapabilityKey {
			valid = true
			risk = capability.Risk
		}
	}
	if !valid {
		return commandOutcome{}, errs.ErrInvalid
	}
	var grantRef string
	if payload.Enabled {
		grantRef, _ = newRef("grt")
		err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantInsertIntegrationGrantsRefConnectionIdTargetKind, grantRef, scope.organizationID, connectionID, payload.CapabilityKey, targetType, targetRef, approvalPolicy(risk), scope.actorID).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantUpdateIntegrationGrantsEnabledVersionUpdatedAt, scope.organizationID, connectionID, payload.CapabilityKey, targetType, targetRef).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
	}
	tag, err := tx.Exec(ctx, queryConfigurationChangeintegrationgrantUpdateIntegrationConnectionsVersion, connectionID, connectionVersion)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	connection, err := readConnection(ctx, tx, scope, payload.ConnectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &connection}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_GRANT", resourceRef: grantRef, summary: "i18n:INTEGRATION_GRANT_UPDATED", platformEvent: "INTEGRATION_GRANT_CHANGED"}, nil
}
func approvalPolicy(risk string) string {
	if risk == "SENSITIVE" || risk == "DESTRUCTIVE" {
		return "OWNER_EACH_EFFECT"
	}
	if risk == "WRITE" {
		return "OWNER_FOR_HIGH_RISK"
	}
	return "NONE"
}

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

func (repository *Repository) changeAssistant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.CreateAssistantConversation:
		return repository.createAssistantConversation(ctx, tx, scope, input)
	case command.UpdateAssistantConversation:
		return repository.updateAssistantConversationTitle(ctx, tx, scope, input)
	case command.AddAssistantTurn:
		return repository.addAssistantTurnCommand(ctx, tx, scope, input)
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
	providerAccountID, err := defaultProviderAccountID(ctx, tx, scope.organizationID)
	if err != nil {
		return commandOutcome{}, err
	}
	var sessionID string
	if err := tx.QueryRow(ctx, queryConfigurationCreateassistantconversationInsertSessionsRefProjectIdTargetRef, sessionRef, scope.organizationID, projectID, providerAccountID, scope.actorID).Scan(&sessionID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	ref, _ := newRef("cnv")
	resolvedContext, err := repository.resolveAssistantContext(ctx, tx, scope, payload.Context, payload.ProjectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	var item entity.AssistantConversation
	if err := tx.QueryRow(ctx, queryConfigurationCreateassistantconversationInsertAssistantConversationsRefProjectIdTitle,
		ref, scope.organizationID, projectID, sessionID, scope.actorID,
		resolvedContext.Route, resolvedContext.EntityKind, resolvedContext.EntityRef,
		resolvedContext.EntityName, resolvedContext.EntityVersion, resolvedContext.AllowedOperations,
	).Scan(&item.Ref, &item.Title, &item.TitleSource, &item.TitleRevision, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.ProjectRef = payload.ProjectRef
	item.SessionRef = sessionRef
	item.Context = resolvedContext
	return commandOutcome{result: command.Result{Conversation: &item}, projectID: stringValue(projectID), projectRef: payload.ProjectRef, resourceKind: "ASSISTANT_CONVERSATION", resourceRef: ref, summary: "i18n:ASSISTANT_CONVERSATION_CREATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) resolveAssistantContext(ctx context.Context, tx pgx.Tx, scope scope, context entity.AssistantContextDescriptor, projectRef string) (entity.AssistantContextDescriptor, error) {
	if len(context.Route) > 500 || len(context.EntityKind) > 80 || len(context.EntityRef) > 96 ||
		len(context.EntityName) > 300 {
		return entity.AssistantContextDescriptor{}, errs.ErrInvalid
	}
	if (context.EntityKind == "") != (context.EntityRef == "") || (context.EntityRef == "" && context.EntityVersion != nil) {
		return entity.AssistantContextDescriptor{}, errs.ErrInvalid
	}
	if context.EntityRef != "" {
		if context.EntityKind == "INTEGRATION_CONNECTION" {
			var version int64
			if err := tx.QueryRow(ctx, queryConfigurationResolveassistantcontextSelectConnection, scope.organizationID, context.EntityRef).Scan(&context.EntityName, &version); errors.Is(err, pgx.ErrNoRows) {
				return entity.AssistantContextDescriptor{}, errs.ErrNotFound
			} else if err != nil {
				return entity.AssistantContextDescriptor{}, errs.ErrUnavailable
			}
			context.EntityVersion = &version
			context.AllowedOperations = []string{"CHANGE_INTEGRATION_GRANT", "TEST_INTEGRATION_CONNECTION"}
			return context, nil
		}
		if !contains([]string{"PROJECT", "AGENT", "WORKFLOW", "RUN"}, context.EntityKind) {
			return entity.AssistantContextDescriptor{}, errs.ErrInvalid
		}
		var resolvedProjectID string
		var version int64
		if err := tx.QueryRow(ctx, queryConfigurationResolveassistantcontextSelectResource, scope.organizationID,
			context.EntityRef, context.EntityKind).Scan(&resolvedProjectID, &context.EntityName, &version); errors.Is(err, pgx.ErrNoRows) {
			return entity.AssistantContextDescriptor{}, errs.ErrNotFound
		} else if err != nil {
			return entity.AssistantContextDescriptor{}, errs.ErrUnavailable
		}
		context.EntityVersion = &version
		if projectRef != "" && resolvedProjectID != mustProjectID(ctx, tx, scope.organizationID, projectRef) {
			return entity.AssistantContextDescriptor{}, errs.ErrForbidden
		}
	} else {
		context.EntityName = ""
		context.EntityVersion = nil
	}
	context.AllowedOperations = []string{}
	switch context.EntityKind {
	case "":
		context.AllowedOperations = []string{"CREATE_PROJECT", "CREATE_INTEGRATION_CONNECTION"}
	case "PROJECT":
		context.AllowedOperations = []string{"CREATE_AGENT", "CREATE_WORKFLOW", "CREATE_SCHEDULE", "LAUNCH_RUN"}
	case "AGENT":
		context.AllowedOperations = []string{"CHANGE_CAPABILITY", "LAUNCH_RUN", "ARCHIVE_AGENT"}
	case "WORKFLOW":
		context.AllowedOperations = []string{"LAUNCH_RUN", "ARCHIVE_WORKFLOW"}
	case "INTEGRATION_CONNECTION":
		context.AllowedOperations = []string{"CHANGE_INTEGRATION_GRANT", "TEST_INTEGRATION_CONNECTION"}
	case "RUN":
		// Run context has no hidden configuration mutation in #997.
	default:
		return entity.AssistantContextDescriptor{}, errs.ErrInvalid
	}
	return context, nil
}

func (repository *Repository) addAssistantTurnCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantTurnInput)
	if !ok || payload.ConversationRef == "" || strings.TrimSpace(payload.Content) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var runtimeReady bool
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(&runtimeReady); err != nil || !runtimeReady {
		return commandOutcome{}, fmt.Errorf("read system assistant runtime readiness: %w", errs.ErrUnavailable)
	}
	var conversationID, sessionID, sessionRef string
	var projectID, projectRef string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState, scope.organizationID, payload.ConversationRef).Scan(&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &version); err != nil {
		return commandOutcome{}, fmt.Errorf("lock system assistant conversation: %w", errs.ErrNotFound)
	}
	if input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	turnRef, _ := newRef("trn")
	artifactRefs := append([]string(nil), payload.ArtifactRefs...)
	if artifactRefs == nil {
		artifactRefs = []string{}
	}
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectSessionsId, sessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, fmt.Errorf("lock system assistant session: %w", errs.ErrUnavailable)
	}
	runRef, _ := newRef("run")
	var runID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType, runRef, scope.organizationID, projectID, sessionID, payload.Content, scope.actorID).Scan(&runID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant run: %w", errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt, runID); err != nil {
		return commandOutcome{}, fmt.Errorf("start system assistant root run: %w", errs.ErrUnavailable)
	}
	var turnID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind,
		turnRef, scope.organizationID, sessionID, runID, turnNumber, scope.actorRef, payload.Content, artifactRefs,
	).Scan(&turnID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant user turn: %w", errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return commandOutcome{}, fmt.Errorf("advance system assistant session: %w", errs.ErrUnavailable)
	}
	nodeRef, _ := newRef("nod")
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType, nodeRef, scope.organizationID, runID, turnID, truncate(payload.Content, 1000)); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant execution node: %w", errs.ErrUnavailable)
	}
	conversation := entity.AssistantConversation{
		Ref: payload.ConversationRef, ProjectRef: projectRef, SessionRef: sessionRef,
	}
	if err := tx.QueryRow(
		ctx,
		queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt,
		conversationID,
	).Scan(
		&conversation.Title,
		&conversation.State,
		&conversation.Version,
		&conversation.CreatedAt,
		&conversation.UpdatedAt,
	); err != nil {
		return commandOutcome{}, fmt.Errorf("advance system assistant conversation: %w", errs.ErrUnavailable)
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, runID, runRef, "TURN_QUEUED", nodeRef, "", "", "", "i18n:ASSISTANT_TURN_QUEUED", "RUNNING", "QUEUED"); err != nil {
		return commandOutcome{}, err
	}
	conversation.Turns = []entity.AssistantTurn{{Ref: turnRef, Actor: "USER", ActorName: scope.actorName, Content: payload.Content, ArtifactRefs: artifactRefs, State: "COMPLETED", CreatedAt: time.Now().UTC()}}
	assistant, err := repository.getAssistantTx(ctx, tx, scope)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Conversation: &conversation, Assistant: &assistant}, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_TURN", resourceRef: turnRef, summary: "i18n:ASSISTANT_TURN_ACCEPTED"}, nil
}

func (repository *Repository) applyAssistantPlanCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
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
	effectTx, err := tx.Begin(ctx)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	created := []string{}
	operationReceipts := []entity.AssistantPlanOperationReceipt{}
	var projectID, projectRef string
	for _, operation := range operations {
		if !operation.Selected {
			continue
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, err
		}
		if err := repository.authorizeCommand(ctx, effectTx, scope, planned); err != nil {
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, err
		}
		outcome, err := repository.applyCommand(ctx, effectTx, scope, planned)
		if err != nil {
			_ = effectTx.Rollback(ctx)
			if errors.Is(err, errs.ErrVersionMismatch) || errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) {
				conflicts := []entity.AssistantPlanConflict{{OperationRef: operation.Key, TargetRef: operation.Target.Ref,
					Field: "version", Expected: valueOrNil(operation.ExpectedVersion), Actual: "CHANGED"}}
				if _, updateErr := tx.Exec(ctx, queryConfigurationMarkAssistantPlanStale, planID, []string{"operation-version-conflict"}); updateErr != nil {
					return commandOutcome{}, errs.ErrUnavailable
				}
				receipt, receiptErr := repository.insertAssistantPlanReceipt(ctx, tx, scope, planID, payload.PlanRef,
					revision, "CONFLICT", nil, conflicts, nil)
				if receiptErr != nil {
					return commandOutcome{}, receiptErr
				}
				plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: conversationProjectRef,
					Summary: summary, State: "STALE", Version: version + 1, Revision: revision, ValidatedRevision: validatedRevision,
					ContentDigest: digest, ValidationProblems: []string{"operation-version-conflict"}, Operations: operations}
				conversation := entity.AssistantConversation{Ref: conversationRef}
				return commandOutcome{result: command.Result{Conversation: &conversation, Plan: &plan, PlanReceipt: &receipt},
					resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_CONFLICT",
					platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
			}
			return commandOutcome{}, fmt.Errorf("apply assistant plan operation: %w", err)
		}
		created = append(created, outcome.resourceRef)
		if outcome.projectID != "" {
			projectID, projectRef = outcome.projectID, outcome.projectRef
		}
		auditRef, err := repository.auditAssistantOperation(ctx, effectTx, scope, outcome, operation.Type)
		if err != nil {
			_ = effectTx.Rollback(ctx)
			return commandOutcome{}, fmt.Errorf("audit assistant plan operation: %w", err)
		}
		operationReceipts = append(operationReceipts, entity.AssistantPlanOperationReceipt{OperationRef: operation.Key,
			ResourceRef: outcome.resourceRef, Outcome: "APPLIED", AuditRef: auditRef})
		if outcome.platformEvent != "" {
			if err := repository.emitPlatformEvent(ctx, effectTx, scope, outcome.platformEvent, outcome.projectRef, outcome.resourceRef, outcome.summary); err != nil {
				_ = effectTx.Rollback(ctx)
				return commandOutcome{}, fmt.Errorf("emit assistant plan operation event: %w", err)
			}
		}
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
	conversation := entity.AssistantConversation{Ref: conversationRef}
	return commandOutcome{result: command.Result{Conversation: &conversation, Plan: &plan, PlanReceipt: &receipt, CreatedRefs: created}, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_APPLIED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func valueOrNil(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func (repository *Repository) auditAssistantOperation(ctx context.Context, tx pgx.Tx, scope scope, outcome commandOutcome, action string) (string, error) {
	ref, err := newRef("aud")
	if err != nil {
		return "", err
	}
	tag, err := tx.Exec(ctx, queryConfigurationAuditassistantoperationInsertAuditEventsRefProjectIdAssistantAgentId, ref, scope.organizationID, nullUUID(outcome.projectID), scope.actorID, "system_assistant."+strings.ToLower(action), outcome.resourceKind, outcome.resourceRef, outcome.summary, "assistant-plan")
	if err != nil || tag.RowsAffected() != 1 {
		return "", errs.ErrUnavailable
	}
	return ref, nil
}
func nullUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func timePointer(value time.Time) *time.Time { return &value }
func stringValue(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

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
