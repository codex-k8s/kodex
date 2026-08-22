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
		err := tx.QueryRow(ctx, `INSERT INTO control_plane.schedules(ref,organization_id,project_id,name,target_type,target_ref,preset,cron_expression,timezone,input,session_policy,notification_policy,enabled,next_run_at,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,true,clock_timestamp()+interval '1 minute',$13::uuid) RETURNING ref,name,preset,cron_expression,timezone,session_policy,notification_policy,enabled,version,next_run_at,last_run_at,created_at,updated_at`, ref, scope.organizationID, projectID, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy, scope.actorID).Scan(&item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &next, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.ProjectRef = payload.ProjectRef
		item.Target = payload.Target
		item.Input = payload.Input
		item.NextRunAt = next
		item.NextActions = []string{"OPEN", "EDIT", "DISABLE"}
		return commandOutcome{result: command.Result{Schedule: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "SCHEDULE", resourceRef: ref, summary: "Расписание создано", platformEvent: "SCHEDULE_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef string
	var item entity.Schedule
	if input.Kind == command.UpdateSchedule {
		err := tx.QueryRow(ctx, `UPDATE control_plane.schedules s SET name=$4,target_type=$5,target_ref=$6,preset=$7,cron_expression=$8,timezone=$9,input=$10,session_policy=$11,notification_policy=$12,version=version+1,updated_at=clock_timestamp() FROM control_plane.projects p WHERE s.project_id=p.id AND s.organization_id=$1::uuid AND s.ref=$2 AND s.version=$3 RETURNING s.project_id::text,p.ref,s.ref,s.name,s.preset,s.cron_expression,s.timezone,s.session_policy,s.notification_policy,s.enabled,s.version,s.next_run_at,s.last_run_at,s.created_at,s.updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Name, payload.Target.Type, payload.Target.Ref, payload.Preset, payload.CronExpression, payload.Timezone, asJSON(payload.Input), payload.SessionPolicy, payload.NotificationPolicy).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.Target = payload.Target
		item.Input = payload.Input
	} else {
		err := tx.QueryRow(ctx, `UPDATE control_plane.schedules s SET enabled=$4,version=version+1,updated_at=clock_timestamp() FROM control_plane.projects p WHERE s.project_id=p.id AND s.organization_id=$1::uuid AND s.ref=$2 AND s.version=$3 RETURNING s.project_id::text,p.ref,s.ref,s.name,s.preset,s.cron_expression,s.timezone,s.session_policy,s.notification_policy,s.enabled,s.version,s.next_run_at,s.last_run_at,s.created_at,s.updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled).Scan(&projectID, &projectRef, &item.Ref, &item.Name, &item.Preset, &item.CronExpression, &item.Timezone, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt)
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
	return commandOutcome{result: command.Result{Schedule: &item}, projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE", resourceRef: item.Ref, summary: "Расписание обновлено", platformEvent: "SCHEDULE_CHANGED"}, nil
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
		err := tx.QueryRow(ctx, `INSERT INTO control_plane.integration_connections(ref,organization_id,definition_key,name,state,enabled,credential_materialization_ref,masked_credentials_state,public_configuration,created_by) SELECT $1,$2::uuid,d.stable_key,$3,'NOT_CONNECTED',true,$4,'CONFIGURED',$5,$6::uuid FROM control_plane.integration_definitions d WHERE d.stable_key=$7 AND d.enabled RETURNING ref,definition_key,name,state,masked_credentials_state,enabled,version,public_configuration,created_at,updated_at`, ref, scope.organizationID, payload.Name, payload.CredentialMaterializationRef, asJSON(payload.PublicConfiguration), scope.actorID, payload.DefinitionKey).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.Enabled, &item.Version, &config, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		_ = json.Unmarshal(config, &item.PublicConfiguration)
		item.NextActions = []string{"OPEN", "TEST", "DISABLE"}
		return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: ref, summary: "Подключение интеграции создано", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var item entity.IntegrationConnection
	if input.Kind == command.TestConnection {
		err := tx.QueryRow(ctx, `UPDATE control_plane.integration_connections SET state='DEGRADED',last_test_summary='Adapter test is pending',last_tested_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 AND enabled RETURNING ref,definition_key,name,state,masked_credentials_state,last_test_summary,enabled,version,last_tested_at,created_at,updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else {
		state := "DISABLED"
		if payload.Enabled {
			state = "NOT_CONNECTED"
		}
		err := tx.QueryRow(ctx, `UPDATE control_plane.integration_connections SET enabled=$4,state=$5,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 RETURNING ref,definition_key,name,state,masked_credentials_state,last_test_summary,enabled,version,last_tested_at,created_at,updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled, state).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !payload.Enabled {
			_, _ = tx.Exec(ctx, `UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1)`, payload.Ref)
		}
	}
	item.NextActions = []string{"OPEN", "TEST"}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: item.Ref, summary: "Подключение интеграции обновлено", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
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
	if err := tx.QueryRow(ctx, `SELECT id::text,definition_key FROM control_plane.integration_connections WHERE organization_id=$1::uuid AND ref=$2 AND enabled FOR UPDATE`, scope.organizationID, payload.ConnectionRef).Scan(&connectionID, &definitionKey); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	var capabilities []byte
	if err := tx.QueryRow(ctx, `SELECT capabilities FROM control_plane.integration_definitions WHERE stable_key=$1`, definitionKey).Scan(&capabilities); err != nil {
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
		err := tx.QueryRow(ctx, `INSERT INTO control_plane.integration_grants(ref,organization_id,connection_id,capability_key,target_kind,target_ref,enabled,approval_policy,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,true,$7,$8::uuid) ON CONFLICT(connection_id,capability_key,target_kind,target_ref) DO UPDATE SET enabled=true,version=control_plane.integration_grants.version+1,updated_at=clock_timestamp() RETURNING ref`, grantRef, scope.organizationID, connectionID, payload.CapabilityKey, targetType, targetRef, approvalPolicy(risk), scope.actorID).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		err := tx.QueryRow(ctx, `UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND connection_id=$2::uuid AND capability_key=$3 AND target_kind=$4 AND target_ref=$5 RETURNING ref`, scope.organizationID, connectionID, payload.CapabilityKey, targetType, targetRef).Scan(&grantRef)
		if err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
	}
	connection := entity.IntegrationConnection{Ref: payload.ConnectionRef}
	return commandOutcome{result: command.Result{Connection: &connection}, resourceKind: "INTEGRATION_GRANT", resourceRef: grantRef, summary: "Grant интеграции обновлён", platformEvent: "INTEGRATION_GRANT_CHANGED"}, nil
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
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,state,created_by) VALUES($1,$2::uuid,$3::uuid,'SYSTEM_ASSISTANT','system-assistant','ACTIVE',$4::uuid) RETURNING id::text`, sessionRef, scope.organizationID, projectID, scope.actorID).Scan(&sessionID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	ref, _ := newRef("cnv")
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "Новый разговор"
	}
	var item entity.AssistantConversation
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.assistant_conversations(ref,organization_id,project_id,session_id,title,state,created_by) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,'ACTIVE',$6::uuid) RETURNING ref,title,state,version,created_at,updated_at`, ref, scope.organizationID, projectID, sessionID, title, scope.actorID).Scan(&item.Ref, &item.Title, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.ProjectRef = payload.ProjectRef
	item.SessionRef = sessionRef
	return commandOutcome{result: command.Result{Conversation: &item}, projectID: stringValue(projectID), projectRef: payload.ProjectRef, resourceKind: "ASSISTANT_CONVERSATION", resourceRef: ref, summary: "Разговор с помощником создан", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) addAssistantTurnCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantTurnInput)
	if !ok || payload.ConversationRef == "" || strings.TrimSpace(payload.Content) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var runtimeReady bool
	if err := tx.QueryRow(ctx, `SELECT runtime_state='READY' AND runtime_revision=desired_runtime_revision AND last_heartbeat_at>clock_timestamp()-interval '45 seconds' FROM control_plane.assistant_runtime WHERE organization_id=$1::uuid`, scope.organizationID).Scan(&runtimeReady); err != nil || !runtimeReady {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var conversationID, sessionID, sessionRef string
	var projectID *string
	var projectRef string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT c.id::text,c.session_id::text,s.ref,c.project_id::text,COALESCE(p.ref,''),c.version FROM control_plane.assistant_conversations c JOIN control_plane.sessions s ON s.id=c.session_id LEFT JOIN control_plane.projects p ON p.id=c.project_id WHERE c.organization_id=$1::uuid AND c.ref=$2 AND c.state='ACTIVE' FOR UPDATE`, scope.organizationID, payload.ConversationRef).Scan(&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	turnRef, _ := newRef("trn")
	var turnNumber int64
	if err := tx.QueryRow(ctx, `SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE`, sessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.session_turns(ref,organization_id,session_id,turn_number,actor_kind,actor_ref,content,artifact_refs,state) VALUES($1,$2::uuid,$3::uuid,$4,'USER',$5,$6,$7,'COMPLETED')`, turnRef, scope.organizationID, sessionID, turnNumber, scope.actorRef, payload.Content, payload.ArtifactRefs); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, _ = tx.Exec(ctx, `UPDATE control_plane.sessions SET next_turn_number=next_turn_number+1,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, sessionID)
	runRef, _ := newRef("run")
	var runID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.runs(ref,organization_id,project_id,session_id,target_type,target_ref,source,title,task,input,state,initiated_by) VALUES($1,$2::uuid,$3::uuid,$4::uuid,'SYSTEM_ASSISTANT','system-assistant','SYSTEM_ASSISTANT','Команда системному помощнику',$5,'{}','RUNNING',$6::uuid) RETURNING id::text`, runRef, scope.organizationID, projectID, sessionID, payload.Content, scope.actorID).Scan(&runID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, _ = tx.Exec(ctx, `UPDATE control_plane.runs SET root_run_id=id,started_at=clock_timestamp() WHERE id=$1::uuid`, runID)
	nodeRef, _ := newRef("nod")
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,type,state,display_name,role,agent_id,input_summary,next_actions) SELECT $1,$2::uuid,$3::uuid,$3::uuid,'AGENT_EXECUTION','QUEUED',a.name,a.role_description,a.id,$4,ARRAY['OPEN','CANCEL'] FROM control_plane.agents a WHERE a.organization_id=$2::uuid AND a.system_key='system-assistant'`, nodeRef, scope.organizationID, runID, truncate(payload.Content, 1000)); err != nil {
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
		if err := tx.QueryRow(ctx, `INSERT INTO control_plane.assistant_plans(ref,organization_id,conversation_ref,summary,operations,state) VALUES($1,$2::uuid,$3,$4,$5,'PROPOSED') RETURNING id::text`, planRef, scope.organizationID, payload.ConversationRef, plan.Summary, asJSON(plan.Operations)).Scan(&planID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		latestPlanID = planID
	} else {
		latestPlanID = nil
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.assistant_conversations SET latest_plan_id=$2::uuid,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, conversationID, latestPlanID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	projectValue := ""
	if projectID != nil {
		projectValue = *projectID
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectValue, runID, runRef, "TURN_QUEUED", nodeRef, "", "", "", "Команда помощнику поставлена в очередь", "RUNNING", "QUEUED"); err != nil {
		return commandOutcome{}, err
	}
	conversation := entity.AssistantConversation{Ref: payload.ConversationRef, ProjectRef: projectRef, SessionRef: sessionRef, State: "ACTIVE", Version: version + 1, LatestPlan: plan}
	conversation.Turns = []entity.AssistantTurn{{Ref: turnRef, Actor: "USER", ActorName: scope.actorName, Content: payload.Content, ArtifactRefs: payload.ArtifactRefs, State: "COMPLETED", CreatedAt: time.Now().UTC()}}
	assistant, err := repository.getAssistantTx(ctx, tx, scope)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Conversation: &conversation, Assistant: &assistant, Plan: plan}, projectID: projectValue, projectRef: projectRef, resourceKind: "ASSISTANT_TURN", resourceRef: turnRef, summary: "Команда помощнику принята"}, nil
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
	if err := tx.QueryRow(ctx, `SELECT id::text,conversation_ref,operations,version FROM control_plane.assistant_plans WHERE organization_id=$1::uuid AND ref=$2 AND state='PROPOSED' FOR UPDATE`, scope.organizationID, payload.PlanRef).Scan(&planID, &conversationRef, &raw, &version); err != nil {
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
	if _, err := tx.Exec(ctx, `UPDATE control_plane.assistant_plans SET state='APPLIED',version=version+1,applied_at=clock_timestamp() WHERE id=$1::uuid`, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, State: "APPLIED", Version: version + 1, Operations: operations, AppliedAt: timePointer(time.Now().UTC())}
	conversation := entity.AssistantConversation{Ref: conversationRef}
	return commandOutcome{result: command.Result{Conversation: &conversation, Plan: &plan, CreatedRefs: created}, projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef, summary: "План помощника применён", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) auditAssistantOperation(ctx context.Context, tx pgx.Tx, scope scope, outcome commandOutcome, action string) error {
	ref, err := newRef("aud")
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,assistant_agent_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref) SELECT $1,$2::uuid,$3::uuid,$4::uuid,a.id,$5,$6,$7,'SUCCEEDED',$8,$9 FROM control_plane.agents a WHERE a.organization_id=$2::uuid AND a.system_key='system-assistant'`, ref, scope.organizationID, nullUUID(outcome.projectID), scope.actorID, "system_assistant."+strings.ToLower(action), outcome.resourceKind, outcome.resourceRef, outcome.summary, "assistant-plan")
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
	err := tx.QueryRow(ctx, `UPDATE control_plane.assistant_runtime SET owner_instructions=$3,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND version=$2 RETURNING stable_key,core_prompt_revision,owner_instructions,runtime_state,runtime_revision,desired_runtime_revision,system_session_ref,resource_limits,last_heartbeat_at,version,updated_at`, scope.organizationID, *input.Mutation.ExpectedVersion, strings.TrimSpace(payload.Instructions)).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.System = true
	assistant.Deletable = false
	return commandOutcome{result: command.Result{Assistant: &assistant}, resourceKind: "SYSTEM_ASSISTANT", resourceRef: assistant.StableKey, summary: "Дополнение к инструкциям помощника обновлено", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}
func (repository *Repository) recoverAssistant(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	if input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	tag, err := tx.Exec(ctx, `UPDATE control_plane.assistant_runtime SET runtime_state='RECOVERING',warm_instance_ref=NULL,last_heartbeat_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND version=$2`, scope.organizationID, *input.Mutation.ExpectedVersion)
	if err != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	assistant, err := repository.getAssistantTx(ctx, tx, scope)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Assistant: &assistant}, resourceKind: "SYSTEM_ASSISTANT", resourceRef: assistant.StableKey, summary: "Восстановление помощника запрошено", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}
func (repository *Repository) getAssistantTx(ctx context.Context, tx pgx.Tx, scope scope) (entity.SystemAssistant, error) {
	var item entity.SystemAssistant
	var limits []byte
	err := tx.QueryRow(ctx, `SELECT a.ref,ar.stable_key,a.name,a.purpose,ar.core_prompt_revision,ar.owner_instructions,ar.runtime_state,ar.runtime_revision,ar.desired_runtime_revision,ar.system_session_ref,ar.resource_limits,ar.last_heartbeat_at,ar.version,ar.updated_at FROM control_plane.assistant_runtime ar JOIN control_plane.agents a ON a.id=ar.agent_id WHERE ar.organization_id=$1::uuid`, scope.organizationID).Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &item.ResourceLimits)
	item.Ready = item.RuntimeState == "READY" && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) < 45*time.Second
	item.System = true
	item.Deletable = false
	return item, nil
}
