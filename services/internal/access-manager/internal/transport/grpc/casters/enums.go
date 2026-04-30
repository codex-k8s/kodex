// Package casters maps access-manager gRPC DTOs to domain types and back.
package casters

import (
	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
)

type enumMap[P comparable, D ~string] map[P]D
type reverseEnumMap[D comparable, P any] map[D]P

var organizationKindFromProto = enumMap[accessaccountsv1.OrganizationKind, enum.OrganizationKind]{
	accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_OWNER:           enum.OrganizationKindOwner,
	accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_CLIENT:          enum.OrganizationKindClient,
	accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_CONTRACTOR:      enum.OrganizationKindContractor,
	accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_SAAS:            enum.OrganizationKindSaaS,
	accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_SAAS_CLIENT:     enum.OrganizationKindSaaSClient,
	accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_SAAS_CONTRACTOR: enum.OrganizationKindSaaSContractor,
}

var organizationKindToProto = reverseEnumMap[enum.OrganizationKind, accessaccountsv1.OrganizationKind]{
	enum.OrganizationKindOwner:          accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_OWNER,
	enum.OrganizationKindClient:         accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_CLIENT,
	enum.OrganizationKindContractor:     accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_CONTRACTOR,
	enum.OrganizationKindSaaS:           accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_SAAS,
	enum.OrganizationKindSaaSClient:     accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_SAAS_CLIENT,
	enum.OrganizationKindSaaSContractor: accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_SAAS_CONTRACTOR,
}

var organizationStatusFromProto = enumMap[accessaccountsv1.OrganizationStatus, enum.OrganizationStatus]{
	accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_ACTIVE:    enum.OrganizationStatusActive,
	accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_PENDING:   enum.OrganizationStatusPending,
	accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_SUSPENDED: enum.OrganizationStatusSuspended,
	accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_ARCHIVED:  enum.OrganizationStatusArchived,
}

var organizationStatusToProto = reverseEnumMap[enum.OrganizationStatus, accessaccountsv1.OrganizationStatus]{
	enum.OrganizationStatusActive:    accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_ACTIVE,
	enum.OrganizationStatusPending:   accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_PENDING,
	enum.OrganizationStatusSuspended: accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_SUSPENDED,
	enum.OrganizationStatusArchived:  accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_ARCHIVED,
}

var userStatusFromProto = enumMap[accessaccountsv1.UserStatus, enum.UserStatus]{
	accessaccountsv1.UserStatus_USER_STATUS_ACTIVE:   enum.UserStatusActive,
	accessaccountsv1.UserStatus_USER_STATUS_PENDING:  enum.UserStatusPending,
	accessaccountsv1.UserStatus_USER_STATUS_BLOCKED:  enum.UserStatusBlocked,
	accessaccountsv1.UserStatus_USER_STATUS_DISABLED: enum.UserStatusDisabled,
}

var userStatusToProto = reverseEnumMap[enum.UserStatus, accessaccountsv1.UserStatus]{
	enum.UserStatusActive:   accessaccountsv1.UserStatus_USER_STATUS_ACTIVE,
	enum.UserStatusPending:  accessaccountsv1.UserStatus_USER_STATUS_PENDING,
	enum.UserStatusBlocked:  accessaccountsv1.UserStatus_USER_STATUS_BLOCKED,
	enum.UserStatusDisabled: accessaccountsv1.UserStatus_USER_STATUS_DISABLED,
}

var groupScopeTypeFromProto = enumMap[accessaccountsv1.GroupScopeType, enum.GroupScopeType]{
	accessaccountsv1.GroupScopeType_GROUP_SCOPE_TYPE_GLOBAL:       enum.GroupScopeGlobal,
	accessaccountsv1.GroupScopeType_GROUP_SCOPE_TYPE_ORGANIZATION: enum.GroupScopeOrganization,
}

var groupScopeTypeToProto = reverseEnumMap[enum.GroupScopeType, accessaccountsv1.GroupScopeType]{
	enum.GroupScopeGlobal:       accessaccountsv1.GroupScopeType_GROUP_SCOPE_TYPE_GLOBAL,
	enum.GroupScopeOrganization: accessaccountsv1.GroupScopeType_GROUP_SCOPE_TYPE_ORGANIZATION,
}

var groupStatusToProto = reverseEnumMap[enum.GroupStatus, accessaccountsv1.GroupStatus]{
	enum.GroupStatusActive:   accessaccountsv1.GroupStatus_GROUP_STATUS_ACTIVE,
	enum.GroupStatusDisabled: accessaccountsv1.GroupStatus_GROUP_STATUS_DISABLED,
	enum.GroupStatusArchived: accessaccountsv1.GroupStatus_GROUP_STATUS_ARCHIVED,
}

