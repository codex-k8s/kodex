// Package agent implements the PostgreSQL repository for agent-manager metadata.
package agent

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentrepo "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/repository/agent"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

// SQLFiles contains named SQL queries for agent-manager repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ agentrepo.Repository = (*Repository)(nil)

type database interface {
	dataRunner
	postgreslib.TxBeginner
}

type dataRunner interface {
	postgreslib.ExecQuerier
	postgreslib.RowQuerier
	queryRowGetter
}

type queryRowGetter interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db database
}

const (
	operationCreateFlow            = "domain.Repository.CreateFlowWithResult"
	operationUpdateFlow            = "domain.Repository.UpdateFlowWithResult"
	operationGetFlow               = "domain.Repository.GetFlow"
	operationListFlows             = "domain.Repository.ListFlows"
	operationCreateFlowVersion     = "domain.Repository.CreateFlowVersionWithResult"
	operationActivateFlowVersion   = "domain.Repository.ActivateFlowVersionWithResult"
	operationGetFlowVersion        = "domain.Repository.GetFlowVersion"
	operationListFlowVersions      = "domain.Repository.ListFlowVersions"
	operationCreateRole            = "domain.Repository.CreateRoleProfileWithResult"
	operationUpdateRole            = "domain.Repository.UpdateRoleProfileWithResult"
	operationGetRole               = "domain.Repository.GetRoleProfile"
	operationListRoles             = "domain.Repository.ListRoleProfiles"
	operationCreatePromptTemplate  = "domain.Repository.CreatePromptTemplateWithResult"
	operationGetPromptTemplate     = "domain.Repository.GetPromptTemplate"
	operationListPromptTemplates   = "domain.Repository.ListPromptTemplates"
	operationCreatePromptVersion   = "domain.Repository.CreatePromptTemplateVersionWithResult"
	operationActivatePromptVersion = "domain.Repository.ActivatePromptTemplateVersionWithResult"
	operationGetPromptVersion      = "domain.Repository.GetPromptTemplateVersion"
	operationListPromptVersions    = "domain.Repository.ListPromptTemplateVersions"
	operationCreateSession         = "domain.Repository.CreateAgentSessionWithResult"
	operationUpdateSession         = "domain.Repository.UpdateAgentSessionWithResult"
	operationGetSession            = "domain.Repository.GetAgentSession"
	operationFindActiveSession     = "domain.Repository.FindActiveAgentSessionByProviderWorkItem"
	operationCreateRun             = "domain.Repository.CreateAgentRunWithResult"
	operationUpdateRun             = "domain.Repository.UpdateAgentRunWithResult"
	operationGetRun                = "domain.Repository.GetAgentRun"
	operationListRuns              = "domain.Repository.ListAgentRuns"
	operationCreateSnapshot        = "domain.Repository.CreateSessionStateSnapshotWithResult"
	operationGetSnapshot           = "domain.Repository.GetSessionStateSnapshot"
	operationCreateAcceptance      = "domain.Repository.CreateAcceptanceResultWithResult"
	operationUpdateAcceptance      = "domain.Repository.UpdateAcceptanceResultWithResult"
	operationGetAcceptance         = "domain.Repository.GetAcceptanceResult"
	operationListAcceptance        = "domain.Repository.ListAcceptanceResults"
	operationCreateFollowUp        = "domain.Repository.CreateFollowUpIntentWithResult"
	operationUpdateFollowUp        = "domain.Repository.UpdateFollowUpIntentWithResult"
	operationGetFollowUp           = "domain.Repository.GetFollowUpIntent"
	operationRecordActivity        = "domain.Repository.RecordAgentActivityWithResult"
	operationGetActivity           = "domain.Repository.GetAgentActivity"
	operationListActivities        = "domain.Repository.ListAgentActivities"
	operationGetCommandResult      = "domain.Repository.GetCommandResult"
	operationRecordCommandResult   = "domain.Repository.RecordCommandResult"
	operationOutboxClaim           = "domain.Repository.ClaimOutboxEvents"
	operationOutboxMarkFailed      = "domain.Repository.MarkOutboxEventFailed"
	operationOutboxMarkPermanent   = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationOutboxMarkPublished   = "domain.Repository.MarkOutboxEventPublished"
)

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateFlowWithResult(ctx context.Context, flow entity.Flow, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationCreateFlow, queryFlowCreate, flowArgs(flow), result, nil)
}

