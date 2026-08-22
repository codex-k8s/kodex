package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type commandOutcome struct {
	result                                                                   command.Result
	projectID, projectRef, resourceKind, resourceRef, summary, platformEvent string
}

func (repository *Repository) Execute(ctx context.Context, input command.Command) (command.Result, error) {
	scope, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return command.Result{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return command.Result{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.authorizeCommand(ctx, tx, scope, input); err != nil {
		return command.Result{}, err
	}
	var storedDigest string
	var storedPayload []byte
	err = tx.QueryRow(ctx, `SELECT intent_digest,response_payload FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid AND actor_id=$2::uuid AND operation=$3 AND idempotency_key=$4 AND expires_at>clock_timestamp() FOR UPDATE`, scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).Scan(&storedDigest, &storedPayload)
	if err == nil {
		if storedDigest != input.Mutation.IntentDigest {
			return command.Result{}, errs.ErrIdempotencyReuse
		}
		var result command.Result
		if json.Unmarshal(storedPayload, &result) != nil {
			return command.Result{}, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return command.Result{}, errs.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return command.Result{}, errs.ErrUnavailable
	}
	outcome, err := repository.applyCommand(ctx, tx, scope, input)
	if err != nil {
		return command.Result{}, err
	}
	if outcome.resourceRef == "" {
		return command.Result{}, errs.ErrConflict
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return command.Result{}, err
	}
	var project any
	if outcome.projectID != "" {
		project = outcome.projectID
	} else {
		project = nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,'SUCCEEDED',$8,$9)`, auditRef, scope.organizationID, project, scope.actorID, input.Mutation.Operation, outcome.resourceKind, outcome.resourceRef, outcome.summary, input.Principal.CorrelationRef); err != nil {
		return command.Result{}, errs.ErrUnavailable
	}
	if outcome.platformEvent != "" {
		if err := repository.emitPlatformEvent(ctx, tx, scope, outcome.platformEvent, outcome.projectRef, outcome.resourceRef, outcome.summary); err != nil {
			return command.Result{}, err
		}
	}
	encoded, err := json.Marshal(outcome.result)
	if err != nil {
		return command.Result{}, errs.ErrConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO control_plane.idempotency_receipts(organization_id,actor_id,operation,idempotency_key,intent_digest,response_type,response_payload,expires_at) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,clock_timestamp()+interval '24 hours')`, scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, string(input.Kind), encoded); err != nil {
		return command.Result{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return command.Result{}, errs.ErrConflict
	}
	return outcome.result, nil
}

func (repository *Repository) applyCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.CompleteOnboarding:
		return repository.completeOnboarding(ctx, tx, scope)
	case command.CreateProject:
		return repository.createProject(ctx, tx, scope, input.Payload)
	case command.UpdateProject:
		return repository.updateProject(ctx, tx, scope, input.Mutation, input.Payload)
	case command.AddMembership, command.ChangeMembership, command.RemoveMembership:
		return repository.changeMembership(ctx, tx, scope, input)
	case command.CreateAgent:
		return repository.createAgent(ctx, tx, scope, input.Payload)
	case command.UpdateAgent, command.SetAgentEnabled, command.ArchiveAgent:
		return repository.changeAgent(ctx, tx, scope, input)
	case command.CreateInstructions, command.ValidateInstructions, command.PublishInstructions, command.RollbackInstructions:
		return repository.changeInstructions(ctx, tx, scope, input)
	case command.ChangeAgentCapability, command.ChangeAgentGrant, command.ChangeAgentKnowledge:
		return repository.changeAgentBinding(ctx, tx, scope, input)
	case command.CreateWorkflow, command.UpdateWorkflow, command.ValidateWorkflow, command.PublishWorkflow, command.ArchiveWorkflow:
		return repository.changeWorkflow(ctx, tx, scope, input)
	case command.LaunchRun:
		return repository.launchRun(ctx, tx, scope, input)
	case command.AddSessionTurn:
		return repository.addSessionTurn(ctx, tx, scope, input)
	case command.CancelRun, command.RetryRun:
		return repository.changeRun(ctx, tx, scope, input)
	case command.ResolveOwnerGate:
		return repository.resolveGate(ctx, tx, scope, input)
	case command.ChangeArtifactBinding:
		return repository.changeArtifactBinding(ctx, tx, scope, input)
	case command.CreateSchedule, command.UpdateSchedule, command.SetScheduleEnabled:
		return repository.changeSchedule(ctx, tx, scope, input)
	case command.CreateConnection, command.TestConnection, command.SetConnectionEnabled, command.ChangeIntegrationGrant:
		return repository.changeConnection(ctx, tx, scope, input)
	case command.CreateAssistantConversation, command.AddAssistantTurn, command.ApplyAssistantPlan, command.UpdateAssistantInstructions, command.RecoverAssistant:
		return repository.changeAssistant(ctx, tx, scope, input)
	case command.ClaimExecution, command.RenewExecution, command.ReportExecutionProgress, command.CompleteExecution, command.DelegateExecution, command.DeliverCallback:
		return repository.changeExecution(ctx, tx, scope, input)
	case command.ReportWarmRuntime:
		return repository.reportWarmRuntime(ctx, tx, scope, input)
	case command.MaterializeOccurrence, command.CompleteOccurrence:
		return repository.changeOccurrence(ctx, tx, scope, input)
	case command.CompleteIntegrationInvocation:
		return repository.completeIntegrationInvocation(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) completeOnboarding(ctx context.Context, tx pgx.Tx, scope scope) (commandOutcome, error) {
	if _, err := tx.Exec(ctx, `UPDATE control_plane.installation SET onboarding_completed_at=COALESCE(onboarding_completed_at,clock_timestamp()) WHERE singleton`); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{CreatedRefs: []string{scope.organizationRef}}, resourceKind: "INSTALLATION", resourceRef: scope.organizationRef, summary: "Первичная настройка завершена", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

func (repository *Repository) createProject(ctx context.Context, tx pgx.Tx, scope scope, payload any) (commandOutcome, error) {
	input, ok := payload.(command.ProjectInput)
	if !ok || strings.TrimSpace(input.Name) == "" || len(input.Name) > 160 {
		return commandOutcome{}, errs.ErrInvalid
	}
	ref, err := newRef("prj")
	if err != nil {
		return commandOutcome{}, err
	}
	language := input.Language
	if language == "" {
		language = "ru"
	}
	var item entity.Project
	err = tx.QueryRow(ctx, `INSERT INTO control_plane.projects(ref,organization_id,name,purpose,language,created_by) VALUES($1,$2::uuid,$3,$4,$5,$6::uuid) RETURNING ref,name,purpose,language,lifecycle,version,created_at,updated_at`, ref, scope.organizationID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), language, scope.actorID).Scan(&item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	membershipRef, _ := newRef("mem")
	if _, err = tx.Exec(ctx, `INSERT INTO control_plane.memberships(ref,organization_id,project_id,subject_id,role,permissions) VALUES($1,$2::uuid,(SELECT id FROM control_plane.projects WHERE ref=$3),$4::uuid,'OWNER',$5)`, membershipRef, scope.organizationID, ref, scope.actorID, allPermissions()); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.NextActions = []string{"OPEN", "EDIT"}
	return commandOutcome{result: command.Result{Project: &item}, projectID: mustProjectID(ctx, tx, scope.organizationID, ref), projectRef: ref, resourceKind: "PROJECT", resourceRef: ref, summary: "Проект создан", platformEvent: "PROJECT_CHANGED"}, nil
}

func (repository *Repository) updateProject(ctx context.Context, tx pgx.Tx, scope scope, mutation value.Mutation, payload any) (commandOutcome, error) {
	input, ok := payload.(command.ProjectInput)
	if !ok || input.Ref == "" || mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var item entity.Project
	var projectID string
	err := tx.QueryRow(ctx, `UPDATE control_plane.projects SET name=$4,purpose=$5,language=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 RETURNING id::text,ref,name,purpose,language,lifecycle,version,created_at,updated_at`, scope.organizationID, input.Ref, *mutation.ExpectedVersion, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), input.Language).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	item.NextActions = []string{"OPEN", "EDIT"}
	return commandOutcome{result: command.Result{Project: &item}, projectID: projectID, projectRef: item.Ref, resourceKind: "PROJECT", resourceRef: item.Ref, summary: "Проект обновлён", platformEvent: "PROJECT_CHANGED"}, nil
}

func (repository *Repository) changeMembership(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.MembershipInput)
	if !ok || payload.ProjectRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	var item entity.Membership
	switch input.Kind {
	case command.AddMembership:
		ref, _ := newRef("mem")
		err := tx.QueryRow(ctx, `INSERT INTO control_plane.memberships(ref,organization_id,project_id,subject_id,role,permissions,active) SELECT $1,$2::uuid,$3::uuid,s.id,$5,$6,true FROM control_plane.subjects s WHERE s.organization_id=$2::uuid AND s.ref=$4 RETURNING ref,role,permissions,active,version`, ref, scope.organizationID, projectID, payload.UserRef, payload.Role, payload.Permissions).Scan(&item.Ref, &item.Role, &item.Permissions, &item.Active, &item.Version)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.User.Ref = payload.UserRef
	case command.ChangeMembership:
		if input.Mutation.ExpectedVersion == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		err := tx.QueryRow(ctx, `UPDATE control_plane.memberships SET role=$4,permissions=$5,active=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 RETURNING ref,role,permissions,active,version`, scope.organizationID, payload.MembershipRef, *input.Mutation.ExpectedVersion, payload.Role, payload.Permissions, payload.Active).Scan(&item.Ref, &item.Role, &item.Permissions, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.RemoveMembership:
		if input.Mutation.ExpectedVersion == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		err := tx.QueryRow(ctx, `UPDATE control_plane.memberships SET active=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND project_id=$3::uuid AND version=$4 AND subject_id<>$5::uuid RETURNING ref,role,permissions,active,version`, scope.organizationID, payload.MembershipRef, projectID, *input.Mutation.ExpectedVersion, scope.actorID).Scan(&item.Ref, &item.Role, &item.Permissions, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	item.ProjectRef = payload.ProjectRef
	return commandOutcome{result: command.Result{Membership: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "MEMBERSHIP", resourceRef: item.Ref, summary: "Доступ к проекту обновлён", platformEvent: "MEMBERSHIP_CHANGED"}, nil
}

func (repository *Repository) createAgent(ctx context.Context, tx pgx.Tx, scope scope, payload any) (commandOutcome, error) {
	input, ok := payload.(command.AgentInput)
	if !ok || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Instructions) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	runtimeKey := input.RuntimeRef
	if runtimeKey == "" {
		runtimeKey = defaultRuntimeKey
	}
	ref, _ := newRef("agt")
	var agentID string
	var item entity.Agent
	err := tx.QueryRow(ctx, `INSERT INTO control_plane.agents(ref,organization_id,project_id,name,purpose,role_description,avatar_url,runtime_key,state,enabled,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,'READY',true,$9::uuid) RETURNING id::text,ref,name,purpose,role_description,avatar_url,state,enabled,version,created_at,updated_at`, ref, scope.organizationID, projectID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), strings.TrimSpace(input.RoleDescription), strings.TrimSpace(input.AvatarURL), runtimeKey, scope.actorID).Scan(&agentID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	instructionRef, _ := newRef("ins")
	digest := sha256.Sum256([]byte(input.Instructions))
	publishedAt := time.Now().UTC()
	if _, err = tx.Exec(ctx, `INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,created_by,published_at) VALUES($1,$2::uuid,$3::uuid,1,'PUBLISHED',$4,$5,$6::uuid,$7)`, instructionRef, scope.organizationID, agentID, input.Instructions, hex.EncodeToString(digest[:]), scope.actorID, publishedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.ProjectRef = input.ProjectRef
	item.RuntimeKey = runtimeKey
	item.PublishedInstructions = &entity.InstructionVersion{Ref: instructionRef, VersionNumber: 1, State: "PUBLISHED", Content: input.Instructions, Digest: hex.EncodeToString(digest[:]), CreatedAt: publishedAt, PublishedAt: &publishedAt}
	item.NextActions = agentActions(item)
	return commandOutcome{result: command.Result{Agent: &item}, projectID: projectID, projectRef: input.ProjectRef, resourceKind: "AGENT", resourceRef: ref, summary: "Агент создан и готов к запуску", platformEvent: "AGENT_CHANGED"}, nil
}

func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") {
		return errs.ErrConflict
	}
	if strings.Contains(message, "violates check constraint") || strings.Contains(message, "violates foreign key") {
		return errs.ErrInvalid
	}
	return errs.ErrUnavailable
}
func mustProjectID(ctx context.Context, tx pgx.Tx, organizationID, ref string) string {
	var id string
	if tx.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2 AND lifecycle='ACTIVE'`, organizationID, ref).Scan(&id) != nil {
		return ""
	}
	return id
}

func (repository *Repository) changeAgent(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentInput)
	if !ok || payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var item entity.Agent
	var projectID string
	switch input.Kind {
	case command.UpdateAgent:
		err := tx.QueryRow(ctx, `UPDATE control_plane.agents a SET name=$4,purpose=$5,role_description=$6,avatar_url=$7,runtime_key=$8,version=version+1,updated_at=clock_timestamp() WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.version=$3 AND a.system_key IS NULL RETURNING a.project_id::text,a.ref,a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,a.created_at,a.updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Name, payload.Purpose, payload.RoleDescription, payload.AvatarURL, payload.RuntimeRef).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.SetAgentEnabled:
		state := "DISABLED"
		if payload.Enabled {
			state = "READY"
		}
		err := tx.QueryRow(ctx, `UPDATE control_plane.agents a SET enabled=$4,state=$5,version=version+1,updated_at=clock_timestamp() WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.version=$3 AND a.system_key IS NULL AND a.state<>'ARCHIVED' RETURNING a.project_id::text,a.ref,a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,a.created_at,a.updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled, state).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.ArchiveAgent:
		err := tx.QueryRow(ctx, `UPDATE control_plane.agents a SET enabled=false,state='ARCHIVED',version=version+1,updated_at=clock_timestamp() WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.version=$3 AND a.system_key IS NULL AND NOT EXISTS(SELECT 1 FROM control_plane.runs r WHERE r.target_ref=a.ref AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')) RETURNING a.project_id::text,a.ref,a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,a.created_at,a.updated_at`, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	_ = tx.QueryRow(ctx, `SELECT p.ref,a.runtime_key,r.name,r.provider,r.model,r.runtime_revision,a.capabilities,a.knowledge_artifact_refs FROM control_plane.agents a JOIN control_plane.projects p ON p.id=a.project_id JOIN control_plane.runtime_profiles r ON r.stable_key=a.runtime_key WHERE a.ref=$1`, item.Ref).Scan(&item.ProjectRef, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs)
	item.NextActions = agentActions(item)
	return commandOutcome{result: command.Result{Agent: &item}, projectID: projectID, projectRef: item.ProjectRef, resourceKind: "AGENT", resourceRef: item.Ref, summary: "Агент обновлён", platformEvent: "AGENT_CHANGED"}, nil
}

func (repository *Repository) changeInstructions(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentInput)
	if !ok || payload.Ref == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var agentID, projectID, projectRef, systemKey string
	var agentVersion int64
	if err := tx.QueryRow(ctx, `SELECT a.id::text,COALESCE(a.project_id::text,''),COALESCE(p.ref,''),COALESCE(a.system_key,''),a.version FROM control_plane.agents a LEFT JOIN control_plane.projects p ON p.id=a.project_id WHERE a.organization_id=$1::uuid AND a.ref=$2 FOR UPDATE`, scope.organizationID, payload.Ref).Scan(&agentID, &projectID, &projectRef, &systemKey, &agentVersion); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if systemKey != "" {
		return commandOutcome{}, errs.ErrProtected
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	switch input.Kind {
	case command.CreateInstructions:
		if strings.TrimSpace(payload.Instructions) == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		var number int32
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version_number),0)+1 FROM control_plane.instruction_versions WHERE agent_id=$1::uuid`, agentID).Scan(&number); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		ref, _ := newRef("ins")
		digest := sha256.Sum256([]byte(payload.Instructions))
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,created_by) VALUES($1,$2::uuid,$3::uuid,$4,'DRAFT',$5,$6,$7::uuid)`, ref, scope.organizationID, agentID, number, payload.Instructions, hex.EncodeToString(digest[:]), scope.actorID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.ValidateInstructions:
		var content string
		var ref string
		if err := tx.QueryRow(ctx, `SELECT ref,content FROM control_plane.instruction_versions WHERE agent_id=$1::uuid AND state IN ('DRAFT','INVALID','VALID') FOR UPDATE`, agentID).Scan(&ref, &content); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		state := "VALID"
		problems := []string{}
		if len(strings.TrimSpace(content)) < 20 {
			state = "INVALID"
			problems = append(problems, "Инструкции должны содержать не менее 20 символов")
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.instruction_versions SET state=$2,validation_problems=$3 WHERE ref=$1`, ref, state, asJSON(problems)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.PublishInstructions:
		tag, err := tx.Exec(ctx, `UPDATE control_plane.instruction_versions SET state='PUBLISHED',published_at=clock_timestamp() WHERE agent_id=$1::uuid AND state='VALID'`, agentID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	case command.RollbackInstructions:
		var content string
		if err := tx.QueryRow(ctx, `SELECT content FROM control_plane.instruction_versions WHERE agent_id=$1::uuid AND ref=$2 AND state='PUBLISHED'`, agentID, payload.Instructions).Scan(&content); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		var number int32
		_ = tx.QueryRow(ctx, `SELECT max(version_number)+1 FROM control_plane.instruction_versions WHERE agent_id=$1::uuid`, agentID).Scan(&number)
		ref, _ := newRef("ins")
		digest := sha256.Sum256([]byte(content))
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,parent_ref,created_by,published_at) VALUES($1,$2::uuid,$3::uuid,$4,'PUBLISHED',$5,$6,$7,$8::uuid,clock_timestamp())`, ref, scope.organizationID, agentID, number, content, hex.EncodeToString(digest[:]), payload.Instructions, scope.actorID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.agents SET version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, agentID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	agent := entity.Agent{Ref: payload.Ref, ProjectRef: projectRef, Version: agentVersion + 1}
	return commandOutcome{result: command.Result{Agent: &agent}, projectID: projectID, projectRef: projectRef, resourceKind: "INSTRUCTIONS", resourceRef: payload.Ref, summary: "Инструкции агента обновлены", platformEvent: "INSTRUCTIONS_PUBLISHED"}, nil
}

func (repository *Repository) changeAgentBinding(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentBindingInput)
	if !ok || payload.AgentRef == "" || payload.BindingRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef string
	var current int64
	if err := tx.QueryRow(ctx, `SELECT a.project_id::text,p.ref,a.version FROM control_plane.agents a JOIN control_plane.projects p ON p.id=a.project_id WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.system_key IS NULL FOR UPDATE`, scope.organizationID, payload.AgentRef).Scan(&projectID, &projectRef, &current); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if current != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	column := "capabilities"
	if input.Kind == command.ChangeAgentKnowledge {
		column = "knowledge_artifact_refs"
	}
	if input.Kind == command.ChangeAgentGrant {
		if payload.Enabled {
			tag, err := tx.Exec(ctx, `UPDATE control_plane.integration_grants SET enabled=true,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND target_kind='AGENT' AND target_ref=$3`, scope.organizationID, payload.BindingRef, payload.AgentRef)
			if err != nil || tag.RowsAffected() != 1 {
				return commandOutcome{}, errs.ErrNotFound
			}
		} else {
			_, _ = tx.Exec(ctx, `UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND target_kind='AGENT' AND target_ref=$3`, scope.organizationID, payload.BindingRef, payload.AgentRef)
		}
	} else if payload.Enabled {
		_, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE control_plane.agents SET %s=array_append(%s,$3) WHERE organization_id=$1::uuid AND ref=$2 AND NOT ($3=ANY(%s))`, column, column, column), scope.organizationID, payload.AgentRef, payload.BindingRef)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else {
		_, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE control_plane.agents SET %s=array_remove(%s,$3) WHERE organization_id=$1::uuid AND ref=$2`, column, column), scope.organizationID, payload.AgentRef, payload.BindingRef)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.agents SET version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2`, scope.organizationID, payload.AgentRef); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	agent := entity.Agent{Ref: payload.AgentRef, ProjectRef: projectRef, Version: current + 1}
	return commandOutcome{result: command.Result{Agent: &agent}, projectID: projectID, projectRef: projectRef, resourceKind: "AGENT", resourceRef: payload.AgentRef, summary: "Разрешения агента обновлены", platformEvent: "AGENT_CHANGED"}, nil
}

func (repository *Repository) changeWorkflow(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.WorkflowInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateWorkflow {
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		ref, _ := newRef("wfl")
		draft := entity.WorkflowVersion{Ref: "draft", VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{}}
		if payload.Draft != nil {
			draft = *payload.Draft
		}
		var item entity.Workflow
		raw := asJSON(draft)
		err := tx.QueryRow(ctx, `INSERT INTO control_plane.workflows(ref,organization_id,project_id,name,purpose,coordinator_agent_id,state,draft_spec,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,(SELECT id FROM control_plane.agents WHERE organization_id=$2::uuid AND ref=$6 AND project_id=$3::uuid),'DRAFT',$7,$8::uuid) RETURNING ref,name,purpose,state,version,created_at,updated_at`, ref, scope.organizationID, projectID, payload.Name, payload.Purpose, payload.CoordinatorAgentRef, raw, scope.actorID).Scan(&item.Ref, &item.Name, &item.Purpose, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.ProjectRef = payload.ProjectRef
		item.CoordinatorAgentRef = payload.CoordinatorAgentRef
		item.Draft = &draft
		item.NextActions = []string{"OPEN", "EDIT"}
		return commandOutcome{result: command.Result{Workflow: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "WORKFLOW", resourceRef: ref, summary: "Workflow создан", platformEvent: "WORKFLOW_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var workflowID, projectID, projectRef, state string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT w.id::text,w.project_id::text,p.ref,w.state,w.version FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id WHERE w.organization_id=$1::uuid AND w.ref=$2 FOR UPDATE`, scope.organizationID, payload.Ref).Scan(&workflowID, &projectID, &projectRef, &state, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	switch input.Kind {
	case command.UpdateWorkflow:
		if payload.Draft == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		_, err := tx.Exec(ctx, `UPDATE control_plane.workflows SET draft_spec=$2,state='DRAFT',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, workflowID, asJSON(payload.Draft))
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.ValidateWorkflow:
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT draft_spec FROM control_plane.workflows WHERE id=$1::uuid`, workflowID).Scan(&raw); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var draft entity.WorkflowVersion
		_ = json.Unmarshal(raw, &draft)
		if draft.Concurrency < 1 || draft.TimeoutSeconds < 1 || len(draft.Steps) == 0 {
			return commandOutcome{}, errs.ErrInvalid
		}
		_, err := tx.Exec(ctx, `UPDATE control_plane.workflows SET state='VALID',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, workflowID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.PublishWorkflow:
		if state != "VALID" {
			return commandOutcome{}, errs.ErrConflict
		}
		var raw []byte
		var next int32
		if err := tx.QueryRow(ctx, `SELECT draft_spec,published_version+1 FROM control_plane.workflows WHERE id=$1::uuid`, workflowID).Scan(&raw, &next); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		digest := sha256.Sum256(raw)
		versionRef, _ := newRef("wfv")
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.workflow_versions(ref,organization_id,workflow_id,version_number,spec,digest,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7::uuid)`, versionRef, scope.organizationID, workflowID, next, raw, hex.EncodeToString(digest[:]), scope.actorID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.workflows SET published_spec=$2,published_version=$3,state='PUBLISHED',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, workflowID, raw, next); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.ArchiveWorkflow:
		tag, err := tx.Exec(ctx, `UPDATE control_plane.workflows w SET state='ARCHIVED',version=version+1,updated_at=clock_timestamp() WHERE w.id=$1::uuid AND NOT EXISTS(SELECT 1 FROM control_plane.runs r WHERE r.target_type='WORKFLOW' AND r.target_ref=w.ref AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING'))`, workflowID)
		if err != nil || tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	workflow := entity.Workflow{Ref: payload.Ref, ProjectRef: projectRef, Version: version + 1}
	return commandOutcome{result: command.Result{Workflow: &workflow}, projectID: projectID, projectRef: projectRef, resourceKind: "WORKFLOW", resourceRef: payload.Ref, summary: "Workflow обновлён", platformEvent: "WORKFLOW_CHANGED"}, nil
}

func (repository *Repository) launchRun(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LaunchRunInput)
	if !ok || payload.ProjectRef == "" || strings.TrimSpace(payload.Task) == "" || payload.Target.Ref == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	source := payload.Source
	if source == "" {
		source = "CONTROL_CENTER"
	}
	if !contains([]string{"CONTROL_CENTER", "SYSTEM_ASSISTANT", "SCHEDULE", "INTEGRATION", "AGENT_DELEGATION", "MATTERMOST"}, source) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var targetName string
	var workflowSpec []byte
	switch payload.Target.Type {
	case "AGENT":
		if err := tx.QueryRow(ctx, `SELECT name FROM control_plane.agents a WHERE a.organization_id=$1::uuid AND a.project_id=$2::uuid AND a.ref=$3 AND a.enabled AND a.state='READY' AND EXISTS(SELECT 1 FROM control_plane.instruction_versions i WHERE i.agent_id=a.id AND i.state='PUBLISHED')`, scope.organizationID, projectID, payload.Target.Ref).Scan(&targetName); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
	case "WORKFLOW":
		if err := tx.QueryRow(ctx, `SELECT name,published_spec FROM control_plane.workflows WHERE organization_id=$1::uuid AND project_id=$2::uuid AND ref=$3 AND state='PUBLISHED'`, scope.organizationID, projectID, payload.Target.Ref).Scan(&targetName, &workflowSpec); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	sessionRef := payload.SessionRef
	var sessionID string
	if sessionRef == "" {
		sessionRef, _ = newRef("ses")
		if err := tx.QueryRow(ctx, `INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,state,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,'ACTIVE',$6::uuid) RETURNING id::text`, sessionRef, scope.organizationID, projectID, payload.Target.Type, payload.Target.Ref, scope.actorID).Scan(&sessionID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else if err := tx.QueryRow(ctx, `SELECT id::text FROM control_plane.sessions WHERE organization_id=$1::uuid AND project_id=$2::uuid AND ref=$3 AND target_type=$4 AND target_ref=$5 AND state='ACTIVE' FOR UPDATE`, scope.organizationID, projectID, sessionRef, payload.Target.Type, payload.Target.Ref).Scan(&sessionID); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	runRef, _ := newRef("run")
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = targetName + ": " + truncate(payload.Task, 120)
	}
	rawInput := asJSON(payload.Input)
	var runID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.runs(ref,organization_id,project_id,session_id,target_type,target_ref,source,title,task,input,input_artifact_refs,state,initiated_by) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,'QUEUED',$12::uuid) RETURNING id::text`, runRef, scope.organizationID, projectID, sessionID, payload.Target.Type, payload.Target.Ref, source, title, payload.Task, rawInput, payload.ArtifactRefs, scope.actorID).Scan(&runID); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET root_run_id=id WHERE id=$1::uuid`, runID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	turnRef, _ := newRef("trn")
	var turnID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.session_turns(ref,organization_id,session_id,run_id,turn_number,actor_kind,actor_ref,content,artifact_refs,state) SELECT $1,$2::uuid,$3::uuid,$4::uuid,next_turn_number,'USER',$5,$6,$7,'QUEUED' FROM control_plane.sessions WHERE id=$3::uuid RETURNING id::text`, turnRef, scope.organizationID, sessionID, runID, scope.actorRef, payload.Task, payload.ArtifactRefs).Scan(&turnID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.sessions SET next_turn_number=next_turn_number+1,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, sessionID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	rootNodeRef, _ := newRef("nod")
	var rootNodeID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,type,state,display_name,role,turn_id,input_summary,next_actions) VALUES($1,$2::uuid,$3::uuid,$3::uuid,'ROOT_PROCESS','RUNNING',$4,'Координация',$5::uuid,$6,ARRAY['OPEN','CANCEL']) RETURNING id::text`, rootNodeRef, scope.organizationID, runID, title, turnID, truncate(payload.Task, 500)).Scan(&rootNodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if payload.Target.Type == "AGENT" {
		if _, _, err := repository.insertAgentNode(ctx, tx, scope, runID, runID, rootNodeID, payload.Target.Ref, targetName, turnID, payload.Task); err != nil {
			return commandOutcome{}, err
		}
	} else {
		var version entity.WorkflowVersion
		if json.Unmarshal(workflowSpec, &version) != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		nodeIDs := map[string]string{}
		nodeRefs := map[string]string{}
		for _, step := range version.Steps {
			agentName := step.Name
			if agentName == "" {
				agentName = step.Key
			}
			nodeID, nodeRef, err := repository.insertAgentNode(ctx, tx, scope, runID, runID, rootNodeID, step.AgentRef, agentName, turnID, step.Instructions)
			if err != nil {
				return commandOutcome{}, err
			}
			nodeIDs[step.Key] = nodeID
			nodeRefs[step.Key] = nodeRef
			if _, err := tx.Exec(ctx, `UPDATE control_plane.run_nodes SET workflow_step_key=$2,human_gate_after=$3 WHERE id=$1::uuid`, nodeID, step.Key, step.HumanGateAfter); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
		for _, step := range version.Steps {
			for _, dependency := range step.DependsOn {
				sourceID, targetID := nodeIDs[dependency], nodeIDs[step.Key]
				if sourceID == "" || targetID == "" {
					return commandOutcome{}, errs.ErrInvalid
				}
				edgeRef, _ := newRef("edg")
				if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'WAITING_FOR','Ожидает завершения')`, edgeRef, scope.organizationID, runID, sourceID, targetID); err != nil {
					return commandOutcome{}, errs.ErrUnavailable
				}
				_ = nodeRefs
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state='RUNNING',started_at=clock_timestamp(),version=version+1 WHERE id=$1::uuid`, runID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, runID, runRef, "RUN_CREATED", rootNodeRef, "", "", "", "Запуск создан", "RUNNING", "RUNNING"); err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, runRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "RUN", resourceRef: runRef, summary: "Запуск создан"}, nil
}

func (repository *Repository) insertAgentNode(ctx context.Context, tx pgx.Tx, scope scope, rootRunID, runID, parentNodeID, agentRef, displayName, turnID, summary string) (string, string, error) {
	var agentID, role string
	if err := tx.QueryRow(ctx, `SELECT id::text,role_description FROM control_plane.agents WHERE organization_id=$1::uuid AND ref=$2 AND enabled AND state='READY'`, scope.organizationID, agentRef).Scan(&agentID, &role); err != nil {
		return "", "", errs.ErrInvalid
	}
	nodeRef, _ := newRef("nod")
	var nodeID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,agent_id,turn_id,input_summary,next_actions) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'AGENT_EXECUTION','QUEUED',$6,$7,$8::uuid,$9::uuid,$10,ARRAY['OPEN','CANCEL']) RETURNING id::text`, nodeRef, scope.organizationID, rootRunID, runID, parentNodeID, displayName, role, agentID, turnID, truncate(summary, 1000)).Scan(&nodeID); err != nil {
		return "", "", errs.ErrUnavailable
	}
	edgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'DELEGATED_TO','Передано агенту')`, edgeRef, scope.organizationID, rootRunID, parentNodeID, nodeID); err != nil {
		return "", "", errs.ErrUnavailable
	}
	return nodeID, nodeRef, nil
}

func truncate(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum]) + "…"
}

