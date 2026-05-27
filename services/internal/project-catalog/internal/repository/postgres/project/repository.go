// Package project implements the PostgreSQL repository for project-catalog.
package project

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectrepo "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/repository/project"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
)

// SQLFiles contains named SQL queries for the project-catalog PostgreSQL repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ projectrepo.Repository = (*Repository)(nil)

type database interface {
	execer
	queryer
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository persists project-catalog aggregates in PostgreSQL.
type Repository struct {
	db database
}

const (
	operationAttachRepository                 = "domain.Repository.AttachRepository"
	operationCancelPolicyOverride             = "domain.Repository.CancelPolicyOverride"
	operationClaimOutboxEvents                = "domain.Repository.ClaimOutboxEvents"
	operationCreatePolicyEditProposal         = "domain.Repository.CreatePolicyEditProposal"
	operationCreatePolicyOverride             = "domain.Repository.CreatePolicyOverride"
	operationCreateProject                    = "domain.Repository.CreateProject"
	operationGetBranchRules                   = "domain.Repository.GetBranchRules"
	operationGetCommandResult                 = "domain.Repository.GetCommandResult"
	operationGetDocumentationSource           = "domain.Repository.GetDocumentationSource"
	operationGetPlacementPolicy               = "domain.Repository.GetPlacementPolicy"
	operationGetPolicyEditProposal            = "domain.Repository.GetPolicyEditProposal"
	operationGetPolicyOverride                = "domain.Repository.GetPolicyOverride"
	operationGetProject                       = "domain.Repository.GetProject"
	operationGetReleaseLine                   = "domain.Repository.GetReleaseLine"
	operationGetReleasePolicy                 = "domain.Repository.GetReleasePolicy"
	operationGetRepository                    = "domain.Repository.GetRepository"
	operationGetRepositoryByProviderRef       = "domain.Repository.GetRepositoryByProviderRef"
	operationGetServicesPolicy                = "domain.Repository.GetServicesPolicy"
	operationGetServicesPolicyBySource        = "domain.Repository.GetServicesPolicyBySource"
	operationImportBootstrapServicesPolicy    = "domain.Repository.ImportBootstrapServicesPolicy"
	operationGetWorkspacePolicy               = "domain.Repository.GetWorkspacePolicy"
	operationImportServicesPolicy             = "domain.Repository.ImportServicesPolicy"
	operationListBranchRules                  = "domain.Repository.ListBranchRules"
	operationListDocumentationSources         = "domain.Repository.ListDocumentationSources"
	operationListPlacementPolicies            = "domain.Repository.ListPlacementPolicies"
	operationListPolicyOverrides              = "domain.Repository.ListPolicyOverrides"
	operationListProjects                     = "domain.Repository.ListProjects"
	operationListReleaseLines                 = "domain.Repository.ListReleaseLines"
	operationListReleasePolicies              = "domain.Repository.ListReleasePolicies"
	operationListRepositories                 = "domain.Repository.ListRepositories"
	operationListServiceDescriptors           = "domain.Repository.ListServiceDescriptors"
	operationMarkOutboxEventFailed            = "domain.Repository.MarkOutboxEventFailed"
	operationMarkOutboxEventPermanentlyFailed = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationMarkOutboxEventPublished         = "domain.Repository.MarkOutboxEventPublished"
	operationRecordOnboardingSignal           = "domain.Repository.RecordOnboardingSignalReconciliation"
	operationPutBranchRules                   = "domain.Repository.PutBranchRules"
	operationPutDocumentationSource           = "domain.Repository.PutDocumentationSource"
	operationPutPlacementPolicy               = "domain.Repository.PutPlacementPolicy"
	operationPutReleaseLine                   = "domain.Repository.PutReleaseLine"
	operationPutReleasePolicy                 = "domain.Repository.PutReleasePolicy"
	operationReserveRepositoryBinding         = "domain.Repository.ReserveRepositoryBinding"
	operationUpdateProject                    = "domain.Repository.UpdateProject"
	operationUpdateRepository                 = "domain.Repository.UpdateRepository"
)

// NewRepository creates a PostgreSQL-backed project repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) CreateProject(ctx context.Context, project entity.Project, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationCreateProject, event, insertMutation(queryProjectCreate, projectArgs(project)), result)
}