func (r *Repository) UpdateFlowWithResult(ctx context.Context, flow entity.Flow, previousVersion int64, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationUpdateFlow, queryFlowUpdate, flowUpdateArgs(flow, previousVersion), result, nil)
}

func (r *Repository) GetFlow(ctx context.Context, id uuid.UUID) (entity.Flow, error) {
	return queryOne(ctx, r.db, operationGetFlow, queryFlowGet, pgx.NamedArgs{"id": id}, scanFlow)
}

func (r *Repository) ListFlows(ctx context.Context, filter query.FlowFilter) ([]entity.Flow, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListFlows, queryFlowList, flowFilterArgs(filter), scanFlow)
}

func (r *Repository) CreateFlowVersionWithResult(ctx context.Context, version entity.FlowVersion, result entity.CommandResult) (entity.FlowVersion, error) {
	err := r.createWithResult(ctx, operationCreateFlowVersion, result, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, queryFlowVersionCreate, flowVersionArgs(version), true); err != nil {
			return err
		}
		return r.createFlowVersionChildren(ctx, tx, version)
	})
	return version, err
}

func (r *Repository) ActivateFlowVersionWithResult(ctx context.Context, flow entity.Flow, previousFlowVersion int64, version entity.FlowVersion, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.activateVersionWithResult(ctx, operationActivateFlowVersion, []postgreslib.Mutation{
		mutation(queryFlowUpdate, flowUpdateArgs(flow, previousFlowVersion), true),
		mutation(queryFlowVersionSupersede, pgx.NamedArgs{"flow_id": version.FlowID, "id": version.ID}, false),
		mutation(queryFlowVersionActivate, flowVersionArgs(version), true),
	}, result, event)
}

func (r *Repository) GetFlowVersion(ctx context.Context, id uuid.UUID) (entity.FlowVersion, error) {
	version, err := queryOne(ctx, r.db, operationGetFlowVersion, queryFlowVersionGet, pgx.NamedArgs{"id": id}, scanFlowVersion)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	return r.loadFlowVersionChildren(ctx, r.db, version)
}

func (r *Repository) ListFlowVersions(ctx context.Context, filter query.FlowVersionFilter) ([]entity.FlowVersion, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListFlowVersions, queryFlowVersionList, flowVersionFilterArgs(filter), scanFlowVersion)
}

func (r *Repository) CreateRoleProfileWithResult(ctx context.Context, role entity.RoleProfile, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationCreateRole, queryRoleCreate, roleProfileArgs(role), result, nil)
}

func (r *Repository) UpdateRoleProfileWithResult(ctx context.Context, role entity.RoleProfile, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationUpdateRole, queryRoleUpdate, roleProfileUpdateArgs(role, previousVersion), result, event)
}

func (r *Repository) GetRoleProfile(ctx context.Context, id uuid.UUID) (entity.RoleProfile, error) {
	return queryOne(ctx, r.db, operationGetRole, queryRoleGet, pgx.NamedArgs{"id": id}, scanRoleProfile)
}

func (r *Repository) ListRoleProfiles(ctx context.Context, filter query.RoleProfileFilter) ([]entity.RoleProfile, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListRoles, queryRoleList, roleProfileFilterArgs(filter), scanRoleProfile)
}

func (r *Repository) CreatePromptTemplateWithResult(ctx context.Context, template entity.PromptTemplate, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationCreatePromptTemplate, queryPromptTemplateCreate, promptTemplateArgs(template), result, nil)
}

func (r *Repository) GetPromptTemplate(ctx context.Context, id uuid.UUID) (entity.PromptTemplate, error) {
	return queryOne(ctx, r.db, operationGetPromptTemplate, queryPromptTemplateGet, pgx.NamedArgs{"id": id}, scanPromptTemplate)
}

func (r *Repository) ListPromptTemplates(ctx context.Context, filter query.PromptTemplateFilter) ([]entity.PromptTemplate, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPromptTemplates, queryPromptTemplateList, promptTemplateFilterArgs(filter), scanPromptTemplate)
}

func (r *Repository) CreatePromptTemplateVersionWithResult(ctx context.Context, newTemplate *entity.PromptTemplate, version entity.PromptTemplateVersion, result entity.CommandResult) (entity.PromptTemplateVersion, error) {
	err := r.createWithResult(ctx, operationCreatePromptVersion, result, func(tx pgx.Tx) error {
		if newTemplate != nil {
			if err := runMutation(ctx, tx, queryPromptTemplateCreate, promptTemplateArgs(*newTemplate), true); err != nil {
				return err
			}
		}
		return runMutation(ctx, tx, queryPromptVersionCreate, promptTemplateVersionArgs(version), true)
	})
	return version, err
}

