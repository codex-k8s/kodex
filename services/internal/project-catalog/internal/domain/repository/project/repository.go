// Package project defines persistence ports owned by the project catalog domain service.
package project

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
)

// Repository is the domain persistence contract for project-catalog use cases.
type Repository interface {
	// GetCommandResult returns a previously applied idempotent command result.
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	// CreateProject stores a project, outbox event and command result atomically.
	CreateProject(ctx context.Context, project entity.Project, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateProject updates a project with optimistic concurrency.
	UpdateProject(ctx context.Context, project entity.Project, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetProject returns a project by id.
	GetProject(ctx context.Context, id uuid.UUID) (entity.Project, error)
	// ListProjects returns projects matching filter.
	ListProjects(ctx context.Context, filter query.ProjectFilter) ([]entity.Project, query.PageResult, error)
	// AttachRepository stores a repository binding, outbox event and command result atomically.
	AttachRepository(ctx context.Context, repository entity.RepositoryBinding, event entity.OutboxEvent, result entity.CommandResult) error
	// UpdateRepository updates a repository binding with optimistic concurrency.
	UpdateRepository(ctx context.Context, repository entity.RepositoryBinding, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetRepository returns a repository binding by id.
	GetRepository(ctx context.Context, id uuid.UUID) (entity.RepositoryBinding, error)
	// ListRepositories returns repository bindings matching filter.
	ListRepositories(ctx context.Context, filter query.RepositoryFilter) ([]entity.RepositoryBinding, query.PageResult, error)
	// ImportServicesPolicy stores checked policy, descriptors, outbox event and command result atomically.
	ImportServicesPolicy(ctx context.Context, policy entity.ServicesPolicy, descriptors []entity.ServiceDescriptor, event entity.OutboxEvent, result entity.CommandResult) error
	// GetServicesPolicy returns active or concrete checked services policy.
	GetServicesPolicy(ctx context.Context, projectID uuid.UUID, policyID *uuid.UUID) (entity.ServicesPolicy, error)
	// ListServiceDescriptors returns typed descriptors matching filter.
	ListServiceDescriptors(ctx context.Context, filter query.ServiceDescriptorFilter) ([]entity.ServiceDescriptor, query.PageResult, error)
	// CreatePolicyEditProposal stores a request to change services.yaml through provider PR.
	CreatePolicyEditProposal(ctx context.Context, proposal entity.PolicyEditProposal, result entity.CommandResult) error
	// CreatePolicyOverride stores an emergency override and its outbox event.
	CreatePolicyOverride(ctx context.Context, override entity.PolicyOverride, event entity.OutboxEvent, result entity.CommandResult) error
	// PutDocumentationSource stores a documentation source and its outbox event.
	PutDocumentationSource(ctx context.Context, source entity.DocumentationSource, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetDocumentationSource returns a documentation source by id.
	GetDocumentationSource(ctx context.Context, id uuid.UUID) (entity.DocumentationSource, error)
	// ListDocumentationSources returns documentation sources matching filter.
	ListDocumentationSources(ctx context.Context, filter query.DocumentationSourceFilter) ([]entity.DocumentationSource, query.PageResult, error)
	// GetWorkspacePolicy returns allowed source set for an agent workspace.
	GetWorkspacePolicy(ctx context.Context, filter query.WorkspacePolicyFilter) (entity.WorkspacePolicy, error)
	// PutBranchRules stores branch rules and its outbox event.
	PutBranchRules(ctx context.Context, rules entity.BranchRules, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetBranchRules returns branch rules by id.
	GetBranchRules(ctx context.Context, id uuid.UUID) (entity.BranchRules, error)
	// ListBranchRules returns branch rules matching filter.
	ListBranchRules(ctx context.Context, filter query.BranchRulesFilter) ([]entity.BranchRules, query.PageResult, error)
	// PutReleasePolicy stores release policy and its outbox event.
	PutReleasePolicy(ctx context.Context, policy entity.ReleasePolicy, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetReleasePolicy returns release policy by id.
	GetReleasePolicy(ctx context.Context, id uuid.UUID) (entity.ReleasePolicy, error)
	// ListReleasePolicies returns release policies matching filter.
	ListReleasePolicies(ctx context.Context, filter query.ReleasePolicyFilter) ([]entity.ReleasePolicy, query.PageResult, error)
	// PutReleaseLine stores release line and its outbox event.
	PutReleaseLine(ctx context.Context, line entity.ReleaseLine, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetReleaseLine returns release line by id.
	GetReleaseLine(ctx context.Context, id uuid.UUID) (entity.ReleaseLine, error)
	// ListReleaseLines returns release lines matching filter.
	ListReleaseLines(ctx context.Context, filter query.ReleaseLineFilter) ([]entity.ReleaseLine, query.PageResult, error)
	// PutPlacementPolicy stores placement policy and its outbox event.
	PutPlacementPolicy(ctx context.Context, policy entity.PlacementPolicy, event entity.OutboxEvent, result *entity.CommandResult) error
	// GetPlacementPolicy returns placement policy by id.
	GetPlacementPolicy(ctx context.Context, id uuid.UUID) (entity.PlacementPolicy, error)
	// ListPlacementPolicies returns placement policies matching filter.
	ListPlacementPolicies(ctx context.Context, filter query.PlacementPolicyFilter) ([]entity.PlacementPolicy, query.PageResult, error)
}

// Clock provides deterministic time for domain commands and tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides aggregate and event identifiers for domain commands.
type IDGenerator interface {
	New() uuid.UUID
}
