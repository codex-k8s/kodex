package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
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
	err = repository.pool.QueryRow(ctx, queryQueriesGetbootstrapstateSelectProjectsOrganizationIdLifecycleSingleton, scope.organizationID).Scan(&bootstrappedAt, &onboardingAt, &state.ProjectCount)
	if err != nil {
		return platformrepo.BootstrapState{}, errs.ErrUnavailable
	}
	state.Bootstrapped = bootstrappedAt != nil
	state.OnboardingCompleted = onboardingAt != nil
	state.OrganizationRef = scope.organizationRef
	state.Assistant = assistant
	state.Actor = entity.User{Ref: scope.actorRef, DisplayName: scope.actorName, Active: true}
	if !state.OnboardingCompleted && (scope.role == "OWNER" || scope.role == "ADMINISTRATOR") {
		state.NextActions = []string{"COMPLETE_ONBOARDING"}
	}
	return state, nil
}

func (repository *Repository) GetPlatformEventCursor(ctx context.Context, principal value.Principal) (string, int64, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return "", 0, err
	}
	var sequence int64
	if err := repository.pool.QueryRow(ctx, queryQueriesGetplatformeventcursorSelectInstallationPlatformSequence).Scan(&sequence); err != nil {
		return "", 0, errs.ErrUnavailable
	}
	return scope.organizationRef, sequence, nil
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
	err = repository.pool.QueryRow(ctx, queryQueriesGetoverviewSelectProjectsOrganizationIdLifecycleState, scope.organizationID).Scan(
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
	rows, err := repository.pool.Query(ctx, queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled)
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

func (repository *Repository) ListRuntimes(ctx context.Context, principal value.Principal) ([]entity.RuntimeSelection, error) {
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListruntimesSelectRuntimeProfilesEnabled)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RuntimeSelection
	for rows.Next() {
		var item entity.RuntimeSelection
		if err := rows.Scan(&item.Ref, &item.Name, &item.Provider, &item.Model, &item.RuntimeRevision); err != nil {
			return nil, errs.ErrUnavailable
		}
		item.Ready = true
		result = append(result, item)
	}
	return result, rows.Err()
}

func (repository *Repository) Search(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.SearchResult, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := repository.pool.Query(ctx, queryQueriesSearchSelectEligibleResources,
		scope.organizationID, scope.role, scope.actorID, filter.Query, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.SearchResult
	for rows.Next() {
		var item entity.SearchResult
		if err := rows.Scan(&item.Kind, &item.Ref, &item.ProjectRef, &item.Title, &item.Subtitle, &item.State, &item.UpdatedAt); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (repository *Repository) ListProjects(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Project, string, []string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", nil, err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListprojectsSelectProjectsOrganizationIdProjectIdSubjectId,
		scope.organizationID, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Project
	for rows.Next() {
		var item entity.Project
		var projectID string
		var permissions []string
		if err := rows.Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt, &permissions, &item.AgentCount, &item.WorkflowCount, &item.ActiveRunCount, &item.PendingGateCount); err != nil {
			return nil, "", nil, errs.ErrUnavailable
		}
		if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
			permissions = allPermissions()
		}
		item.NextActions = projectActions(permissions)
		result = append(result, item)
	}
	actions := collectionCreateActions(scope.role, "CREATE_PROJECT")
	return result, "", actions, rows.Err()
}

func (repository *Repository) GetProject(ctx context.Context, principal value.Principal, ref string) (entity.Project, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Project{}, err
	}
	var item entity.Project
	var projectID string
	err = repository.pool.QueryRow(ctx, queryQueriesGetprojectSelectProjectsOrganizationIdRefProjectId,
		scope.organizationID, ref, scope.role, scope.actorID).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.AgentCount, &item.WorkflowCount, &item.ActiveRunCount, &item.PendingGateCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Project{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Project{}, errs.ErrUnavailable
	}
	permissions := allPermissions()
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		if err := repository.pool.QueryRow(ctx, queryListProjectPermissions, scope.organizationID, projectID, scope.actorID).Scan(&permissions); err != nil {
			return entity.Project{}, errs.ErrUnavailable
		}
	}
	item.NextActions = projectActions(permissions)
	return item, nil
}