func (r *Repository) ActivatePromptTemplateVersionWithResult(ctx context.Context, template entity.PromptTemplate, previousTemplateVersion int64, version entity.PromptTemplateVersion, result entity.CommandResult, event entity.OutboxEvent) error {
	mutations := []postgreslib.Mutation{
		mutation(queryPromptTemplateUpdate, promptTemplateUpdateArgs(template, previousTemplateVersion), true),
		mutation(queryPromptVersionSupersede, pgx.NamedArgs{"prompt_template_id": version.PromptTemplateID, "id": version.ID}, false),
		mutation(queryPromptVersionActivate, promptTemplateVersionArgs(version), true),
	}
	return r.activateVersionWithResult(ctx, operationActivatePromptVersion, mutations, result, event)
}

func (r *Repository) GetPromptTemplateVersion(ctx context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
	return queryOne(ctx, r.db, operationGetPromptVersion, queryPromptVersionGet, pgx.NamedArgs{"id": id}, scanPromptTemplateVersion)
}

func (r *Repository) ListPromptTemplateVersions(ctx context.Context, filter query.PromptTemplateVersionFilter) ([]entity.PromptTemplateVersion, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPromptVersions, queryPromptVersionList, promptTemplateVersionFilterArgs(filter), scanPromptTemplateVersion)
}

func (r *Repository) CreateAgentSessionWithResult(ctx context.Context, session entity.AgentSession, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreateSession, querySessionCreate, agentSessionArgs(session), result, &event)
}

func (r *Repository) UpdateAgentSessionWithResult(ctx context.Context, session entity.AgentSession, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationUpdateSession, querySessionUpdate, agentSessionUpdateArgs(session, previousVersion), result, &event)
}

func (r *Repository) GetAgentSession(ctx context.Context, id uuid.UUID) (entity.AgentSession, error) {
	return queryOne(ctx, r.db, operationGetSession, querySessionGet, pgx.NamedArgs{"id": id}, scanAgentSession)
}

func (r *Repository) FindActiveAgentSessionByProviderWorkItem(ctx context.Context, scope value.ScopeRef, providerWorkItemRef string) (entity.AgentSession, error) {
	return queryOne(ctx, r.db, operationFindActiveSession, querySessionFindActiveByTarget, pgx.NamedArgs{
		"scope_type":             scope.Type,
		"scope_ref":              scope.Ref,
		"provider_work_item_ref": providerWorkItemRef,
	}, scanAgentSession)
}

func (r *Repository) CreateAgentRunWithResult(ctx context.Context, run entity.AgentRun, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreateRun, queryRunCreate, agentRunArgs(run), result, &event)
}

func (r *Repository) UpdateAgentRunWithResult(ctx context.Context, run entity.AgentRun, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationUpdateRun, queryRunUpdate, agentRunUpdateArgs(run, previousVersion), result, event)
}

func (r *Repository) GetAgentRun(ctx context.Context, id uuid.UUID) (entity.AgentRun, error) {
	return queryOne(ctx, r.db, operationGetRun, queryRunGet, pgx.NamedArgs{"id": id}, scanAgentRun)
}

func (r *Repository) ListAgentRuns(ctx context.Context, filter query.AgentRunFilter) ([]entity.AgentRun, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListRuns, queryRunList, agentRunFilterArgs(filter), scanAgentRun)
}

func (r *Repository) CreateSessionStateSnapshotWithResult(ctx context.Context, snapshot entity.AgentSessionStateSnapshot, session entity.AgentSession, previousSessionVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, querySessionStateSnapshotCreate, sessionStateSnapshotArgs(snapshot), true); err != nil {
			return err
		}
		if err := runMutation(ctx, tx, querySessionUpdate, agentSessionUpdateArgs(session, previousSessionVersion), true); err != nil {
			return err
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		return runOutboxEvent(ctx, tx, event)
	})
	return wrapError(operationCreateSnapshot, err)
}

func (r *Repository) GetSessionStateSnapshot(ctx context.Context, id uuid.UUID) (entity.AgentSessionStateSnapshot, error) {
	return queryOne(ctx, r.db, operationGetSnapshot, querySessionStateSnapshotGet, pgx.NamedArgs{"id": id}, scanSessionStateSnapshot)
}