func (r *Repository) UpdateProject(ctx context.Context, project entity.Project, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return r.updateWithOptionalCommand(ctx, operationUpdateProject, event, result, projectUpdateMutation(project, previousVersion))
}

func (r *Repository) GetProject(ctx context.Context, id uuid.UUID) (entity.Project, error) {
	return queryOne(ctx, r.db, operationGetProject, queryProjectGetByID, pgx.NamedArgs{"id": id}, scanProject)
}

func (r *Repository) ListProjects(ctx context.Context, filter query.ProjectFilter) ([]entity.Project, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListProjects, queryProjectList, projectFilterArgs(filter), scanProject)
}

func (r *Repository) AttachRepository(ctx context.Context, repository entity.RepositoryBinding, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationAttachRepository, event, insertMutation(queryRepositoryCreate, repositoryArgs(repository)), result)
}

func (r *Repository) ReserveRepositoryBinding(ctx context.Context, repository entity.RepositoryBinding, event entity.OutboxEvent) error {
	return r.mutateWithOutbox(ctx, operationReserveRepositoryBinding, event, insertMutation(queryRepositoryCreate, repositoryArgs(repository)))
}

func (r *Repository) UpdateRepository(ctx context.Context, repository entity.RepositoryBinding, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	update := repositoryUpdateMutation(repository, previousVersion)
	return r.updateWithOptionalCommand(ctx, operationUpdateRepository, event, result, update)
}

func (r *Repository) GetRepository(ctx context.Context, id uuid.UUID) (entity.RepositoryBinding, error) {
	return queryOne(ctx, r.db, operationGetRepository, queryRepositoryGetByID, pgx.NamedArgs{"id": id}, scanRepository)
}

func (r *Repository) GetRepositoryByProviderRef(ctx context.Context, provider enum.RepositoryProvider, owner string, name string) (entity.RepositoryBinding, error) {
	return queryOne(ctx, r.db, operationGetRepositoryByProviderRef, queryRepositoryGetByProviderRef, pgx.NamedArgs{
		"provider":       string(provider),
		"provider_owner": owner,
		"provider_name":  name,
	}, scanRepository)
}

func (r *Repository) ListRepositories(ctx context.Context, filter query.RepositoryFilter) ([]entity.RepositoryBinding, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListRepositories, queryRepositoryList, repositoryFilterArgs(filter), scanRepository)
}

func (r *Repository) RecordOnboardingSignalReconciliation(ctx context.Context, signal entity.OnboardingSignalReconciliation) (entity.OnboardingSignalReconciliation, error) {
	stored, err := queryOne(ctx, r.db, operationRecordOnboardingSignal, queryOnboardingSignalReconciliationUpsert, onboardingSignalReconciliationArgs(signal), scanOnboardingSignalReconciliation)
	if errors.Is(err, errs.ErrNotFound) {
		return entity.OnboardingSignalReconciliation{}, errs.ErrConflict
	}
	return stored, err
}

func (r *Repository) ImportServicesPolicy(ctx context.Context, policy entity.ServicesPolicy, descriptors []entity.ServiceDescriptor, documentationSources []entity.DocumentationSource, result entity.CommandResult, buildEvent projectrepo.ServicesPolicyEventBuilder) (entity.ServicesPolicy, error) {
	if buildEvent == nil {
		return entity.ServicesPolicy{}, errs.ErrInvalidArgument
	}
	var imported entity.ServicesPolicy
	err := r.withTx(ctx, operationImportServicesPolicy, func(tx pgx.Tx) error {
		policyVersion, err := nextServicesPolicyVersion(ctx, tx, policy.ProjectID)
		if err != nil {
			return err
		}
		policy.PolicyVersion = policyVersion
		event, err := buildEvent(policy)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryServicesPolicyInsert, servicesPolicyArgs(policy)); err != nil {
			return err
		}
		if err := applyActiveServicesPolicyProjection(ctx, tx, policy, descriptors, documentationSources); err != nil {
			return err
		}
		if err := insertOutboxEvent(ctx, tx, event); err != nil {
			return err
		}
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, commandResultMutation(result)); err != nil {
			return err
		}
		imported = policy
		return nil
	})
	if err != nil {
		return entity.ServicesPolicy{}, err
	}
	return imported, nil
}

