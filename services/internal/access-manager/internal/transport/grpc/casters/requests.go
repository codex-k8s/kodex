package casters

import (
	"strings"

	"github.com/google/uuid"

	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	accessservice "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// BootstrapUserFromIdentityInput maps a gRPC request to the domain command input.
func BootstrapUserFromIdentityInput(request *accessaccountsv1.BootstrapUserFromIdentityRequest) (accessservice.BootstrapUserFromIdentityInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.BootstrapUserFromIdentityInput{}, err
	}
	return accessservice.BootstrapUserFromIdentityInput{
		Provider:    enum.IdentityProvider(strings.TrimSpace(request.GetProvider())),
		Subject:     strings.TrimSpace(request.GetSubject()),
		Email:       strings.TrimSpace(request.GetEmail()),
		DisplayName: strings.TrimSpace(request.GetDisplayName()),
		Locale:      strings.TrimSpace(request.GetLocale()),
		Meta:        meta,
	}, nil
}

// SetUserStatusInput maps a gRPC request to the domain command input.
func SetUserStatusInput(request *accessaccountsv1.SetUserStatusRequest) (accessservice.SetUserStatusInput, error) {
	return commandInputWithIDAndStatus(request.GetMeta(), request.GetUserId(), request.GetStatus(), userStatusFromProto, setUserStatusInput)
}

// CreateOrganizationInput maps a gRPC request to the domain command input.
func CreateOrganizationInput(request *accessaccountsv1.CreateOrganizationRequest) (accessservice.CreateOrganizationInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.CreateOrganizationInput{}, err
	}
	kind, err := requiredEnum(request.GetKind(), organizationKindFromProto)
	if err != nil {
		return accessservice.CreateOrganizationInput{}, err
	}
	status, err := optionalEnum(
		request.GetStatus(),
		accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_UNSPECIFIED,
		organizationStatusFromProto,
	)
	if err != nil {
		return accessservice.CreateOrganizationInput{}, err
	}
	parentID, err := optionalUUIDPtr(request.GetParentOrganizationId())
	if err != nil {
		return accessservice.CreateOrganizationInput{}, err
	}
	return accessservice.CreateOrganizationInput{
		Kind:                 kind,
		Slug:                 strings.TrimSpace(request.GetSlug()),
		DisplayName:          strings.TrimSpace(request.GetDisplayName()),
		ImageAssetRef:        strings.TrimSpace(request.GetImageAssetRef()),
		Status:               status,
		ParentOrganizationID: parentID,
		Meta:                 meta,
	}, nil
}

// CreateGroupInput maps a gRPC request to the domain command input.
func CreateGroupInput(request *accessaccountsv1.CreateGroupRequest) (accessservice.CreateGroupInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.CreateGroupInput{}, err
	}
	scopeType, err := requiredEnum(request.GetScopeType(), groupScopeTypeFromProto)
	if err != nil {
		return accessservice.CreateGroupInput{}, err
	}
	scopeID, err := optionalUUIDPtr(request.GetScopeId())
	if err != nil {
		return accessservice.CreateGroupInput{}, err
	}
	parentID, err := optionalUUIDPtr(request.GetParentGroupId())
	if err != nil {
		return accessservice.CreateGroupInput{}, err
	}
	return accessservice.CreateGroupInput{
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		Slug:          strings.TrimSpace(request.GetSlug()),
		DisplayName:   strings.TrimSpace(request.GetDisplayName()),
		ParentGroupID: parentID,
		ImageAssetRef: strings.TrimSpace(request.GetImageAssetRef()),
		Meta:          meta,
	}, nil
}

