package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Organization struct {
	Base
	Kind                 enum.OrganizationKind
	Slug                 string
	DisplayName          string
	ImageAssetRef        string
	Status               enum.OrganizationStatus
	ParentOrganizationID *uuid.UUID
}

type User struct {
	Base
	PrimaryEmail   string
	DisplayName    string
	AvatarAssetRef string
	Status         enum.UserStatus
	Locale         string
}

type UserIdentity struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Provider     enum.IdentityProvider
	Subject      string
	EmailAtLogin string
	LastLoginAt  *time.Time
}

type AllowlistEntry struct {
	ID             uuid.UUID
	MatchType      enum.AllowlistMatchType
	Value          string
	OrganizationID *uuid.UUID
	DefaultStatus  enum.UserStatus
	Status         enum.AllowlistStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

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

type AccessAction struct {
	Base
	Key          string
	DisplayName  string
	Description  string
	ResourceType string
	Status       enum.AccessActionStatus
}

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

type ExternalProvider struct {
	Base
	Slug         string
	ProviderKind enum.ExternalProviderKind
	DisplayName  string
	IconAssetRef string
	Status       enum.ExternalProviderStatus
}

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

type ExternalAccountBinding struct {
	ID                uuid.UUID
	ExternalAccountID uuid.UUID
	UsageScopeType    enum.ExternalAccountScopeType
	UsageScopeID      string
	AllowedActionKeys []string
	Status            enum.ExternalAccountBindingStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SecretBindingRef struct {
	ID               uuid.UUID
	StoreType        enum.SecretStoreType
	StoreRef         string
	ValueFingerprint string
	RotatedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AccessDecisionAudit struct {
	ID            uuid.UUID
	Subject       value.SubjectRef
	ActionKey     string
	Resource      value.ResourceRef
	Decision      enum.AccessDecision
	ReasonCode    string
	PolicyVersion int64
	Explanation   value.DecisionExplanation
	CreatedAt     time.Time
}

type OutboxEvent struct {
	ID            uuid.UUID
	EventType     string
	SchemaVersion int
	AggregateType string
	AggregateID   uuid.UUID
	Payload       []byte
	OccurredAt    time.Time
	PublishedAt   *time.Time
}