func (r *Repository) ImportBootstrapServicesPolicy(
	ctx context.Context,
	repository entity.RepositoryBinding,
	previousVersion int64,
	policy entity.ServicesPolicy,
	descriptors []entity.ServiceDescriptor,
	documentationSources []entity.DocumentationSource,
	repositoryEvent entity.OutboxEvent,
	result entity.CommandResult,
	buildPolicyEvent projectrepo.ServicesPolicyEventBuilder,
) (entity.ServicesPolicy, entity.RepositoryBinding, error) {
	if buildPolicyEvent == nil {
		return entity.ServicesPolicy{}, entity.RepositoryBinding{}, errs.ErrInvalidArgument
	}
	var imported entity.ServicesPolicy
	err := r.withTx(ctx, operationImportBootstrapServicesPolicy, func(tx pgx.Tx) error {
		policyVersion, err := nextServicesPolicyVersion(ctx, tx, policy.ProjectID)
		if err != nil {
			return err
		}
		policy.PolicyVersion = policyVersion
		policyEvent, err := buildPolicyEvent(policy)
		if err != nil {
			return err
		}
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, repositoryUpdateMutation(repository, previousVersion)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryServicesPolicyInsert, servicesPolicyArgs(policy)); err != nil {
			return err
		}
		if err := applyActiveServicesPolicyProjection(ctx, tx, policy, descriptors, documentationSources); err != nil {
			return err
		}
		if err := insertOutboxEvent(ctx, tx, repositoryEvent); err != nil {
			return err
		}
		if err := insertOutboxEvent(ctx, tx, policyEvent); err != nil {
			return err
		}
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, commandResultMutation(result)); err != nil {
			return err
		}
		imported = policy
		return nil
	})
	if err != nil {
		return entity.ServicesPolicy{}, entity.RepositoryBinding{}, err
	}
	return imported, repository, nil
}

func applyActiveServicesPolicyProjection(ctx context.Context, tx pgx.Tx, policy entity.ServicesPolicy, descriptors []entity.ServiceDescriptor, documentationSources []entity.DocumentationSource) error {
	if !isActiveServicesPolicy(policy) {
		return nil
	}
	if _, err := tx.Exec(ctx, queryServiceDescriptorMarkProjectStale, pgx.NamedArgs{"project_id": policy.ProjectID, "updated_at": policy.UpdatedAt}); err != nil {
		return err
	}
	if err := insertServiceDescriptors(ctx, tx, descriptors); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryDocumentationSourceMarkPolicyManagedDisabled, pgx.NamedArgs{"project_id": policy.ProjectID, "updated_at": policy.UpdatedAt}); err != nil {
		return err
	}
	return upsertPolicyDocumentationSources(ctx, tx, documentationSources)
}

func nextServicesPolicyVersion(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, queryServicesPolicyNextVersion, pgx.NamedArgs{"project_id": projectID}).Scan(&version)
	return version, err
}

func insertServiceDescriptors(ctx context.Context, tx pgx.Tx, descriptors []entity.ServiceDescriptor) (err error) {
	return execBatch(ctx, tx, descriptors, func(batch *pgx.Batch, descriptor entity.ServiceDescriptor) {
		batch.Queue(queryServiceDescriptorInsert, serviceDescriptorArgs(descriptor))
	})
}

func upsertPolicyDocumentationSources(ctx context.Context, tx pgx.Tx, sources []entity.DocumentationSource) error {
	return execBatch(ctx, tx, sources, func(batch *pgx.Batch, source entity.DocumentationSource) {
		batch.Queue(queryDocumentationSourceUpsertPolicy, documentationSourceArgs(source))
	})
}