var membershipSubjectTypeFromProto = enumMap[accessaccountsv1.MembershipSubjectType, enum.MembershipSubjectType]{
	accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_USER:             enum.MembershipSubjectUser,
	accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_GROUP:            enum.MembershipSubjectGroup,
	accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_EXTERNAL_ACCOUNT: enum.MembershipSubjectExternalAccount,
}

var membershipSubjectTypeToProto = reverseEnumMap[enum.MembershipSubjectType, accessaccountsv1.MembershipSubjectType]{
	enum.MembershipSubjectUser:            accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_USER,
	enum.MembershipSubjectGroup:           accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_GROUP,
	enum.MembershipSubjectExternalAccount: accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_EXTERNAL_ACCOUNT,
}

var membershipTargetTypeFromProto = enumMap[accessaccountsv1.MembershipTargetType, enum.MembershipTargetType]{
	accessaccountsv1.MembershipTargetType_MEMBERSHIP_TARGET_TYPE_ORGANIZATION: enum.MembershipTargetOrganization,
	accessaccountsv1.MembershipTargetType_MEMBERSHIP_TARGET_TYPE_GROUP:        enum.MembershipTargetGroup,
}

var membershipTargetTypeToProto = reverseEnumMap[enum.MembershipTargetType, accessaccountsv1.MembershipTargetType]{
	enum.MembershipTargetOrganization: accessaccountsv1.MembershipTargetType_MEMBERSHIP_TARGET_TYPE_ORGANIZATION,
	enum.MembershipTargetGroup:        accessaccountsv1.MembershipTargetType_MEMBERSHIP_TARGET_TYPE_GROUP,
}

var membershipStatusFromProto = enumMap[accessaccountsv1.MembershipStatus, enum.MembershipStatus]{
	accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE:   enum.MembershipStatusActive,
	accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_PENDING:  enum.MembershipStatusPending,
	accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_BLOCKED:  enum.MembershipStatusBlocked,
	accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_DISABLED: enum.MembershipStatusDisabled,
}

var membershipStatusToProto = reverseEnumMap[enum.MembershipStatus, accessaccountsv1.MembershipStatus]{
	enum.MembershipStatusActive:   accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_ACTIVE,
	enum.MembershipStatusPending:  accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_PENDING,
	enum.MembershipStatusBlocked:  accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_BLOCKED,
	enum.MembershipStatusDisabled: accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_DISABLED,
}

var membershipSourceFromProto = enumMap[accessaccountsv1.MembershipSource, enum.MembershipSource]{
	accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_MANUAL:    enum.MembershipSourceManual,
	accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_BOOTSTRAP: enum.MembershipSourceBootstrap,
	accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_SYNC:      enum.MembershipSourceSync,
	accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_SYSTEM:    enum.MembershipSourceSystem,
}

var membershipSourceToProto = reverseEnumMap[enum.MembershipSource, accessaccountsv1.MembershipSource]{
	enum.MembershipSourceManual:    accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_MANUAL,
	enum.MembershipSourceBootstrap: accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_BOOTSTRAP,
	enum.MembershipSourceSync:      accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_SYNC,
	enum.MembershipSourceSystem:    accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_SYSTEM,
}

var allowlistMatchTypeFromProto = enumMap[accessaccountsv1.AllowlistMatchType, enum.AllowlistMatchType]{
	accessaccountsv1.AllowlistMatchType_ALLOWLIST_MATCH_TYPE_EMAIL:  enum.AllowlistMatchEmail,
	accessaccountsv1.AllowlistMatchType_ALLOWLIST_MATCH_TYPE_DOMAIN: enum.AllowlistMatchDomain,
}

var allowlistMatchTypeToProto = reverseEnumMap[enum.AllowlistMatchType, accessaccountsv1.AllowlistMatchType]{
	enum.AllowlistMatchEmail:  accessaccountsv1.AllowlistMatchType_ALLOWLIST_MATCH_TYPE_EMAIL,
	enum.AllowlistMatchDomain: accessaccountsv1.AllowlistMatchType_ALLOWLIST_MATCH_TYPE_DOMAIN,
}

var allowlistStatusFromProto = enumMap[accessaccountsv1.AllowlistStatus, enum.AllowlistStatus]{
	accessaccountsv1.AllowlistStatus_ALLOWLIST_STATUS_ACTIVE:   enum.AllowlistStatusActive,
	accessaccountsv1.AllowlistStatus_ALLOWLIST_STATUS_DISABLED: enum.AllowlistStatusDisabled,
}

