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

// SetUserStatusInput changes the lifecycle status of an existing user.
type SetUserStatusInput struct {
	UserID uuid.UUID
	Status enum.UserStatus
	Meta   value.CommandMeta
}

// DisableAllowlistEntryInput disables an allowlist entry without deleting history.
type DisableAllowlistEntryInput struct {
	AllowlistEntryID uuid.UUID
	Meta             value.CommandMeta
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

// UpdateExternalProviderInput changes an existing external provider by id.
type UpdateExternalProviderInput struct {
	ExternalProviderID uuid.UUID
	Slug               string
	ProviderKind       enum.ExternalProviderKind
	DisplayName        string
	IconAssetRef       string
	Status             enum.ExternalProviderStatus
	Meta               value.CommandMeta
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

// UpdateExternalAccountStatusInput changes external-account lifecycle status.
type UpdateExternalAccountStatusInput struct {
	ExternalAccountID uuid.UUID
	Status            enum.ExternalAccountStatus
	Meta              value.CommandMeta
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

// DisableExternalAccountBindingInput disables a usage binding without deleting history.
type DisableExternalAccountBindingInput struct {
	ExternalAccountBindingID uuid.UUID
	Meta                     value.CommandMeta
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

// ExplainAccessInput identifies a previously audited access decision.
type ExplainAccessInput struct {
	AuditID uuid.UUID
	Scope   value.ScopeRef
	Meta    value.CommandMeta
}

// ExplainAccessResult returns the stored access decision explanation.
type ExplainAccessResult struct {
	Audit entity.AccessDecisionAudit
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

// ListPendingAccessInput selects operator-visible access items that need attention.
type ListPendingAccessInput struct {
	Scope  value.ScopeRef
	Limit  int
	Cursor string
	Meta   value.CommandMeta
}

// ListPendingAccessResult returns a page of pending or blocked access items.
type ListPendingAccessResult struct {
	Items      []entity.PendingAccessItem
	NextCursor string
}