func projectActions(permissions []string) []string {
	actions := []string{"OPEN"}
	mappings := []struct{ permission, action string }{
		{"MANAGE", "EDIT"},
		{"MANAGE_AGENTS", "CREATE_AGENT"},
		{"MANAGE_WORKFLOWS", "CREATE_WORKFLOW"},
		{"LAUNCH_RUNS", "CREATE_RUN"},
		{"MANAGE_SCHEDULES", "CREATE_SCHEDULE"},
		{"MANAGE_INTEGRATIONS", "MANAGE_INTEGRATIONS"},
		{"MANAGE_MEMBERS", "MANAGE_MEMBERS"},
		{"MANAGE_ARTIFACTS", "UPLOAD_ARTIFACT"},
	}
	for _, mapping := range mappings {
		if contains(permissions, mapping.permission) {
			actions = append(actions, mapping.action)
		}
	}
	return actions
}

func (repository *Repository) ListPlatformMemberships(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return nil, "", errs.ErrForbidden
	}
	rows, err := repository.pool.Query(ctx, queryPlatformMembershipList, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"page_size":       boundedPage(filter.Page),
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Membership
	for rows.Next() {
		var item entity.Membership
		if err := rows.Scan(
			&item.Ref, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked,
			&item.User.Active, &item.Role, &item.Active, &item.Version,
		); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.Permissions = []string{}
		item.NextActions = platformMembershipActions(scope, item)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, "", nil
}

func (repository *Repository) ListPlatformMembershipCandidates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.User, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return nil, "", errs.ErrForbidden
	}
	rows, err := repository.pool.Query(ctx, queryPlatformMembershipListCandidates, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"query":           strings.TrimSpace(filter.Query),
		"page_size":       boundedPage(filter.Page),
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.User
	for rows.Next() {
		var item entity.User
		if err := rows.Scan(&item.Ref, &item.DisplayName, &item.EmailMasked, &item.Active); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, "", nil
}

func (repository *Repository) ListMemberships(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryProjectMembershipList, pgx.StrictNamedArgs{
		"organization_id":     scope.organizationID,
		"project_ref":         filter.ProjectRef,
		"actor_platform_role": scope.role,
		"actor_id":            scope.actorID,
		"page_size":           boundedPage(filter.Page),
	})
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
		item.NextActions = projectMembershipActions(scope, item)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, "", nil
}

func (repository *Repository) ListMembershipCandidates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.User, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryProjectMembershipListCandidates, pgx.StrictNamedArgs{
		"organization_id":     scope.organizationID,
		"project_ref":         filter.ProjectRef,
		"actor_platform_role": scope.role,
		"actor_id":            scope.actorID,
		"query":               strings.TrimSpace(filter.Query),
		"page_size":           boundedPage(filter.Page),
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.User
	for rows.Next() {
		var item entity.User
		if err := rows.Scan(&item.Ref, &item.DisplayName, &item.EmailMasked, &item.Active); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, "", nil
}

func platformMembershipActions(scope scope, item entity.Membership) []string {
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return []string{}
	}
	if scope.role != "OWNER" && item.Role == "OWNER" {
		return []string{}
	}
	actions := []string{"EDIT"}
	if item.Active && item.User.Ref != scope.actorRef {
		actions = append(actions, "REVOKE")
	}
	return actions
}

func projectMembershipActions(scope scope, item entity.Membership) []string {
	if item.User.Ref == scope.actorRef && scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return []string{}
	}
	actions := []string{"EDIT"}
	if item.Active && item.User.Ref != scope.actorRef {
		actions = append(actions, "REVOKE")
	}
	return actions
}

