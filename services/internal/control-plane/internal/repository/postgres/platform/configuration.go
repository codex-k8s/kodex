package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) changeSchedule(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ScheduleInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateSchedule {
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		if !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, _ := newRef("sch")
		var item entity.Schedule
		var next *time.Time
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleInsertSchedulesRefProjectIdTargetType, ref, scope.organizationID, projectID, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, scope.actorID).Scan(&item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &next, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.ProjectRef = payload.ProjectRef
		item.Target = payload.Target
		item.Input = payload.Input
		item.NextRunAt = next
		item.NextActions = scheduleActions(item, true)
		return commandOutcome{result: command.Result{Schedule: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "SCHEDULE", resourceRef: ref, summary: "i18n:SCHEDULE_CREATED", platformEvent: "SCHEDULE_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef string
	var item entity.Schedule
	if input.Kind == command.UpdateSchedule {
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleUpdateSchedulesNameTargetTypeTargetRef, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.Target = payload.Target
		item.Input = payload.Input
	} else {
		err := tx.QueryRow(ctx, queryConfigurationChangescheduleUpdateSchedulesEnabledVersionUpdatedAt, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	item.ProjectRef = projectRef
	item.NextActions = scheduleActions(item, true)
	return commandOutcome{result: command.Result{Schedule: &item}, projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE", resourceRef: item.Ref, summary: "i18n:SCHEDULE_UPDATED", platformEvent: "SCHEDULE_CHANGED"}, nil
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
	case command.AddAssistantTurn:
		return repository.addAssistantTurnCommand(ctx, tx, scope, input)
	case command.ApplyAssistantPlan:
		return repository.applyAssistantPlanCommand(ctx, tx, scope, input)
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
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "i18n:NEW_ASSISTANT_CONVERSATION"
	}
	var item entity.AssistantConversation
	if err := tx.QueryRow(ctx, queryConfigurationCreateassistantconversationInsertAssistantConversationsRefProjectIdTitle, ref, scope.organizationID, projectID, sessionID, title, scope.actorID).Scan(&item.Ref, &item.Title, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.ProjectRef = payload.ProjectRef
	item.SessionRef = sessionRef
	return commandOutcome{result: command.Result{Conversation: &item}, projectID: stringValue(projectID), projectRef: payload.ProjectRef, resourceKind: "ASSISTANT_CONVERSATION", resourceRef: ref, summary: "i18n:ASSISTANT_CONVERSATION_CREATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
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
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind, turnRef, scope.organizationID, sessionID, turnNumber, scope.actorRef, payload.Content, artifactRefs); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant user turn: %w", errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return commandOutcome{}, fmt.Errorf("advance system assistant session: %w", errs.ErrUnavailable)
	}
	runRef, _ := newRef("run")
	var runID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType, runRef, scope.organizationID, projectID, sessionID, payload.Content, scope.actorID).Scan(&runID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant run: %w", errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt, runID); err != nil {
		return commandOutcome{}, fmt.Errorf("start system assistant root run: %w", errs.ErrUnavailable)
	}
	nodeRef, _ := newRef("nod")
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType, nodeRef, scope.organizationID, runID, truncate(payload.Content, 1000)); err != nil {
		return commandOutcome{}, fmt.Errorf("insert system assistant execution node: %w", errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateAssistantConversationsVersionUpdatedAt, conversationID); err != nil {
		return commandOutcome{}, fmt.Errorf("advance system assistant conversation: %w", errs.ErrUnavailable)
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, runID, runRef, "TURN_QUEUED", nodeRef, "", "", "", "i18n:ASSISTANT_TURN_QUEUED", "RUNNING", "QUEUED"); err != nil {
		return commandOutcome{}, err
	}
	conversation := entity.AssistantConversation{Ref: payload.ConversationRef, ProjectRef: projectRef, SessionRef: sessionRef, State: "ACTIVE", Version: version + 1}
	conversation.Turns = []entity.AssistantTurn{{Ref: turnRef, Actor: "USER", ActorName: scope.actorName, Content: payload.Content, ArtifactRefs: artifactRefs, State: "COMPLETED", CreatedAt: time.Now().UTC()}}
	assistant, err := repository.getAssistantTx(ctx, tx, scope)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Conversation: &conversation, Assistant: &assistant}, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_TURN", resourceRef: turnRef, summary: "i18n:ASSISTANT_TURN_ACCEPTED"}, nil
}

func (repository *Repository) applyAssistantPlanCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantPlanInput)
	if !ok || payload.PlanRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, conversationRef string
	var raw []byte
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationApplyassistantplancommandSelectAssistantPlansOrganizationIdRefState, scope.organizationID, payload.PlanRef).Scan(&planID, &conversationRef, &raw, &version); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var operations []entity.AssistantPlanOperation
	if json.Unmarshal(raw, &operations) != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	created := []string{}
	var projectID, projectRef string
	for _, operation := range operations {
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return commandOutcome{}, err
		}
		if err := repository.authorizeCommand(ctx, tx, scope, planned); err != nil {
			return commandOutcome{}, err
		}
		outcome, err := repository.applyCommand(ctx, tx, scope, planned)
		if err != nil {
			return commandOutcome{}, err
		}
		created = append(created, outcome.resourceRef)
		if outcome.projectID != "" {
			projectID, projectRef = outcome.projectID, outcome.projectRef
		}
		if err := repository.auditAssistantOperation(ctx, tx, scope, outcome, operation.Type); err != nil {
			return commandOutcome{}, err
		}
		if outcome.platformEvent != "" {
			if err := repository.emitPlatformEvent(ctx, tx, scope, outcome.platformEvent, outcome.projectRef, outcome.resourceRef, outcome.summary); err != nil {
				return commandOutcome{}, err
			}
		}
	}
	if _, err := tx.Exec(ctx, queryConfigurationApplyassistantplancommandUpdateAssistantPlansStateVersionAppliedAt, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, State: "APPLIED", Version: version + 1, Operations: operations, AppliedAt: timePointer(time.Now().UTC())}
	conversation := entity.AssistantConversation{Ref: conversationRef}
	return commandOutcome{result: command.Result{Conversation: &conversation, Plan: &plan, CreatedRefs: created}, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_APPLIED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) auditAssistantOperation(ctx context.Context, tx pgx.Tx, scope scope, outcome commandOutcome, action string) error {
	ref, err := newRef("aud")
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, queryConfigurationAuditassistantoperationInsertAuditEventsRefProjectIdAssistantAgentId, ref, scope.organizationID, nullUUID(outcome.projectID), scope.actorID, "system_assistant."+strings.ToLower(action), outcome.resourceKind, outcome.resourceRef, outcome.summary, "assistant-plan")
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrUnavailable
	}
	return nil
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