var allowlistStatusToProto = reverseEnumMap[enum.AllowlistStatus, accessaccountsv1.AllowlistStatus]{
	enum.AllowlistStatusActive:   accessaccountsv1.AllowlistStatus_ALLOWLIST_STATUS_ACTIVE,
	enum.AllowlistStatusDisabled: accessaccountsv1.AllowlistStatus_ALLOWLIST_STATUS_DISABLED,
}

var externalProviderKindFromProto = enumMap[accessaccountsv1.ExternalProviderKind, enum.ExternalProviderKind]{
	accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_REPOSITORY: enum.ExternalProviderRepository,
	accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_IDENTITY:   enum.ExternalProviderIdentity,
	accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_MODEL:      enum.ExternalProviderModel,
	accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_MESSAGING:  enum.ExternalProviderMessaging,
	accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_PAYMENTS:   enum.ExternalProviderPayments,
	accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_OTHER:      enum.ExternalProviderOther,
}

var externalProviderKindToProto = reverseEnumMap[enum.ExternalProviderKind, accessaccountsv1.ExternalProviderKind]{
	enum.ExternalProviderRepository: accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_REPOSITORY,
	enum.ExternalProviderIdentity:   accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_IDENTITY,
	enum.ExternalProviderModel:      accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_MODEL,
	enum.ExternalProviderMessaging:  accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_MESSAGING,
	enum.ExternalProviderPayments:   accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_PAYMENTS,
	enum.ExternalProviderOther:      accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_OTHER,
}

var externalProviderStatusFromProto = enumMap[accessaccountsv1.ExternalProviderStatus, enum.ExternalProviderStatus]{
	accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_ACTIVE:   enum.ExternalProviderStatusActive,
	accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_DISABLED: enum.ExternalProviderStatusDisabled,
}

var externalProviderStatusToProto = reverseEnumMap[enum.ExternalProviderStatus, accessaccountsv1.ExternalProviderStatus]{
	enum.ExternalProviderStatusActive:   accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_ACTIVE,
	enum.ExternalProviderStatusDisabled: accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_DISABLED,
}

var externalAccountTypeFromProto = enumMap[accessaccountsv1.ExternalAccountType, enum.ExternalAccountType]{
	accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_USER:        enum.ExternalAccountUser,
	accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_BOT:         enum.ExternalAccountBot,
	accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_SERVICE:     enum.ExternalAccountService,
	accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_INTEGRATION: enum.ExternalAccountIntegration,
}

var externalAccountTypeToProto = reverseEnumMap[enum.ExternalAccountType, accessaccountsv1.ExternalAccountType]{
	enum.ExternalAccountUser:        accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_USER,
	enum.ExternalAccountBot:         accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_BOT,
	enum.ExternalAccountService:     accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_SERVICE,
	enum.ExternalAccountIntegration: accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_INTEGRATION,
}

var externalAccountStatusFromProto = enumMap[accessaccountsv1.ExternalAccountStatus, enum.ExternalAccountStatus]{
	accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_ACTIVE:       enum.ExternalAccountStatusActive,
	accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_PENDING:      enum.ExternalAccountStatusPending,
	accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_NEEDS_REAUTH: enum.ExternalAccountStatusNeedsReauth,
	accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_LIMITED:      enum.ExternalAccountStatusLimited,
	accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_BLOCKED:      enum.ExternalAccountStatusBlocked,
	accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_DISABLED:     enum.ExternalAccountStatusDisabled,
}

var externalAccountStatusToProto = reverseEnumMap[enum.ExternalAccountStatus, accessaccountsv1.ExternalAccountStatus]{
	enum.ExternalAccountStatusActive:      accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_ACTIVE,
	enum.ExternalAccountStatusPending:     accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_PENDING,
	enum.ExternalAccountStatusNeedsReauth: accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_NEEDS_REAUTH,
	enum.ExternalAccountStatusLimited:     accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_LIMITED,
	enum.ExternalAccountStatusBlocked:     accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_BLOCKED,
	enum.ExternalAccountStatusDisabled:    accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_DISABLED,
}

var externalAccountScopeTypes = []struct {
	proto  accessaccountsv1.ExternalAccountScopeType
	domain enum.ExternalAccountScopeType
}{
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_GLOBAL, enum.ExternalAccountScopeGlobal},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_ORGANIZATION, enum.ExternalAccountScopeOrganization},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_PROJECT, enum.ExternalAccountScopeProject},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_REPOSITORY, enum.ExternalAccountScopeRepository},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_USER, enum.ExternalAccountScopeUser},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_GROUP, enum.ExternalAccountScopeGroup},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_AGENT, enum.ExternalAccountScopeAgent},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_AGENT_ROLE, enum.ExternalAccountScopeAgentRole},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_FLOW, enum.ExternalAccountScopeFlow},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_STAGE, enum.ExternalAccountScopeStage},
	{accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_PACKAGE, enum.ExternalAccountScopePackage},
}

