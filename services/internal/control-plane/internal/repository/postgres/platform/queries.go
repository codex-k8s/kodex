package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
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
	state.PlatformRole = scope.role
	state.SpeechTranscription.Reason = "STT_NOT_CONFIGURED"
	configuration, configurationErr := repository.GetSystemSTTConfiguration(ctx, principal)
	if configurationErr == nil {
		state.SpeechTranscription.Eligible = configuration.Ready
		state.SpeechTranscription.Reason = "STT_RUNTIME_UNVERIFIED"
		if len(configuration.ReadinessBlockers) != 0 {
			state.SpeechTranscription.Reason = configuration.ReadinessBlockers[0]
		}
	} else if !errors.Is(configurationErr, errs.ErrNotFound) {
		state.SpeechTranscription.Reason = "STT_CONFIGURATION_UNAVAILABLE"
	}
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
	runs, _, _, err := repository.ListRuns(ctx, principal, filter)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	gates, _, _, err := repository.ListOwnerGates(ctx, principal, query.Filter{ProjectRef: projectRef, State: "OPEN", Page: query.Page{Size: 20}})
	if err != nil {
		return platformrepo.Overview{}, err
	}
	artifacts, _, _, err := repository.ListArtifacts(ctx, principal, filter)
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

func (repository *Repository) Search(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.SearchResult, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	if limit > 50 {
		limit = 50
	}
	cursor, err := decodeSearchCursor(filter.Page.Token, filter.Query, filter.ProjectRef)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryQueriesSearchSelectEligibleResources, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "query": filter.Query,
		"project_ref": strings.TrimSpace(filter.ProjectRef),
	})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	type rankedResult struct {
		entity.SearchResult
		relevance int
		orderTime time.Time
	}
	candidates := make([]rankedResult, 0)
	for rows.Next() {
		var item rankedResult
		if err := rows.Scan(&item.Kind, &item.Ref, &item.ProjectRef, &item.Title, &item.Subtitle,
			&item.State, &item.UpdatedAt, &item.relevance, &item.orderTime); err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	rows.Close()
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, 0, "", err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, 0, "", err
	}
	evaluatedAt := time.Now().UTC()
	ranked := make([]rankedResult, 0, len(candidates))
	for _, item := range candidates {
		visible, visibilityErr := repository.resourceVisible(ctx, tx, current, subject.AccessSubject, bindings,
			item.Kind, item.Ref, item.ProjectRef, evaluatedAt)
		if visibilityErr != nil {
			return nil, 0, "", visibilityErr
		}
		if visible {
			ranked = append(ranked, item)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].relevance != ranked[j].relevance {
			return ranked[i].relevance < ranked[j].relevance
		}
		if !ranked[i].orderTime.Equal(ranked[j].orderTime) {
			return ranked[i].orderTime.After(ranked[j].orderTime)
		}
		return ranked[i].Kind+"\x00"+ranked[i].Ref < ranked[j].Kind+"\x00"+ranked[j].Ref
	})
	total := int64(len(ranked))
	if cursor.Time != nil {
		remaining := ranked[:0]
		for _, item := range ranked {
			if searchResultAfterCursor(item.relevance, item.orderTime, item.Kind, item.Ref, cursor) {
				remaining = append(remaining, item)
			}
		}
		ranked = remaining
	}
	next := ""
	if len(ranked) > int(limit) {
		ranked = ranked[:limit]
		last := ranked[len(ranked)-1]
		next = encodeSearchCursor(searchCursor{Relevance: last.relevance, Time: &last.orderTime, Kind: last.Kind, Ref: last.Ref}, filter.Query, filter.ProjectRef)
		if next == filter.Page.Token {
			return nil, 0, "", errs.ErrConflict
		}
	}
	result := make([]entity.SearchResult, 0, len(ranked))
	for _, item := range ranked {
		result = append(result, item.SearchResult)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrConflict
	}
	return result, total, next, nil
}

func searchResultAfterCursor(relevance int, createdAt time.Time, kind, ref string, cursor searchCursor) bool {
	return relevance > cursor.Relevance ||
		relevance == cursor.Relevance && createdAt.Before(*cursor.Time) ||
		relevance == cursor.Relevance && createdAt.Equal(*cursor.Time) && kind+"\x00"+ref > cursor.Kind+"\x00"+cursor.Ref
}

