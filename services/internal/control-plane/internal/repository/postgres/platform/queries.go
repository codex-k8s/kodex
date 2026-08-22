package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) GetBootstrapState(ctx context.Context, principal value.Principal) (platformrepo.BootstrapState, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.BootstrapState{}, err
	}
	assistant, err := repository.getAssistant(ctx, scope)
	if err != nil {
		return platformrepo.BootstrapState{}, err
	}
	var state platformrepo.BootstrapState
	var bootstrappedAt, onboardingAt *time.Time
	err = repository.pool.QueryRow(ctx, `SELECT i.bootstrapped_at,i.onboarding_completed_at,
		(SELECT count(*) FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.lifecycle='ACTIVE')
		FROM control_plane.installation i WHERE singleton`, scope.organizationID).Scan(&bootstrappedAt, &onboardingAt, &state.ProjectCount)
	if err != nil {
		return platformrepo.BootstrapState{}, errs.ErrUnavailable
	}
	state.Bootstrapped = bootstrappedAt != nil
	state.OnboardingCompleted = onboardingAt != nil
	state.OrganizationRef = scope.organizationRef
	state.Assistant = assistant
	state.Actor = entity.User{Ref: scope.actorRef, DisplayName: scope.actorName, Active: true}
	return state, nil
}

func (repository *Repository) GetOverview(ctx context.Context, principal value.Principal, projectRef string) (platformrepo.Overview, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	filter := query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}}
	runs, _, err := repository.ListRuns(ctx, principal, filter)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	gates, _, err := repository.ListOwnerGates(ctx, principal, query.Filter{ProjectRef: projectRef, State: "OPEN", Page: query.Page{Size: 20}})
	if err != nil {
		return platformrepo.Overview{}, err
	}
	artifacts, _, err := repository.ListArtifacts(ctx, principal, filter)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	var result platformrepo.Overview
	err = repository.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.lifecycle='ACTIVE'),
		(SELECT count(*) FROM control_plane.agents a WHERE a.organization_id=$1::uuid AND a.system_key IS NULL AND a.state<>'ARCHIVED'),
		(SELECT count(*) FROM control_plane.runs r WHERE r.organization_id=$1::uuid AND r.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
		(SELECT count(*) FROM control_plane.owner_gates g WHERE g.organization_id=$1::uuid AND g.state='OPEN')`, scope.organizationID).Scan(
		&result.ProjectCount, &result.AgentCount, &result.ActiveRunCount, &result.PendingGateCount)
	if err != nil {
		return platformrepo.Overview{}, errs.ErrUnavailable
	}
	for _, run := range runs {
		if run.State == "QUEUED" || run.State == "RUNNING" || run.State == "WAITING_HUMAN" || run.State == "CANCELLING" {
			result.ActiveRuns = append(result.ActiveRuns, run)
		}
	}
	result.PendingGates = gates
	result.RecentArtifacts = artifacts
	return result, nil
}

func (repository *Repository) ListCapabilities(ctx context.Context, principal value.Principal) ([]entity.IntegrationCapability, error) {
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, `SELECT stable_key,name,description,risk FROM control_plane.platform_capabilities WHERE enabled ORDER BY name`)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationCapability
	for rows.Next() {
		var item entity.IntegrationCapability
		if err := rows.Scan(&item.Key, &item.Name, &item.Description, &item.Risk); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (repository *Repository) ListProjects(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Project, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT p.ref,p.name,p.purpose,p.language,p.lifecycle,p.version,p.created_at,p.updated_at
		FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.lifecycle<>'ARCHIVED'
		AND ($2 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$3::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
		AND ($4='' OR p.name ILIKE '%'||$4||'%' OR p.purpose ILIKE '%'||$4||'%') ORDER BY p.updated_at DESC LIMIT $5`,
		scope.organizationID, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Project
	for rows.Next() {
		var item entity.Project
		if err := rows.Scan(&item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.NextActions = []string{"OPEN", "EDIT"}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

func (repository *Repository) GetProject(ctx context.Context, principal value.Principal, ref string) (entity.Project, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Project{}, err
	}
	var item entity.Project
	err = repository.pool.QueryRow(ctx, `SELECT p.ref,p.name,p.purpose,p.language,p.lifecycle,p.version,p.created_at,p.updated_at
		FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.ref=$2
		AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))`,
		scope.organizationID, ref, scope.role, scope.actorID).Scan(&item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Project{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Project{}, errs.ErrUnavailable
	}
	item.NextActions = []string{"OPEN", "EDIT"}
	return item, nil
}

func (repository *Repository) ListMemberships(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT m.ref,p.ref,s.ref,s.display_name,s.email_masked,s.active,m.role,m.permissions,m.active,m.version
		FROM control_plane.memberships m JOIN control_plane.projects p ON p.id=m.project_id JOIN control_plane.subjects s ON s.id=m.subject_id
		WHERE m.organization_id=$1::uuid AND p.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships own WHERE own.project_id=p.id AND own.subject_id=$4::uuid AND own.active AND 'MANAGE_MEMBERS'=ANY(own.permissions)))
		ORDER BY s.display_name LIMIT $5`, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Membership
	for rows.Next() {
		var item entity.Membership
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked, &item.User.Active, &item.Role, &item.Permissions, &item.Active, &item.Version); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

func (repository *Repository) ListAgents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT a.ref,p.ref,COALESCE(a.system_key,''),a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,
		a.runtime_key,r.name,r.provider,r.model,r.runtime_revision,a.capabilities,a.knowledge_artifact_refs,a.created_at,a.updated_at
		FROM control_plane.agents a LEFT JOIN control_plane.projects p ON p.id=a.project_id JOIN control_plane.runtime_profiles r ON r.stable_key=a.runtime_key
		WHERE a.organization_id=$1::uuid AND a.system_key IS NULL AND p.ref=$2 AND a.state<>'ARCHIVED'
		AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
		AND ($5='' OR a.name ILIKE '%'||$5||'%' OR a.purpose ILIKE '%'||$5||'%') AND ($6='' OR a.state=$6)
		ORDER BY a.updated_at DESC LIMIT $7`, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Agent
	for rows.Next() {
		var item entity.Agent
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.NextActions = agentActions(item)
		result = append(result, item)
	}
	for index := range result {
		_ = repository.attachInstructions(ctx, scope, &result[index])
		_ = repository.attachAgentGrants(ctx, scope, &result[index])
	}
	return result, "", rows.Err()
}

func (repository *Repository) GetAgent(ctx context.Context, principal value.Principal, ref string) (entity.Agent, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Agent{}, err
	}
	var item entity.Agent
	err = repository.pool.QueryRow(ctx, `SELECT a.ref,COALESCE(p.ref,''),COALESCE(a.system_key,''),a.name,a.purpose,a.role_description,a.avatar_url,a.state,a.enabled,a.version,
		a.runtime_key,r.name,r.provider,r.model,r.runtime_revision,a.capabilities,a.knowledge_artifact_refs,a.created_at,a.updated_at
		FROM control_plane.agents a LEFT JOIN control_plane.projects p ON p.id=a.project_id JOIN control_plane.runtime_profiles r ON r.stable_key=a.runtime_key
		WHERE a.organization_id=$1::uuid AND a.ref=$2 AND (a.system_key='system-assistant' OR $3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=a.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))`, scope.organizationID, ref, scope.role, scope.actorID).Scan(
		&item.Ref, &item.ProjectRef, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	item.System = item.SystemKey != ""
	item.NextActions = agentActions(item)
	_ = repository.attachInstructions(ctx, scope, &item)
	_ = repository.attachAgentGrants(ctx, scope, &item)
	return item, nil
}

func agentActions(agent entity.Agent) []string {
	if agent.System {
		return []string{"OPEN", "RECOVER"}
	}
	actions := []string{"OPEN", "EDIT"}
	if agent.Enabled && agent.State == "READY" {
		actions = append(actions, "LAUNCH", "DISABLE")
	}
	if !agent.Enabled {
		actions = append(actions, "ENABLE")
	}
	if agent.State != "ARCHIVED" {
		actions = append(actions, "ARCHIVE")
	}
	return actions
}

func (repository *Repository) attachInstructions(ctx context.Context, scope scope, agent *entity.Agent) error {
	rows, err := repository.pool.Query(ctx, `SELECT ref,version_number,state,content,digest,core,COALESCE(parent_ref,''),validation_problems,created_at,published_at
		FROM control_plane.instruction_versions WHERE organization_id=$1::uuid AND agent_id=(SELECT id FROM control_plane.agents WHERE ref=$2)
		ORDER BY version_number DESC`, scope.organizationID, agent.Ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item entity.InstructionVersion
		var problems []byte
		if err := rows.Scan(&item.Ref, &item.VersionNumber, &item.State, &item.Content, &item.Digest, &item.Core, &item.ParentRef, &problems, &item.CreatedAt, &item.PublishedAt); err != nil {
			return err
		}
		_ = json.Unmarshal(problems, &item.ValidationProblems)
		if item.State == "PUBLISHED" && agent.PublishedInstructions == nil {
			copy := item
			agent.PublishedInstructions = &copy
		} else if item.State != "PUBLISHED" && agent.DraftInstructions == nil {
			copy := item
			agent.DraftInstructions = &copy
		}
	}
	return rows.Err()
}

func (repository *Repository) attachAgentGrants(ctx context.Context, scope scope, agent *entity.Agent) error {
	rows, err := repository.pool.Query(ctx, `SELECT g.ref FROM control_plane.integration_grants g WHERE g.organization_id=$1::uuid AND g.target_kind='AGENT' AND g.target_ref=$2 AND g.enabled ORDER BY g.ref`, scope.organizationID, agent.Ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return err
		}
		agent.IntegrationGrantRefs = append(agent.IntegrationGrantRefs, ref)
	}
	return rows.Err()
}