func (r *Repository) CreateAcceptanceResultWithResult(ctx context.Context, acceptance entity.AcceptanceResult, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreateAcceptance, queryAcceptanceResultCreate, acceptanceResultArgs(acceptance), result, &event)
}

func (r *Repository) UpdateAcceptanceResultWithResult(ctx context.Context, acceptance entity.AcceptanceResult, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	args := acceptanceResultUpdateArgs(acceptance, previousVersion)
	return r.mutateWithResult(ctx, operationUpdateAcceptance, queryAcceptanceResultUpdate, args, result, event)
}

func (r *Repository) GetAcceptanceResult(ctx context.Context, id uuid.UUID) (entity.AcceptanceResult, error) {
	return queryOne(ctx, r.db, operationGetAcceptance, queryAcceptanceResultGet, pgx.NamedArgs{"id": id}, scanAcceptanceResult)
}

func (r *Repository) ListAcceptanceResults(ctx context.Context, filter query.AcceptanceResultFilter) ([]entity.AcceptanceResult, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListAcceptance, queryAcceptanceResultList, acceptanceResultFilterArgs(filter), scanAcceptanceResult)
}

func (r *Repository) CreateFollowUpIntentWithResult(ctx context.Context, intent entity.FollowUpIntent, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreateFollowUp, queryFollowUpIntentCreate, followUpIntentArgs(intent), result, &event)
}

func (r *Repository) UpdateFollowUpIntentWithResult(ctx context.Context, intent entity.FollowUpIntent, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationUpdateFollowUp, queryFollowUpIntentUpdate, followUpIntentUpdateArgs(intent, previousVersion), result, event)
}

func (r *Repository) GetFollowUpIntent(ctx context.Context, id uuid.UUID) (entity.FollowUpIntent, error) {
	return queryOne(ctx, r.db, operationGetFollowUp, queryFollowUpIntentGet, pgx.NamedArgs{"id": id}, scanFollowUpIntent)
}

func (r *Repository) RecordAgentActivityWithResult(ctx context.Context, activity entity.AgentActivity, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationRecordActivity, queryAgentActivityCreate, agentActivityArgs(activity), result, nil)
}

func (r *Repository) GetAgentActivity(ctx context.Context, id uuid.UUID) (entity.AgentActivity, error) {
	return queryOne(ctx, r.db, operationGetActivity, queryAgentActivityGet, pgx.NamedArgs{"id": id}, scanAgentActivity)
}

func (r *Repository) ListAgentActivities(ctx context.Context, filter query.AgentActivityFilter) ([]entity.AgentActivity, value.PageResult, error) {
	page, err := agentActivityFilterArgs(filter)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	items, err := queryMany(ctx, r.db, operationListActivities, queryAgentActivityList, page.NamedArgs, scanAgentActivity)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	trimmed, result := activityPageResult(items, page)
	return trimmed, result, nil
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) RecordCommandResult(ctx context.Context, result entity.CommandResult) error {
	return wrapError(operationRecordCommandResult, runCommandResult(ctx, r.db, result))
}

func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	events, queryRan, err := postgreslib.ClaimOutboxRows(ctx, r.db, queryOutboxEventClaim, limit, now, lockedUntil, scanOutboxEvent)
	if !queryRan {
		return nil, wrapError(operationOutboxClaim, errs.ErrInvalidArgument)
	}
	if err != nil {
		return nil, wrapError(operationOutboxClaim, err)
	}
	return events, nil
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	args, ok := postgreslib.OutboxPublishedArgs(id, attemptCount, publishedAt)
	if !ok {
		return wrapError(operationOutboxMarkPublished, errs.ErrInvalidArgument)
	}
	_, err := r.db.Exec(ctx, queryOutboxEventMarkPublished, args)
	return wrapError(operationOutboxMarkPublished, err)
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxDeliveryFailure(ctx, operationOutboxMarkFailed, queryOutboxEventMarkFailed, "next_attempt_at", id, attemptCount, nextAttemptAt, lastError)
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxDeliveryFailure(ctx, operationOutboxMarkPermanent, queryOutboxEventMarkPermanent, "failed_permanently_at", id, attemptCount, failedAt, lastError)
}

func (r *Repository) mutateWithResult(ctx context.Context, operation string, mutationQuery string, mutationArgs pgx.NamedArgs, result entity.CommandResult, event *entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, mutationQuery, mutationArgs, true); err != nil {
			return err
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		if event == nil {
			return nil
		}
		return runOutboxEvent(ctx, tx, *event)
	})
	return wrapError(operation, err)
}

