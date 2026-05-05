// Package entity contains persisted aggregate models owned by project-catalog.
package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

// Base stores common aggregate metadata used for optimistic concurrency.
type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Project represents a product or delivery scope owned by an organization.
type Project struct {
	Base
	OrganizationID uuid.UUID
	Slug           string
	DisplayName    string
	Description    string
	IconObjectURI  string
	Status         enum.ProjectStatus
}

// RepositoryBinding links a provider repository to a project.
type RepositoryBinding struct {
	Base
	ProjectID            uuid.UUID
	Provider             enum.RepositoryProvider
	ProviderOwner        string
	ProviderName         string
	WebURL               string
	DefaultBranch        string
	Status               enum.RepositoryStatus
	ProviderRepositoryID string
	IconObjectURI        string
}

// ServicesPolicy stores a checked projection of services.yaml.
type ServicesPolicy struct {
	Base
	ProjectID          uuid.UUID
	SourceRepositoryID *uuid.UUID
	SourcePath         string
	SourceRef          string
	SourceCommitSHA    string
	SourceBlobSHA      string
	PolicyVersion      int64
	ContentHash        string
	ValidatedPayload   []byte
	ValidationStatus   enum.ServicesPolicyValidationStatus
	ProjectionStatus   enum.ServicesPolicyProjectionStatus
	ImportedAt         time.Time
}

// ServiceDescriptor is the typed, indexed part of checked services.yaml.
type ServiceDescriptor struct {
	Base
	ProjectID            uuid.UUID
	ServicesPolicyID     uuid.UUID
	RepositoryID         *uuid.UUID
	ServiceKey           string
	DisplayName          string
	Kind                 enum.ServiceKind
	RootPath             string
	DocumentationScopeID string
	DependsOnServiceKeys []string
	Status               enum.ServiceStatus
}

// DocumentationSource describes project, service, dependency or guidance docs.
type DocumentationSource struct {
	Base
	ProjectID    uuid.UUID
	RepositoryID *uuid.UUID
	ScopeType    enum.DocumentationScopeType
	ScopeID      string
	LocalPath    string
	AccessMode   enum.DocumentationAccessMode
	Status       enum.DocumentationSourceStatus
}

// BranchRules stores branch protection and merge policy.
type BranchRules struct {
	Base
	ProjectID      uuid.UUID
	RepositoryID   *uuid.UUID
	Pattern        string
	RequiredChecks []string
	MergePolicy    enum.MergePolicy
	Status         enum.BranchRulesStatus
}

// ReleasePolicy stores release branch and rollout policy.
type ReleasePolicy struct {
	Base
	ProjectID       uuid.UUID
	Name            string
	BranchPattern   string
	RolloutStrategy enum.RolloutStrategy
	RollbackPolicy  enum.RollbackPolicy
	RiskProfileRef  string
	Status          enum.ReleasePolicyStatus
}

// ReleaseLine represents one concrete release line inside a project.
type ReleaseLine struct {
	Base
	ProjectID       uuid.UUID
	ReleasePolicyID uuid.UUID
	Name            string
	BranchPattern   string
	Status          enum.ReleasePolicyStatus
}

// PlacementPolicy stores allowed infrastructure placement references.
type PlacementPolicy struct {
	Base
	ProjectID          uuid.UUID
	RepositoryID       *uuid.UUID
	ServiceKey         string
	AllowedClusterRefs []string
	Status             enum.PlacementPolicyStatus
}

// PolicyOverride stores a time-bound emergency override of Git-managed policy.
type PolicyOverride struct {
	Base
	ProjectID         uuid.UUID
	TargetType        enum.PolicyOverrideTargetType
	TargetID          *uuid.UUID
	Payload           []byte
	Reason            string
	Status            enum.PolicyOverrideStatus
	ExpiresAt         time.Time
	CreatedByActorRef string
}

// WorkspaceCodeSource describes one code checkout source allowed for a task.
type WorkspaceCodeSource struct {
	RepositoryID  uuid.UUID
	Provider      enum.RepositoryProvider
	ProviderOwner string
	ProviderName  string
	DefaultBranch string
	LocalPath     string
	AccessMode    enum.DocumentationAccessMode
}

// WorkspaceDocumentationSource describes one documentation checkout source.
type WorkspaceDocumentationSource struct {
	DocumentationSourceID uuid.UUID
	RepositoryID          *uuid.UUID
	ScopeType             enum.DocumentationScopeType
	ScopeID               string
	LocalPath             string
	AccessMode            enum.DocumentationAccessMode
}

// WorkspacePolicy is the source set allowed for an agent workspace.
type WorkspacePolicy struct {
	ProjectID            uuid.UUID
	CodeSources          []WorkspaceCodeSource
	DocumentationSources []WorkspaceDocumentationSource
	GuidancePackageRefs  []string
	PolicyVersion        int64
}

// PolicyEditProposal records a request to change services.yaml through a PR.
type PolicyEditProposal struct {
	ID           uuid.UUID
	ProjectID    uuid.UUID
	RepositoryID uuid.UUID
	SourcePath   string
	Status       string
	CreatedAt    time.Time
}

// CommandResult stores the aggregate produced by an idempotent mutation command.
type CommandResult struct {
	Key            string
	CommandID      uuid.UUID
	IdempotencyKey string
	Operation      string
	AggregateType  string
	AggregateID    uuid.UUID
	CreatedAt      time.Time
}

// OutboxEvent stores a domain event until it is published to consumers.
type OutboxEvent struct {
	ID                  uuid.UUID
	AggregateType       string
	AggregateID         uuid.UUID
	EventType           string
	SchemaVersion       int
	Payload             []byte
	PublishedAt         *time.Time
	OccurredAt          time.Time
	NextAttemptAt       time.Time
	AttemptCount        int
	LockedUntil         *time.Time
	FailureKind         string
	FailedPermanentlyAt *time.Time
	LastError           string
}