func execBatch[T any](ctx context.Context, tx pgx.Tx, items []T, queue func(*pgx.Batch, T)) (err error) {
	if queue == nil {
		return errs.ErrInvalidArgument
	}
	if len(items) == 0 {
		return nil
	}
	var batch pgx.Batch
	for _, item := range items {
		queue(&batch, item)
	}
	results := tx.SendBatch(ctx, &batch)
	defer func() {
		if closeErr := results.Close(); err == nil {
			err = closeErr
		}
	}()
	for range items {
		if _, err = results.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func isActiveServicesPolicy(policy entity.ServicesPolicy) bool {
	if policy.ValidationStatus != enum.ServicesPolicyValidationValid {
		return false
	}
	return policy.ProjectionStatus == enum.ServicesPolicyProjectionSynced || policy.ProjectionStatus == enum.ServicesPolicyProjectionOverridden
}

func (r *Repository) GetServicesPolicy(ctx context.Context, projectID uuid.UUID, policyID *uuid.UUID) (entity.ServicesPolicy, error) {
	args := pgx.NamedArgs{"project_id": projectID}
	if policyID != nil {
		args["id"] = *policyID
		return queryOne(ctx, r.db, operationGetServicesPolicy, queryServicesPolicyGetByID, args, scanServicesPolicy)
	}
	return queryOne(ctx, r.db, operationGetServicesPolicy, queryServicesPolicyGetActive, args, scanServicesPolicy)
}

func (r *Repository) GetServicesPolicyBySource(ctx context.Context, projectID uuid.UUID, sourceRepositoryID uuid.UUID, sourcePath string, sourceCommitSHA string) (entity.ServicesPolicy, error) {
	return queryOne(ctx, r.db, operationGetServicesPolicyBySource, queryServicesPolicyGetBySource, pgx.NamedArgs{
		"project_id":           projectID,
		"source_repository_id": sourceRepositoryID,
		"source_path":          sourcePath,
		"source_commit_sha":    sourceCommitSHA,
	}, scanServicesPolicy)
}

func (r *Repository) ListServiceDescriptors(ctx context.Context, filter query.ServiceDescriptorFilter) ([]entity.ServiceDescriptor, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListServiceDescriptors, queryServiceDescriptorList, serviceDescriptorFilterArgs(filter), scanServiceDescriptor)
}

func (r *Repository) CreatePolicyEditProposal(ctx context.Context, proposal entity.PolicyEditProposal, result entity.CommandResult) error {
	return r.mutate(ctx, operationCreatePolicyEditProposal, insertMutation(queryPolicyEditProposalCreate, policyEditProposalArgs(proposal)), commandResultMutation(result))
}

func (r *Repository) GetPolicyEditProposal(ctx context.Context, id uuid.UUID) (entity.PolicyEditProposal, error) {
	return queryOne(ctx, r.db, operationGetPolicyEditProposal, queryPolicyEditProposalGetByID, pgx.NamedArgs{"id": id}, scanPolicyEditProposal)
}

func (r *Repository) CreatePolicyOverride(ctx context.Context, override entity.PolicyOverride, event entity.OutboxEvent, result entity.CommandResult) error {
	return r.createWithCommandResult(ctx, operationCreatePolicyOverride, event, affectedMutation(queryPolicyOverrideCreate, policyOverrideArgs(override)), result)
}

func (r *Repository) CancelPolicyOverride(ctx context.Context, override entity.PolicyOverride, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	mutations := []mutation{policyOverrideCancelMutation(override, previousVersion)}
	mutations = appendOptionalCommandResult(mutations, result)
	return r.mutateWithOutbox(ctx, operationCancelPolicyOverride, event, mutations...)
}

func (r *Repository) GetPolicyOverride(ctx context.Context, id uuid.UUID) (entity.PolicyOverride, error) {
	return queryOne(ctx, r.db, operationGetPolicyOverride, queryPolicyOverrideGetByID, pgx.NamedArgs{"id": id}, scanPolicyOverride)
}

func (r *Repository) ListPolicyOverrides(ctx context.Context, filter query.PolicyOverrideFilter) ([]entity.PolicyOverride, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListPolicyOverrides, queryPolicyOverrideList, policyOverrideFilterArgs(filter), scanPolicyOverride)
}

func (r *Repository) PutDocumentationSource(ctx context.Context, source entity.DocumentationSource, previousVersion *int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	return r.putWithCommandResult(ctx, operationPutDocumentationSource, event, documentationSourceMutation(source, previousVersion), result)
}

func (r *Repository) GetDocumentationSource(ctx context.Context, id uuid.UUID) (entity.DocumentationSource, error) {
	return queryOne(ctx, r.db, operationGetDocumentationSource, queryDocumentationSourceGetByID, pgx.NamedArgs{"id": id}, scanDocumentationSource)
}