// SetMembershipInput maps a gRPC request to the domain command input.
func SetMembershipInput(request *accessaccountsv1.SetMembershipRequest) (accessservice.SetMembershipInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	subjectType, err := requiredEnum(request.GetSubjectType(), membershipSubjectTypeFromProto)
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	targetType, err := requiredEnum(request.GetTargetType(), membershipTargetTypeFromProto)
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	status, err := optionalEnum(request.GetStatus(), accessaccountsv1.MembershipStatus_MEMBERSHIP_STATUS_UNSPECIFIED, membershipStatusFromProto)
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	source, err := optionalEnum(request.GetSource(), accessaccountsv1.MembershipSource_MEMBERSHIP_SOURCE_UNSPECIFIED, membershipSourceFromProto)
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	subjectID, err := requiredUUID(request.GetSubjectId())
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	targetID, err := requiredUUID(request.GetTargetId())
	if err != nil {
		return accessservice.SetMembershipInput{}, err
	}
	return accessservice.SetMembershipInput{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		TargetType:  targetType,
		TargetID:    targetID,
		RoleHint:    strings.TrimSpace(request.GetRoleHint()),
		Status:      status,
		Source:      source,
		Meta:        meta,
	}, nil
}

// PutAllowlistEntryInput maps a gRPC request to the domain command input.
func PutAllowlistEntryInput(request *accessaccountsv1.PutAllowlistEntryRequest) (accessservice.PutAllowlistEntryInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.PutAllowlistEntryInput{}, err
	}
	matchType, err := requiredEnum(request.GetMatchType(), allowlistMatchTypeFromProto)
	if err != nil {
		return accessservice.PutAllowlistEntryInput{}, err
	}
	defaultStatus, err := optionalEnum(request.GetDefaultStatus(), accessaccountsv1.UserStatus_USER_STATUS_UNSPECIFIED, userStatusFromProto)
	if err != nil {
		return accessservice.PutAllowlistEntryInput{}, err
	}
	status, err := optionalEnum(request.GetStatus(), accessaccountsv1.AllowlistStatus_ALLOWLIST_STATUS_UNSPECIFIED, allowlistStatusFromProto)
	if err != nil {
		return accessservice.PutAllowlistEntryInput{}, err
	}
	organizationID, err := optionalUUIDPtr(request.GetOrganizationId())
	if err != nil {
		return accessservice.PutAllowlistEntryInput{}, err
	}
	return accessservice.PutAllowlistEntryInput{
		MatchType:      matchType,
		Value:          strings.TrimSpace(request.GetValue()),
		OrganizationID: organizationID,
		DefaultStatus:  defaultStatus,
		Status:         status,
		Meta:           meta,
	}, nil
}

// DisableAllowlistEntryInput maps a gRPC request to the domain command input.
func DisableAllowlistEntryInput(request *accessaccountsv1.DisableAllowlistEntryRequest) (accessservice.DisableAllowlistEntryInput, error) {
	return commandInputWithID(request.GetMeta(), request.GetAllowlistEntryId(), disableAllowlistEntryInput)
}

// PutExternalProviderInput maps a gRPC request to the domain command input.
func PutExternalProviderInput(request *accessaccountsv1.RegisterExternalProviderRequest) (accessservice.PutExternalProviderInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.PutExternalProviderInput{}, err
	}
	providerKind, err := requiredEnum(request.GetProviderKind(), externalProviderKindFromProto)
	if err != nil {
		return accessservice.PutExternalProviderInput{}, err
	}
	status, err := optionalEnum(
		request.GetStatus(),
		accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_UNSPECIFIED,
		externalProviderStatusFromProto,
	)
	if err != nil {
		return accessservice.PutExternalProviderInput{}, err
	}
	return accessservice.PutExternalProviderInput{
		Slug:         strings.TrimSpace(request.GetSlug()),
		ProviderKind: providerKind,
		DisplayName:  strings.TrimSpace(request.GetDisplayName()),
		IconAssetRef: strings.TrimSpace(request.GetIconAssetRef()),
		Status:       status,
		CreateOnly:   true,
		Meta:         meta,
	}, nil
}

