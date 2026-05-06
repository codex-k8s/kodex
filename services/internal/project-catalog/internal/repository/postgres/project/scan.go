package project

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

func scanProject(row postgreslib.RowScanner) (entity.Project, error) {
	var project entity.Project
	var status string
	err := row.Scan(
		&project.ID,
		&project.OrganizationID,
		&project.Slug,
		&project.DisplayName,
		&project.Description,
		&project.IconObjectURI,
		&status,
		&project.Version,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	project.Status = enum.ProjectStatus(status)
	return project, err
}

func scanRepository(row postgreslib.RowScanner) (entity.RepositoryBinding, error) {
	var repository entity.RepositoryBinding
	var provider, status string
	err := row.Scan(
		&repository.ID,
		&repository.ProjectID,
		&provider,
		&repository.ProviderOwner,
		&repository.ProviderName,
		&repository.WebURL,
		&repository.DefaultBranch,
		&status,
		&repository.ProviderRepositoryID,
		&repository.IconObjectURI,
		&repository.Version,
		&repository.CreatedAt,
		&repository.UpdatedAt,
	)
	repository.Provider = enum.RepositoryProvider(provider)
	repository.Status = enum.RepositoryStatus(status)
	return repository, err
}

func scanServicesPolicy(row postgreslib.RowScanner) (entity.ServicesPolicy, error) {
	var policy entity.ServicesPolicy
	var sourceRepositoryID pgtype.UUID
	var validatedPayload []byte
	var validationStatus, projectionStatus string
	err := row.Scan(
		&policy.ID,
		&policy.ProjectID,
		&sourceRepositoryID,
		&policy.SourcePath,
		&policy.SourceRef,
		&policy.SourceCommitSHA,
		&policy.SourceBlobSHA,
		&policy.PolicyVersion,
		&policy.ContentHash,
		&validatedPayload,
		&validationStatus,
		&projectionStatus,
		&policy.ImportedAt,
		&policy.Version,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)
	policy.SourceRepositoryID = postgreslib.UUIDPtrFromPG(sourceRepositoryID)
	policy.ValidatedPayload = append(policy.ValidatedPayload[:0], validatedPayload...)
	policy.ValidationStatus = enum.ServicesPolicyValidationStatus(validationStatus)
	policy.ProjectionStatus = enum.ServicesPolicyProjectionStatus(projectionStatus)
	return policy, err
}

func scanServiceDescriptor(row postgreslib.RowScanner) (entity.ServiceDescriptor, error) {
	var descriptor entity.ServiceDescriptor
	var repositoryID pgtype.UUID
	var kind, status string
	err := row.Scan(
		&descriptor.ID,
		&descriptor.ProjectID,
		&descriptor.ServicesPolicyID,
		&repositoryID,
		&descriptor.ServiceKey,
		&descriptor.DisplayName,
		&kind,
		&descriptor.RootPath,
		&descriptor.DocumentationScopeID,
		&descriptor.DependsOnServiceKeys,
		&status,
		&descriptor.Version,
		&descriptor.CreatedAt,
		&descriptor.UpdatedAt,
	)
	descriptor.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	descriptor.Kind = enum.ServiceKind(kind)
	descriptor.Status = enum.ServiceStatus(status)
	return descriptor, err
}

func scanDocumentationSource(row postgreslib.RowScanner) (entity.DocumentationSource, error) {
	var source entity.DocumentationSource
	var repositoryID pgtype.UUID
	var scopeType, accessMode, status string
	err := row.Scan(
		&source.ID,
		&source.ProjectID,
		&repositoryID,
		&scopeType,
		&source.ScopeID,
		&source.LocalPath,
		&accessMode,
		&status,
		&source.Version,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	source.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	source.ScopeType = enum.DocumentationScopeType(scopeType)
	source.AccessMode = enum.DocumentationAccessMode(accessMode)
	source.Status = enum.DocumentationSourceStatus(status)
	return source, err
}

func scanBranchRules(row postgreslib.RowScanner) (entity.BranchRules, error) {
	var rules entity.BranchRules
	var repositoryID pgtype.UUID
	var mergePolicy, status string
	err := row.Scan(
		&rules.ID,
		&rules.ProjectID,
		&repositoryID,
		&rules.Pattern,
		&rules.RequiredChecks,
		&mergePolicy,
		&status,
		&rules.Version,
		&rules.CreatedAt,
		&rules.UpdatedAt,
	)
	rules.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	rules.MergePolicy = enum.MergePolicy(mergePolicy)
	rules.Status = enum.BranchRulesStatus(status)
	return rules, err
}

func scanReleasePolicy(row postgreslib.RowScanner) (entity.ReleasePolicy, error) {
	var policy entity.ReleasePolicy
	var rolloutStrategy, rollbackPolicy, status string
	err := row.Scan(
		&policy.ID,
		&policy.ProjectID,
		&policy.Name,
		&policy.BranchPattern,
		&rolloutStrategy,
		&rollbackPolicy,
		&policy.RiskProfileRef,
		&status,
		&policy.Version,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)
	policy.RolloutStrategy = enum.RolloutStrategy(rolloutStrategy)
	policy.RollbackPolicy = enum.RollbackPolicy(rollbackPolicy)
	policy.Status = enum.ReleasePolicyStatus(status)
	return policy, err
}

func scanReleaseLine(row postgreslib.RowScanner) (entity.ReleaseLine, error) {
	var scanned releaseLineRow
	err := row.Scan(releaseLineDestinations(&scanned)...)
	return scanned.toEntity(), err
}

func releaseLineDestinations(scanned *releaseLineRow) []any {
	return []any{
		&scanned.id,
		&scanned.projectID,
		&scanned.releasePolicyID,
		&scanned.name,
		&scanned.branchPattern,
		&scanned.status,
		&scanned.version,
		&scanned.createdAt,
		&scanned.updatedAt,
	}
}

type releaseLineRow struct {
	id              uuid.UUID
	projectID       uuid.UUID
	releasePolicyID uuid.UUID
	name            string
	branchPattern   string
	status          string
	version         int64
	createdAt       time.Time
	updatedAt       time.Time
}

func (row releaseLineRow) toEntity() entity.ReleaseLine {
	return entity.ReleaseLine{
		Base: entity.Base{
			ID:        row.id,
			Version:   row.version,
			CreatedAt: row.createdAt,
			UpdatedAt: row.updatedAt,
		},
		ProjectID:       row.projectID,
		ReleasePolicyID: row.releasePolicyID,
		Name:            row.name,
		BranchPattern:   row.branchPattern,
		Status:          enum.ReleasePolicyStatus(row.status),
	}
}

func scanPlacementPolicy(row postgreslib.RowScanner) (entity.PlacementPolicy, error) {
	var policy entity.PlacementPolicy
	var repositoryID pgtype.UUID
	var status string
	err := row.Scan(
		&policy.ID,
		&policy.ProjectID,
		&repositoryID,
		&policy.ServiceKey,
		&policy.AllowedClusterRefs,
		&status,
		&policy.Version,
		&policy.CreatedAt,
		&policy.UpdatedAt,
	)
	policy.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	policy.Status = enum.PlacementPolicyStatus(status)
	return policy, err
}

func scanPolicyOverride(row postgreslib.RowScanner) (entity.PolicyOverride, error) {
	var override entity.PolicyOverride
	var targetID pgtype.UUID
	var payload []byte
	var targetType, status string
	err := row.Scan(
		&override.ID,
		&override.ProjectID,
		&targetType,
		&targetID,
		&payload,
		&override.Reason,
		&status,
		&override.ExpiresAt,
		&override.CreatedByActorRef,
		&override.Version,
		&override.CreatedAt,
		&override.UpdatedAt,
	)
	override.TargetID = postgreslib.UUIDPtrFromPG(targetID)
	override.Payload = append(override.Payload[:0], payload...)
	override.TargetType = enum.PolicyOverrideTargetType(targetType)
	override.Status = enum.PolicyOverrideStatus(status)
	return override, err
}

func scanPolicyEditProposal(row postgreslib.RowScanner) (entity.PolicyEditProposal, error) {
	var proposal entity.PolicyEditProposal
	var requestedChanges []byte
	err := row.Scan(
		&proposal.ID,
		&proposal.ProjectID,
		&proposal.RepositoryID,
		&proposal.SourcePath,
		&requestedChanges,
		&proposal.Status,
		&proposal.CreatedAt,
	)
	if err != nil {
		return entity.PolicyEditProposal{}, err
	}
	if len(requestedChanges) > 0 {
		if err := json.Unmarshal(requestedChanges, &proposal.RequestedChanges); err != nil {
			return entity.PolicyEditProposal{}, err
		}
	}
	return proposal, nil
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	var payload []byte
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.Operation,
		&result.AggregateType,
		&result.AggregateID,
		&payload,
		&result.CreatedAt,
	)
	if id := postgreslib.UUIDPtrFromPG(commandID); id != nil {
		result.CommandID = *id
	}
	result.ResultPayload = append(result.ResultPayload[:0], payload...)
	return result, err
}

func scanOutboxEvent(row postgreslib.RowScanner) (entity.OutboxEvent, error) {
	scanned, err := postgreslib.ScanOutboxEventRow(row)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	var event entity.OutboxEvent
	event.ID = scanned.Identity.RowID
	event.AggregateType = scanned.Identity.SubjectKind
	event.AggregateID = scanned.Identity.SubjectID
	event.EventType = scanned.Identity.TypeName
	event.SchemaVersion = scanned.Identity.ContractVersion
	event.Payload = scanned.Body
	event.PublishedAt = scanned.Delivery.SentAt
	event.OccurredAt = scanned.Identity.CreatedAt
	event.NextAttemptAt = scanned.Delivery.RetryAt
	event.AttemptCount = scanned.Delivery.Attempts
	event.LockedUntil = scanned.Delivery.LeaseUntil
	event.FailureKind = scanned.Failure.FailureCode
	event.FailedPermanentlyAt = scanned.Failure.DeadAt
	event.LastError = scanned.Failure.ErrorText
	return event, err
}

func scanWorkspaceCodeSource(row postgreslib.RowScanner) (entity.WorkspaceCodeSource, error) {
	var source entity.WorkspaceCodeSource
	var provider string
	err := row.Scan(
		&source.RepositoryID,
		&provider,
		&source.ProviderOwner,
		&source.ProviderName,
		&source.DefaultBranch,
	)
	source.Provider = enum.RepositoryProvider(provider)
	source.LocalPath = "src/" + source.ProviderOwner + "/" + source.ProviderName
	source.AccessMode = enum.SourceAccessWrite
	return source, err
}

func scanWorkspaceDocumentationSource(row postgreslib.RowScanner) (entity.WorkspaceDocumentationSource, error) {
	var source entity.WorkspaceDocumentationSource
	var repositoryID pgtype.UUID
	var scopeType, accessMode string
	err := row.Scan(
		&source.DocumentationSourceID,
		&repositoryID,
		&scopeType,
		&source.ScopeID,
		&source.LocalPath,
		&accessMode,
	)
	source.RepositoryID = postgreslib.UUIDPtrFromPG(repositoryID)
	source.ScopeType = enum.DocumentationScopeType(scopeType)
	source.AccessMode = enum.SourceAccessMode(accessMode)
	return source, err
}