func (repository *Repository) emitPlatformEvent(ctx context.Context, tx pgx.Tx, scope scope, eventName, projectRef, aggregateRef, summary string) error {
	var sequence int64
	if err := tx.QueryRow(ctx, `UPDATE control_plane.installation SET platform_sequence=platform_sequence+1 WHERE singleton RETURNING platform_sequence`).Scan(&sequence); err != nil {
		return errs.ErrUnavailable
	}
	eventID := uuid.New()
	payload := map[string]any{"eventId": eventID.String(), "eventName": eventName, "eventVersion": 1, "occurredAt": time.Now().UTC(), "organizationRef": scope.organizationRef, "aggregateRef": aggregateRef, "aggregateVersion": 1, "sequence": sequence, "correlationRef": scope.correlationRef, "data": map[string]any{"kind": platformEventKind(eventName), "safeSummary": summary}}
	if projectRef != "" {
		payload["projectRef"] = projectRef
	}
	subject := "control_plane.platform." + scope.organizationRef + ".events"
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.outbox_events(event_id,subject,ordering_key,sequence,payload) VALUES($1,$2,$3,$4,$5)`, eventID, subject, "platform:"+scope.organizationRef, sequence, asJSON(payload)); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) emitRunEvent(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, summary, runState, nodeState string) (entity.RunEvent, error) {
	var sequence, version int64
	var rootRef, projectRef string
	var projectValue any
	if err := tx.QueryRow(ctx, `UPDATE control_plane.runs SET event_sequence=event_sequence+1,graph_revision=graph_revision+1,updated_at=clock_timestamp() WHERE id=$1::uuid RETURNING ref,event_sequence,version`, rootRunID).Scan(&rootRef, &sequence, &version); err != nil {
		return entity.RunEvent{}, errs.ErrUnavailable
	}
	if projectID != "" {
		if err := tx.QueryRow(ctx, `SELECT ref FROM control_plane.projects WHERE id=$1::uuid`, projectID).Scan(&projectRef); err != nil {
			return entity.RunEvent{}, errs.ErrUnavailable
		}
		projectValue = projectID
	}
	ref, _ := newRef("evt")
	eventID := uuid.New()
	event := entity.RunEvent{Ref: ref, RunRef: rootRef, Sequence: sequence, Type: eventType, NodeRef: nodeRef, EdgeRef: edgeRef, GateRef: gateRef, ArtifactRef: artifactRef, Summary: summary, RunState: runState, NodeState: nodeState, OccurredAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_events(event_id,ref,organization_id,project_id,root_run_id,aggregate_ref,aggregate_version,sequence,type,node_ref,edge_ref,gate_ref,artifact_ref,safe_summary,run_state,node_state,correlation_ref,occurred_at) VALUES($1,$2,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''),$14,NULLIF($15,''),NULLIF($16,''),$17,$18)`, eventID, ref, scope.organizationID, projectValue, rootRunID, aggregateRef, version, sequence, eventType, nodeRef, edgeRef, gateRef, artifactRef, summary, runState, nodeState, scope.actorRef, event.OccurredAt); err != nil {
		return entity.RunEvent{}, errs.ErrUnavailable
	}
	data := map[string]any{"kind": eventKind(eventType), "runRef": rootRef, "safeSummary": summary}
	for key, value := range map[string]string{"nodeRef": nodeRef, "edgeRef": edgeRef, "gateRef": gateRef, "artifactRef": artifactRef} {
		if value != "" {
			data[key] = value
		}
	}
	if state := eventRunState(runState); state != "" {
		data["state"] = state
	}
	payload := map[string]any{"eventId": eventID.String(), "eventName": eventType, "eventVersion": 1, "occurredAt": event.OccurredAt, "organizationRef": scope.organizationRef, "rootRunRef": rootRef, "aggregateRef": aggregateRef, "aggregateVersion": version, "sequence": sequence, "correlationRef": scope.correlationRef, "data": data}
	if projectRef != "" {
		payload["projectRef"] = projectRef
	}
	subject := "control_plane.run." + scope.organizationRef + "." + rootRef + ".events"
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.outbox_events(event_id,subject,ordering_key,sequence,payload) VALUES($1,$2,$3,$4,$5)`, eventID, subject, "run:"+rootRef, sequence, asJSON(payload)); err != nil {
		return entity.RunEvent{}, errs.ErrUnavailable
	}
	return event, nil
}

func platformEventKind(eventName string) string {
	switch eventName {
	case "PROJECT_CHANGED":
		return "PROJECT"
	case "AGENT_CHANGED":
		return "AGENT"
	case "INSTRUCTIONS_PUBLISHED":
		return "INSTRUCTIONS"
	case "WORKFLOW_CHANGED":
		return "WORKFLOW"
	case "SCHEDULE_CHANGED":
		return "SCHEDULE"
	case "INTEGRATION_CONNECTION_CHANGED":
		return "INTEGRATION_CONNECTION"
	case "INTEGRATION_GRANT_CHANGED":
		return "INTEGRATION_GRANT"
	case "MEMBERSHIP_CHANGED":
		return "MEMBERSHIP"
	case "SYSTEM_ASSISTANT_CHANGED":
		return "SYSTEM_ASSISTANT"
	default:
		return "SYSTEM_ASSISTANT"
	}
}

func eventRunState(value string) string {
	switch value {
	case "WAITING_HUMAN":
		return "WAITING_OWNER"
	case "QUEUED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED":
		return value
	default:
		return ""
	}
}
func eventKind(eventType string) string {
	switch eventType {
	case "NODE_ADDED", "NODE_STATE_CHANGED":
		return "NODE"
	case "EDGE_ADDED":
		return "EDGE"
	case "TURN_QUEUED", "TURN_STARTED", "TURN_PROGRESS", "TURN_COMPLETED":
		return "TURN"
	case "DELEGATION_CREATED":
		return "DELEGATION"
	case "CALLBACK_DELIVERED":
		return "CALLBACK"
	case "OWNER_GATE_OPENED", "OWNER_GATE_RESOLVED":
		return "OWNER_GATE"
	case "ARTIFACT_AVAILABLE":
		return "ARTIFACT"
	case "INCIDENT_LINKED":
		return "INCIDENT"
	default:
		return "RUN"
	}
}

func (repository *Repository) readRunGraphTx(ctx context.Context, tx pgx.Tx, scope scope, runRef string) (entity.Run, entity.RunGraph, error) {
	run, err := scanRun(tx.QueryRow(ctx, runSelect+` WHERE r.organization_id=$1::uuid AND r.ref=$2`, scope.organizationID, runRef))
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	graph := entity.RunGraph{RunRef: run.RootRunRef, Revision: run.GraphRevision, Sequence: run.EventSequence}
	rows, err := tx.Query(ctx, `SELECT n.ref,run.ref,COALESCE(parent.ref,''),n.type,n.state,n.display_name,n.role,COALESCE(a.ref,''),COALESCE(t.ref,''),n.attempt,n.input_summary,n.progress_summary,n.integration_names,n.callback_summary,n.safe_error_code,n.safe_error_message,n.next_actions,n.created_at,n.started_at,n.finished_at,'{}'::text[],'{}'::text[] FROM control_plane.run_nodes n JOIN control_plane.runs run ON run.id=n.run_id LEFT JOIN control_plane.run_nodes parent ON parent.id=n.parent_node_id LEFT JOIN control_plane.agents a ON a.id=n.agent_id LEFT JOIN control_plane.session_turns t ON t.id=n.turn_id WHERE n.organization_id=$1::uuid AND (n.root_run_id=(SELECT root_run_id FROM control_plane.runs WHERE ref=$2) OR EXISTS(SELECT 1 FROM control_plane.run_edges e WHERE e.root_run_id=(SELECT root_run_id FROM control_plane.runs WHERE ref=$2) AND (e.source_node_id=n.id OR e.target_node_id=n.id))) ORDER BY n.created_at`, scope.organizationID, runRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var n entity.RunNode
		if err := rows.Scan(&n.Ref, &n.RunRef, &n.ParentNodeRef, &n.Type, &n.State, &n.DisplayName, &n.Role, &n.AgentRef, &n.TurnRef, &n.Attempt, &n.InputSummary, &n.ProgressSummary, &n.IntegrationNames, &n.CallbackSummary, &n.SafeErrorCode, &n.SafeErrorMessage, &n.NextActions, &n.CreatedAt, &n.StartedAt, &n.FinishedAt, &n.ArtifactRefs, &n.ChildRunRefs); err != nil {
			return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
		}
		graph.Nodes = append(graph.Nodes, n)
	}
	edgeRows, err := tx.Query(ctx, `SELECT e.ref,root.ref,s.ref,t.ref,e.type,e.label FROM control_plane.run_edges e JOIN control_plane.runs root ON root.id=e.root_run_id JOIN control_plane.run_nodes s ON s.id=e.source_node_id JOIN control_plane.run_nodes t ON t.id=e.target_node_id WHERE e.organization_id=$1::uuid AND e.root_run_id=(SELECT root_run_id FROM control_plane.runs WHERE ref=$2) ORDER BY e.created_at`, scope.organizationID, runRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var e entity.RunEdge
		if err := edgeRows.Scan(&e.Ref, &e.RunRef, &e.SourceNodeRef, &e.TargetNodeRef, &e.Type, &e.Label); err != nil {
			return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
		}
		graph.Edges = append(graph.Edges, e)
	}
	return run, graph, nil
}

func (repository *Repository) addSessionTurn(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SessionTurnInput)
	if !ok || payload.SessionRef == "" || strings.TrimSpace(payload.Task) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef, targetType, targetRef string
	if err := tx.QueryRow(ctx, `SELECT s.project_id::text,p.ref,s.target_type,s.target_ref FROM control_plane.sessions s JOIN control_plane.projects p ON p.id=s.project_id WHERE s.organization_id=$1::uuid AND s.ref=$2 AND s.state='ACTIVE' FOR UPDATE`, scope.organizationID, payload.SessionRef).Scan(&projectID, &projectRef, &targetType, &targetRef); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	launch := command.LaunchRunInput{ProjectRef: projectRef, Title: "Продолжение сессии", Task: payload.Task, SessionRef: payload.SessionRef, Source: "CONTROL_CENTER", Target: entity.RunTarget{Type: targetType, Ref: targetRef}, ArtifactRefs: payload.ArtifactRefs}
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = launch
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	if outcome.result.Run != nil && payload.RunRef != "" {
		var previousRootID, newRootID, previousNodeID, newNodeID string
		if err := tx.QueryRow(ctx, `SELECT r.root_run_id::text FROM control_plane.runs r JOIN control_plane.sessions s ON s.id=r.session_id WHERE r.organization_id=$1::uuid AND r.ref=$2 AND s.ref=$3`, scope.organizationID, payload.RunRef, payload.SessionRef).Scan(&previousRootID); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err := tx.QueryRow(ctx, `SELECT root_run_id::text FROM control_plane.runs WHERE ref=$1`, outcome.result.Run.Ref).Scan(&newRootID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		_ = tx.QueryRow(ctx, `SELECT id::text FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='ROOT_PROCESS' LIMIT 1`, previousRootID).Scan(&previousNodeID)
		_ = tx.QueryRow(ctx, `SELECT id::text FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='ROOT_PROCESS' LIMIT 1`, newRootID).Scan(&newNodeID)
		edgeRef, _ := newRef("edg")
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'CONTINUES','Продолжает сессию')`, edgeRef, scope.organizationID, newRootID, previousNodeID, newNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, newRootID, edgeRef, "EDGE_ADDED", "", edgeRef, "", "", "Сессия продолжена", "QUEUED", ""); err != nil {
			return commandOutcome{}, err
		}
		continuedRun, graph, err := repository.readRunGraphTx(ctx, tx, scope, outcome.result.Run.Ref)
		if err != nil {
			return commandOutcome{}, err
		}
		outcome.result.Run = &continuedRun
		outcome.result.Graph = &graph
	}
	outcome.summary = "Сессия продолжена"
	return outcome, nil
}