func (r *Repository) ListDocumentationSources(ctx context.Context, filter query.DocumentationSourceFilter) ([]entity.DocumentationSource, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListDocumentationSources, queryDocumentationSourceList, documentationSourceFilterArgs(filter), scanDocumentationSource)
}

func (r *Repository) GetWorkspacePolicy(ctx context.Context, filter query.WorkspacePolicyFilter) (entity.WorkspacePolicy, error) {
	args := pgx.NamedArgs{
		"project_id":     filter.ProjectID,
		"repository_ids": postgreslib.UUIDValues(filter.RepositoryIDs),
		"service_keys":   postgreslib.StringValues(filter.ServiceKeys),
	}
	codeSources, err := queryMany(ctx, r.db, operationGetWorkspacePolicy, queryWorkspaceCodeSourceList, args, scanWorkspaceCodeSource)
	if err != nil {
		return entity.WorkspacePolicy{}, err
	}
	documentationSources, err := queryMany(ctx, r.db, operationGetWorkspacePolicy, queryWorkspaceDocumentationSourceList, args, scanWorkspaceDocumentationSource)
	if err != nil {
		return entity.WorkspacePolicy{}, err
	}
	activeOverrides, _, err := r.ListPolicyOverrides(ctx, query.PolicyOverrideFilter{
		ProjectID:  filter.ProjectID,
		ActiveOnly: true,
	})
	if err != nil {
		return entity.WorkspacePolicy{}, err
	}
	policyVersion := int64(0)
	policy, err := r.GetServicesPolicy(ctx, filter.ProjectID, nil)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.WorkspacePolicy{}, err
	}
	if err == nil {
		policyVersion = policy.PolicyVersion
	}
	result := entity.WorkspacePolicy{
		ProjectID:             filter.ProjectID,
		CodeSources:           codeSources,
		DocumentationSources:  documentationSources,
		ActivePolicyOverrides: activeOverrides,
		PolicyVersion:         policyVersion,
	}
	if filter.IncludeGuidancePackages {
		refs, err := queryManyRowTo(ctx, r.db, operationGetWorkspacePolicy, queryWorkspaceGuidanceRefList, pgx.NamedArgs{"project_id": filter.ProjectID}, pgx.RowTo[string])
		if err != nil {
			return entity.WorkspacePolicy{}, err
		}
		result.GuidancePackageRefs = refs
	}
	return result, nil
}

func (r *Repository) PutBranchRules(ctx context.Context, rules entity.BranchRules, previousVersion *int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	change := branchRulesMutation(rules, previousVersion)
	return r.putWithCommandResult(ctx, operationPutBranchRules, event, change, result)
}

func (r *Repository) GetBranchRules(ctx context.Context, id uuid.UUID) (entity.BranchRules, error) {
	return queryOne(ctx, r.db, operationGetBranchRules, queryBranchRulesGetByID, pgx.NamedArgs{"id": id}, scanBranchRules)
}

func (r *Repository) ListBranchRules(ctx context.Context, filter query.BranchRulesFilter) ([]entity.BranchRules, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListBranchRules, queryBranchRulesList, branchRulesFilterArgs(filter), scanBranchRules)
}

func (r *Repository) PutReleasePolicy(ctx context.Context, policy entity.ReleasePolicy, previousVersion *int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	change := releasePolicyMutation(policy, previousVersion)
	err := r.putWithCommandResult(ctx, operationPutReleasePolicy, event, change, result)
	return err
}

func (r *Repository) GetReleasePolicy(ctx context.Context, id uuid.UUID) (entity.ReleasePolicy, error) {
	return queryOne(ctx, r.db, operationGetReleasePolicy, queryReleasePolicyGetByID, pgx.NamedArgs{"id": id}, scanReleasePolicy)
}

func (r *Repository) ListReleasePolicies(ctx context.Context, filter query.ReleasePolicyFilter) ([]entity.ReleasePolicy, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListReleasePolicies, queryReleasePolicyList, releasePolicyFilterArgs(filter), scanReleasePolicy)
}

