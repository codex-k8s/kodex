package enum

type OrganizationKind string

const (
	OrganizationKindOwner          OrganizationKind = "owner"
	OrganizationKindClient         OrganizationKind = "client"
	OrganizationKindContractor     OrganizationKind = "contractor"
	OrganizationKindSaaS           OrganizationKind = "saas"
	OrganizationKindSaaSClient     OrganizationKind = "saas_client"
	OrganizationKindSaaSContractor OrganizationKind = "saas_contractor"
)

type OrganizationStatus string

const (
	OrganizationStatusActive    OrganizationStatus = "active"
	OrganizationStatusPending   OrganizationStatus = "pending"
	OrganizationStatusSuspended OrganizationStatus = "suspended"
	OrganizationStatusArchived  OrganizationStatus = "archived"
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusPending  UserStatus = "pending"
	UserStatusBlocked  UserStatus = "blocked"
	UserStatusDisabled UserStatus = "disabled"
)

type IdentityProvider string

const (
	IdentityProviderKeycloak IdentityProvider = "keycloak"
	IdentityProviderGitHub   IdentityProvider = "github"
	IdentityProviderGitLab   IdentityProvider = "gitlab"
	IdentityProviderGoogle   IdentityProvider = "google"
)

type AllowlistMatchType string

const (
	AllowlistMatchEmail  AllowlistMatchType = "email"
	AllowlistMatchDomain AllowlistMatchType = "domain"
)

type AllowlistStatus string

const (
	AllowlistStatusActive   AllowlistStatus = "active"
	AllowlistStatusDisabled AllowlistStatus = "disabled"
)

type GroupScopeType string

const (
	GroupScopeGlobal       GroupScopeType = "global"
	GroupScopeOrganization GroupScopeType = "organization"
)

type GroupStatus string

const (
	GroupStatusActive   GroupStatus = "active"
	GroupStatusDisabled GroupStatus = "disabled"
	GroupStatusArchived GroupStatus = "archived"
)

type MembershipSubjectType string

const (
	MembershipSubjectUser            MembershipSubjectType = "user"
	MembershipSubjectGroup           MembershipSubjectType = "group"
	MembershipSubjectExternalAccount MembershipSubjectType = "external_account"
)

type MembershipTargetType string

const (
	MembershipTargetOrganization MembershipTargetType = "organization"
	MembershipTargetGroup        MembershipTargetType = "group"
)

type MembershipStatus string

const (
	MembershipStatusActive   MembershipStatus = "active"
	MembershipStatusPending  MembershipStatus = "pending"
	MembershipStatusBlocked  MembershipStatus = "blocked"
	MembershipStatusDisabled MembershipStatus = "disabled"
)

type MembershipSource string

const (
	MembershipSourceManual    MembershipSource = "manual"
	MembershipSourceBootstrap MembershipSource = "bootstrap"
	MembershipSourceSync      MembershipSource = "sync"
	MembershipSourceSystem    MembershipSource = "system"
)

type AccessActionStatus string

const (
	AccessActionStatusActive   AccessActionStatus = "active"
	AccessActionStatusDisabled AccessActionStatus = "disabled"
)

type AccessEffect string

const (
	AccessEffectAllow AccessEffect = "allow"
	AccessEffectDeny  AccessEffect = "deny"
)

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

type AccessRuleStatus string

const (
	AccessRuleStatusActive   AccessRuleStatus = "active"
	AccessRuleStatusDisabled AccessRuleStatus = "disabled"
)

type AccessDecision string

const (
	AccessDecisionAllow   AccessDecision = "allow"
	AccessDecisionDeny    AccessDecision = "deny"
	AccessDecisionPending AccessDecision = "pending"
)

type ExternalProviderKind string

const (
	ExternalProviderRepository ExternalProviderKind = "repository"
	ExternalProviderIdentity   ExternalProviderKind = "identity"
	ExternalProviderModel      ExternalProviderKind = "model"
	ExternalProviderMessaging  ExternalProviderKind = "messaging"
	ExternalProviderPayments   ExternalProviderKind = "payments"
	ExternalProviderOther      ExternalProviderKind = "other"
)

type ExternalProviderStatus string

const (
	ExternalProviderStatusActive   ExternalProviderStatus = "active"
	ExternalProviderStatusDisabled ExternalProviderStatus = "disabled"
)

type ExternalAccountType string

const (
	ExternalAccountUser        ExternalAccountType = "user"
	ExternalAccountBot         ExternalAccountType = "bot"
	ExternalAccountService     ExternalAccountType = "service"
	ExternalAccountIntegration ExternalAccountType = "integration"
)

type ExternalAccountStatus string

const (
	ExternalAccountStatusActive      ExternalAccountStatus = "active"
	ExternalAccountStatusPending     ExternalAccountStatus = "pending"
	ExternalAccountStatusNeedsReauth ExternalAccountStatus = "needs_reauth"
	ExternalAccountStatusLimited     ExternalAccountStatus = "limited"
	ExternalAccountStatusBlocked     ExternalAccountStatus = "blocked"
	ExternalAccountStatusDisabled    ExternalAccountStatus = "disabled"
)

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

type ExternalAccountBindingStatus string

const (
	ExternalAccountBindingStatusActive   ExternalAccountBindingStatus = "active"
	ExternalAccountBindingStatusDisabled ExternalAccountBindingStatus = "disabled"
)

type SecretStoreType string

const (
	SecretStoreVault            SecretStoreType = "vault"
	SecretStoreKubernetesSecret SecretStoreType = "kubernetes_secret"
)
