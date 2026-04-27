// Package enum contains closed domain vocabularies for access-manager.
package enum

// OrganizationKind classifies how an organization participates in the platform.
type OrganizationKind string

const (
	OrganizationKindOwner          OrganizationKind = "owner"
	OrganizationKindClient         OrganizationKind = "client"
	OrganizationKindContractor     OrganizationKind = "contractor"
	OrganizationKindSaaS           OrganizationKind = "saas"
	OrganizationKindSaaSClient     OrganizationKind = "saas_client"
	OrganizationKindSaaSContractor OrganizationKind = "saas_contractor"
)

// OrganizationStatus describes the lifecycle state of an organization.
type OrganizationStatus string

const (
	OrganizationStatusActive    OrganizationStatus = "active"
	OrganizationStatusPending   OrganizationStatus = "pending"
	OrganizationStatusSuspended OrganizationStatus = "suspended"
	OrganizationStatusArchived  OrganizationStatus = "archived"
)

// UserStatus describes admission and access state for a human user.
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusPending  UserStatus = "pending"
	UserStatusBlocked  UserStatus = "blocked"
	UserStatusDisabled UserStatus = "disabled"
)

// IdentityProvider identifies an external identity source.
type IdentityProvider string

const (
	IdentityProviderKeycloak IdentityProvider = "keycloak"
	IdentityProviderGitHub   IdentityProvider = "github"
	IdentityProviderGitLab   IdentityProvider = "gitlab"
	IdentityProviderGoogle   IdentityProvider = "google"
)

// AllowlistMatchType defines how an allowlist entry matches a login email.
type AllowlistMatchType string

const (
	AllowlistMatchEmail  AllowlistMatchType = "email"
	AllowlistMatchDomain AllowlistMatchType = "domain"
)

// AllowlistStatus controls whether an allowlist entry may admit users.
type AllowlistStatus string

const (
	AllowlistStatusActive   AllowlistStatus = "active"
	AllowlistStatusDisabled AllowlistStatus = "disabled"
)

// GroupScopeType defines where a group slug is unique.
type GroupScopeType string

const (
	GroupScopeGlobal       GroupScopeType = "global"
	GroupScopeOrganization GroupScopeType = "organization"
)

// GroupStatus describes whether a group can participate in membership checks.
type GroupStatus string

const (
	GroupStatusActive   GroupStatus = "active"
	GroupStatusDisabled GroupStatus = "disabled"
	GroupStatusArchived GroupStatus = "archived"
)

// MembershipSubjectType identifies a subject that can be added to membership.
type MembershipSubjectType string

const (
	MembershipSubjectUser            MembershipSubjectType = "user"
	MembershipSubjectGroup           MembershipSubjectType = "group"
	MembershipSubjectExternalAccount MembershipSubjectType = "external_account"
)

// MembershipTargetType identifies a membership target.
type MembershipTargetType string

const (
	MembershipTargetOrganization MembershipTargetType = "organization"
	MembershipTargetGroup        MembershipTargetType = "group"
)

// MembershipStatus describes whether a membership is effective.
type MembershipStatus string

const (
	MembershipStatusActive   MembershipStatus = "active"
	MembershipStatusPending  MembershipStatus = "pending"
	MembershipStatusBlocked  MembershipStatus = "blocked"
	MembershipStatusDisabled MembershipStatus = "disabled"
)

// MembershipSource records how a membership was created or synchronized.
type MembershipSource string

const (
	MembershipSourceManual    MembershipSource = "manual"
	MembershipSourceBootstrap MembershipSource = "bootstrap"
	MembershipSourceSync      MembershipSource = "sync"
	MembershipSourceSystem    MembershipSource = "system"
)

// AccessActionStatus controls whether a catalog action may be used by rules.
type AccessActionStatus string

const (
	AccessActionStatusActive   AccessActionStatus = "active"
	AccessActionStatusDisabled AccessActionStatus = "disabled"
)

// AccessEffect defines whether a rule allows or denies access.
type AccessEffect string

const (
	AccessEffectAllow AccessEffect = "allow"
	AccessEffectDeny  AccessEffect = "deny"
)

// AccessSubjectType identifies subjects that can receive access rules.
type AccessSubjectType string