func (repository *Repository) ListAgents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Agent
	for rows.Next() {
		var item entity.Agent
		var canManage, canLaunch bool
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.NextActions = agentActions(item, canManage, canLaunch)
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
	var canManage, canLaunch bool
	err = repository.pool.QueryRow(ctx, queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey, scope.organizationID, ref, scope.role, scope.actorID).Scan(
		&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	item.System = item.SystemKey != ""
	item.NextActions = agentActions(item, canManage, canLaunch)
	_ = repository.attachInstructions(ctx, scope, &item)
	_ = repository.attachAgentGrants(ctx, scope, &item)
	return item, nil
}

func agentActions(agent entity.Agent, canManage, canLaunch bool) []string {
	if agent.System {
		actions := []string{"OPEN"}
		if canManage {
			actions = append(actions, "RECOVER")
		}
		return actions
	}
	actions := []string{"OPEN"}
	if agent.State == "ARCHIVED" {
		return actions
	}
	if canManage {
		actions = append(actions, "EDIT", "MANAGE_CAPABILITIES")
	}
	if canLaunch && agent.Enabled && agent.State == "READY" {
		actions = append(actions, "LAUNCH")
	}
	if canManage && agent.Enabled && agent.State == "READY" {
		actions = append(actions, "DISABLE")
	}
	if canManage && !agent.Enabled {
		actions = append(actions, "ENABLE")
	}
	if canManage {
		actions = append(actions, "ARCHIVE")
	}
	return actions
}

func (repository *Repository) attachInstructions(ctx context.Context, scope scope, agent *entity.Agent) error {
	rows, err := repository.pool.Query(ctx, queryQueriesAttachinstructionsSelectInstructionVersionsOrganizationIdAgentIdRef, scope.organizationID, agent.Ref)
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
	rows, err := repository.pool.Query(ctx, queryQueriesAttachagentgrantsSelectIntegrationGrantsOrganizationIdTargetKindTargetRef, scope.organizationID, agent.Ref)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Workflow
	for rows.Next() {
		item, scanErr := scanWorkflow(rows, true)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

type rowScanner interface{ Scan(...any) error }

type queryRunner interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanWorkflow(row rowScanner, actorScoped bool) (entity.Workflow, error) {
	var item entity.Workflow
	var draft, published []byte
	var publishedVersion int32
	var canManage, canLaunch bool
	destinations := []any{&item.Ref, &item.ProjectRef, &item.Name, &item.Purpose, &item.CoordinatorAgentRef, &item.State, &item.Version, &draft, &published, &publishedVersion, &item.CreatedAt, &item.UpdatedAt}
	if actorScoped {
		destinations = append(destinations, &canManage, &canLaunch)
	} else {
		canManage, canLaunch = true, true
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Workflow{}, errs.ErrNotFound
		}
		return entity.Workflow{}, errs.ErrUnavailable
	}
	item.Draft = &entity.WorkflowVersion{}
	if err := json.Unmarshal(draft, item.Draft); err != nil || !validWorkflowVersion(*item.Draft) {
		return entity.Workflow{}, errs.ErrUnavailable
	}
	if len(published) > 0 {
		item.Published = &entity.WorkflowVersion{}
		if err := json.Unmarshal(published, item.Published); err != nil || !validWorkflowVersion(*item.Published) {
			return entity.Workflow{}, errs.ErrUnavailable
		}
		item.Published.VersionNumber = publishedVersion
	}
	item.NextActions = workflowActions(item, canManage, canLaunch)
	return item, nil
}

func workflowActions(item entity.Workflow, canManage, canLaunch bool) []string {
	actions := []string{"OPEN"}
	if canManage && item.State != "ARCHIVED" {
		actions = append(actions, "EDIT")
	}
	if canLaunch && item.State == "PUBLISHED" {
		actions = append(actions, "LAUNCH")
	}
	return actions
}

func (repository *Repository) GetWorkflow(ctx context.Context, principal value.Principal, ref string) (entity.Workflow, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Workflow{}, err
	}
	row := repository.pool.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, scope.organizationID, ref, scope.role, scope.actorID)
	return scanWorkflow(row, true)
}

func (repository *Repository) ListRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Run, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListrunsSelectRunsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Run
	for rows.Next() {
		item, scanErr := scanRun(rows, true)
		if scanErr != nil {
			return nil, "", scanErr
		}
		if len(filter.States) == 0 || contains(filter.States, item.State) {
			result = append(result, item)
		}
	}
	return result, "", rows.Err()
}

