package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

type CreateOrganizationInput struct {
	Kind                 enum.OrganizationKind
	Slug                 string
	DisplayName          string
	ImageAssetRef        string
	Status               enum.OrganizationStatus
	ParentOrganizationID *uuid.UUID
	Meta                 value.CommandMeta
}

type CreateGroupInput struct {
	ScopeType     enum.GroupScopeType
	ScopeID       *uuid.UUID
	Slug          string
	DisplayName   string
	ParentGroupID *uuid.UUID
	ImageAssetRef string
	Meta          value.CommandMeta
}

type SetMembershipInput struct {
	SubjectType enum.MembershipSubjectType
	SubjectID   uuid.UUID
	TargetType  enum.MembershipTargetType
	TargetID    uuid.UUID
	RoleHint    string
	Status      enum.MembershipStatus
	Source      enum.MembershipSource
	Meta        value.CommandMeta
}

type PutAllowlistEntryInput struct {
	MatchType      enum.AllowlistMatchType
	Value          string
	OrganizationID *uuid.UUID
	DefaultStatus  enum.UserStatus
	Status         enum.AllowlistStatus
	Meta           value.CommandMeta
}

type BootstrapUserFromIdentityInput struct {
	Provider    enum.IdentityProvider
	Subject     string
	Email       string
	DisplayName string
	Locale      string
	Meta        value.CommandMeta
}

type BootstrapUserFromIdentityResult struct {
	User         entity.User
	Decision     enum.AccessDecision
	ReasonCode   string
	Organization *entity.Organization
}

type PutExternalProviderInput struct {
	Slug         string
	ProviderKind enum.ExternalProviderKind
	DisplayName  string
	IconAssetRef string
	Status       enum.ExternalProviderStatus
	Meta         value.CommandMeta
}

type RegisterExternalAccountInput struct {
	ExternalProviderID uuid.UUID
	AccountType        enum.ExternalAccountType
	DisplayName        string
	ImageAssetRef      string
	OwnerScopeType     enum.ExternalAccountScopeType
	OwnerScopeID       string
	Status             enum.ExternalAccountStatus
	SecretBindingRefID *uuid.UUID
	Meta               value.CommandMeta
}

type BindExternalAccountInput struct {
	ExternalAccountID uuid.UUID
	UsageScopeType    enum.ExternalAccountScopeType
	UsageScopeID      string
	AllowedActionKeys []string
	Status            enum.ExternalAccountBindingStatus
	Meta              value.CommandMeta
}

type PutAccessActionInput struct {
	Key          string
	DisplayName  string
	Description  string
	ResourceType string
	Status       enum.AccessActionStatus
	Meta         value.CommandMeta
}

type PutAccessRuleInput struct {
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
	Meta         value.CommandMeta
}

type CheckAccessInput struct {
	Subject   value.SubjectRef
	ActionKey string
	Resource  value.ResourceRef
	Scope     value.ScopeRef
	Audit     bool
	Meta      value.CommandMeta
}

type CheckAccessResult struct {
	Decision    enum.AccessDecision
	ReasonCode  string
	Explanation value.DecisionExplanation
}

type ResolveExternalAccountUsageInput struct {
	ExternalAccountID uuid.UUID
	ActionKey         string
	UsageScope        value.ScopeRef
}

type ResolveExternalAccountUsageResult struct {
	ExternalAccount entity.ExternalAccount
	SecretRef       entity.SecretBindingRef
	AllowedActions  []string
}