func (r *Repository) createWithResult(ctx context.Context, operation string, result entity.CommandResult, create func(pgx.Tx) error) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := create(tx); err != nil {
			return err
		}
		return runCommandResult(ctx, tx, result)
	})
	return wrapError(operation, err)
}

func (r *Repository) markOutboxDeliveryFailure(ctx context.Context, operation string, query string, timestampName string, id uuid.UUID, attemptCount int, timestamp time.Time, lastError string) error {
	args, ok := postgreslib.OutboxDeliveryFailureArgs(id, attemptCount, timestampName, timestamp, lastError)
	if !ok {
		return wrapError(operation, errs.ErrInvalidArgument)
	}
	_, err := r.db.Exec(ctx, query, args)
	return wrapError(operation, err)
}

func (r *Repository) activateVersionWithResult(ctx context.Context, operation string, mutations []postgreslib.Mutation, result entity.CommandResult, event entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		for _, item := range mutations {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, item); err != nil {
				return err
			}
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		return runOutboxEvent(ctx, tx, event)
	})
	return wrapError(operation, err)
}

func (r *Repository) createFlowVersionChildren(ctx context.Context, db dataRunner, version entity.FlowVersion) error {
	for _, stage := range version.Stages {
		if err := runMutation(ctx, db, queryStageCreate, stageArgs(stage), true); err != nil {
			return err
		}
	}
	for _, transition := range version.Transitions {
		if err := runMutation(ctx, db, queryStageTransitionCreate, stageTransitionArgs(transition), true); err != nil {
			return err
		}
	}
	for _, binding := range version.RoleBindings {
		if err := runMutation(ctx, db, queryStageRoleBindingCreate, stageRoleBindingArgs(binding), true); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) loadFlowVersionChildren(ctx context.Context, db dataRunner, version entity.FlowVersion) (entity.FlowVersion, error) {
	stages, err := queryMany(ctx, db, operationGetFlowVersion, queryStageListByFlowVersion, pgx.NamedArgs{"flow_version_id": version.ID}, scanStage)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	transitions, err := queryMany(ctx, db, operationGetFlowVersion, queryStageTransitionListByFlowVersion, pgx.NamedArgs{"flow_version_id": version.ID}, scanStageTransition)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	bindings, err := queryMany(ctx, db, operationGetFlowVersion, queryStageRoleBindingListByFlowVersion, pgx.NamedArgs{"flow_version_id": version.ID}, scanStageRoleBinding)
	if err != nil {
		return entity.FlowVersion{}, err
	}
	version.Stages = stages
	version.Transitions = transitions
	version.RoleBindings = bindings
	return version, nil
}

func runMutation(ctx context.Context, db dataRunner, query string, args pgx.NamedArgs, requireAffected bool) error {
	return postgreslib.RunMutation(ctx, db, errs.ErrConflict, mutation(query, args, requireAffected))
}

func mutation(query string, args pgx.NamedArgs, requireAffected bool) postgreslib.Mutation {
	return postgreslib.Mutation{Query: query, Args: args, RequireAffected: requireAffected}
}

func runCommandResult(ctx context.Context, db dataRunner, result entity.CommandResult) error {
	return runMutation(ctx, db, queryCommandResultCreate, commandResultArgs(result), true)
}

func runOutboxEvent(ctx context.Context, db dataRunner, event entity.OutboxEvent) error {
	return runMutation(ctx, db, queryOutboxEventCreate, outboxEventArgs(event), true)
}

func queryOne[T any](ctx context.Context, db dataRunner, operation string, query string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	row := db.QueryRow(ctx, query, args)
	item, err := scan(row)
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	return item, nil
}

func queryMany[T any](ctx context.Context, db dataRunner, operation string, query string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, query, args)
	if err != nil {
		var none []T
		return none, wrapError(operation, err)
	}
	items, scanErr := postgreslib.ScanRows(rows, scan)
	if scanErr != nil {
		var none []T
		return none, wrapError(operation, scanErr)
	}
	return items, nil
}

func queryPage[T any](ctx context.Context, db dataRunner, operation string, query string, page pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, value.PageResult, error) {
	items, err := queryMany(ctx, db, operation, query, page.NamedArgs, scan)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	trimmed, result := pageResult(items, page)
	return trimmed, result, nil
}
