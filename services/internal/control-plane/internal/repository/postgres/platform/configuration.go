package platform

import (
	"context"
	"encoding/json"
	"errors"
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
		item.NextActions = []string{"OPEN", "EDIT", "DISABLE"}
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
	item.NextActions = []string{"OPEN", "EDIT"}
	if item.Enabled {
		item.NextActions = append(item.NextActions, "DISABLE")
	} else {
		item.NextActions = append(item.NextActions, "ENABLE")
	}
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
		if strings.TrimSpace(payload.CredentialMaterializationRef) == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, _ := newRef("int")
		var item entity.IntegrationConnection
		var config []byte
		err := tx.QueryRow(ctx, queryConfigurationChangeconnectionInsertIntegrationConnectionsRefDefinitionKeyState, ref, scope.organizationID, payload.Name, payload.CredentialMaterializationRef, asJSON(payload.PublicConfiguration), scope.actorID, payload.DefinitionKey).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.Enabled, &item.Version, &config, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		_ = json.Unmarshal(config, &item.PublicConfiguration)
		item.NextActions = []string{"OPEN", "TEST", "DISABLE"}
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
	item.NextActions = []string{"OPEN"}
	if item.State != "TESTING" {
		item.NextActions = append(item.NextActions, "TEST")
	}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref, summary: "i18n:INTEGRATION_CONNECTION_UPDATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
}

func (repository *Repository) changeIntegrationGrant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationGrantInput)
	if !ok || payload.ConnectionRef == "" || payload.CapabilityKey == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	targetType, targetRef := "AGENT", payload.AgentRef
	if targetRef == "" {
		targetType, targetRef = "WORKFLOW", payload.WorkflowRef
	}
	if targetRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var connectionID, definitionKey string
	if err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantSelectIntegrationConnectionsOrganizationIdRefEnabled, scope.organizationID, payload.ConnectionRef).Scan(&connectionID, &definitionKey); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	var capabilities []byte
	if err := tx.QueryRow(ctx, queryConfigurationChangeintegrationgrantSelectIntegrationDefinitionsStableKey, definitionKey).Scan(&capabilities); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var catalog []entity.IntegrationCapability
	_ = json.Unmarshal(capabilities, &catalog)
	valid := false
	risk := "LOW"
	for _, capability := range catalog {
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
	connection := entity.IntegrationConnection{Ref: payload.ConnectionRef}
	return commandOutcome{result: command.Result{Connection: &connection}, resourceKind: "INTEGRATION_GRANT", resourceRef: grantRef, summary: "i18n:INTEGRATION_GRANT_UPDATED", platformEvent: "INTEGRATION_GRANT_CHANGED"}, nil
}
func approvalPolicy(risk string) string {
	if risk == "HIGH" {
		return "OWNER_EACH_EFFECT"
	}
	return "OWNER_FOR_HIGH_RISK"
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
	var sessionID string
	if err := tx.QueryRow(ctx, queryConfigurationCreateassistantconversationInsertSessionsRefProjectIdTargetRef, sessionRef, scope.organizationID, projectID, scope.actorID).Scan(&sessionID); err != nil {
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
		return commandOutcome{}, errs.ErrUnavailable
	}
	var conversationID, sessionID, sessionRef string
	var projectID *string
	var projectRef string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState, scope.organizationID, payload.ConversationRef).Scan(&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	turnRef, _ := newRef("trn")
	var turnNumber int64
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectSessionsId, sessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandInsertSessionTurnsRefSessionIdActorKind, turnRef, scope.organizationID, sessionID, turnNumber, scope.actorRef, payload.Content, payload.ArtifactRefs); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, _ = tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID)
	runRef, _ := newRef("run")
	var runID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertRunsRefProjectIdTargetType, runRef, scope.organizationID, projectID, sessionID, payload.Content, scope.actorID).Scan(&runID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, _ = tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateRunsRootRunIdStartedAt, runID)
	nodeRef, _ := newRef("nod")
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandInsertRunNodesRefRootRunIdType, nodeRef, scope.organizationID, runID, truncate(payload.Content, 1000)); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := assistantFallbackPlan(payload.Content, projectRef)
	var latestPlanID any
	if plan != nil {
		planRef, _ := newRef("pln")
		plan.Ref = planRef
		plan.State = "PROPOSED"
		plan.Version = 1
		plan.CreatedAt = time.Now().UTC()
		var planID string
		if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations, planRef, scope.organizationID, payload.ConversationRef, plan.Summary, asJSON(plan.Operations)).Scan(&planID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		latestPlanID = planID
	} else {
		latestPlanID = nil
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt, conversationID, latestPlanID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	projectValue := ""
	if projectID != nil {
		projectValue = *projectID
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectValue, runID, runRef, "TURN_QUEUED", nodeRef, "", "", "", "i18n:ASSISTANT_TURN_QUEUED", "RUNNING", "QUEUED"); err != nil {
		return commandOutcome{}, err
	}
	conversation := entity.AssistantConversation{Ref: payload.ConversationRef, ProjectRef: projectRef, SessionRef: sessionRef, State: "ACTIVE", Version: version + 1, LatestPlan: plan}
	conversation.Turns = []entity.AssistantTurn{{Ref: turnRef, Actor: "USER", ActorName: scope.actorName, Content: payload.Content, ArtifactRefs: payload.ArtifactRefs, State: "COMPLETED", CreatedAt: time.Now().UTC()}}
	assistant, err := repository.getAssistantTx(ctx, tx, scope)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Conversation: &conversation, Assistant: &assistant, Plan: plan}, projectID: projectValue, projectRef: projectRef, resourceKind: "ASSISTANT_TURN", resourceRef: turnRef, summary: "i18n:ASSISTANT_TURN_ACCEPTED"}, nil
}

