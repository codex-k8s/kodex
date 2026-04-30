// Package service implements access-manager domain use cases.
package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// CreateOrganizationInput contains fields required to create an organization.
type CreateOrganizationInput struct {
	Kind                 enum.OrganizationKind
	Slug                 string
	DisplayName          string
	ImageAssetRef        string
	Status               enum.OrganizationStatus
	ParentOrganizationID *uuid.UUID
	Meta                 value.CommandMeta
}

// CreateGroupInput contains fields required to create a group.
type CreateGroupInput struct {
	ScopeType     enum.GroupScopeType
	ScopeID       *uuid.UUID
	Slug          string
	DisplayName   string
	ParentGroupID *uuid.UUID
	ImageAssetRef string
	Meta          value.CommandMeta
}

// SetMembershipInput creates or updates a membership edge.
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

// PutAllowlistEntryInput creates or replaces an allowlist entry.
type PutAllowlistEntryInput struct {
	MatchType      enum.AllowlistMatchType
	Value          string
	OrganizationID *uuid.UUID
	DefaultStatus  enum.UserStatus
	Status         enum.AllowlistStatus
	Meta           value.CommandMeta
}

// BootstrapUserFromIdentityInput carries SSO identity data after provider login.
type BootstrapUserFromIdentityInput struct {
	Provider    enum.IdentityProvider
	Subject     string
	Email       string
	DisplayName string
	Locale      string
	Meta        value.CommandMeta
}

// BootstrapUserFromIdentityResult returns the admitted or linked user state.
type BootstrapUserFromIdentityResult struct {
	User         entity.User
	Decision     enum.AccessDecision
	ReasonCode   string
	Organization *entity.Organization
}

// PutExternalProviderInput creates or updates an external provider.
type PutExternalProviderInput struct {
	Slug         string
	ProviderKind enum.ExternalProviderKind
	DisplayName  string
	IconAssetRef string
	Status       enum.ExternalProviderStatus
	CreateOnly   bool
	Meta         value.CommandMeta
}

// RegisterExternalAccountInput creates an external account catalog entry.
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

// BindExternalAccountInput permits account usage in a concrete scope.
type BindExternalAccountInput struct {
	ExternalAccountID uuid.UUID
	UsageScopeType    enum.ExternalAccountScopeType
	UsageScopeID      string
	AllowedActionKeys []string
	Status            enum.ExternalAccountBindingStatus
	Meta              value.CommandMeta
}

// PutAccessActionInput creates or updates an action catalog entry.
type PutAccessActionInput struct {
	Key          string
	DisplayName  string
	Description  string
	ResourceType string
	Status       enum.AccessActionStatus
	Meta         value.CommandMeta
}

// PutAccessRuleInput creates or updates a policy rule.
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

// CheckAccessInput describes a policy decision request.
type CheckAccessInput struct {
	Subject   value.SubjectRef
	ActionKey string
	Resource  value.ResourceRef
	Scope     value.ScopeRef
	Audit     bool
	Meta      value.CommandMeta
}

// CheckAccessResult returns the policy decision and explanation.
type CheckAccessResult struct {
	Decision    enum.AccessDecision
	ReasonCode  string
	Explanation value.DecisionExplanation
}

// ResolveExternalAccountUsageInput asks whether an account can be used.
type ResolveExternalAccountUsageInput struct {
	ExternalAccountID uuid.UUID
	ActionKey         string
	UsageScope        value.ScopeRef
}

// ResolveExternalAccountUsageResult returns the permitted account and secret ref.
type ResolveExternalAccountUsageResult struct {
	ExternalAccount entity.ExternalAccount
	SecretRef       entity.SecretBindingRef
	AllowedActions  []string
}