func (repository *Repository) resourceVisible(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	subject entity.AccessSubject,
	bindings []entity.AccessBinding,
	resourceKind, resourceRef, projectRef string,
	evaluatedAt time.Time,
) (bool, error) {
	permission := visibilityPermission(resourceKind)
	if permission == "" {
		return false, nil
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: resourceKind, ResourceRef: resourceRef,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	resourceBindings := bindings
	if resourceKind != "PROJECT" {
		resourceBindings = make([]entity.AccessBinding, 0, len(bindings))
		for _, binding := range bindings {
			if binding.PresentationKind != "PROJECT_MEMBERSHIP" {
				resourceBindings = append(resourceBindings, binding)
			}
		}
	}
	return accessservice.Evaluate(subject, permission, target.scope, target.ownerSubjectRef, resourceBindings, evaluatedAt).Allowed, nil
}

type searchCursor struct {
	Version   int        `json:"v"`
	Filter    string     `json:"f"`
	Relevance int        `json:"r"`
	Time      *time.Time `json:"t"`
	Kind      string     `json:"k"`
	Ref       string     `json:"i"`
}

func searchFilterDigest(queryValue, projectRef string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(queryValue) + "\x00" + strings.TrimSpace(projectRef)))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}

func encodeSearchCursor(cursor searchCursor, queryValue, projectRef string) string {
	cursor.Version, cursor.Filter = 2, searchFilterDigest(queryValue, projectRef)
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSearchCursor(token, queryValue, projectRef string) (searchCursor, error) {
	if strings.TrimSpace(token) == "" {
		return searchCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 512 {
		return searchCursor{}, errs.ErrInvalid
	}
	var cursor searchCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 2 || cursor.Filter != searchFilterDigest(queryValue, projectRef) ||
		cursor.Time == nil || cursor.Relevance < 0 || cursor.Relevance > 2 || cursor.Kind == "" || cursor.Ref == "" {
		return searchCursor{}, errs.ErrInvalid
	}
	return cursor, nil
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
	return authorizedCatalog(ctx, repository, scope, "MEMBERSHIP", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Membership, error) {
			rows, err := tx.Query(ctx, queryProjectMembershipList, pgx.StrictNamedArgs{
				"actor_id": scope.actorID, "authority_project": scope.authorityProjectID,
				"organization_id": scope.organizationID, "project_ref": filter.ProjectRef,
				"query": strings.TrimSpace(filter.Query), "cursor_ref": cursor, "page_size": limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Membership
			for rows.Next() {
				var item entity.Membership
				if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked, &item.User.Active, &item.Role, &item.Permissions, &item.Active, &item.Version); err != nil {
					return nil, errs.ErrUnavailable
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.Membership) entity.AccessScope {
			return entity.AccessScope{ResourceKind: "MEMBERSHIP", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Membership, _ func(string) bool) error {
			item.NextActions = projectMembershipActions(scope, *item)
			return nil
		})
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
	result, next, err := authorizedCatalog(ctx, repository, scope, "AGENT", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Agent, error) {
			rows, err := tx.Query(ctx, queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, limit, cursor, scope.authorityProjectID)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var result []entity.Agent
			for rows.Next() {
				var item entity.Agent
				var canManage, canLaunch bool
				if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch); err != nil {
					return nil, errs.ErrUnavailable
				}
				setAgentAvatarReadback(&item)
				result = append(result, item)
			}
			if rows.Err() != nil {
				return nil, errs.ErrUnavailable
			}
			return result, nil
		}, func(item entity.Agent) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(tx pgx.Tx, item *entity.Agent, allowed func(string) bool) error {
			if err := repository.attachInstructionsFrom(ctx, tx, scope, item); err != nil {
				return errs.ErrUnavailable
			}
			if err := repository.attachAgentGrantsFrom(ctx, tx, scope, item); err != nil {
				return errs.ErrUnavailable
			}
			item.NextActions = agentActions(*item, allowed("agent.manage"), allowed("agent.launch"))
			return nil
		})
	if err != nil {
		return nil, "", err
	}
	return result, next, nil
}

func (repository *Repository) GetAgent(ctx context.Context, principal value.Principal, ref string) (entity.Agent, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Agent{}, err
	}
	var item entity.Agent
	var canManage, canLaunch bool
	err = repository.pool.QueryRow(ctx, queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey, scope.organizationID, ref, scope.role, scope.actorID).Scan(
		&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	setAgentAvatarReadback(&item)
	item.System = item.SystemKey != ""
	item.NextActions = agentActions(item, canManage, canLaunch)
	if err := repository.attachInstructions(ctx, scope, &item); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	item.NextActions = agentActions(item, canManage, canLaunch)
	if err := repository.attachAgentGrants(ctx, scope, &item); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
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
		switch {
		case agent.DraftInstructions != nil && agent.DraftInstructions.State == "VALID":
			actions = append(actions, "PUBLISH")
		case agent.DraftInstructions != nil:
			actions = append(actions, "VALIDATE")
		}
		if len(agent.PublishedInstructionVersions) > 1 {
			actions = append(actions, "ROLLBACK")
		}
	}
	return actions
}

func canManageAgent(actions []string) bool {
	return slices.Contains(actions, "EDIT")
}

func canLaunchAgent(actions []string) bool {
	return slices.Contains(actions, "LAUNCH")
}

func (repository *Repository) attachInstructions(ctx context.Context, scope scope, agent *entity.Agent) error {
	return repository.attachInstructionsFrom(ctx, repository.pool, scope, agent)
}

func (repository *Repository) attachInstructionsFrom(ctx context.Context, runner queryRunner, scope scope, agent *entity.Agent) error {
	rows, err := runner.Query(ctx, queryQueriesAttachinstructionsSelectInstructionVersionsOrganizationIdAgentIdRef, scope.organizationID, agent.Ref)
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
		}
		if item.State == "PUBLISHED" {
			agent.PublishedInstructionVersions = append(agent.PublishedInstructionVersions, item)
		} else if agent.DraftInstructions == nil {
			copy := item
			agent.DraftInstructions = &copy
		}
	}
	return rows.Err()
}