func (r *Repository) PutReleaseLine(ctx context.Context, line entity.ReleaseLine, previousVersion *int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	if previousVersion == nil {
		return r.putWithCommandResult(ctx, operationPutReleaseLine, event, releaseLineMutation(line, nil), result)
	}
	return r.putWithCommandResult(ctx, operationPutReleaseLine, event, releaseLineMutation(line, previousVersion), result)
}

func (r *Repository) GetReleaseLine(ctx context.Context, id uuid.UUID) (entity.ReleaseLine, error) {
	return queryOne(ctx, r.db, operationGetReleaseLine, queryReleaseLineGetByID, pgx.NamedArgs{"id": id}, scanReleaseLine)
}

func (r *Repository) ListReleaseLines(ctx context.Context, filter query.ReleaseLineFilter) ([]entity.ReleaseLine, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListReleaseLines, queryReleaseLineList, releaseLineFilterArgs(filter), scanReleaseLine)
}

func (r *Repository) PutPlacementPolicy(ctx context.Context, policy entity.PlacementPolicy, previousVersion *int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	change := placementPolicyMutation(policy, previousVersion)
	mutations := []mutation{change}
	mutations = appendOptionalCommandResult(mutations, result)
	return r.mutateWithOutbox(ctx, operationPutPlacementPolicy, event, mutations...)
}

func (r *Repository) GetPlacementPolicy(ctx context.Context, id uuid.UUID) (entity.PlacementPolicy, error) {
	return queryOne(ctx, r.db, operationGetPlacementPolicy, queryPlacementPolicyGetByID, pgx.NamedArgs{"id": id}, scanPlacementPolicy)
}

func (r *Repository) ListPlacementPolicies(ctx context.Context, filter query.PlacementPolicyFilter) ([]entity.PlacementPolicy, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListPlacementPolicies, queryPlacementPolicyList, placementPolicyFilterArgs(filter), scanPlacementPolicy)
}

func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	args, ok := postgreslib.OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, wrapError(operationClaimOutboxEvents, errs.ErrInvalidArgument)
	}
	events, err := queryMany(ctx, r.db, operationClaimOutboxEvents, queryOutboxEventClaim, args, scanOutboxEvent)
	if err != nil {
		return nil, err
	}
	return events, nil
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	return r.markProjectOutboxPublished(ctx, id, attemptCount, publishedAt)
}

func (r *Repository) markProjectOutboxPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	ok, err := postgreslib.ExecOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, id, attemptCount, publishedAt)
	if ok {
		return wrapError(operationMarkOutboxEventPublished, err)
	}
	return wrapError(operationMarkOutboxEventPublished, errs.ErrInvalidArgument)
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markProjectOutboxFailure(ctx, operationMarkOutboxEventFailed, queryOutboxEventMarkFailed, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError)
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markProjectOutboxFailure(ctx, operationMarkOutboxEventPermanentlyFailed, queryOutboxEventMarkPermanentlyFailed, id, attemptCount, "failed_permanently_at", failedAt, lastError)
}

func (r *Repository) markProjectOutboxFailure(ctx context.Context, operation string, sql string, id uuid.UUID, attempts int, timestampColumn string, timestamp time.Time, message string) error {
	ok, err := postgreslib.ExecOutboxDeliveryFailure(ctx, r.db, sql, id, attempts, timestampColumn, timestamp, message)
	if ok {
		return wrapError(operation, err)
	}
	return wrapError(operation, errs.ErrInvalidArgument)
}

func (r *Repository) withTx(ctx context.Context, operation string, fn func(tx pgx.Tx) error) error {
	return wrapError(operation, postgreslib.WithTx(ctx, r.db, fn))
}

type mutation = postgreslib.Mutation

