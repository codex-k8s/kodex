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
	err = repository.pool.QueryRow(ctx, queryQueriesGetbootstrapstate1, scope.organizationID).Scan(&bootstrappedAt, &onboardingAt, &state.ProjectCount)
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
	err = repository.pool.QueryRow(ctx, queryQueriesGetoverview1, scope.organizationID).Scan(
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
	rows, err := repository.pool.Query(ctx, queryQueriesListcapabilities1)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListprojects1,
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
	err = repository.pool.QueryRow(ctx, queryQueriesGetproject1,
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
	rows, err := repository.pool.Query(ctx, queryQueriesListmemberships1, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, boundedPage(filter.Page))
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
	rows, err := repository.pool.Query(ctx, queryQueriesListagents1, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, boundedPage(filter.Page))
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
	err = repository.pool.QueryRow(ctx, queryQueriesGetagent1, scope.organizationID, ref, scope.role, scope.actorID).Scan(
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
	rows, err := repository.pool.Query(ctx, queryQueriesAttachinstructions1, scope.organizationID, agent.Ref)
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
	rows, err := repository.pool.Query(ctx, queryQueriesAttachagentgrants1, scope.organizationID, agent.Ref)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListworkflows1, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, boundedPage(filter.Page))
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
	row := repository.pool.QueryRow(ctx, queryQueriesGetworkflow1, scope.organizationID, ref, scope.role, scope.actorID)
	return scanWorkflow(row)
}

func (repository *Repository) ListRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Run, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListruns1, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
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
	return scanRun(repository.pool.QueryRow(ctx, queryQueriesGetrun1, scope.organizationID, ref, scope.role, scope.actorID))
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
	rows, err := repository.pool.Query(ctx, queryQueriesGetrungraph1, scope.organizationID, run.RootRunRef)
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
	edgeRows, err := repository.pool.Query(ctx, queryQueriesGetrungraph2, scope.organizationID, run.RootRunRef)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListrunevents1, scope.organizationID, run.RootRunRef, filter.AfterSequence, limit)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListownergates1, scope.organizationID, filter.ProjectRef, filter.State, scope.role, scope.actorID, boundedPage(filter.Page))
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
	return scanGate(repository.pool.QueryRow(ctx, queryQueriesGetownergate1, scope.organizationID, ref, scope.role, scope.actorID))
}

func (repository *Repository) ListArtifacts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Artifact, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListartifacts1, scope.organizationID, filter.ProjectRef, filter.ResourceRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), boundedPage(filter.Page))
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
	return scanArtifact(repository.pool.QueryRow(ctx, queryQueriesGetartifact1, scope.organizationID, ref, scope.role, scope.actorID))
}

func (repository *Repository) ListSchedules(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListschedules1, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, boundedPage(filter.Page))
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
	rows, err := repository.pool.Query(ctx, queryQueriesListintegrationdefinitions1, category)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListintegrationconnections1, scope.organizationID, filter.Category, boundedPage(filter.Page))
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
	rows, err := repository.pool.Query(ctx, queryQueriesAttachconnection1, scope.organizationID, item.Ref)
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
	item, err := scanConnection(repository.pool.QueryRow(ctx, queryQueriesGetintegrationconnection1, scope.organizationID, ref))
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
	err := repository.pool.QueryRow(ctx, queryQueriesGetassistant1, scope.organizationID).Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &item.ResourceLimits)
	item.Ready = contains([]string{"READY", "BUSY"}, item.RuntimeState) && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) < 45*time.Second
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
	rows, err := repository.pool.Query(ctx, queryQueriesListassistantconversations1, scope.organizationID, filter.ProjectRef, boundedPage(filter.Page))
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
	rows, err := repository.pool.Query(ctx, queryQueriesAttachconversation1, scope.organizationID, item.Ref)
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
	err = repository.pool.QueryRow(ctx, queryQueriesAttachconversation2, scope.organizationID, item.Ref).Scan(&plan.Ref, &plan.Summary, &plan.State, &plan.Version, &raw, &plan.CreatedAt, &plan.AppliedAt)
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
	rows, err := repository.pool.Query(ctx, queryQueriesListauditevents1, scope.organizationID, filter.ProjectRef, filter.Action, filter.Outcome, scope.role, scope.actorID, boundedPage(filter.Page))
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