func (repository *Repository) attachAgentGrants(ctx context.Context, scope scope, agent *entity.Agent) error {
	return repository.attachAgentGrantsFrom(ctx, repository.pool, scope, agent)
}

func (repository *Repository) attachAgentGrantsFrom(ctx context.Context, runner queryRunner, scope scope, agent *entity.Agent) error {
	rows, err := runner.Query(ctx, queryQueriesAttachagentgrantsSelectIntegrationGrantsOrganizationIdTargetKindTargetRef, scope.organizationID, agent.Ref)
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
	return authorizedCatalog(ctx, repository, scope, "WORKFLOW", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Workflow, error) {
			rows, err := tx.Query(ctx, queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, limit, cursor, scope.authorityProjectID)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Workflow
			for rows.Next() {
				item, err := scanWorkflow(rows, true)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			if rows.Err() != nil {
				return nil, errs.ErrUnavailable
			}
			return items, nil
		}, func(item entity.Workflow) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "WORKFLOW", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Workflow, allowed func(string) bool) error {
			item.NextActions = workflowActions(*item, allowed("workflow.manage"), allowed("workflow.launch"))
			return nil
		})
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
	if canManage {
		switch item.State {
		case "DRAFT":
			actions = append(actions, "EDIT", "VALIDATE", "ARCHIVE")
		case "VALID":
			actions = append(actions, "EDIT", "PUBLISH", "ARCHIVE")
		case "PUBLISHED":
			actions = append(actions, "EDIT", "ARCHIVE")
		}
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

func (repository *Repository) ListRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Run, int64, string, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.States = append([]string{}, filter.States...)
	sort.Strings(filter.States)
	for i, state := range filter.States {
		if !slices.Contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING", "SUCCEEDED", "FAILED", "CANCELLED"}, state) || (i > 0 && state == filter.States[i-1]) {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	if filter.ResumableSessionsOnly {
		return repository.listResumableSessions(ctx, scope, filter)
	}
	if filter.TargetType != "" || filter.TargetRef != "" {
		return nil, 0, "", errs.ErrInvalid
	}
	return authorizedCatalogWithTotal(ctx, repository, scope, "RUN", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Run, error) {
			rows, err := tx.Query(ctx, queryQueriesListrunsSelectRunsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef,
				scope.role, scope.actorID, strings.TrimSpace(filter.Query), limit, cursor, append([]string{}, filter.States...), scope.authorityProjectID)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Run
			for rows.Next() {
				item, err := scanRun(rows, true)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.Run) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Run, allowed func(string) bool) error {
			item.NextActions = runActions(item.State, allowed("run.cancel") || allowed("run.cancel.own"), false)
			return nil
		}, func(ctx context.Context, tx pgx.Tx) (int64, error) {
			var total int64
			err := tx.QueryRow(ctx, queryCatalogRunsCount, scope.organizationID, filter.ProjectRef, scope.actorID, filter.Query, filter.States, scope.authorityProjectID).Scan(&total)
			if err != nil {
				return 0, errs.ErrUnavailable
			}
			return total, nil
		})
}

func scanRun(row rowScanner, actorScoped bool) (entity.Run, error) {
	return scanRunWithPrefix(row, actorScoped)
}

