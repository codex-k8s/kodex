package project

import (
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

func projectArgs(project entity.Project) pgx.NamedArgs {
	return withBaseArgs(project.Base, pgx.NamedArgs{
		"organization_id": project.OrganizationID,
		"slug":            project.Slug,
		"display_name":    project.DisplayName,
		"description":     project.Description,
		"icon_object_uri": project.IconObjectURI,
		"status":          string(project.Status),
	})
}

func projectUpdateArgs(project entity.Project, previousVersion int64) pgx.NamedArgs {
	args := projectArgs(project)
	args["previous_version"] = previousVersion
	return args
}

func repositoryArgs(repository entity.RepositoryBinding) pgx.NamedArgs {
	return withBaseArgs(repository.Base, pgx.NamedArgs{
		"project_id":             repository.ProjectID,
		"provider":               string(repository.Provider),
		"provider_owner":         repository.ProviderOwner,
		"provider_name":          repository.ProviderName,
		"web_url":                repository.WebURL,
		"default_branch":         repository.DefaultBranch,
		"status":                 string(repository.Status),
		"provider_repository_id": repository.ProviderRepositoryID,
		"icon_object_uri":        repository.IconObjectURI,
	})
}

func repositoryUpdateArgs(repository entity.RepositoryBinding, previousVersion int64) pgx.NamedArgs {
	args := repositoryArgs(repository)
	args["previous_version"] = previousVersion
	return args
}

func servicesPolicyArgs(policy entity.ServicesPolicy) pgx.NamedArgs {
	return withBaseArgs(policy.Base, pgx.NamedArgs{
		"project_id":           policy.ProjectID,
		"source_repository_id": postgreslib.NullableUUID(policy.SourceRepositoryID),
		"source_path":          policy.SourcePath,
		"source_ref":           policy.SourceRef,
		"source_commit_sha":    policy.SourceCommitSHA,
		"source_blob_sha":      policy.SourceBlobSHA,
		"policy_version":       policy.PolicyVersion,
		"content_hash":         policy.ContentHash,
		"validated_payload":    postgreslib.JSONPayload(policy.ValidatedPayload),
		"validation_status":    string(policy.ValidationStatus),
		"projection_status":    string(policy.ProjectionStatus),
		"imported_at":          policy.ImportedAt,
	})
}

func serviceDescriptorArgs(descriptor entity.ServiceDescriptor) pgx.NamedArgs {
	return withBaseArgs(descriptor.Base, pgx.NamedArgs{
		"project_id":              descriptor.ProjectID,
		"services_policy_id":      descriptor.ServicesPolicyID,
		"repository_id":           postgreslib.NullableUUID(descriptor.RepositoryID),
		"service_key":             descriptor.ServiceKey,
		"display_name":            descriptor.DisplayName,
		"kind":                    string(descriptor.Kind),
		"root_path":               descriptor.RootPath,
		"documentation_scope_id":  descriptor.DocumentationScopeID,
		"depends_on_service_keys": descriptor.DependsOnServiceKeys,
		"status":                  string(descriptor.Status),
	})
}

func documentationSourceArgs(source entity.DocumentationSource) pgx.NamedArgs {
	return withBaseArgs(source.Base, pgx.NamedArgs{
		"project_id":    source.ProjectID,
		"repository_id": postgreslib.NullableUUID(source.RepositoryID),
		"scope_type":    string(source.ScopeType),
		"scope_id":      source.ScopeID,
		"local_path":    source.LocalPath,
		"access_mode":   string(source.AccessMode),
		"status":        string(source.Status),
	})
}

func branchRulesArgs(rules entity.BranchRules) pgx.NamedArgs {
	return withBaseArgs(rules.Base, pgx.NamedArgs{
		"project_id":      rules.ProjectID,
		"repository_id":   postgreslib.NullableUUID(rules.RepositoryID),
		"pattern":         rules.Pattern,
		"required_checks": rules.RequiredChecks,
		"merge_policy":    string(rules.MergePolicy),
		"status":          string(rules.Status),
	})
}

func releasePolicyArgs(policy entity.ReleasePolicy) pgx.NamedArgs {
	return withBaseArgs(policy.Base, pgx.NamedArgs{
		"project_id":       policy.ProjectID,
		"name":             policy.Name,
		"branch_pattern":   policy.BranchPattern,
		"rollout_strategy": string(policy.RolloutStrategy),
		"rollback_policy":  string(policy.RollbackPolicy),
		"risk_profile_ref": policy.RiskProfileRef,
		"status":           string(policy.Status),
	})
}

func releaseLineArgs(line entity.ReleaseLine) pgx.NamedArgs {
	return withBaseArgs(line.Base, pgx.NamedArgs{
		"project_id":        line.ProjectID,
		"release_policy_id": line.ReleasePolicyID,
		"name":              line.Name,
		"branch_pattern":    line.BranchPattern,
		"status":            string(line.Status),
	})
}

func placementPolicyArgs(policy entity.PlacementPolicy) pgx.NamedArgs {
	return withBaseArgs(policy.Base, pgx.NamedArgs{
		"project_id":           policy.ProjectID,
		"repository_id":        postgreslib.NullableUUID(policy.RepositoryID),
		"service_key":          policy.ServiceKey,
		"allowed_cluster_refs": policy.AllowedClusterRefs,
		"status":               string(policy.Status),
	})
}

func policyOverrideArgs(override entity.PolicyOverride) pgx.NamedArgs {
	return withBaseArgs(override.Base, pgx.NamedArgs{
		"project_id":           override.ProjectID,
		"target_type":          string(override.TargetType),
		"target_id":            postgreslib.NullableUUID(override.TargetID),
		"payload":              postgreslib.JSONPayload(override.Payload),
		"reason":               override.Reason,
		"status":               string(override.Status),
		"expires_at":           override.ExpiresAt,
		"created_by_actor_ref": override.CreatedByActorRef,
	})
}

func policyEditProposalArgs(proposal entity.PolicyEditProposal) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                proposal.ID,
		"project_id":        proposal.ProjectID,
		"repository_id":     proposal.RepositoryID,
		"source_path":       proposal.SourcePath,
		"requested_changes": postgreslib.JSONPayload(proposal.RequestedChangesJSON),
		"status":            proposal.Status,
		"created_at":        proposal.CreatedAt,
	}
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"command_id":      postgreslib.NullableCommandID(identity.CommandID),
		"idempotency_key": postgreslib.IdempotencyLookupKey(identity.CommandID, identity.IdempotencyKey),
		"operation":       identity.Operation,
	}
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	return pgx.NamedArgs{
		"key":             result.Key,
		"command_id":      postgreslib.NullableCommandID(result.CommandID),
		"idempotency_key": result.IdempotencyKey,
		"operation":       result.Operation,
		"aggregate_type":  result.AggregateType,
		"aggregate_id":    result.AggregateID,
		"result_payload":  postgreslib.JSONPayload(result.ResultPayload),
		"created_at":      result.CreatedAt,
	}
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":             event.ID,
		"event_type":     event.EventType,
		"schema_version": event.SchemaVersion,
		"aggregate_type": event.AggregateType,
		"aggregate_id":   event.AggregateID,
		"payload":        postgreslib.JSONPayload(event.Payload),
		"occurred_at":    event.OccurredAt,
		"published_at":   postgreslib.NullableTime(event.PublishedAt),
	}
}