const (
	AccessSubjectUser            AccessSubjectType = "user"
	AccessSubjectGroup           AccessSubjectType = "group"
	AccessSubjectOrganization    AccessSubjectType = "organization"
	AccessSubjectExternalAccount AccessSubjectType = "external_account"
	AccessSubjectAgent           AccessSubjectType = "agent"
	AccessSubjectAgentRole       AccessSubjectType = "agent_role"
	AccessSubjectFlow            AccessSubjectType = "flow"
	AccessSubjectPackage         AccessSubjectType = "package"
)

// AccessRuleStatus controls whether a rule participates in access checks.
type AccessRuleStatus string

const (
	AccessRuleStatusActive   AccessRuleStatus = "active"
	AccessRuleStatusDisabled AccessRuleStatus = "disabled"
)

// AccessDecision is the result of an access check.
type AccessDecision string

const (
	AccessDecisionAllow   AccessDecision = "allow"
	AccessDecisionDeny    AccessDecision = "deny"
	AccessDecisionPending AccessDecision = "pending"
)

// ExternalProviderKind classifies external account providers.
type ExternalProviderKind string

const (
	ExternalProviderRepository ExternalProviderKind = "repository"
	ExternalProviderIdentity   ExternalProviderKind = "identity"
	ExternalProviderModel      ExternalProviderKind = "model"
	ExternalProviderMessaging  ExternalProviderKind = "messaging"
	ExternalProviderPayments   ExternalProviderKind = "payments"
	ExternalProviderOther      ExternalProviderKind = "other"
)

// ExternalProviderStatus controls whether provider accounts can be managed.
type ExternalProviderStatus string

const (
	ExternalProviderStatusActive   ExternalProviderStatus = "active"
	ExternalProviderStatusDisabled ExternalProviderStatus = "disabled"
)

// ExternalAccountType classifies a provider account principal.
type ExternalAccountType string

const (
	ExternalAccountUser        ExternalAccountType = "user"
	ExternalAccountBot         ExternalAccountType = "bot"
	ExternalAccountService     ExternalAccountType = "service"
	ExternalAccountIntegration ExternalAccountType = "integration"
)

// ExternalAccountStatus describes whether an external account can be used.
type ExternalAccountStatus string

const (
	ExternalAccountStatusActive      ExternalAccountStatus = "active"
	ExternalAccountStatusPending     ExternalAccountStatus = "pending"
	ExternalAccountStatusNeedsReauth ExternalAccountStatus = "needs_reauth"
	ExternalAccountStatusLimited     ExternalAccountStatus = "limited"
	ExternalAccountStatusBlocked     ExternalAccountStatus = "blocked"
	ExternalAccountStatusDisabled    ExternalAccountStatus = "disabled"
)

// ExternalAccountScopeType identifies account ownership or usage scope.
type ExternalAccountScopeType string

const (
	ExternalAccountScopeGlobal       ExternalAccountScopeType = "global"
	ExternalAccountScopeOrganization ExternalAccountScopeType = "organization"
	ExternalAccountScopeProject      ExternalAccountScopeType = "project"
	ExternalAccountScopeRepository   ExternalAccountScopeType = "repository"
	ExternalAccountScopeUser         ExternalAccountScopeType = "user"
	ExternalAccountScopeGroup        ExternalAccountScopeType = "group"
	ExternalAccountScopeAgent        ExternalAccountScopeType = "agent"
	ExternalAccountScopeAgentRole    ExternalAccountScopeType = "agent_role"
	ExternalAccountScopeFlow         ExternalAccountScopeType = "flow"
	ExternalAccountScopeStage        ExternalAccountScopeType = "stage"
	ExternalAccountScopePackage      ExternalAccountScopeType = "package"
)

// ExternalAccountBindingStatus controls whether an account binding is effective.
type ExternalAccountBindingStatus string

const (
	ExternalAccountBindingStatusActive   ExternalAccountBindingStatus = "active"
	ExternalAccountBindingStatusDisabled ExternalAccountBindingStatus = "disabled"
)

// SecretStoreType identifies the secret backend referenced by a binding.
type SecretStoreType string

const (
	SecretStoreVault            SecretStoreType = "vault"
	SecretStoreKubernetesSecret SecretStoreType = "kubernetes_secret"
)