func (repository *Repository) changeRun(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RunCommandInput)
	if !ok || payload.RunRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var runID, rootRunID, projectID, projectRef, state string
	var version int64
	var attempt int32
	if err := tx.QueryRow(ctx, `SELECT r.id::text,r.root_run_id::text,r.project_id::text,p.ref,r.state,r.version,r.attempt FROM control_plane.runs r JOIN control_plane.projects p ON p.id=r.project_id WHERE r.organization_id=$1::uuid AND r.ref=$2 FOR UPDATE`, scope.organizationID, payload.RunRef).Scan(&runID, &rootRunID, &projectID, &projectRef, &state, &version, &attempt); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if input.Kind == command.CancelRun {
		if !contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"}, state) {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state='CANCELLED',safe_error_code='cancelled_by_owner',safe_error_message=$2,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE root_run_id=$1::uuid AND state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')`, rootRunID, truncate(payload.Reason, 500)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		_, _ = tx.Exec(ctx, `UPDATE control_plane.run_nodes SET state='CANCELLED',next_actions=ARRAY['OPEN','RETRY'],finished_at=clock_timestamp(),version=version+1 WHERE root_run_id=$1::uuid AND state IN ('QUEUED','RUNNING','WAITING')`, rootRunID)
		_, _ = tx.Exec(ctx, `UPDATE control_plane.runtime_leases SET state='CANCELLED',updated_at=clock_timestamp() WHERE run_id IN (SELECT id FROM control_plane.runs WHERE root_run_id=$1::uuid) AND state='CLAIMED'`, rootRunID)
		_, _ = tx.Exec(ctx, `UPDATE control_plane.owner_gates SET state='CANCELLED',decision='CANCEL',decision_comment='Запуск отменён',resolved_by=$2::uuid,resolved_at=clock_timestamp(),version=version+1 WHERE root_run_id=$1::uuid AND state='OPEN'`, rootRunID, scope.actorID)
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.RunRef, "RUN_STATE_CHANGED", "", "", "", "", "Запуск отменён", "CANCELLED", ""); err != nil {
			return commandOutcome{}, err
		}
		run, graph, err := repository.readRunGraphTx(ctx, tx, scope, payload.RunRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Run: &run, Graph: &graph}, projectID: projectID, projectRef: projectRef, resourceKind: "RUN", resourceRef: payload.RunRef, summary: "Запуск отменён"}, nil
	}
	if !contains([]string{"FAILED", "CANCELLED"}, state) {
		return commandOutcome{}, errs.ErrConflict
	}
	var targetType, targetRef, title, task, sessionRef, source string
	var raw []byte
	var artifacts []string
	if err := tx.QueryRow(ctx, `SELECT r.target_type,r.target_ref,r.title,r.task,s.ref,r.source,r.input,r.input_artifact_refs FROM control_plane.runs r JOIN control_plane.sessions s ON s.id=r.session_id WHERE r.id=$1::uuid`, runID).Scan(&targetType, &targetRef, &title, &task, &sessionRef, &source, &raw, &artifacts); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var launchInput map[string]any
	_ = json.Unmarshal(raw, &launchInput)
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: title, Task: task, SessionRef: sessionRef, Source: source, Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: launchInput, ArtifactRefs: artifacts}
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	var newRunID, newRootID, newRootNodeID, oldRootNodeID string
	_ = tx.QueryRow(ctx, `SELECT id::text,root_run_id::text FROM control_plane.runs WHERE ref=$1`, outcome.result.Run.Ref).Scan(&newRunID, &newRootID)
	_, _ = tx.Exec(ctx, `UPDATE control_plane.runs SET retry_of_run_id=$2::uuid,attempt=$3 WHERE id=$1::uuid`, newRunID, runID, attempt+1)
	_ = tx.QueryRow(ctx, `SELECT id::text FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='ROOT_PROCESS' LIMIT 1`, rootRunID).Scan(&oldRootNodeID)
	_ = tx.QueryRow(ctx, `SELECT id::text FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='ROOT_PROCESS' LIMIT 1`, newRootID).Scan(&newRootNodeID)
	edgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'RETRY_OF','Повторная попытка')`, edgeRef, scope.organizationID, newRootID, oldRootNodeID, newRootNodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, newRootID, edgeRef, "EDGE_ADDED", "", edgeRef, "", "", "Создана повторная попытка", "QUEUED", ""); err != nil {
		return commandOutcome{}, err
	}
	retryRun, graph, err := repository.readRunGraphTx(ctx, tx, scope, outcome.result.Run.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Run = &retryRun
	outcome.result.Graph = &graph
	outcome.summary = "Создана новая попытка"
	return outcome, nil
}

func (repository *Repository) resolveGate(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.GateResolutionInput)
	if !ok || payload.GateRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	stateMap := map[string]string{"APPROVE": "APPROVED", "REJECT": "REJECTED", "REQUEST_CHANGES": "CHANGES_REQUESTED", "CANCEL": "CANCELLED"}
	nextState := stateMap[payload.Decision]
	if nextState == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var gateID, nodeID, rootRunID, projectID, projectRef string
	var version int64
	var allowed []string
	err := tx.QueryRow(ctx, `SELECT g.id::text,g.node_id::text,g.root_run_id::text,g.project_id::text,p.ref,g.version,g.allowed_decisions FROM control_plane.owner_gates g JOIN control_plane.projects p ON p.id=g.project_id WHERE g.organization_id=$1::uuid AND g.ref=$2 AND g.state='OPEN' FOR UPDATE`, scope.organizationID, payload.GateRef).Scan(&gateID, &nodeID, &rootRunID, &projectID, &projectRef, &version, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if !contains(allowed, payload.Decision) {
		return commandOutcome{}, errs.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.owner_gates SET state=$2,decision=$3,decision_comment=$4,resolved_by=$5::uuid,resolved_at=clock_timestamp(),version=version+1 WHERE id=$1::uuid`, gateID, nextState, payload.Decision, truncate(payload.Comment, 2000), scope.actorID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	nodeState := "SUCCEEDED"
	runState := "RUNNING"
	if payload.Decision == "REJECT" || payload.Decision == "CANCEL" {
		nodeState = "FAILED"
		runState = "FAILED"
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.run_nodes SET state=$2,finished_at=clock_timestamp(),version=version+1,next_actions=ARRAY['OPEN'] WHERE id=$1::uuid`, nodeID, nodeState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state=$2,version=version+1,updated_at=clock_timestamp(),finished_at=CASE WHEN $2='FAILED' THEN clock_timestamp() ELSE NULL END WHERE id=$1::uuid`, rootRunID, runState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.GateRef, "OWNER_GATE_RESOLVED", "", "", payload.GateRef, "", "Решение принято", runState, nodeState)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, mustRunRef(ctx, tx, rootRunID))
	if err != nil {
		return commandOutcome{}, err
	}
	gate := entity.OwnerGate{Ref: payload.GateRef, RunRef: run.RootRunRef, ProjectRef: projectRef, State: nextState, Decision: payload.Decision, DecisionComment: payload.Comment, Version: version + 1}
	return commandOutcome{result: command.Result{Gate: &gate, Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "OWNER_GATE", resourceRef: payload.GateRef, summary: "Human Gate разрешён"}, nil
}

func mustRunRef(ctx context.Context, tx pgx.Tx, id string) string {
	var ref string
	_ = tx.QueryRow(ctx, `SELECT ref FROM control_plane.runs WHERE id=$1::uuid`, id).Scan(&ref)
	return ref
}