func scanRunWithPrefix(row rowScanner, actorScoped bool, prefix ...any) (entity.Run, error) {
	var item entity.Run
	var input, usage []byte
	var canCancel, canLaunch bool
	destinations := append(prefix, &item.Ref, &item.ProjectRef, &item.SessionRef, &item.RootRunRef, &item.ParentRunRef, &item.RetryOfRunRef, &item.Title, &item.TitleSource, &item.ActivitySummary, &item.Task, &item.State, &item.Source, &item.ResultSummary, &item.SafeErrorCode, &item.SafeErrorMessage, &item.InitiatorName, &item.Target.Type, &item.Target.Ref, &item.Target.Name, &item.Attempt, &item.GraphRevision, &item.EventSequence, &item.Version, &input, &item.InputAttachmentSetRef, &item.ArtifactRefs, &item.GateRefs, &usage, &item.CreatedAt, &item.StartedAt, &item.FinishedAt)
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
	if json.Unmarshal(input, &item.Input) != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	decodedUsage, err := decodeRunUsage(usage)
	if err != nil {
		return entity.Run{}, err
	}
	item.Usage = decodedUsage
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
	if errors.Is(err, errs.ErrNotFound) {
		permissions = actorActionPermissions{}
	} else if err != nil {
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
		if err := repository.applyContinuationAction(ctx, runner, scope, result.Run); err != nil {
			return err
		}
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
		result.Artifact.NextActions = artifactActions(result.Artifact.ScanState, result.Artifact.LifecycleState, permissions.canManageArtifacts)
	}
	if result.Schedule != nil {
		result.Schedule.NextActions = scheduleActions(*result.Schedule, permissions.canManageSchedules)
	}
	if result.Event != nil {
		applyEventActionPermissions(result.Event, permissions)
		if err := repository.applyContinuationEventAction(ctx, runner, scope, result.Event); err != nil {
			return err
		}
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
		event.Delta.Artifact.NextActions = artifactActions(event.Delta.Artifact.ScanState, event.Delta.Artifact.LifecycleState, permissions.canManageArtifacts)
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
		var attempt, maximumAttempts int
		if err := rows.Scan(&incident.Ref, &incident.ProjectRef, &incident.RunRef, &deliveryState, &attempt, &maximumAttempts, &incident.CreatedAt); err != nil {
			return entity.Run{}, errs.ErrUnavailable
		}
		incident = projectInteractionIncident(incident, deliveryState, attempt, maximumAttempts)
		item.Incidents = append(item.Incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	if err := repository.applyContinuationAction(ctx, runner, scope, &item); err != nil {
		return entity.Run{}, err
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
	if err := row.Scan(&node.Ref, &node.RunRef, &node.ParentNodeRef, &node.Type, &node.State, &node.DisplayName, &node.Role, &node.AgentRef, &node.TurnRef, &node.Attempt, &node.InputSummary, &node.ProgressSummary, &node.IntegrationNames, &node.CallbackSummary, &node.SafeErrorCode, &node.SafeErrorMessage, &node.NextActions, &node.MaterializationState, &node.CreatedAt, &node.StartedAt, &node.FinishedAt, &node.ArtifactRefs, &node.ChildRunRefs); err != nil {
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
		var delta, toolCall []byte
		if err := rows.Scan(&e.Ref, &e.RunRef, &e.Sequence, &e.Type, &e.NodeRef, &e.EdgeRef, &e.GateRef, &e.ArtifactRef, &e.Summary, &e.Progress, &e.RunState, &e.NodeState, &delta, &e.Actor.Kind, &e.Actor.Ref, &e.Actor.Name, &e.MessageKind, &toolCall, &e.OccurredAt); err != nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		if err := json.Unmarshal(delta, &e.Delta); err != nil || e.Delta.Run == nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		if e.Delta.Incident != nil {
			e.IncidentRef = e.Delta.Incident.Ref
		}
		if len(toolCall) != 0 && json.Unmarshal(toolCall, &e.ToolCall) != nil {
			return nil, 0, false, errs.ErrUnavailable
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
	for index := range result {
		result[index].Delta.Run.NextActions = runActions(result[index].Delta.Run.State, permissions.canCancelRuns, slices.Contains(run.NextActions, "ADD_TURN"))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	return result, run.EventSequence, complete, nil
}

func scanGate(row rowScanner, actorScoped bool) (entity.OwnerGate, error) {
	var item entity.OwnerGate
	canResolve := true
	destinations := []any{&item.Ref, &item.ProjectRef, &item.RunRef, &item.NodeRef, &item.Title, &item.Prompt, &item.ContextSummary, &item.RequestedByRef, &item.RequestedByName, &item.AllowedDecisions, &item.State, &item.Decision, &item.DecisionComment, &item.ResolvedByName, &item.Version, &item.CreatedAt, &item.ResolvedAt, &item.ResolutionAttachmentSetRef}
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

func (repository *Repository) ListArtifacts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Artifact, int64, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	lifecycleState := strings.TrimSpace(filter.State)
	if lifecycleState == "" {
		lifecycleState = "ACTIVE"
	}
	if !contains([]string{"ACTIVE", "DELETED", "PURGE_PENDING", "PURGED"}, lifecycleState) {
		return nil, 0, "", errs.ErrInvalid
	}
	artifactType := strings.TrimSpace(filter.ArtifactType)
	if artifactType != "" && !contains([]string{"TEXT", "DOCUMENT", "IMAGE"}, artifactType) {
		return nil, 0, "", errs.ErrInvalid
	}
	scanState := strings.TrimSpace(filter.ScanState)
	if scanState != "" && !contains([]string{"PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED"}, scanState) {
		return nil, 0, "", errs.ErrInvalid
	}
	sourceKind := strings.TrimSpace(filter.SourceKind)
	if sourceKind != "" && !contains([]string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT"}, sourceKind) {
		return nil, 0, "", errs.ErrInvalid
	}
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.ResourceRef = strings.TrimSpace(filter.ResourceRef)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.State, filter.ArtifactType, filter.ScanState, filter.SourceKind = lifecycleState, artifactType, scanState, sourceKind
	return authorizedCatalogWithTotal(ctx, repository, scope, "ARTIFACT", filter,
		func(ctx context.Context, tx pgx.Tx, cursorRef string, limit int32) ([]entity.Artifact, error) {
			rows, err := tx.Query(ctx, queryQueriesListartifactsSelectArtifactBindingsArtifactIdIdOrganizationId, pgx.StrictNamedArgs{
				"authority_project": scope.authorityProjectID,
				"organization_id":   scope.organizationID,
				"project_ref":       strings.TrimSpace(filter.ProjectRef),
				"run_ref":           strings.TrimSpace(filter.ResourceRef),
				"role":              scope.role,
				"actor_id":          scope.actorID,
				"query":             strings.TrimSpace(filter.Query),
				"lifecycle_state":   lifecycleState,
				"artifact_type":     artifactType,
				"scan_state":        scanState,
				"source_kind":       sourceKind,
				"cursor_ref":        cursorRef,
				"limit":             limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var result []entity.Artifact
			for rows.Next() {
				item, scanErr := scanArtifact(rows)
				if scanErr != nil {
					return nil, scanErr
				}
				result = append(result, item)
			}
			if rows.Err() != nil {
				return nil, errs.ErrUnavailable
			}
			return result, nil
		}, func(item entity.Artifact) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Artifact, allowed func(string) bool) error {
			item.NextActions = nil
			if item.LifecycleState == "DELETED" {
				if allowed("artifact.restore") {
					item.NextActions = append(item.NextActions, "RESTORE")
				}
				if allowed("artifact.purge") {
					item.NextActions = append(item.NextActions, "PURGE")
				}
			} else if item.LifecycleState == "ACTIVE" {
				if item.ScanState == "CLEAN" {
					if allowed("artifact.download") {
						item.NextActions = append(item.NextActions, "DOWNLOAD")
					}
					if allowed("artifact.bind") {
						item.NextActions = append(item.NextActions, "BIND")
					}
				}
				if allowed("artifact.delete") {
					item.NextActions = append(item.NextActions, "DELETE")
				}
			}
			return nil
		}, func(ctx context.Context, tx pgx.Tx) (int64, error) {
			var total int64
			err := tx.QueryRow(ctx, queryCatalogArtifactsCount, pgx.StrictNamedArgs{
				"authority_project": scope.authorityProjectID, "organization_id": scope.organizationID,
				"project_ref": filter.ProjectRef, "run_ref": filter.ResourceRef, "actor_id": scope.actorID,
				"query": filter.Query, "lifecycle_state": lifecycleState, "artifact_type": artifactType,
				"scan_state": scanState, "source_kind": sourceKind,
			}).Scan(&total)
			if err != nil {
				return 0, errs.ErrUnavailable
			}
			return total, nil
		})
}

const artifactCursorVersion = "v1"

func encodeArtifactCursor(createdAt time.Time, ref string) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "\n" + ref
	return artifactCursorVersion + "." + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeArtifactCursor(token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	version, payload, found := strings.Cut(token, ".")
	if !found || version != artifactCursorVersion || len(payload) > 256 {
		return nil, "", errs.ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	createdAtText, ref, found := strings.Cut(string(decoded), "\n")
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtText)
	if !found || err != nil || !strings.HasPrefix(ref, "art_") || len(ref) > 96 || strings.ContainsAny(ref, "\r\n") {
		return nil, "", errs.ErrInvalid
	}
	createdAt = createdAt.UTC()
	return &createdAt, ref, nil
}

func scanArtifact(row rowScanner) (entity.Artifact, error) {
	var item entity.Artifact
	var canManage bool
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.RunRef, &item.SessionRef, &item.NodeRef, &item.FileName, &item.MediaType, &item.Digest, &item.ScanState, &item.PreviewState, &item.Source, &item.SizeBytes, &item.Revision, &item.Version, &item.LifecycleState, &item.CreatedAt, &item.DeletedAt, &item.PurgeAfter, &item.Bindings, &canManage); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Artifact{}, errs.ErrNotFound
		}
		return entity.Artifact{}, errs.ErrUnavailable
	}
	item.NextActions = artifactActions(item.ScanState, item.LifecycleState, canManage)
	return item, nil
}

func artifactActions(scanState, lifecycleState string, canManage bool) []string {
	if lifecycleState == "" {
		lifecycleState = "ACTIVE"
	}
	switch lifecycleState {
	case "DELETED":
		if canManage {
			return []string{"RESTORE", "PURGE"}
		}
		return []string{}
	case "PURGE_PENDING", "PURGED":
		return []string{}
	}
	if scanState != "CLEAN" {
		if canManage {
			return []string{"DELETE"}
		}
		return []string{}
	}
	actions := []string{"DOWNLOAD"}
	if canManage {
		actions = append(actions, "BIND", "DELETE")
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
	return authorizedCatalog(ctx, repository, scope, "SCHEDULE", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Schedule, error) {
			rows, err := tx.Query(ctx, queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId, pgx.StrictNamedArgs{
				"authority_project": scope.authorityProjectID,
				"organization_id":   scope.organizationID, "project_ref": filter.ProjectRef, "role": scope.role, "actor_id": scope.actorID,
				"search_query": strings.TrimSpace(filter.Query), "cursor_ref": cursor, "page_size": limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Schedule
			for rows.Next() {
				item, err := scanSchedule(rows)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.Schedule) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SCHEDULE", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Schedule, allowed func(string) bool) error {
			item.NextActions = scheduleActions(*item, allowed("schedule.manage"))
			return nil
		})
}

func (repository *Repository) GetSchedule(ctx context.Context, principal value.Principal, ref string) (entity.Schedule, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Schedule{}, err
	}
	return scanSchedule(repository.pool.QueryRow(ctx, queryQueriesGetscheduleSelectSchedulesOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"schedule_ref":    ref,
		"role":            scope.role,
		"actor_id":        scope.actorID,
	}))
}

func scanSchedule(row rowScanner) (entity.Schedule, error) {
	var item entity.Schedule
	var input, currentRevisionInput, currentRevisionPromptInputs []byte
	var continueSessionExpected bool
	var continueSessionRef *string
	var canManage bool
	if err := row.Scan(
		&item.Ref, &item.ProjectRef, &item.Name, &item.Target.Type, &item.Target.Ref, &item.Target.Name,
		&item.Preset, &item.CronExpression, &item.Timezone, &input, &item.SessionPolicy,
		&item.NotificationPolicy, &item.State, &item.Enabled, &item.Version, &item.NextRunAt,
		&item.LastRunAt, &item.CreatedAt, &item.UpdatedAt,
		&item.CurrentRevision.Ref, &item.CurrentRevision.Revision, &item.CurrentRevision.Digest,
		&item.CurrentRevision.Name, &item.CurrentRevision.Target.Type, &item.CurrentRevision.Target.Ref,
		&item.CurrentRevision.Preset, &item.CurrentRevision.CronExpression, &item.CurrentRevision.Timezone,
		&currentRevisionInput, &item.CurrentRevision.SessionPolicy,
		&item.CurrentRevision.NotificationPolicy, &item.CurrentRevision.DSTGapPolicy,
		&item.CurrentRevision.DSTFoldPolicy, &item.CurrentRevision.MisfirePolicy,
		&item.CurrentRevision.OverlapPolicy, &item.CurrentRevision.TargetVersion,
		&item.CurrentRevision.TargetDigest, &item.CurrentRevision.AutomationText,
		&currentRevisionPromptInputs, &item.CurrentRevision.CreatedAt,
		&continueSessionExpected, &continueSessionRef, &item.LastOutcome, &canManage,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Schedule{}, errs.ErrNotFound
		}
		return entity.Schedule{}, errs.ErrUnavailable
	}
	if err := attachScheduleDisplay(&item); err != nil ||
		json.Unmarshal(input, &item.Input) != nil ||
		json.Unmarshal(currentRevisionInput, &item.CurrentRevision.Input) != nil ||
		json.Unmarshal(currentRevisionPromptInputs, &item.CurrentRevision.PromptInputs) != nil {
		return entity.Schedule{}, errs.ErrUnavailable
	}
	item.DSTGapPolicy, item.DSTFoldPolicy = item.CurrentRevision.DSTGapPolicy, item.CurrentRevision.DSTFoldPolicy
	item.MisfirePolicy, item.OverlapPolicy = item.CurrentRevision.MisfirePolicy, item.CurrentRevision.OverlapPolicy
	item.TargetVersion, item.TargetDigest = item.CurrentRevision.TargetVersion, item.CurrentRevision.TargetDigest
	item.AutomationText, item.PromptInputs = item.CurrentRevision.AutomationText, item.CurrentRevision.PromptInputs
	if continueSessionExpected {
		if continueSessionRef == nil || strings.TrimSpace(*continueSessionRef) == "" {
			return entity.Schedule{}, errs.ErrUnavailable
		}
		item.ContinueSessionRef = *continueSessionRef
	} else if continueSessionRef != nil {
		return entity.Schedule{}, errs.ErrUnavailable
	}
	item.NextActions = scheduleActions(item, canManage)
	return item, nil
}

func scheduleActions(item entity.Schedule, canManage bool) []string {
	actions := []string{"OPEN"}
	if !canManage {
		return actions
	}
	if item.State == "ARCHIVED" {
		return append(actions, "DELETE")
	}
	actions = append(actions, "EDIT", "ARCHIVE")
	if item.Enabled {
		return append(actions, "DISABLE")
	}
	return append(actions, "ENABLE")
}

func (repository *Repository) ListIntegrationDefinitions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.IntegrationDefinition, string, []string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", nil, err
	}
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!validStableKey(cursor) || len(cursor) > 96) {
		return nil, "", nil, errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryQueriesListintegrationdefinitionsSelectIntegrationDefinitionsCategory,
		strings.TrimSpace(filter.Category), strings.TrimSpace(filter.Query), cursor, limit+1)
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationDefinition
	for rows.Next() {
		var item entity.IntegrationDefinition
		var capabilities, schema []byte
		if err := rows.Scan(
			&item.Key, &item.Name, &item.Description, &item.Category, &item.Optional, &item.Enabled,
			&capabilities, &schema, &item.SchemaVersion, &item.DefinitionVersion, &item.Origin,
			&item.Digest, &item.Adapter, &item.CredentialSecretKey,
			&item.AdapterOwner, &item.ExecutionRoute, &item.AdapterReadiness,
		); err != nil {
			return nil, "", nil, errs.ErrUnavailable
		}
		if json.Unmarshal(capabilities, &item.Capabilities) != nil || json.Unmarshal(schema, &item.ConfigurationFields) != nil {
			return nil, "", nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		next = result[len(result)-1].Key
	}
	actions := collectionCreateActions(scope.role, "CREATE_CONNECTION")
	return result, next, actions, nil
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
	var credential entity.IntegrationCredentialRevision
	var credentialCreatedAt *time.Time
	if err := row.Scan(
		&item.Ref, &item.DefinitionKey, &item.DefinitionName, &item.Name, &item.State,
		&item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version,
		&configuration, &capabilities, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt,
		&item.DefinitionVersion, &item.DefinitionDigest, &item.CredentialSecretKey,
		&credential.Ref, &credential.Revision,
		&credential.SecretRef, &credential.SecretUID, &credential.SecretResourceVersion,
		&credential.ContentSHA256, &credentialCreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.IntegrationConnection{}, errs.ErrNotFound
		}
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	if json.Unmarshal(configuration, &item.PublicConfiguration) != nil || json.Unmarshal(capabilities, &item.Capabilities) != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	if credential.Ref != "" && credentialCreatedAt != nil {
		credential.CreatedAt = *credentialCreatedAt
		item.CredentialRevision = &credential
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
			return append(actions, "EDIT", "ENABLE", "DELETE")
		}
		return actions
	}
	if manageConnection {
		actions = append(actions, "EDIT")
	}
	if manageConnection && item.CredentialSecretKey != "" && item.State != "TESTING" {
		actions = append(actions, "CONFIGURE_CREDENTIAL")
	}
	if manageConnection && !item.TestRequiresApproval && item.State != "TESTING" && item.MaskedCredentialsState == "CONFIGURED" {
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
	if err := projectConnectionPackage(ctx, querier, scope, item); err != nil {
		return err
	}
	rows, err := querier.Query(ctx, queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef, scope.organizationID, item.Ref, scope.role, scope.actorID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var grant entity.IntegrationGrant
		var resourceScope []byte
		if err := rows.Scan(
			&grant.Ref, &grant.CapabilityKey, &grant.TargetType, &grant.TargetRef, &grant.TargetName,
			&grant.Enabled, &grant.ApprovalPolicy, &grant.Version, &grant.Risk, &grant.ResourceKind,
			&resourceScope, &grant.ResourceScopeDigest,
		); err != nil {
			return err
		}
		if json.Unmarshal(resourceScope, &grant.ResourceScope) != nil {
			return errors.New("decode integration grant resource scope")
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	if len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, 0) || (filter.State != "ACTIVE" && filter.State != "CLOSED" && filter.State != "ARCHIVED") {
		return nil, "", errs.ErrInvalid
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	cursor, err := decodeCatalogCursor(scope, "ASSISTANT_CONVERSATIONS", filter)
	if err != nil {
		return nil, "", err
	}
	var cursorAt time.Time
	var cursorRef string
	if cursor != "" {
		at, ref, ok := strings.Cut(cursor, "|")
		if !ok {
			return nil, "", errs.ErrInvalid
		}
		cursorAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil || ref == "" {
			return nil, "", errs.ErrInvalid
		}
		cursorRef = ref
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "actor_id": scope.actorID, "project_ref": filter.ProjectRef, "authority_project": scope.authorityProjectID,
		"query": filter.Query, "state": filter.State, "evaluated_at": time.Now().UTC(), "cursor_at": cursorAt, "cursor_ref": cursorRef, "page_size": limit + 1})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AssistantConversation
	for rows.Next() {
		var item entity.AssistantConversation
		if err := rows.Scan(&item.Ref, &item.Title, &item.TitleSource, &item.TitleRevision, &item.ProjectRef,
			&item.SessionRef, &item.State, &item.Version, &item.Context.Route, &item.Context.EntityKind,
			&item.Context.EntityRef, &item.Context.EntityName, &item.Context.EntityVersion,
			&item.Context.AllowedOperations, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeCatalogCursor(scope, "ASSISTANT_CONVERSATIONS", filter, last.CreatedAt.UTC().Format(time.RFC3339Nano)+"|"+last.Ref)
	}
	for index := range result {
		if err := repository.attachConversation(ctx, scope, &result[index]); err != nil {
			return nil, "", err
		}
	}
	return result, next, nil
}
func (repository *Repository) attachConversation(ctx context.Context, scope scope, item *entity.AssistantConversation) error {
	rows, err := repository.pool.Query(ctx, queryQueriesAttachconversationSelectSessionTurnsOrganizationIdSessionIdRef, scope.organizationID, item.Ref)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var turn entity.AssistantTurn
		if err := rows.Scan(&turn.Ref, &turn.Sequence, &turn.Actor, &turn.ActorName, &turn.Content, &turn.State, &turn.AttachmentSetRef, &turn.CreatedAt, &turn.CompletedAt); err != nil {
			return errs.ErrUnavailable
		}
		item.Turns = append(item.Turns, turn)
	}
	var raw []byte
	var plan entity.AssistantPlan
	err = repository.pool.QueryRow(ctx, queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef, scope.organizationID, item.Ref).Scan(
		&plan.Ref, &plan.Summary, &plan.State, &plan.Version, &plan.Revision, &plan.ValidatedRevision,
		&plan.ContentDigest, &plan.ValidationProblems, &raw, &plan.CreatedAt, &plan.ValidatedAt, &plan.AppliedAt,
	)
	if err == nil {
		_ = json.Unmarshal(raw, &plan.Operations)
		plan.ConversationRef = item.Ref
		plan.ProjectRef = item.ProjectRef
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
	definitions, _, _, err := repository.ListIntegrationDefinitions(ctx, principal, query.Filter{})
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
		var attempt, maximumAttempts int
		if err := incidentRows.Scan(&incident.Ref, &incident.ProjectRef, &incident.RunRef, &deliveryState, &attempt, &maximumAttempts, &incident.CreatedAt); err != nil {
			return platformrepo.Administration{}, errs.ErrUnavailable
		}
		incident = projectInteractionIncident(incident, deliveryState, attempt, maximumAttempts)
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
	cursorOccurredAt, cursorRef, err := decodeAuditCursor(filter.Page.Token)
	if err != nil {
		return nil, "", err
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryQueriesListauditeventsSelectAuditEventsOrganizationIdRefAction,
		scope.organizationID, filter.ProjectRef, filter.Action, filter.Outcome, filter.Query,
		scope.role, scope.actorID, cursorOccurredAt, cursorRef, limit+1,
	)
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	result := make([]entity.AuditEvent, 0, limit+1)
	for rows.Next() {
		var item entity.AuditEvent
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.ActorRef, &item.ActorName, &item.Executor, &item.Source, &item.Action, &item.ResourceKind, &item.ResourceRef, &item.ResourceName, &item.Outcome, &item.Summary, &item.CorrelationRef, &item.OccurredAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeAuditCursor(last.OccurredAt, last.Ref)
	}
	return result, next, nil
}

func encodeAuditCursor(occurredAt time.Time, ref string) string {
	return encodeMVPCursor("audit", occurredAt, ref)
}

func decodeAuditCursor(token string) (*time.Time, string, error) {
	occurredAt, ref, err := decodeMVPCursor("audit", token)
	if err != nil {
		return nil, "", err
	}
	if ref != "" && (!strings.HasPrefix(ref, "aud_") || strings.ContainsAny(ref, "\r\n")) {
		return nil, "", errs.ErrInvalid
	}
	return occurredAt, ref, nil
}