// UpdateExternalProviderInput maps a gRPC request to the domain command input.
func UpdateExternalProviderInput(request *accessaccountsv1.UpdateExternalProviderRequest) (accessservice.UpdateExternalProviderInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.UpdateExternalProviderInput{}, err
	}
	providerID, err := requiredUUID(request.GetExternalProviderId())
	if err != nil {
		return accessservice.UpdateExternalProviderInput{}, err
	}
	providerKind, err := optionalEnum(
		request.GetProviderKind(),
		accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_UNSPECIFIED,
		externalProviderKindFromProto,
	)
	if err != nil {
		return accessservice.UpdateExternalProviderInput{}, err
	}
	status, err := optionalEnum(
		request.GetStatus(),
		accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_UNSPECIFIED,
		externalProviderStatusFromProto,
	)
	if err != nil {
		return accessservice.UpdateExternalProviderInput{}, err
	}
	slug, displayName, iconAssetRef := updateExternalProviderStringFields(request)
	return accessservice.UpdateExternalProviderInput{
		ExternalProviderID: providerID,
		Slug:               slug,
		ProviderKind:       providerKind,
		DisplayName:        displayName,
		IconAssetRef:       iconAssetRef,
		Status:             status,
		Meta:               meta,
	}, nil
}

// RegisterExternalAccountInput maps a gRPC request to the domain command input.
func RegisterExternalAccountInput(request *accessaccountsv1.RegisterExternalAccountRequest) (accessservice.RegisterExternalAccountInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.RegisterExternalAccountInput{}, err
	}
	providerID, err := requiredUUID(request.GetExternalProviderId())
	if err != nil {
		return accessservice.RegisterExternalAccountInput{}, err
	}
	accountType, err := requiredEnum(request.GetAccountType(), externalAccountTypeFromProto)
	if err != nil {
		return accessservice.RegisterExternalAccountInput{}, err
	}
	scopeType, err := optionalExternalAccountScopeType(request.GetOwnerScopeType())
	if err != nil {
		return accessservice.RegisterExternalAccountInput{}, err
	}
	status, err := optionalEnum(
		request.GetStatus(),
		accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_UNSPECIFIED,
		externalAccountStatusFromProto,
	)
	if err != nil {
		return accessservice.RegisterExternalAccountInput{}, err
	}
	secretID, err := optionalUUIDPtr(request.GetSecretBindingRefId())
	if err != nil {
		return accessservice.RegisterExternalAccountInput{}, err
	}
	return accessservice.RegisterExternalAccountInput{
		ExternalProviderID: providerID,
		AccountType:        accountType,
		DisplayName:        strings.TrimSpace(request.GetDisplayName()),
		ImageAssetRef:      strings.TrimSpace(request.GetImageAssetRef()),
		OwnerScopeType:     scopeType,
		OwnerScopeID:       strings.TrimSpace(request.GetOwnerScopeId()),
		Status:             status,
		SecretBindingRefID: secretID,
		Meta:               meta,
	}, nil
}

// UpdateExternalAccountStatusInput maps a gRPC request to the domain command input.
func UpdateExternalAccountStatusInput(request *accessaccountsv1.UpdateExternalAccountStatusRequest) (accessservice.UpdateExternalAccountStatusInput, error) {
	return commandInputWithIDAndStatus(request.GetMeta(), request.GetExternalAccountId(), request.GetStatus(), externalAccountStatusFromProto, updateExternalAccountStatusInput)
}

// BindExternalAccountInput maps a gRPC request to the domain command input.
func BindExternalAccountInput(request *accessaccountsv1.BindExternalAccountRequest) (accessservice.BindExternalAccountInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.BindExternalAccountInput{}, err
	}
	accountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return accessservice.BindExternalAccountInput{}, err
	}
	scopeType, err := requiredExternalAccountScopeType(request.GetUsageScopeType())
	if err != nil {
		return accessservice.BindExternalAccountInput{}, err
	}
	status, err := optionalEnum(
		request.GetStatus(),
		accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_UNSPECIFIED,
		externalAccountBindingStatusFromProto,
	)
	if err != nil {
		return accessservice.BindExternalAccountInput{}, err
	}
	return accessservice.BindExternalAccountInput{
		ExternalAccountID: accountID,
		UsageScopeType:    scopeType,
		UsageScopeID:      strings.TrimSpace(request.GetUsageScopeId()),
		AllowedActionKeys: trimStrings(request.GetAllowedActionKeys()),
		Status:            status,
		Meta:              meta,
	}, nil
}

