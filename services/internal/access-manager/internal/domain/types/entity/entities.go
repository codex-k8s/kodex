// Package entity contains persisted aggregate models owned by access-manager.
package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// Base stores common aggregate metadata used for optimistic concurrency.
type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Organization represents a platform tenant, owner, client or contractor scope.
type Organization struct {
	Base
	Kind                 enum.OrganizationKind
	Slug                 string
	DisplayName          string
	ImageAssetRef        string
	Status               enum.OrganizationStatus
	ParentOrganizationID *uuid.UUID
}

// User represents a human platform principal admitted through SSO and allowlist.
type User struct {
	Base
	PrimaryEmail   string
	DisplayName    string
	AvatarAssetRef string
	Status         enum.UserStatus
	Locale         string
}

// UserIdentity links a user profile to an external identity provider subject.
type UserIdentity struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Provider     enum.IdentityProvider
	Subject      string
	EmailAtLogin string
	LastLoginAt  *time.Time
}

// AllowlistEntry defines primary admission rules for first user login.
type AllowlistEntry struct {
	Base
	MatchType      enum.AllowlistMatchType
	Value          string
	OrganizationID *uuid.UUID
	DefaultStatus  enum.UserStatus
	Status         enum.AllowlistStatus
}

// PendingAccessItem is an operator-visible access item that needs attention.
type PendingAccessItem struct {
	ItemID     string
	ItemType   string
	Subject    value.SubjectRef
	Status     string
	ReasonCode string
	CreatedAt  time.Time
}

// Group groups users or accounts inside global or organization scope.
type Group struct {
	Base
	ScopeType     enum.GroupScopeType
	ScopeID       *uuid.UUID
	Slug          string
	DisplayName   string
	ParentGroupID *uuid.UUID
	ImageAssetRef string
	Status        enum.GroupStatus
}

// Membership connects a subject to a group or organization target.
type Membership struct {
	Base
	SubjectType enum.MembershipSubjectType
	SubjectID   uuid.UUID
	TargetType  enum.MembershipTargetType
	TargetID    uuid.UUID
	RoleHint    string
	Status      enum.MembershipStatus
	Source      enum.MembershipSource
}

// AccessAction describes a catalog action that can be referenced by rules.
type AccessAction struct {
	Base
	Key          string
	DisplayName  string
	Description  string
	ResourceType string
	Status       enum.AccessActionStatus
}

// AccessRule grants or denies an action to a subject in a concrete scope.
type AccessRule struct {
	Base
	Effect       enum.AccessEffect
	SubjectType  enum.AccessSubjectType
	SubjectID    string
	ActionKey    string
	ResourceType string
	ResourceID   string
	ScopeType    string
	ScopeID      string
	Priority     int
	Status       enum.AccessRuleStatus
}

// ExternalProvider describes a provider that owns external accounts.
type ExternalProvider struct {
	Base
	Slug         string
	ProviderKind enum.ExternalProviderKind
	DisplayName  string
	IconAssetRef string
	Status       enum.ExternalProviderStatus
}

// ExternalAccount represents a provider account usable by people, services or agents.
type ExternalAccount struct {
	Base
	ExternalProviderID uuid.UUID
	AccountType        enum.ExternalAccountType
	DisplayName        string
	ImageAssetRef      string
	OwnerScopeType     enum.ExternalAccountScopeType
	OwnerScopeID       string
	Status             enum.ExternalAccountStatus
	SecretBindingRefID *uuid.UUID
}

// ExternalAccountBinding permits an external account to be used in a target scope.
type ExternalAccountBinding struct {
	Base
	ExternalAccountID uuid.UUID
	UsageScopeType    enum.ExternalAccountScopeType
	UsageScopeID      string
	AllowedActionKeys []string
	Status            enum.ExternalAccountBindingStatus
}

// SecretBindingRef stores a reference to a secret in the canonical secret store.
type SecretBindingRef struct {
	Base
	StoreType        enum.SecretStoreType
	StoreRef         string
	ValueFingerprint string
	RotatedAt        *time.Time
}

// AccessDecisionAudit records auditable access decisions and their explanation.
type AccessDecisionAudit struct {
	ID             uuid.UUID
	Subject        value.SubjectRef
	ActionKey      string
	Resource       value.ResourceRef
	Scope          value.ScopeRef
	RequestContext value.RequestContext
	Decision       enum.AccessDecision
	ReasonCode     string
	PolicyVersion  int64
	Explanation    value.DecisionExplanation
	CreatedAt      time.Time
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
	EventType           string
	SchemaVersion       int
	AggregateType       string
	AggregateID         uuid.UUID
	Payload             []byte
	OccurredAt          time.Time
	PublishedAt         *time.Time
	AttemptCount        int
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailedPermanentlyAt *time.Time
	FailureKind         string
	LastError           string
}