func scanRun(row rowScanner, actorScoped bool) (entity.Run, error) {
	var item entity.Run
	var input []byte
	var canCancel, canLaunch bool
	destinations := []any{&item.Ref, &item.ProjectRef, &item.SessionRef, &item.RootRunRef, &item.ParentRunRef, &item.RetryOfRunRef, &item.Title, &item.Task, &item.State, &item.Source, &item.ResultSummary, &item.SafeErrorCode, &item.SafeErrorMessage, &item.InitiatorName, &item.Target.Type, &item.Target.Ref, &item.Target.Name, &item.Attempt, &item.GraphRevision, &item.EventSequence, &item.Version, &input, &item.InputArtifactRefs, &item.ArtifactRefs, &item.GateRefs, &item.CreatedAt, &item.StartedAt, &item.FinishedAt}
	if actorScoped {
		destinations = append(destinations, &canCancel, &canLaunch)
	} else {
		canCancel, canLaunch = true, true
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Run{}, errs.ErrNotFound
		}
		return entity.Run{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(input, &item.Input)
	item.NextActions = runActions(item.State, canCancel, canLaunch)
	return item, nil
}
func runActions(state string, canCancel, canLaunch bool) []string {
	switch state {
	case "QUEUED", "RUNNING", "WAITING_HUMAN":
		if canCancel {
			return []string{"OPEN", "CANCEL"}
		}
	case "FAILED", "CANCELLED":
		if canCancel {
			return []string{"OPEN", "RETRY"}
		}
	case "SUCCEEDED":
		if canLaunch {
			return []string{"OPEN", "ADD_TURN"}
		}
	}
	return []string{"OPEN"}
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type actorActionPermissions struct {
	canManageAgents    bool
	canManageWorkflows bool
	canCancelRuns      bool
	canLaunchRuns      bool
	canResolveGates    bool
	canManageArtifacts bool
	canManageSchedules bool
}

func (repository *Repository) projectActionPermissions(
	ctx context.Context,
	runner queryRunner,
	scope scope,
	projectRef string,
) (actorActionPermissions, error) {
	if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
		return actorActionPermissions{
			canManageAgents:    true,
			canManageWorkflows: true,
			canCancelRuns:      true,
			canLaunchRuns:      true,
			canResolveGates:    true,
			canManageArtifacts: true,
			canManageSchedules: true,
		}, nil
	}
	if projectRef == "" {
		return actorActionPermissions{}, nil
	}
	var permissions []string
	if err := runner.QueryRow(
		ctx,
		queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef,
		scope.organizationID,
		projectRef,
		scope.actorID,
	).Scan(&permissions); errors.Is(err, pgx.ErrNoRows) {
		return actorActionPermissions{}, errs.ErrNotFound
	} else if err != nil {
		return actorActionPermissions{}, errs.ErrUnavailable
	}
	return actorActionPermissions{
		canManageAgents:    contains(permissions, "MANAGE_AGENTS"),
		canManageWorkflows: contains(permissions, "MANAGE_WORKFLOWS"),
		canCancelRuns:      contains(permissions, "CANCEL_RUNS"),
		canLaunchRuns:      contains(permissions, "LAUNCH_RUNS"),
		canResolveGates:    contains(permissions, "RESOLVE_GATES"),
		canManageArtifacts: contains(permissions, "MANAGE_ARTIFACTS"),
		canManageSchedules: contains(permissions, "MANAGE_SCHEDULES"),
	}, nil
}

func (repository *Repository) applyResultActionPermissions(
	ctx context.Context,
	runner queryRunner,
	scope scope,
	result *command.Result,
	projectRef string,
) error {
	if result == nil {
		return nil
	}
	if projectRef == "" {
		switch {
		case result.Agent != nil:
			projectRef = result.Agent.ProjectRef
		case result.Workflow != nil:
			projectRef = result.Workflow.ProjectRef
		case result.Run != nil:
			projectRef = result.Run.ProjectRef
		case result.Gate != nil:
			projectRef = result.Gate.ProjectRef
		case result.Artifact != nil:
			projectRef = result.Artifact.ProjectRef
		case result.Schedule != nil:
			projectRef = result.Schedule.ProjectRef
		}
	}
	if projectRef == "" {
		return nil
	}
	permissions, err := repository.projectActionPermissions(ctx, runner, scope, projectRef)
	if err != nil {
		return err
	}
	if result.Agent != nil {
		result.Agent.NextActions = agentActions(*result.Agent, permissions.canManageAgents, permissions.canLaunchRuns)
	}
	if result.Workflow != nil {
		result.Workflow.NextActions = workflowActions(*result.Workflow, permissions.canManageWorkflows, permissions.canLaunchRuns)
	}
	if result.Run != nil {
		result.Run.NextActions = runActions(result.Run.State, permissions.canCancelRuns, permissions.canLaunchRuns)
	}
	if result.Graph != nil {
		for index := range result.Graph.Nodes {
			result.Graph.Nodes[index].NextActions = filterNodeActions(result.Graph.Nodes[index].NextActions, permissions)
		}
	}
	if result.Gate != nil {
		result.Gate.NextActions = gateActions(result.Gate.State, permissions.canResolveGates)
	}
	if result.Artifact != nil {
		result.Artifact.NextActions = artifactActions(result.Artifact.ScanState, permissions.canManageArtifacts)
	}
	if result.Schedule != nil {
		result.Schedule.NextActions = scheduleActions(*result.Schedule, permissions.canManageSchedules)
	}
	if result.Event != nil {
		applyEventActionPermissions(result.Event, permissions)
	}
	return nil
}

func filterNodeActions(actions []string, permissions actorActionPermissions) []string {
	result := make([]string, 0, len(actions))
	for _, action := range actions {
		switch action {
		case "OPEN":
			result = append(result, action)
		case "CANCEL", "RETRY":
			if permissions.canCancelRuns {
				result = append(result, action)
			}
		case "RESOLVE_GATE":
			if permissions.canResolveGates {
				result = append(result, action)
			}
		}
	}
	return result
}

func applyEventActionPermissions(event *entity.RunEvent, permissions actorActionPermissions) {
	if event.Delta.Run != nil {
		event.Delta.Run.NextActions = runActions(
			event.Delta.Run.State,
			permissions.canCancelRuns,
			permissions.canLaunchRuns,
		)
	}
	if event.Delta.Node != nil {
		event.Delta.Node.NextActions = filterNodeActions(event.Delta.Node.NextActions, permissions)
	}
	if event.Delta.Gate != nil {
		event.Delta.Gate.NextActions = gateActions(event.Delta.Gate.State, permissions.canResolveGates)
	}
	if event.Delta.Artifact != nil {
		event.Delta.Artifact.NextActions = artifactActions(event.Delta.Artifact.ScanState, permissions.canManageArtifacts)
	}
}

func (repository *Repository) GetRun(ctx context.Context, principal value.Principal, ref string) (entity.Run, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Run{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := repository.readRunWithIncidents(ctx, tx, scope, ref)
	if err != nil {
		return entity.Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) readRunWithIncidents(ctx context.Context, runner queryRunner, scope scope, ref string) (entity.Run, error) {
	item, err := scanRun(runner.QueryRow(ctx, queryQueriesGetrunSelectRunsOrganizationIdRefProjectId, scope.organizationID, ref, scope.role, scope.actorID), true)
	if err != nil {
		return entity.Run{}, err
	}
	rows, err := runner.Query(ctx, queryInteractionListRunIncidents, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"run_ref":         ref,
		"platform_role":   scope.role,
		"actor_id":        scope.actorID,
	})
	if err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var incident entity.Incident
		var deliveryState string
		var attempt int
		if err := rows.Scan(&incident.Ref, &incident.ProjectRef, &incident.RunRef, &deliveryState, &attempt, &incident.CreatedAt); err != nil {
			return entity.Run{}, errs.ErrUnavailable
		}
		incident = projectInteractionIncident(incident, deliveryState, attempt)
		item.Incidents = append(item.Incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) GetRunGraph(ctx context.Context, principal value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := repository.readRunWithIncidents(ctx, tx, scope, ref)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	permissions, err := repository.projectActionPermissions(ctx, tx, scope, run.ProjectRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	graph := entity.RunGraph{RunRef: run.RootRunRef, Revision: run.GraphRevision, Sequence: run.EventSequence}
	rows, err := tx.Query(ctx, queryQueriesGetrungraphSelectArtifactsNodeIdRef, scope.organizationID, run.RootRunRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	for rows.Next() {
		n, scanErr := scanRunNode(rows)
		if scanErr != nil {
			rows.Close()
			return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
		}
		n.NextActions = filterNodeActions(n.NextActions, permissions)
		graph.Nodes = append(graph.Nodes, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	rows.Close()
	edgeRows, err := tx.Query(ctx, queryQueriesGetrungraphSelectRunEdgesOrganizationIdRef, scope.organizationID, run.RootRunRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	for edgeRows.Next() {
		var e entity.RunEdge
		if err := edgeRows.Scan(&e.Ref, &e.RunRef, &e.SourceNodeRef, &e.TargetNodeRef, &e.Type, &e.Label); err != nil {
			edgeRows.Close()
			return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
		}
		graph.Edges = append(graph.Edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		edgeRows.Close()
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	edgeRows.Close()
	if err := tx.Commit(ctx); err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	return run, graph, nil
}

func scanRunNode(row rowScanner) (entity.RunNode, error) {
	var node entity.RunNode
	if err := row.Scan(&node.Ref, &node.RunRef, &node.ParentNodeRef, &node.Type, &node.State, &node.DisplayName, &node.Role, &node.AgentRef, &node.TurnRef, &node.Attempt, &node.InputSummary, &node.ProgressSummary, &node.IntegrationNames, &node.CallbackSummary, &node.SafeErrorCode, &node.SafeErrorMessage, &node.NextActions, &node.CreatedAt, &node.StartedAt, &node.FinishedAt, &node.ArtifactRefs, &node.ChildRunRefs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.RunNode{}, errs.ErrNotFound
		}
		return entity.RunNode{}, errs.ErrUnavailable
	}
	return node, nil
}

func (repository *Repository) ListRunEvents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RunEvent, int64, bool, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, false, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := repository.readRunWithIncidents(ctx, tx, scope, filter.ResourceRef)
	if err != nil {
		return nil, 0, false, err
	}
	permissions, err := repository.projectActionPermissions(ctx, tx, scope, run.ProjectRef)
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
	rows, err := tx.Query(ctx, queryQueriesListruneventsSelectRunEventsOrganizationIdRef, scope.organizationID, run.RootRunRef, filter.AfterSequence, limit)
	if err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RunEvent
	for rows.Next() {
		var e entity.RunEvent
		var delta []byte
		if err := rows.Scan(&e.Ref, &e.RunRef, &e.Sequence, &e.Type, &e.NodeRef, &e.EdgeRef, &e.GateRef, &e.ArtifactRef, &e.Summary, &e.Progress, &e.RunState, &e.NodeState, &delta, &e.OccurredAt); err != nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		if err := json.Unmarshal(delta, &e.Delta); err != nil || e.Delta.Run == nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		if e.Delta.Incident != nil {
			e.IncidentRef = e.Delta.Incident.Ref
		}
		applyEventActionPermissions(&e, permissions)
		e.GraphRevision = e.Delta.Run.GraphRevision
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	rows.Close()
	complete := len(result) < int(limit) || len(result) > 0 && result[len(result)-1].Sequence == run.EventSequence
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	return result, run.EventSequence, complete, nil
}

func (repository *Repository) ListOwnerGates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.OwnerGate, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListownergatesSelectOwnerGatesOrganizationIdRefState, scope.organizationID, filter.ProjectRef, filter.State, scope.role, scope.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.OwnerGate
	for rows.Next() {
		item, scanErr := scanGate(rows, true)
		if scanErr != nil {
			return nil, "", scanErr
		}
		result = append(result, item)
	}
	return result, "", rows.Err()
}

func scanGate(row rowScanner, actorScoped bool) (entity.OwnerGate, error) {
	var item entity.OwnerGate
	canResolve := true
	destinations := []any{&item.Ref, &item.ProjectRef, &item.RunRef, &item.NodeRef, &item.Title, &item.Prompt, &item.ContextSummary, &item.RequestedByRef, &item.RequestedByName, &item.AllowedDecisions, &item.State, &item.Decision, &item.DecisionComment, &item.ResolvedByName, &item.Version, &item.CreatedAt, &item.ResolvedAt, &item.ArtifactRefs}
	if actorScoped {
		canResolve = false
		destinations = append(destinations, &canResolve)
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.OwnerGate{}, errs.ErrNotFound
		}
		return entity.OwnerGate{}, errs.ErrUnavailable
	}
	item.NextActions = gateActions(item.State, canResolve)
	return item, nil
}

func gateActions(state string, canResolve bool) []string {
	if state == "OPEN" && canResolve {
		return []string{"RESOLVE_GATE"}
	}
	return []string{}
}
func (repository *Repository) GetOwnerGate(ctx context.Context, principal value.Principal, ref string) (entity.OwnerGate, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	return scanGate(repository.pool.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId, scope.organizationID, ref, scope.role, scope.actorID), true)
}

func (repository *Repository) ListArtifacts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Artifact, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListartifactsSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, filter.ProjectRef, filter.ResourceRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
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

func scanArtifact(row rowScanner) (entity.Artifact, error) {
	var item entity.Artifact
	var canManage bool
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.RunRef, &item.SessionRef, &item.NodeRef, &item.FileName, &item.MediaType, &item.Digest, &item.ScanState, &item.PreviewState, &item.Source, &item.SizeBytes, &item.Revision, &item.Version, &item.CreatedAt, &item.Bindings, &canManage); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Artifact{}, errs.ErrNotFound
		}
		return entity.Artifact{}, errs.ErrUnavailable
	}
	item.NextActions = artifactActions(item.ScanState, canManage)
	return item, nil
}

func artifactActions(scanState string, canManage bool) []string {
	if scanState != "CLEAN" {
		return []string{}
	}
	actions := []string{"DOWNLOAD"}
	if canManage {
		actions = append(actions, "BIND")
	}
	return actions
}
func (repository *Repository) GetArtifact(ctx context.Context, principal value.Principal, ref string) (entity.Artifact, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Artifact{}, err
	}
	return scanArtifact(repository.pool.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, ref, scope.role, scope.actorID))
}

func (repository *Repository) ListSchedules(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Schedule
	for rows.Next() {
		var item entity.Schedule
		var input []byte
		var canManage bool
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.Name, &item.Target.Type, &item.Target.Ref, &item.Target.Name, &item.Preset, &item.CronExpression, &item.Timezone, &input, &item.SessionPolicy, &item.NotificationPolicy, &item.Enabled, &item.Version, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt, &canManage); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		_ = json.Unmarshal(input, &item.Input)
		item.NextActions = scheduleActions(item, canManage)
		result = append(result, item)
	}
	return result, "", rows.Err()
}