// DisableExternalAccountBindingInput maps a gRPC request to the domain command input.
func DisableExternalAccountBindingInput(request *accessaccountsv1.DisableExternalAccountBindingRequest) (accessservice.DisableExternalAccountBindingInput, error) {
	return commandInputWithID(request.GetMeta(), request.GetExternalAccountBindingId(), disableExternalAccountBindingInput)
}

// PutAccessActionInput maps a gRPC request to the domain command input.
func PutAccessActionInput(request *accessaccountsv1.PutAccessActionRequest) (accessservice.PutAccessActionInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.PutAccessActionInput{}, err
	}
	status, err := optionalEnum(request.GetStatus(), accessaccountsv1.AccessActionStatus_ACCESS_ACTION_STATUS_UNSPECIFIED, accessActionStatusFromProto)
	if err != nil {
		return accessservice.PutAccessActionInput{}, err
	}
	return accessservice.PutAccessActionInput{
		Key:          strings.TrimSpace(request.GetKey()),
		DisplayName:  strings.TrimSpace(request.GetDisplayName()),
		Description:  strings.TrimSpace(request.GetDescription()),
		ResourceType: strings.TrimSpace(request.GetResourceType()),
		Status:       status,
		Meta:         meta,
	}, nil
}

// PutAccessRuleInput maps a gRPC request to the domain command input.
func PutAccessRuleInput(request *accessaccountsv1.PutAccessRuleRequest) (accessservice.PutAccessRuleInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.PutAccessRuleInput{}, err
	}
	effect, err := requiredEnum(request.GetEffect(), accessEffectFromProto)
	if err != nil {
		return accessservice.PutAccessRuleInput{}, err
	}
	status, err := optionalEnum(request.GetStatus(), accessaccountsv1.AccessRuleStatus_ACCESS_RULE_STATUS_UNSPECIFIED, accessRuleStatusFromProto)
	if err != nil {
		return accessservice.PutAccessRuleInput{}, err
	}
	return accessservice.PutAccessRuleInput{
		Effect:       effect,
		SubjectType:  enum.AccessSubjectType(strings.TrimSpace(request.GetSubjectType())),
		SubjectID:    strings.TrimSpace(request.GetSubjectId()),
		ActionKey:    strings.TrimSpace(request.GetActionKey()),
		ResourceType: strings.TrimSpace(request.GetResourceType()),
		ResourceID:   strings.TrimSpace(request.GetResourceId()),
		ScopeType:    strings.TrimSpace(request.GetScopeType()),
		ScopeID:      strings.TrimSpace(request.GetScopeId()),
		Priority:     int(request.GetPriority()),
		Status:       status,
		Meta:         meta,
	}, nil
}

// CheckAccessInput maps a gRPC request to the domain read input.
func CheckAccessInput(request *accessaccountsv1.CheckAccessRequest) (accessservice.CheckAccessInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.CheckAccessInput{}, err
	}
	return accessservice.CheckAccessInput{
		Subject:   SubjectRefFromProto(request.GetSubject()),
		ActionKey: strings.TrimSpace(request.GetActionKey()),
		Resource:  ResourceRefFromProto(request.GetResource()),
		Scope:     ScopeRefFromProto(request.GetScope()),
		Audit:     true,
		Meta:      meta,
	}, nil
}

// ExplainAccessInput maps a gRPC request to the domain read input.
func ExplainAccessInput(request *accessaccountsv1.ExplainAccessRequest) (accessservice.ExplainAccessInput, error) {
	auditID, err := requiredUUID(request.GetAuditId())
	if err != nil {
		return accessservice.ExplainAccessInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.ExplainAccessInput{}, err
	}
	return accessservice.ExplainAccessInput{AuditID: auditID, Scope: ScopeRefFromProto(request.GetScope()), Meta: meta}, nil
}