func assistantFallbackPlan(content, projectRef string) *entity.AssistantPlan {
	normalized := strings.ToLower(strings.TrimSpace(content))
	name := quotedName(content)
	if strings.Contains(normalized, "создай проект") || strings.Contains(normalized, "create project") {
		if name == "" {
			name = "Новый проект"
		}
		return &entity.AssistantPlan{Summary: "Создать проект «" + name + "»", Operations: []entity.AssistantPlanOperation{{Key: "create-project", Type: "CREATE_PROJECT", Summary: "Создать универсальный проект", TargetKind: "PROJECT", Input: map[string]any{"name": name, "purpose": "Создано системным помощником", "language": "ru"}}}}
	}
	if projectRef != "" && (strings.Contains(normalized, "создай агента") || strings.Contains(normalized, "create agent")) {
		if name == "" {
			name = "Новый сотрудник"
		}
		return &entity.AssistantPlan{Summary: "Создать агента «" + name + "»", Operations: []entity.AssistantPlanOperation{{Key: "create-agent", Type: "CREATE_AGENT", Summary: "Создать агента в выбранном проекте", TargetKind: "AGENT", Input: map[string]any{"projectRef": projectRef, "name": name, "purpose": "Помощь команде", "roleDescription": "Выполняй поручения в рамках проекта", "instructions": "Уточняй ожидаемый результат, выполняй задачу последовательно и возвращай проверяемый итог."}}}}
	}
	return nil
}
func quotedName(content string) string {
	for _, pair := range [][2]string{{"«", "»"}, {"\"", "\""}} {
		start := strings.Index(content, pair[0])
		if start < 0 {
			continue
		}
		rest := content[start+len(pair[0]):]
		end := strings.Index(rest, pair[1])
		if end > 0 {
			return truncate(rest[:end], 160)
		}
	}
	return ""
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
		switch operation.Type {
		case "CREATE_PROJECT":
			if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
				return commandOutcome{}, errs.ErrForbidden
			}
			name, _ := operation.Input["name"].(string)
			purpose, _ := operation.Input["purpose"].(string)
			language, _ := operation.Input["language"].(string)
			outcome, err := repository.createProject(ctx, tx, scope, command.ProjectInput{Name: name, Purpose: purpose, Language: language})
			if err != nil {
				return commandOutcome{}, err
			}
			created = append(created, outcome.resourceRef)
			projectID, projectRef = outcome.projectID, outcome.projectRef
			if err := repository.auditAssistantOperation(ctx, tx, scope, outcome, operation.Type); err != nil {
				return commandOutcome{}, err
			}
		case "CREATE_AGENT":
			project, _ := operation.Input["projectRef"].(string)
			allowedProjectID := mustProjectID(ctx, tx, scope.organizationID, project)
			if allowedProjectID == "" {
				return commandOutcome{}, errs.ErrNotFound
			}
			if err := requireProjectPermission(ctx, tx, scope, allowedProjectID, "MANAGE_AGENTS"); err != nil {
				return commandOutcome{}, err
			}
			name, _ := operation.Input["name"].(string)
			purpose, _ := operation.Input["purpose"].(string)
			role, _ := operation.Input["roleDescription"].(string)
			instructions, _ := operation.Input["instructions"].(string)
			outcome, err := repository.createAgent(ctx, tx, scope, command.AgentInput{ProjectRef: project, Name: name, Purpose: purpose, RoleDescription: role, Instructions: instructions})
			if err != nil {
				return commandOutcome{}, err
			}
			created = append(created, outcome.resourceRef)
			projectID, projectRef = outcome.projectID, outcome.projectRef
			if err := repository.auditAssistantOperation(ctx, tx, scope, outcome, operation.Type); err != nil {
				return commandOutcome{}, err
			}
		default:
			return commandOutcome{}, errs.ErrInvalid
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
	return item, nil
}