func (r *Repository) mutate(ctx context.Context, operation string, mutations ...mutation) error {
	return r.withTx(ctx, operation, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
}

func (r *Repository) mutateWithOutbox(ctx context.Context, operation string, event entity.OutboxEvent, mutations ...mutation) error {
	mutations = append(mutations, mutation{Query: queryOutboxEventCreate, Args: outboxEventArgs(event), RequireAffected: true})
	return r.mutate(ctx, operation, mutations...)
}

func (r *Repository) createWithCommandResult(ctx context.Context, operation string, event entity.OutboxEvent, create mutation, result entity.CommandResult) error {
	return r.mutateWithOutbox(ctx, operation, event, create, commandResultMutation(result))
}

func (r *Repository) putWithCommandResult(ctx context.Context, operation string, event entity.OutboxEvent, put mutation, result *entity.CommandResult) error {
	mutations := []mutation{put}
	mutations = appendOptionalCommandResult(mutations, result)
	return r.mutateWithOutbox(ctx, operation, event, mutations...)
}

func (r *Repository) updateWithOptionalCommand(ctx context.Context, operation string, event entity.OutboxEvent, result *entity.CommandResult, update mutation) error {
	return r.putWithCommandResult(ctx, operation, event, update, result)
}

func commandResultMutation(result entity.CommandResult) mutation {
	return affectedMutation(queryCommandResultCreate, commandResultArgs(result))
}

func appendOptionalCommandResult(mutations []mutation, result *entity.CommandResult) []mutation {
	if result == nil {
		return mutations
	}
	return append(mutations, commandResultMutation(*result))
}

func insertOutboxEvent(ctx context.Context, db execer, event entity.OutboxEvent) error {
	return postgreslib.RunMutation(ctx, db, errs.ErrConflict, affectedMutation(queryOutboxEventCreate, outboxEventArgs(event)))
}

func queryOne[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	value, err := scan(db.QueryRow(ctx, sql, args))
	return value, wrapError(operation, err)
}

func queryMany[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, sql, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	return items, wrapError(operation, err)
}

func queryManyRowTo[T any](ctx context.Context, db queryer, operation string, sql string, args pgx.NamedArgs, scan pgx.RowToFunc[T]) ([]T, error) {
	rows, err := db.Query(ctx, sql, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	items, err := pgx.CollectRows(rows, scan)
	return items, wrapError(operation, err)
}

func queryPage[T any](ctx context.Context, db queryer, operation string, sql string, paging pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, query.PageResult, error) {
	items, err := queryMany(ctx, db, operation, sql, paging.args, scan)
	if err != nil {
		return nil, query.PageResult{}, err
	}
	values, page := pageResult(items, paging.limit, paging.nextOffset)
	return values, page, nil
}

func insertMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args}
}

func affectedMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args, RequireAffected: true}
}

func projectUpdateMutation(project entity.Project, previousVersion int64) mutation {
	return affectedMutation(queryProjectUpdate, projectUpdateArgs(project, previousVersion))
}

func repositoryUpdateMutation(repository entity.RepositoryBinding, previousVersion int64) mutation {
	return affectedMutation(queryRepositoryUpdate, repositoryUpdateArgs(repository, previousVersion))
}

func versionedPutMutation(createQuery string, updateQuery string, args pgx.NamedArgs, previousVersion *int64) mutation {
	if previousVersion == nil {
		return affectedMutation(createQuery, args)
	}
	args["previous_version"] = *previousVersion
	return affectedMutation(updateQuery, args)
}

func documentationSourceMutation(source entity.DocumentationSource, previousVersion *int64) mutation {
	return versionedPutMutation(queryDocumentationSourceCreate, queryDocumentationSourceUpdate, documentationSourceArgs(source), previousVersion)
}

func branchRulesMutation(rules entity.BranchRules, previousVersion *int64) mutation {
	return versionedPutMutation(queryBranchRulesCreate, queryBranchRulesUpdate, branchRulesArgs(rules), previousVersion)
}

func releasePolicyMutation(policy entity.ReleasePolicy, previousVersion *int64) mutation {
	return versionedPutMutation(queryReleasePolicyCreate, queryReleasePolicyUpdate, releasePolicyArgs(policy), previousVersion)
}

func releaseLineMutation(line entity.ReleaseLine, previousVersion *int64) mutation {
	return versionedPutMutation(queryReleaseLineCreate, queryReleaseLineUpdate, releaseLineArgs(line), previousVersion)
}

func placementPolicyMutation(policy entity.PlacementPolicy, previousVersion *int64) mutation {
	return versionedPutMutation(queryPlacementPolicyCreate, queryPlacementPolicyUpdate, placementPolicyArgs(policy), previousVersion)
}

func policyOverrideCancelMutation(override entity.PolicyOverride, previousVersion int64) mutation {
	args := policyOverrideArgs(override)
	args["previous_version"] = previousVersion
	return affectedMutation(queryPolicyOverrideCancel, args)
}
