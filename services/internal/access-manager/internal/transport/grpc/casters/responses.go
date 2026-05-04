package casters

import (
	"time"

	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// BootstrapUserFromIdentityResponse maps a domain bootstrap result to gRPC.
func BootstrapUserFromIdentityResponse(result service.BootstrapUserFromIdentityResult) *accessaccountsv1.BootstrapUserFromIdentityResponse {
	var organizationID string
	if result.Organization != nil {
		organizationID = uuidString(result.Organization.ID)
	}
	return &accessaccountsv1.BootstrapUserFromIdentityResponse{
		UserId:         uuidString(result.User.ID),
		Status:         UserStatusToProto(result.User.Status),
		Decision:       AccessDecisionToProto(result.Decision),
		ReasonCode:     result.ReasonCode,
		OrganizationId: organizationID,
	}
}

// UserResponse maps a domain user to gRPC.
func UserResponse(user entity.User) *accessaccountsv1.UserResponse {
	return &accessaccountsv1.UserResponse{
		UserId:       uuidString(user.ID),
		PrimaryEmail: user.PrimaryEmail,
		DisplayName:  user.DisplayName,
		Status:       UserStatusToProto(user.Status),
		Locale:       user.Locale,
		Version:      user.Version,
	}
}

// OrganizationResponse maps a domain organization to gRPC.
func OrganizationResponse(organization entity.Organization) *accessaccountsv1.OrganizationResponse {
	return &accessaccountsv1.OrganizationResponse{
		OrganizationId:       uuidString(organization.ID),
		Kind:                 OrganizationKindToProto(organization.Kind),
		Slug:                 organization.Slug,
		DisplayName:          organization.DisplayName,
		ImageAssetRef:        organization.ImageAssetRef,
		Status:               OrganizationStatusToProto(organization.Status),
		ParentOrganizationId: uuidPtrString(organization.ParentOrganizationID),
		Version:              organization.Version,
	}
}

// GroupResponse maps a domain group to gRPC.
func GroupResponse(group entity.Group) *accessaccountsv1.GroupResponse {
	return &accessaccountsv1.GroupResponse{
		GroupId:       uuidString(group.ID),
		ScopeType:     GroupScopeTypeToProto(group.ScopeType),
		ScopeId:       uuidPtrString(group.ScopeID),
		Slug:          group.Slug,
		DisplayName:   group.DisplayName,
		ParentGroupId: uuidPtrString(group.ParentGroupID),
		ImageAssetRef: group.ImageAssetRef,
		Status:        GroupStatusToProto(group.Status),
		Version:       group.Version,
	}
}

// MembershipResponse maps a domain membership to gRPC.
func MembershipResponse(membership entity.Membership) *accessaccountsv1.MembershipResponse {
	return &accessaccountsv1.MembershipResponse{
		MembershipId: uuidString(membership.ID),
		SubjectType:  MembershipSubjectTypeToProto(membership.SubjectType),
		SubjectId:    uuidString(membership.SubjectID),
		TargetType:   MembershipTargetTypeToProto(membership.TargetType),
		TargetId:     uuidString(membership.TargetID),
		RoleHint:     membership.RoleHint,
		Status:       MembershipStatusToProto(membership.Status),
		Source:       MembershipSourceToProto(membership.Source),
		Version:      membership.Version,
	}
}

// AllowlistEntryResponse maps a domain allowlist entry to gRPC.
func AllowlistEntryResponse(entry entity.AllowlistEntry) *accessaccountsv1.AllowlistEntryResponse {
	return &accessaccountsv1.AllowlistEntryResponse{
		AllowlistEntryId: uuidString(entry.ID),
		MatchType:        AllowlistMatchTypeToProto(entry.MatchType),
		Value:            entry.Value,
		OrganizationId:   uuidPtrString(entry.OrganizationID),
		DefaultStatus:    UserStatusToProto(entry.DefaultStatus),
		Status:           AllowlistStatusToProto(entry.Status),
		Version:          entry.Version,
	}
}

// ExternalProviderResponse maps a domain provider to gRPC.
func ExternalProviderResponse(provider entity.ExternalProvider) *accessaccountsv1.ExternalProviderResponse {
	return &accessaccountsv1.ExternalProviderResponse{
		ExternalProviderId: uuidString(provider.ID),
		Slug:               provider.Slug,
		ProviderKind:       ExternalProviderKindToProto(provider.ProviderKind),
		DisplayName:        provider.DisplayName,
		IconAssetRef:       provider.IconAssetRef,
		Status:             ExternalProviderStatusToProto(provider.Status),
		Version:            provider.Version,
	}
}

// ExternalAccountResponse maps a domain external account to gRPC.
func ExternalAccountResponse(account entity.ExternalAccount) *accessaccountsv1.ExternalAccountResponse {
	return &accessaccountsv1.ExternalAccountResponse{
		ExternalAccountId:  uuidString(account.ID),
		ExternalProviderId: uuidString(account.ExternalProviderID),
		AccountType:        ExternalAccountTypeToProto(account.AccountType),
		DisplayName:        account.DisplayName,
		ImageAssetRef:      account.ImageAssetRef,
		OwnerScopeType:     ExternalAccountScopeTypeToProto(account.OwnerScopeType),
		OwnerScopeId:       account.OwnerScopeID,
		Status:             ExternalAccountStatusToProto(account.Status),
		SecretBindingRefId: uuidPtrString(account.SecretBindingRefID),
		Version:            account.Version,
	}
}

// ExternalAccountBindingResponse maps a domain account binding to gRPC.
func ExternalAccountBindingResponse(binding entity.ExternalAccountBinding) *accessaccountsv1.ExternalAccountBindingResponse {
	return &accessaccountsv1.ExternalAccountBindingResponse{
		ExternalAccountBindingId: uuidString(binding.ID),
		ExternalAccountId:        uuidString(binding.ExternalAccountID),
		UsageScopeType:           ExternalAccountScopeTypeToProto(binding.UsageScopeType),
		UsageScopeId:             binding.UsageScopeID,
		AllowedActionKeys:        binding.AllowedActionKeys,
		Status:                   ExternalAccountBindingStatusToProto(binding.Status),
		Version:                  binding.Version,
	}
}

// AccessActionResponse maps a domain access action to gRPC.
func AccessActionResponse(action entity.AccessAction) *accessaccountsv1.AccessActionResponse {
	return &accessaccountsv1.AccessActionResponse{
		AccessActionId: uuidString(action.ID),
		Key:            action.Key,
		DisplayName:    action.DisplayName,
		Description:    action.Description,
		ResourceType:   action.ResourceType,
		Status:         AccessActionStatusToProto(action.Status),
		Version:        action.Version,
	}
}

// AccessRuleResponse maps a domain access rule to gRPC.
func AccessRuleResponse(rule entity.AccessRule) *accessaccountsv1.AccessRuleResponse {
	return &accessaccountsv1.AccessRuleResponse{
		AccessRuleId: uuidString(rule.ID),
		Effect:       AccessEffectToProto(rule.Effect),
		SubjectType:  string(rule.SubjectType),
		SubjectId:    rule.SubjectID,
		ActionKey:    rule.ActionKey,
		ResourceType: rule.ResourceType,
		ResourceId:   rule.ResourceID,
		ScopeType:    rule.ScopeType,
		ScopeId:      rule.ScopeID,
		Priority:     int32(rule.Priority),
		Status:       AccessRuleStatusToProto(rule.Status),
		Version:      rule.Version,
	}
}

// CheckAccessResponse maps a domain access decision to gRPC.
func CheckAccessResponse(result service.CheckAccessResult) *accessaccountsv1.CheckAccessResponse {
	return &accessaccountsv1.CheckAccessResponse{
		Decision:      AccessDecisionToProto(result.Decision),
		ReasonCode:    result.ReasonCode,
		PolicyVersion: result.Explanation.PolicyVersion,
		MatchedRules:  MatchedRulesToProto(result.Explanation.MatchedRules),
	}
}

// ExplainAccessResponse maps a stored access decision explanation to gRPC.
func ExplainAccessResponse(result service.ExplainAccessResult) *accessaccountsv1.ExplainAccessResponse {
	audit := result.Audit
	return &accessaccountsv1.ExplainAccessResponse{
		AuditId:        uuidString(audit.ID),
		Decision:       AccessDecisionToProto(audit.Decision),
		ReasonCode:     audit.ReasonCode,
		PolicyVersion:  audit.PolicyVersion,
		MatchedRules:   MatchedRulesToProto(audit.Explanation.MatchedRules),
		Subject:        SubjectRefToProto(audit.Subject),
		ActionKey:      audit.ActionKey,
		Resource:       ResourceRefToProto(audit.Resource),
		Scope:          ScopeRefToProto(audit.Scope),
		RequestContext: RequestContextToProto(audit.RequestContext),
		CreatedAt:      audit.CreatedAt.Format(time.RFC3339Nano),
	}
}

// ResolveExternalAccountUsageResponse maps a domain account usage result to gRPC.
func ResolveExternalAccountUsageResponse(result service.ResolveExternalAccountUsageResult) *accessaccountsv1.ResolveExternalAccountUsageResponse {
	return &accessaccountsv1.ResolveExternalAccountUsageResponse{
		ExternalAccountId: uuidString(result.ExternalAccount.ID),
		ProviderId:        uuidString(result.ExternalAccount.ExternalProviderID),
		SecretRefId:       uuidString(result.SecretRef.ID),
		SecretStoreType:   secretStoreTypeToProtoString[result.SecretRef.StoreType],
		SecretStoreRef:    result.SecretRef.StoreRef,
		AllowedActionKeys: result.AllowedActions,
	}
}

// ListPendingAccessResponse maps pending access items to gRPC.
func ListPendingAccessResponse(result service.ListPendingAccessResult) *accessaccountsv1.ListPendingAccessResponse {
	items := make([]*accessaccountsv1.PendingAccessItem, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, &accessaccountsv1.PendingAccessItem{
			ItemId:     item.ItemID,
			ItemType:   item.ItemType,
			Subject:    SubjectRefToProto(item.Subject),
			Status:     item.Status,
			ReasonCode: item.ReasonCode,
			CreatedAt:  item.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	return &accessaccountsv1.ListPendingAccessResponse{Items: items, NextCursor: result.NextCursor}
}

// MatchedRulesToProto maps access decision explanation rules to gRPC.
func MatchedRulesToProto(rules []value.RuleExplanation) []*accessaccountsv1.MatchedRule {
	result := make([]*accessaccountsv1.MatchedRule, 0, len(rules))
	for _, rule := range rules {
		result = append(result, &accessaccountsv1.MatchedRule{
			RuleId:     uuidString(rule.RuleID),
			Effect:     AccessEffectToProto(enum.AccessEffect(rule.Effect)),
			Subject:    SubjectRefToProto(rule.Subject),
			ActionKey:  rule.ActionKey,
			Scope:      ScopeRefToProto(rule.Scope),
			Priority:   int32(rule.Priority),
			ReasonCode: rule.ReasonCode,
		})
	}
	return result
}

// OrganizationKindToProto maps a domain organization kind to gRPC.
func OrganizationKindToProto(value enum.OrganizationKind) accessaccountsv1.OrganizationKind {
	return protoEnum(value, organizationKindToProto, accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_UNSPECIFIED)
}

// OrganizationStatusToProto maps a domain organization status to gRPC.
func OrganizationStatusToProto(value enum.OrganizationStatus) accessaccountsv1.OrganizationStatus {
	return protoEnum(value, organizationStatusToProto, accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_UNSPECIFIED)
}

// UserStatusToProto maps a domain user status to gRPC.
func UserStatusToProto(value enum.UserStatus) accessaccountsv1.UserStatus {
	return protoEnum(value, userStatusToProto, accessaccountsv1.UserStatus_USER_STATUS_UNSPECIFIED)
}

// GroupScopeTypeToProto maps a domain group scope type to gRPC.
func GroupScopeTypeToProto(value enum.GroupScopeType) accessaccountsv1.GroupScopeType {
	return protoEnum(value, groupScopeTypeToProto, accessaccountsv1.GroupScopeType_GROUP_SCOPE_TYPE_UNSPECIFIED)
}

// GroupStatusToProto maps a domain group status to gRPC.
func GroupStatusToProto(value enum.GroupStatus) accessaccountsv1.GroupStatus {
	return protoEnum(value, groupStatusToProto, accessaccountsv1.GroupStatus_GROUP_STATUS_UNSPECIFIED)
}

// MembershipSubjectTypeToProto maps a domain membership subject type to gRPC.
func MembershipSubjectTypeToProto(value enum.MembershipSubjectType) accessaccountsv1.MembershipSubjectType {
	return protoEnum(value, membershipSubjectTypeToProto, accessaccountsv1.MembershipSubjectType_MEMBERSHIP_SUBJECT_TYPE_UNSPECIFIED)
}

// MembershipTargetTypeToProto maps a domain membership target type to gRPC.
func MembershipTargetTypeToProto(value enum.MembershipTargetType) accessaccountsv1.MembershipTargetType {
	return protoEnum(value, membershipTargetTypeToProto, accessaccountsv1.MembershipTargetType_MEMBERSHIP_TARGET_TYPE_UNSPECIFIED)
}

// MembershipStatusToProto maps a domain membership status to gRPC.
func MembershipStatusToProto(value enum.MembershipStatus) accessaccountsv1.MembershipStatus {
	return protoEnum(value, membershipStatusToProto, accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_UNSPECIFIED)
}

// MembershipSourceToProto maps a domain membership source to gRPC.
func MembershipSourceToProto(value enum.MembershipSource) accessaccountsv1.MembershipSource {
	return protoEnum(value, membershipSourceToProto, accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_UNSPECIFIED)
}

// AllowlistMatchTypeToProto maps a domain allowlist match type to gRPC.
func AllowlistMatchTypeToProto(value enum.AllowlistMatchType) accessaccountsv1.AllowlistMatchType {
	return protoEnum(value, allowlistMatchTypeToProto, accessaccountsv1.AllowlistMatchType_ALLOWLIST_MATCH_TYPE_UNSPECIFIED)
}

// AllowlistStatusToProto maps a domain allowlist status to gRPC.
func AllowlistStatusToProto(value enum.AllowlistStatus) accessaccountsv1.AllowlistStatus {
	return protoEnum(value, allowlistStatusToProto, accessaccountsv1.AllowlistStatus_ALLOWLIST_STATUS_UNSPECIFIED)
}

// ExternalProviderKindToProto maps a domain provider kind to gRPC.
func ExternalProviderKindToProto(value enum.ExternalProviderKind) accessaccountsv1.ExternalProviderKind {
	return protoEnum(value, externalProviderKindToProto, accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_UNSPECIFIED)
}

// ExternalProviderStatusToProto maps a domain provider status to gRPC.
func ExternalProviderStatusToProto(value enum.ExternalProviderStatus) accessaccountsv1.ExternalProviderStatus {
	return protoEnum(value, externalProviderStatusToProto, accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_UNSPECIFIED)
}

// ExternalAccountTypeToProto maps a domain external account type to gRPC.
func ExternalAccountTypeToProto(value enum.ExternalAccountType) accessaccountsv1.ExternalAccountType {
	return protoEnum(value, externalAccountTypeToProto, accessaccountsv1.ExternalAccountType_EXTERNAL_ACCOUNT_TYPE_UNSPECIFIED)
}

// ExternalAccountStatusToProto maps a domain external account status to gRPC.
func ExternalAccountStatusToProto(value enum.ExternalAccountStatus) accessaccountsv1.ExternalAccountStatus {
	return protoEnum(value, externalAccountStatusToProto, accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_UNSPECIFIED)
}

// ExternalAccountScopeTypeToProto maps a domain external account scope type to gRPC.
func ExternalAccountScopeTypeToProto(value enum.ExternalAccountScopeType) accessaccountsv1.ExternalAccountScopeType {
	for _, known := range externalAccountScopeTypes {
		if known.domain == value {
			return known.proto
		}
	}
	return accessaccountsv1.ExternalAccountScopeType_EXTERNAL_ACCOUNT_SCOPE_TYPE_UNSPECIFIED
}

// ExternalAccountBindingStatusToProto maps a domain account binding status to gRPC.
func ExternalAccountBindingStatusToProto(value enum.ExternalAccountBindingStatus) accessaccountsv1.ExternalAccountBindingStatus {
	return protoEnum(value, externalAccountBindingStatusToProto, accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_UNSPECIFIED)
}

// AccessActionStatusToProto maps a domain access action status to gRPC.
func AccessActionStatusToProto(value enum.AccessActionStatus) accessaccountsv1.AccessActionStatus {
	return protoEnum(value, accessActionStatusToProto, accessaccountsv1.AccessActionStatus_ACCESS_ACTION_STATUS_UNSPECIFIED)
}

// AccessEffectToProto maps a domain access effect to gRPC.
func AccessEffectToProto(value enum.AccessEffect) accessaccountsv1.AccessEffect {
	return protoEnum(value, accessEffectToProto, accessaccountsv1.AccessEffect_ACCESS_EFFECT_UNSPECIFIED)
}

// AccessRuleStatusToProto maps a domain access rule status to gRPC.
func AccessRuleStatusToProto(value enum.AccessRuleStatus) accessaccountsv1.AccessRuleStatus {
	return protoEnum(value, accessRuleStatusToProto, accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_UNSPECIFIED)
}

// AccessDecisionToProto maps a domain access decision to gRPC.
func AccessDecisionToProto(value enum.AccessDecision) accessaccountsv1.AccessDecision {
	return protoEnum(value, accessDecisionToProto, accessaccountsv1.AccessDecision_ACCESS_DECISION_UNSPECIFIED)
}