var externalAccountBindingStatusFromProto = enumMap[accessaccountsv1.ExternalAccountBindingStatus, enum.ExternalAccountBindingStatus]{
	accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_ACTIVE:   enum.ExternalAccountBindingStatusActive,
	accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_DISABLED: enum.ExternalAccountBindingStatusDisabled,
}

var externalAccountBindingStatusToProto = reverseEnumMap[enum.ExternalAccountBindingStatus, accessaccountsv1.ExternalAccountBindingStatus]{
	enum.ExternalAccountBindingStatusActive:   accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_ACTIVE,
	enum.ExternalAccountBindingStatusDisabled: accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_DISABLED,
}

var accessActionStatusFromProto = enumMap[accessaccountsv1.AccessActionStatus, enum.AccessActionStatus]{
	accessaccountsv1.AccessActionStatus_ACCESS_ACTION_STATUS_ACTIVE:   enum.AccessActionStatusActive,
	accessaccountsv1.AccessActionStatus_ACCESS_ACTION_STATUS_DISABLED: enum.AccessActionStatusDisabled,
}

var accessActionStatusToProto = reverseEnumMap[enum.AccessActionStatus, accessaccountsv1.AccessActionStatus]{
	enum.AccessActionStatusActive:   accessaccountsv1.AccessActionStatus_ACCESS_ACTION_STATUS_ACTIVE,
	enum.AccessActionStatusDisabled: accessaccountsv1.AccessActionStatus_ACCESS_ACTION_STATUS_DISABLED,
}

var accessEffectFromProto = enumMap[accessaccountsv1.AccessEffect, enum.AccessEffect]{
	accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW: enum.AccessEffectAllow,
	accessaccountsv1.AccessEffect_ACCESS_EFFECT_DENY:  enum.AccessEffectDeny,
}

var accessEffectToProto = reverseEnumMap[enum.AccessEffect, accessaccountsv1.AccessEffect]{
	enum.AccessEffectAllow: accessaccountsv1.AccessEffect_ACCESS_EFFECT_ALLOW,
	enum.AccessEffectDeny:  accessaccountsv1.AccessEffect_ACCESS_EFFECT_DENY,
}

var accessRuleStatusFromProto = enumMap[accessaccountsv1.AccessRuleStatus, enum.AccessRuleStatus]{
	accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE:   enum.AccessRuleStatusActive,
	accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_DISABLED: enum.AccessRuleStatusDisabled,
}

var accessRuleStatusToProto = reverseEnumMap[enum.AccessRuleStatus, accessaccountsv1.AccessRuleStatus]{
	enum.AccessRuleStatusActive:   accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_ACTIVE,
	enum.AccessRuleStatusDisabled: accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_DISABLED,
}

var accessDecisionToProto = reverseEnumMap[enum.AccessDecision, accessaccountsv1.AccessDecision]{
	enum.AccessDecisionAllow:   accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW,
	enum.AccessDecisionDeny:    accessaccountsv1.AccessDecision_ACCESS_DECISION_DENY,
	enum.AccessDecisionPending: accessaccountsv1.AccessDecision_ACCESS_DECISION_PENDING,
}

var secretStoreTypeToProtoString = map[enum.SecretStoreType]string{
	enum.SecretStoreVault:            "vault",
	enum.SecretStoreKubernetesSecret: "kubernetes_secret",
}

func requiredEnum[P comparable, D ~string](value P, values enumMap[P, D]) (D, error) {
	if mapped, ok := values[value]; ok {
		return mapped, nil
	}
	return "", errs.ErrInvalidArgument
}

func optionalEnum[P comparable, D ~string](value P, zero P, values enumMap[P, D]) (D, error) {
	if value == zero {
		return "", nil
	}
	return requiredEnum(value, values)
}

func protoEnum[D comparable, P any](value D, values reverseEnumMap[D, P], unspecified P) P {
	if mapped, ok := values[value]; ok {
		return mapped
	}
	return unspecified
}

func requiredExternalAccountScopeType(value accessaccountsv1.ExternalAccountScopeType) (enum.ExternalAccountScopeType, error) {
	for _, known := range externalAccountScopeTypes {
		if known.proto == value {
			return known.domain, nil
		}
	}
	return "", errs.ErrInvalidArgument
}

func optionalExternalAccountScopeType(value accessaccountsv1.ExternalAccountScopeType) (enum.ExternalAccountScopeType, error) {
	if value == accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_UNSPECIFIED {
		return "", nil
	}
	return requiredExternalAccountScopeType(value)
}
