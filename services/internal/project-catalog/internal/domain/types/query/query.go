// Package query contains read filters for project-catalog repositories.
package query

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// PageResult returns list continuation state.
type PageResult = value.PageResult

// CommandIdentity identifies a previously applied idempotent command.
type CommandIdentity struct {
	CommandID      uuid.UUID
	IdempotencyKey string
	Operation      string
}

// ProjectFilter selects projects for list queries.
type ProjectFilter struct {
	OrganizationID *uuid.UUID
	Statuses       []enum.ProjectStatus
	Page           value.PageRequest
}

// RepositoryFilter selects repository bindings for list queries.
type RepositoryFilter struct {
	ProjectID uuid.UUID
	Statuses  []enum.RepositoryStatus
	Page      value.PageRequest
}

// ServiceDescriptorFilter selects typed services from checked policy.
type ServiceDescriptorFilter struct {
	ProjectID        uuid.UUID
	ServicesPolicyID *uuid.UUID
	RepositoryID     *uuid.UUID
	ServiceKeys      []string
	Statuses         []enum.ServiceStatus
	Page             value.PageRequest
}

// DocumentationSourceFilter selects project documentation sources.
type DocumentationSourceFilter struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	ScopeType    enum.DocumentationScopeType
	ScopeID      string
	Statuses     []enum.DocumentationSourceStatus
	Page         value.PageRequest
}

// BranchRulesFilter selects branch rules for project or repository.
type BranchRulesFilter struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	Statuses     []enum.BranchRulesStatus
	Page         value.PageRequest
}

// ReleasePolicyFilter selects release policies for a project.
type ReleasePolicyFilter struct {
	ProjectID uuid.UUID
	Statuses  []enum.ReleasePolicyStatus
	Page      value.PageRequest
}

// ReleaseLineFilter selects release lines for a project or release policy.
type ReleaseLineFilter struct {
	ProjectID       uuid.UUID
	ReleasePolicyID *uuid.UUID
	Statuses        []enum.ReleasePolicyStatus
	Page            value.PageRequest
}

// PlacementPolicyFilter selects placement policies for project, repository or service.
type PlacementPolicyFilter struct {
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	ServiceKey   string
	Statuses     []enum.PlacementPolicyStatus
	Page         value.PageRequest
}

// WorkspacePolicyFilter selects sources for an agent workspace.
type WorkspacePolicyFilter struct {
	ProjectID               uuid.UUID
	RepositoryIDs           []uuid.UUID
	ServiceKeys             []string
	IncludeGuidancePackages bool
}

// PolicyOverrideFilter selects operator policy overrides.
type PolicyOverrideFilter struct {
	ProjectID   uuid.UUID
	TargetTypes []enum.PolicyOverrideTargetType
	TargetID    *uuid.UUID
	Statuses    []enum.PolicyOverrideStatus
	ActiveOnly  bool
	ActiveAt    *time.Time
	Page        value.PageRequest
}