// ResolveExternalAccountUsageInput maps a gRPC request to the domain read input.
func ResolveExternalAccountUsageInput(request *accessaccountsv1.ResolveExternalAccountUsageRequest) (accessservice.ResolveExternalAccountUsageInput, error) {
	accountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return accessservice.ResolveExternalAccountUsageInput{}, err
	}
	return accessservice.ResolveExternalAccountUsageInput{
		ExternalAccountID: accountID,
		ActionKey:         strings.TrimSpace(request.GetActionKey()),
		UsageScope:        ScopeRefFromProto(request.GetUsageScope()),
	}, nil
}

// ListPendingAccessInput maps a gRPC request to the domain read input.
func ListPendingAccessInput(request *accessaccountsv1.ListPendingAccessRequest) (accessservice.ListPendingAccessInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return accessservice.ListPendingAccessInput{}, err
	}
	return accessservice.ListPendingAccessInput{
		Scope:  ScopeRefFromProto(request.GetScope()),
		Limit:  int(request.GetLimit()),
		Cursor: strings.TrimSpace(request.GetCursor()),
		Meta:   meta,
	}, nil
}

func trimStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func optionalTrimmedString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func updateExternalProviderStringFields(request *accessaccountsv1.UpdateExternalProviderRequest) (*string, *string, *string) {
	if request == nil {
		return nil, nil, nil
	}
	return optionalTrimmedString(request.Slug), optionalTrimmedString(request.DisplayName), optionalTrimmedString(request.IconAssetRef)
}

func commandMetaRequiredID(metaProto *accessaccountsv1.CommandMeta, rawID string) (value.CommandMeta, uuid.UUID, error) {
	meta, err := CommandMetaFromProto(metaProto)
	if err != nil {
		return value.CommandMeta{}, uuid.Nil, err
	}
	id, err := requiredUUID(rawID)
	if err != nil {
		return value.CommandMeta{}, uuid.Nil, err
	}
	return meta, id, nil
}

func commandInputWithID[T any](
	metaProto *accessaccountsv1.CommandMeta,
	rawID string,
	build func(value.CommandMeta, uuid.UUID) T,
) (T, error) {
	meta, id, err := commandMetaRequiredID(metaProto, rawID)
	if err != nil {
		var zero T
		return zero, err
	}
	return build(meta, id), nil
}

func commandInputWithIDAndStatus[P comparable, D ~string, T any](
	metaProto *accessaccountsv1.CommandMeta,
	rawID string,
	rawStatus P,
	values enumMap[P, D],
	build func(value.CommandMeta, uuid.UUID, D) T,
) (T, error) {
	meta, id, err := commandMetaRequiredID(metaProto, rawID)
	if err != nil {
		var zero T
		return zero, err
	}
	status, err := requiredEnum(rawStatus, values)
	if err != nil {
		var zero T
		return zero, err
	}
	return build(meta, id, status), nil
}

func setUserStatusInput(meta value.CommandMeta, userID uuid.UUID, status enum.UserStatus) accessservice.SetUserStatusInput {
	return accessservice.SetUserStatusInput{UserID: userID, Status: status, Meta: meta}
}

func disableAllowlistEntryInput(meta value.CommandMeta, allowlistEntryID uuid.UUID) accessservice.DisableAllowlistEntryInput {
	return accessservice.DisableAllowlistEntryInput{AllowlistEntryID: allowlistEntryID, Meta: meta}
}

func updateExternalAccountStatusInput(meta value.CommandMeta, accountID uuid.UUID, status enum.ExternalAccountStatus) accessservice.UpdateExternalAccountStatusInput {
	return accessservice.UpdateExternalAccountStatusInput{ExternalAccountID: accountID, Status: status, Meta: meta}
}

func disableExternalAccountBindingInput(meta value.CommandMeta, bindingID uuid.UUID) accessservice.DisableExternalAccountBindingInput {
	return accessservice.DisableExternalAccountBindingInput{ExternalAccountBindingID: bindingID, Meta: meta}
}