func (repository *Repository) ListWorkflows(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Workflow, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT w.ref,p.ref,w.name,w.purpose,COALESCE(a.ref,''),w.state,w.version,w.draft_spec,w.published_spec,w.published_version,w.created_at,w.updated_at
		FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id LEFT JOIN control_plane.agents a ON a.id=w.coordinator_agent_id
		WHERE w.organization_id=$1::uuid AND p.ref=$2 AND w.state<>'ARCHIVED' AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=p.id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))
		AND ($5='' OR w.name ILIKE '%'||$5||'%') AND ($6='' OR w.state=$6) ORDER BY w.updated_at DESC LIMIT $7`, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Workflow
	for rows.Next() {
		item, scanErr := scanWorkflow(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanWorkflow(row rowScanner) (entity.Workflow, error) {
	var item entity.Workflow
	var draft, published []byte
	var publishedVersion int32
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.Name, &item.Purpose, &item.CoordinatorAgentRef, &item.State, &item.Version, &draft, &published, &publishedVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Workflow{}, errs.ErrNotFound
		}
		return entity.Workflow{}, errs.ErrUnavailable
	}
	item.Draft = &entity.WorkflowVersion{}
	_ = json.Unmarshal(draft, item.Draft)
	if len(published) > 0 {
		item.Published = &entity.WorkflowVersion{}
		_ = json.Unmarshal(published, item.Published)
		item.Published.VersionNumber = publishedVersion
	}
	item.NextActions = []string{"OPEN", "EDIT"}
	if item.State == "PUBLISHED" {
		item.NextActions = append(item.NextActions, "LAUNCH")
	}
	return item, nil
}

func (repository *Repository) GetWorkflow(ctx context.Context, principal value.Principal, ref string) (entity.Workflow, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Workflow{}, err
	}
	row := repository.pool.QueryRow(ctx, `SELECT w.ref,p.ref,w.name,w.purpose,COALESCE(a.ref,''),w.state,w.version,w.draft_spec,w.published_spec,w.published_version,w.created_at,w.updated_at FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id LEFT JOIN control_plane.agents a ON a.id=w.coordinator_agent_id WHERE w.organization_id=$1::uuid AND w.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=w.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))`, scope.organizationID, ref, scope.role, scope.actorID)
	return scanWorkflow(row)
}

func (repository *Repository) ListRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Run, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, runSelect+` WHERE r.organization_id=$1::uuid AND ($2='' OR p.ref=$2) AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=r.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions))) AND ($5='' OR r.title ILIKE '%'||$5||'%' OR r.task ILIKE '%'||$5||'%') ORDER BY r.created_at DESC LIMIT $6`, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Run
	for rows.Next() {
		item, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		if len(filter.States) == 0 || contains(filter.States, item.State) {
			result = append(result, item)
		}
	}
	return result, "", rows.Err()
}

const runSelect = `SELECT r.ref,COALESCE(p.ref,''),s.ref,root.ref,COALESCE(parent.ref,''),COALESCE(retry.ref,''),r.title,r.task,r.state,r.source,r.result_summary,r.safe_error_code,r.safe_error_message,sub.display_name,r.target_type,r.target_ref,COALESCE(a.name,w.name,sa.name,r.target_ref),r.attempt,r.graph_revision,r.event_sequence,r.version,r.input,r.input_artifact_refs,r.created_at,r.started_at,r.finished_at FROM control_plane.runs r LEFT JOIN control_plane.projects p ON p.id=r.project_id JOIN control_plane.sessions s ON s.id=r.session_id JOIN control_plane.runs root ON root.id=r.root_run_id LEFT JOIN control_plane.runs parent ON parent.id=r.parent_run_id LEFT JOIN control_plane.runs retry ON retry.id=r.retry_of_run_id JOIN control_plane.subjects sub ON sub.id=r.initiated_by LEFT JOIN control_plane.agents a ON r.target_type IN ('AGENT','SYSTEM_ASSISTANT') AND a.ref=r.target_ref LEFT JOIN control_plane.workflows w ON r.target_type='WORKFLOW' AND w.ref=r.target_ref LEFT JOIN control_plane.agents sa ON r.target_type='SYSTEM_ASSISTANT' AND sa.system_key='system-assistant'`

func scanRun(row rowScanner) (entity.Run, error) {
	var item entity.Run
	var input []byte
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.SessionRef, &item.RootRunRef, &item.ParentRunRef, &item.RetryOfRunRef, &item.Title, &item.Task, &item.State, &item.Source, &item.ResultSummary, &item.SafeErrorCode, &item.SafeErrorMessage, &item.InitiatorName, &item.Target.Type, &item.Target.Ref, &item.Target.Name, &item.Attempt, &item.GraphRevision, &item.EventSequence, &item.Version, &input, &item.InputArtifactRefs, &item.CreatedAt, &item.StartedAt, &item.FinishedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Run{}, errs.ErrNotFound
		}
		return entity.Run{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(input, &item.Input)
	item.NextActions = runActions(item.State)
	return item, nil
}
func runActions(state string) []string {
	switch state {
	case "QUEUED", "RUNNING", "WAITING_HUMAN":
		return []string{"OPEN", "CANCEL"}
	case "FAILED", "CANCELLED":
		return []string{"OPEN", "RETRY"}
	case "SUCCEEDED":
		return []string{"OPEN", "ADD_TURN"}
	default:
		return []string{"OPEN"}
	}
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (repository *Repository) GetRun(ctx context.Context, principal value.Principal, ref string) (entity.Run, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Run{}, err
	}
	return scanRun(repository.pool.QueryRow(ctx, runSelect+` WHERE r.organization_id=$1::uuid AND r.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=r.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))`, scope.organizationID, ref, scope.role, scope.actorID))
}

func (repository *Repository) GetRunGraph(ctx context.Context, principal value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	run, err := repository.GetRun(ctx, principal, ref)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	graph := entity.RunGraph{RunRef: run.RootRunRef, Revision: run.GraphRevision, Sequence: run.EventSequence}
	rows, err := repository.pool.Query(ctx, `SELECT n.ref,run.ref,COALESCE(parent.ref,''),n.type,n.state,n.display_name,n.role,COALESCE(a.ref,''),COALESCE(t.ref,''),n.attempt,n.input_summary,n.progress_summary,n.integration_names,n.callback_summary,n.safe_error_code,n.safe_error_message,n.next_actions,n.created_at,n.started_at,n.finished_at,
		COALESCE((SELECT array_agg(ar.ref ORDER BY ar.created_at) FROM control_plane.artifacts ar WHERE ar.node_id=n.id),'{}'),COALESCE((SELECT array_agg(cr.ref ORDER BY cr.created_at) FROM control_plane.runs cr WHERE cr.parent_run_id=n.run_id),'{}')
		FROM control_plane.run_nodes n JOIN control_plane.runs root ON root.id=n.root_run_id JOIN control_plane.runs run ON run.id=n.run_id LEFT JOIN control_plane.run_nodes parent ON parent.id=n.parent_node_id LEFT JOIN control_plane.agents a ON a.id=n.agent_id LEFT JOIN control_plane.session_turns t ON t.id=n.turn_id WHERE n.organization_id=$1::uuid AND (root.ref=$2 OR EXISTS(SELECT 1 FROM control_plane.run_edges e JOIN control_plane.runs eroot ON eroot.id=e.root_run_id WHERE eroot.ref=$2 AND (e.source_node_id=n.id OR e.target_node_id=n.id))) ORDER BY n.created_at`, scope.organizationID, run.RootRunRef)
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
	edgeRows, err := repository.pool.Query(ctx, `SELECT e.ref,root.ref,s.ref,t.ref,e.type,e.label FROM control_plane.run_edges e JOIN control_plane.runs root ON root.id=e.root_run_id JOIN control_plane.run_nodes s ON s.id=e.source_node_id JOIN control_plane.run_nodes t ON t.id=e.target_node_id WHERE e.organization_id=$1::uuid AND root.ref=$2 ORDER BY e.created_at`, scope.organizationID, run.RootRunRef)
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

func (repository *Repository) ListRunEvents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RunEvent, int64, bool, error) {
	run, err := repository.GetRun(ctx, principal, filter.ResourceRef)
	if err != nil {
		return nil, 0, false, err
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, false, err
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := repository.pool.Query(ctx, `SELECT e.ref,root.ref,e.sequence,e.type,COALESCE(e.node_ref,''),COALESCE(e.edge_ref,''),COALESCE(e.gate_ref,''),COALESCE(e.artifact_ref,''),e.safe_summary,e.safe_progress,COALESCE(e.run_state,''),COALESCE(e.node_state,''),e.occurred_at FROM control_plane.run_events e JOIN control_plane.runs root ON root.id=e.root_run_id WHERE e.organization_id=$1::uuid AND root.ref=$2 AND e.sequence>$3 ORDER BY e.sequence LIMIT $4`, scope.organizationID, run.RootRunRef, filter.AfterSequence, limit)
	if err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RunEvent
	for rows.Next() {
		var e entity.RunEvent
		if err := rows.Scan(&e.Ref, &e.RunRef, &e.Sequence, &e.Type, &e.NodeRef, &e.EdgeRef, &e.GateRef, &e.ArtifactRef, &e.Summary, &e.Progress, &e.RunState, &e.NodeState, &e.OccurredAt); err != nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		result = append(result, e)
	}
	complete := len(result) < int(limit) || len(result) > 0 && result[len(result)-1].Sequence == run.EventSequence
	return result, run.EventSequence, complete, rows.Err()
}

func (repository *Repository) ListOwnerGates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.OwnerGate, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, gateSelect+` WHERE g.organization_id=$1::uuid AND ($2='' OR p.ref=$2) AND ($3='' OR g.state=$3) AND ($4 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=g.project_id AND m.subject_id=$5::uuid AND m.active AND 'VIEW'=ANY(m.permissions))) ORDER BY g.created_at DESC LIMIT $6`, scope.organizationID, filter.ProjectRef, filter.State, scope.role, scope.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.OwnerGate
	for rows.Next() {
		item, scanErr := scanGate(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

const gateSelect = `SELECT g.ref,p.ref,root.ref,n.ref,g.title,g.prompt,g.context_summary,g.allowed_decisions,g.state,COALESCE(g.decision,''),g.decision_comment,COALESCE(s.display_name,''),g.version,g.created_at,g.resolved_at FROM control_plane.owner_gates g JOIN control_plane.projects p ON p.id=g.project_id JOIN control_plane.runs root ON root.id=g.root_run_id JOIN control_plane.run_nodes n ON n.id=g.node_id LEFT JOIN control_plane.subjects s ON s.id=g.resolved_by`

func scanGate(row rowScanner) (entity.OwnerGate, error) {
	var item entity.OwnerGate
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.RunRef, &item.NodeRef, &item.Title, &item.Prompt, &item.ContextSummary, &item.AllowedDecisions, &item.State, &item.Decision, &item.DecisionComment, &item.ResolvedByName, &item.Version, &item.CreatedAt, &item.ResolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.OwnerGate{}, errs.ErrNotFound
		}
		return entity.OwnerGate{}, errs.ErrUnavailable
	}
	if item.State == "OPEN" {
		item.NextActions = []string{"RESOLVE_GATE"}
	}
	return item, nil
}
func (repository *Repository) GetOwnerGate(ctx context.Context, principal value.Principal, ref string) (entity.OwnerGate, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	return scanGate(repository.pool.QueryRow(ctx, gateSelect+` WHERE g.organization_id=$1::uuid AND g.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=g.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))`, scope.organizationID, ref, scope.role, scope.actorID))
}

func (repository *Repository) ListArtifacts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Artifact, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, artifactSelect+` WHERE ar.organization_id=$1::uuid AND ($2='' OR p.ref=$2) AND ($3='' OR r.ref=$3) AND ($4 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=ar.project_id AND m.subject_id=$5::uuid AND m.active AND 'VIEW'=ANY(m.permissions))) AND ($6='' OR ar.file_name ILIKE '%'||$6||'%') ORDER BY ar.created_at DESC LIMIT $7`, scope.organizationID, filter.ProjectRef, filter.ResourceRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Artifact
	for rows.Next() {
		item, scanErr := scanArtifact(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

const artifactSelect = `SELECT ar.ref,p.ref,COALESCE(r.ref,''),COALESCE(n.ref,''),ar.file_name,ar.media_type,ar.digest,ar.scan_state,ar.preview_state,ar.size_bytes,ar.version,ar.created_at,COALESCE((SELECT array_agg(b.target_kind||':'||b.target_ref ORDER BY b.created_at) FROM control_plane.artifact_bindings b WHERE b.artifact_id=ar.id),'{}') FROM control_plane.artifacts ar JOIN control_plane.projects p ON p.id=ar.project_id LEFT JOIN control_plane.runs r ON r.id=ar.run_id LEFT JOIN control_plane.run_nodes n ON n.id=ar.node_id`

func scanArtifact(row rowScanner) (entity.Artifact, error) {
	var item entity.Artifact
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.RunRef, &item.NodeRef, &item.FileName, &item.MediaType, &item.Digest, &item.ScanState, &item.PreviewState, &item.SizeBytes, &item.Version, &item.CreatedAt, &item.Bindings); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Artifact{}, errs.ErrNotFound
		}
		return entity.Artifact{}, errs.ErrUnavailable
	}
	if item.ScanState == "CLEAN" {
		item.NextActions = []string{"DOWNLOAD", "BIND"}
	}
	return item, nil
}
func (repository *Repository) GetArtifact(ctx context.Context, principal value.Principal, ref string) (entity.Artifact, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Artifact{}, err
	}
	return scanArtifact(repository.pool.QueryRow(ctx, artifactSelect+` WHERE ar.organization_id=$1::uuid AND ar.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=ar.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions)))`, scope.organizationID, ref, scope.role, scope.actorID))
}

func (repository *Repository) ListSchedules(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT s.ref,p.ref,s.name,s.target_type,s.target_ref,COALESCE(a.name,w.name,s.target_ref),s.preset,s.cron_expression,s.timezone,s.input,s.session_policy,s.notification_policy,s.enabled,s.version,s.next_run_at,s.last_run_at,s.created_at,s.updated_at FROM control_plane.schedules s JOIN control_plane.projects p ON p.id=s.project_id LEFT JOIN control_plane.agents a ON s.target_type='AGENT' AND a.ref=s.target_ref LEFT JOIN control_plane.workflows w ON s.target_type='WORKFLOW' AND w.ref=s.target_ref WHERE s.organization_id=$1::uuid AND p.ref=$2 AND ($3 IN ('OWNER','ADMINISTRATOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=s.project_id AND m.subject_id=$4::uuid AND m.active AND 'VIEW'=ANY(m.permissions))) ORDER BY s.updated_at DESC LIMIT $5`, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Schedule
	for rows.Next() {
		var item entity.Schedule
		var input []byte
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.Name, &item.Target.Type, &item.Target.Ref, &item.Target.Name, &item.Preset, &item.CronExpression, &item.Timezone, &input, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		_ = json.Unmarshal(input, &item.Input)
		item.NextActions = []string{"OPEN", "EDIT"}
		if item.Enabled {
			item.NextActions = append(item.NextActions, "DISABLE")
		} else {
			item.NextActions = append(item.NextActions, "ENABLE")
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

func (repository *Repository) ListIntegrationDefinitions(ctx context.Context, principal value.Principal, category string) ([]entity.IntegrationDefinition, error) {
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, `SELECT stable_key,name,description,category,optional,enabled,capabilities,configuration_schema FROM control_plane.integration_definitions WHERE ($1='' OR category=$1) ORDER BY category,name`, category)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationDefinition
	for rows.Next() {
		var item entity.IntegrationDefinition
		var capabilities, schema []byte
		if err := rows.Scan(&item.Key, &item.Name, &item.Description, &item.Category, &item.Optional, &item.Enabled, &capabilities, &schema); err != nil {
			return nil, errs.ErrUnavailable
		}
		_ = json.Unmarshal(capabilities, &item.Capabilities)
		_ = json.Unmarshal(schema, &item.ConfigurationSchema)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (repository *Repository) ListIntegrationConnections(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.IntegrationConnection, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, connectionSelect+` WHERE c.organization_id=$1::uuid AND ($2='' OR c.definition_key=$2) ORDER BY c.updated_at DESC LIMIT $3`, scope.organizationID, filter.Category, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationConnection
	for rows.Next() {
		item, scanErr := scanConnection(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		_ = repository.attachConnection(ctx, scope, &item)
		result = append(result, item)
	}
	return result, "", rows.Err()
}

const connectionSelect = `SELECT c.ref,c.definition_key,d.name,c.name,c.state,c.masked_credentials_state,c.last_test_summary,c.enabled,c.version,c.public_configuration,d.capabilities,c.last_tested_at,c.created_at,c.updated_at FROM control_plane.integration_connections c JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key`

func scanConnection(row rowScanner) (entity.IntegrationConnection, error) {
	var item entity.IntegrationConnection
	var configuration, capabilities []byte
	if err := row.Scan(&item.Ref, &item.DefinitionKey, &item.DefinitionName, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &configuration, &capabilities, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.IntegrationConnection{}, errs.ErrNotFound
		}
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(configuration, &item.PublicConfiguration)
	_ = json.Unmarshal(capabilities, &item.Capabilities)
	item.NextActions = []string{"OPEN", "TEST"}
	if item.Enabled {
		item.NextActions = append(item.NextActions, "DISABLE")
	} else {
		item.NextActions = append(item.NextActions, "ENABLE")
	}
	return item, nil
}
func (repository *Repository) attachConnection(ctx context.Context, scope scope, item *entity.IntegrationConnection) error {
	rows, err := repository.pool.Query(ctx, `SELECT g.ref,g.capability_key,g.target_kind,g.target_ref,COALESCE(a.name,w.name,g.target_ref),g.enabled,g.approval_policy,g.version FROM control_plane.integration_grants g LEFT JOIN control_plane.agents a ON g.target_kind='AGENT' AND a.ref=g.target_ref LEFT JOIN control_plane.workflows w ON g.target_kind='WORKFLOW' AND w.ref=g.target_ref WHERE g.organization_id=$1::uuid AND g.connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$2) ORDER BY g.created_at`, scope.organizationID, item.Ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var grant entity.IntegrationGrant
		if err := rows.Scan(&grant.Ref, &grant.CapabilityKey, &grant.TargetType, &grant.TargetRef, &grant.TargetName, &grant.Enabled, &grant.ApprovalPolicy, &grant.Version); err != nil {
			return err
		}
		item.Grants = append(item.Grants, grant)
	}
	return rows.Err()
}
func (repository *Repository) GetIntegrationConnection(ctx context.Context, principal value.Principal, ref string) (entity.IntegrationConnection, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	item, err := scanConnection(repository.pool.QueryRow(ctx, connectionSelect+` WHERE c.organization_id=$1::uuid AND c.ref=$2`, scope.organizationID, ref))
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	if err := repository.attachConnection(ctx, scope, &item); err != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) getAssistant(ctx context.Context, scope scope) (entity.SystemAssistant, error) {
	var item entity.SystemAssistant
	var limits []byte
	err := repository.pool.QueryRow(ctx, `SELECT a.ref,ar.stable_key,a.name,a.purpose,ar.core_prompt_revision,ar.owner_instructions,ar.runtime_state,ar.runtime_revision,ar.desired_runtime_revision,ar.system_session_ref,ar.resource_limits,ar.last_heartbeat_at,ar.version,ar.updated_at FROM control_plane.assistant_runtime ar JOIN control_plane.agents a ON a.id=ar.agent_id WHERE ar.organization_id=$1::uuid`, scope.organizationID).Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &item.ResourceLimits)
	item.Ready = item.RuntimeState == "READY" && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) < 45*time.Second
	item.System = true
	item.Deletable = false
	item.NextActions = []string{"OPEN"}
	if !item.Ready {
		item.NextActions = append(item.NextActions, "RECOVER")
	}
	return item, nil
}
func (repository *Repository) GetSystemAssistant(ctx context.Context, principal value.Principal) (entity.SystemAssistant, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	return repository.getAssistant(ctx, scope)
}

func (repository *Repository) ListAssistantConversations(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.AssistantConversation, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT c.ref,c.title,COALESCE(p.ref,''),s.ref,c.state,c.version,c.created_at,c.updated_at FROM control_plane.assistant_conversations c LEFT JOIN control_plane.projects p ON p.id=c.project_id JOIN control_plane.sessions s ON s.id=c.session_id WHERE c.organization_id=$1::uuid AND ($2='' OR p.ref=$2) ORDER BY c.updated_at DESC LIMIT $3`, scope.organizationID, filter.ProjectRef, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AssistantConversation
	for rows.Next() {
		var item entity.AssistantConversation
		if err := rows.Scan(&item.Ref, &item.Title, &item.ProjectRef, &item.SessionRef, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		if err := repository.attachConversation(ctx, scope, &item); err != nil {
			return nil, "", err
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}
func (repository *Repository) attachConversation(ctx context.Context, scope scope, item *entity.AssistantConversation) error {
	rows, err := repository.pool.Query(ctx, `SELECT t.ref,t.actor_kind,COALESCE(s.display_name,a.name,t.actor_ref),t.content,t.state,t.artifact_refs,t.created_at,t.completed_at FROM control_plane.session_turns t LEFT JOIN control_plane.subjects s ON t.actor_kind='USER' AND s.ref=t.actor_ref LEFT JOIN control_plane.agents a ON t.actor_kind<>'USER' AND a.ref=t.actor_ref WHERE t.organization_id=$1::uuid AND t.session_id=(SELECT session_id FROM control_plane.assistant_conversations WHERE ref=$2) ORDER BY t.turn_number`, scope.organizationID, item.Ref)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var turn entity.AssistantTurn
		if err := rows.Scan(&turn.Ref, &turn.Actor, &turn.ActorName, &turn.Content, &turn.State, &turn.ArtifactRefs, &turn.CreatedAt, &turn.CompletedAt); err != nil {
			return errs.ErrUnavailable
		}
		item.Turns = append(item.Turns, turn)
	}
	var raw []byte
	var plan entity.AssistantPlan
	err = repository.pool.QueryRow(ctx, `SELECT p.ref,p.summary,p.state,p.version,p.operations,p.created_at,p.applied_at FROM control_plane.assistant_plans p JOIN control_plane.assistant_conversations c ON c.latest_plan_id=p.id WHERE c.organization_id=$1::uuid AND c.ref=$2`, scope.organizationID, item.Ref).Scan(&plan.Ref, &plan.Summary, &plan.State, &plan.Version, &raw, &plan.CreatedAt, &plan.AppliedAt)
	if err == nil {
		_ = json.Unmarshal(raw, &plan.Operations)
		item.LatestPlan = &plan
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	return rows.Err()
}

func (repository *Repository) GetAdministration(ctx context.Context, principal value.Principal) (platformrepo.Administration, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.Administration{}, err
	}
	assistant, err := repository.getAssistant(ctx, scope)
	if err != nil {
		return platformrepo.Administration{}, err
	}
	definitions, err := repository.ListIntegrationDefinitions(ctx, principal, "")
	if err != nil {
		return platformrepo.Administration{}, err
	}
	result := platformrepo.Administration{Profile: "WEB_ONLY", CoreReady: assistant.Ready, CoreSummary: "Core работает независимо от внешних интеграций", Assistant: assistant, OptionalAdapters: definitions, ObservedAt: time.Now().UTC()}
	return result, nil
}
func (repository *Repository) ListAuditEvents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.AuditEvent, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, `SELECT e.ref,COALESCE(p.ref,''),s.ref,s.display_name,COALESCE(a.ref,''),e.action,e.resource_kind,e.resource_ref,e.outcome,e.safe_summary,e.correlation_ref,e.occurred_at FROM control_plane.audit_events e LEFT JOIN control_plane.projects p ON p.id=e.project_id JOIN control_plane.subjects s ON s.id=e.actor_id LEFT JOIN control_plane.agents a ON a.id=e.assistant_agent_id WHERE e.organization_id=$1::uuid AND ($2='' OR p.ref=$2) AND ($3='' OR e.action=$3) AND ($4='' OR e.outcome=$4) AND ($5 IN ('OWNER','ADMINISTRATOR','AUDITOR') OR EXISTS(SELECT 1 FROM control_plane.memberships m WHERE m.project_id=e.project_id AND m.subject_id=$6::uuid AND m.active AND 'VIEW_AUDIT'=ANY(m.permissions))) ORDER BY e.occurred_at DESC LIMIT $7`, scope.organizationID, filter.ProjectRef, filter.Action, filter.Outcome, scope.role, scope.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AuditEvent
	for rows.Next() {
		var item entity.AuditEvent
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.ActorRef, &item.ActorName, &item.AssistantRef, &item.Action, &item.ResourceKind, &item.ResourceRef, &item.Outcome, &item.Summary, &item.CorrelationRef, &item.OccurredAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}