type pageQueryArgs struct {
	args       pgx.NamedArgs
	limit      int32
	nextOffset int32
}

func projectFilterArgs(filter query.ProjectFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"organization_id": postgreslib.NullableUUID(filter.OrganizationID),
		"statuses":        postgreslib.StringValues(filter.Statuses),
	})
}

func repositoryFilterArgs(filter query.RepositoryFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{"project_id": filter.ProjectID, "statuses": postgreslib.StringValues(filter.Statuses)})
}

func serviceDescriptorFilterArgs(filter query.ServiceDescriptorFilter) pageQueryArgs {
	paging := repositoryStatusesPage(filter.Page, filter.ProjectID, filter.RepositoryID, filter.Statuses)
	paging.args["service_keys"] = postgreslib.StringValues(filter.ServiceKeys)
	return paging
}

func documentationSourceFilterArgs(filter query.DocumentationSourceFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_id":    filter.ProjectID,
		"repository_id": postgreslib.NullableUUID(filter.RepositoryID),
		"scope_type":    string(filter.ScopeType),
		"scope_id":      filter.ScopeID,
		"statuses":      postgreslib.StringValues(filter.Statuses),
	})
}

func branchRulesFilterArgs(filter query.BranchRulesFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_id":    filter.ProjectID,
		"repository_id": postgreslib.NullableUUID(filter.RepositoryID),
		"statuses":      postgreslib.StringValues(filter.Statuses),
	})
}

func releasePolicyFilterArgs(filter query.ReleasePolicyFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_id": filter.ProjectID,
		"statuses":   postgreslib.StringValues(filter.Statuses),
	})
}

func releaseLineFilterArgs(filter query.ReleaseLineFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"project_id":        filter.ProjectID,
		"release_policy_id": postgreslib.NullableUUID(filter.ReleasePolicyID),
		"statuses":          postgreslib.StringValues(filter.Statuses),
	})
}

func placementPolicyFilterArgs(filter query.PlacementPolicyFilter) pageQueryArgs {
	paging := repositoryStatusesPage(filter.Page, filter.ProjectID, filter.RepositoryID, filter.Statuses)
	paging.args["service_key"] = filter.ServiceKey
	return paging
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	limit, offset, nextOffset := pageBounds(page)
	args["limit"] = limit + 1
	args["offset"] = offset
	return pageQueryArgs{args: args, limit: limit, nextOffset: nextOffset}
}

func repositoryStatusesPage[T ~string](page value.PageRequest, projectID uuid.UUID, repositoryID *uuid.UUID, statuses []T) pageQueryArgs {
	return withPage(page, pgx.NamedArgs{
		"project_id":    projectID,
		"repository_id": postgreslib.NullableUUID(repositoryID),
		"statuses":      postgreslib.StringValues(statuses),
	})
}

func pageBounds(page value.PageRequest) (limit int32, offset int32, nextOffset int32) {
	limit = page.PageSize
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	parsedOffset, err := strconv.ParseInt(page.PageToken, 10, 32)
	if err == nil && parsedOffset > 0 {
		offset = int32(parsedOffset)
	}
	return limit, offset, offset + limit
}

func pageResult[T any](items []T, limit int32, nextOffset int32) ([]T, value.PageResult) {
	if int32(len(items)) <= limit {
		return items, value.PageResult{}
	}
	return items[:len(items)-1], value.PageResult{NextPageToken: strconv.FormatInt(int64(nextOffset), 10)}
}

func withBaseArgs(base entity.Base, args pgx.NamedArgs) pgx.NamedArgs {
	args = postgreslib.AddBaseArgs(args, base.ID, base.Version, base.CreatedAt, base.UpdatedAt)
	return args
}