func scheduleActions(item entity.Schedule, canManage bool) []string {
	actions := []string{"OPEN"}
	if !canManage {
		return actions
	}
	actions = append(actions, "EDIT")
	if item.Enabled {
		return append(actions, "DISABLE")
	}
	return append(actions, "ENABLE")
}

func (repository *Repository) ListIntegrationDefinitions(ctx context.Context, principal value.Principal, category string) ([]entity.IntegrationDefinition, []string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, nil, err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListintegrationdefinitionsSelectIntegrationDefinitionsCategory, category)
	if err != nil {
		return nil, nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationDefinition
	for rows.Next() {
		var item entity.IntegrationDefinition
		var capabilities, schema []byte
		if err := rows.Scan(&item.Key, &item.Name, &item.Description, &item.Category, &item.Optional, &item.Enabled, &capabilities, &schema); err != nil {
			return nil, nil, errs.ErrUnavailable
		}
		if json.Unmarshal(capabilities, &item.Capabilities) != nil || json.Unmarshal(schema, &item.ConfigurationFields) != nil {
			return nil, nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	actions := collectionCreateActions(scope.role, "CREATE_CONNECTION")
	return result, actions, rows.Err()
}

func collectionCreateActions(role, action string) []string {
	if role == "OWNER" || role == "ADMINISTRATOR" {
		return []string{action}
	}
	return []string{}
}

func assistantActions(role string, ready bool) []string {
	actions := []string{"OPEN"}
	if ready {
		actions = append(actions, "CREATE_CONVERSATION", "ADD_TURN")
	}
	if role == "OWNER" || role == "ADMINISTRATOR" {
		actions = append(actions, "EDIT")
		if !ready {
			actions = append(actions, "RECOVER")
		}
	}
	return actions
}

func (repository *Repository) ListIntegrationConnections(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.IntegrationConnection, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListintegrationconnectionsSelectIntegrationConnectionsOrganizationIdDefinitionKey, scope.organizationID, filter.Category, boundedPage(filter.Page))
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	manageConnection, manageGrants, err := connectionAuthority(ctx, repository.pool, scope)
	if err != nil {
		return nil, "", err
	}
	var result []entity.IntegrationConnection
	for rows.Next() {
		item, scanErr := scanConnection(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		if err := attachConnection(ctx, repository.pool, scope, &item); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.NextActions = connectionActions(item, manageConnection, manageGrants)
		result = append(result, item)
	}
	return result, "", rows.Err()
}

func scanConnection(row rowScanner) (entity.IntegrationConnection, error) {
	var item entity.IntegrationConnection
	var configuration, capabilities []byte
	if err := row.Scan(&item.Ref, &item.DefinitionKey, &item.DefinitionName, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &configuration, &capabilities, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.IntegrationConnection{}, errs.ErrNotFound
		}
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	if json.Unmarshal(configuration, &item.PublicConfiguration) != nil || json.Unmarshal(capabilities, &item.Capabilities) != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	item.NextActions = []string{"OPEN"}
	return item, nil
}

type connectionQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func connectionActions(item entity.IntegrationConnection, manageConnection, manageGrants bool) []string {
	actions := []string{"OPEN"}
	if !manageConnection && !manageGrants {
		return actions
	}
	if !item.Enabled {
		if manageConnection {
			return append(actions, "ENABLE")
		}
		return actions
	}
	if manageConnection && item.State != "TESTING" {
		actions = append(actions, "TEST")
	}
	if manageConnection {
		actions = append(actions, "DISABLE")
	}
	if manageGrants && item.State == "CONNECTED" {
		actions = append(actions, "MANAGE_GRANTS")
	}
	return actions
}

func connectionAuthority(ctx context.Context, querier connectionQuerier, scope scope) (bool, bool, error) {
	if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
		return true, true, nil
	}
	var manageGrants bool
	if err := querier.QueryRow(ctx, queryQueriesConnectionauthoritySelectMembershipsOrganizationIdSubjectId, scope.organizationID, scope.actorID).Scan(&manageGrants); err != nil {
		return false, false, errs.ErrUnavailable
	}
	return false, manageGrants, nil
}

func attachConnection(ctx context.Context, querier connectionQuerier, scope scope, item *entity.IntegrationConnection) error {
	rows, err := querier.Query(ctx, queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef, scope.organizationID, item.Ref, scope.role, scope.actorID)
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

func readConnection(ctx context.Context, querier connectionQuerier, scope scope, ref string) (entity.IntegrationConnection, error) {
	item, err := scanConnection(querier.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, scope.organizationID, ref))
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	if err := attachConnection(ctx, querier, scope, &item); err != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	manageConnection, manageGrants, err := connectionAuthority(ctx, querier, scope)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	item.NextActions = connectionActions(item, manageConnection, manageGrants)
	return item, nil
}

func (repository *Repository) GetIntegrationConnection(ctx context.Context, principal value.Principal, ref string) (entity.IntegrationConnection, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	return readConnection(ctx, repository.pool, scope, ref)
}

func (repository *Repository) getAssistant(ctx context.Context, scope scope) (entity.SystemAssistant, error) {
	var item entity.SystemAssistant
	var limits []byte
	err := repository.pool.QueryRow(ctx, queryQueriesGetassistantSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef, scope.organizationID, filter.ProjectRef, boundedPage(filter.Page))
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
	rows, err := repository.pool.Query(ctx, queryQueriesAttachconversationSelectSessionTurnsOrganizationIdSessionIdRef, scope.organizationID, item.Ref)
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
	err = repository.pool.QueryRow(ctx, queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef, scope.organizationID, item.Ref).Scan(&plan.Ref, &plan.Summary, &plan.State, &plan.Version, &raw, &plan.CreatedAt, &plan.AppliedAt)
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
	definitions, _, err := repository.ListIntegrationDefinitions(ctx, principal, "")
	if err != nil {
		return platformrepo.Administration{}, err
	}
	profile := "WEB_ONLY"
	var activeAdapters int
	if err := repository.pool.QueryRow(ctx, queryInteractionCountActiveAdapters, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
	}).Scan(&activeAdapters); err != nil {
		return platformrepo.Administration{}, errs.ErrUnavailable
	}
	if activeAdapters > 0 {
		profile = "WEB_WITH_OPTIONAL_ADAPTERS"
	}
	result := platformrepo.Administration{Profile: profile, CoreReady: assistant.Ready, CoreSummary: "i18n:WEB_ONLY_CORE_SUMMARY", Assistant: assistant, OptionalAdapters: definitions, ObservedAt: time.Now().UTC()}
	incidentRows, err := repository.pool.Query(ctx, queryInteractionListFailedIncidents, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
	})
	if err != nil {
		return platformrepo.Administration{}, errs.ErrUnavailable
	}
	defer incidentRows.Close()
	for incidentRows.Next() {
		var incident entity.Incident
		var deliveryState string
		var attempt int
		if err := incidentRows.Scan(&incident.Ref, &incident.ProjectRef, &incident.RunRef, &deliveryState, &attempt, &incident.CreatedAt); err != nil {
			return platformrepo.Administration{}, errs.ErrUnavailable
		}
		incident = projectInteractionIncident(incident, deliveryState, attempt)
		result.Incidents = append(result.Incidents, incident)
	}
	if err := incidentRows.Err(); err != nil {
		return platformrepo.Administration{}, errs.ErrUnavailable
	}
	return result, nil
}
func (repository *Repository) ListAuditEvents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.AuditEvent, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListauditeventsSelectAuditEventsOrganizationIdRefAction, scope.organizationID, filter.ProjectRef, filter.Action, filter.Outcome, scope.role, scope.actorID, boundedPage(filter.Page))
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
