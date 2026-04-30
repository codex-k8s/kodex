package service

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/helpers"
	accessrepo "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/repository/access"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// Service coordinates access-manager domain commands and reads.
type Service struct {
	repository accessrepo.Repository
	clock      accessrepo.Clock
	ids        accessrepo.IDGenerator
}

// New creates a domain service with injected persistence, clock and id generator.
func New(repository accessrepo.Repository, clock accessrepo.Clock, ids accessrepo.IDGenerator) *Service {
	return &Service{repository: repository, clock: clock, ids: ids}
}

// CreateOrganization creates a tenant organization and enforces owner invariants.
func (s *Service) CreateOrganization(ctx context.Context, input CreateOrganizationInput) (entity.Organization, error) {
	if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.DisplayName) == "" {
		return entity.Organization{}, errs.ErrInvalidArgument
	}
	status := defaultOrganizationStatus(input.Status)
	applied, ok, err := s.findCommandResult(ctx, input.Meta, accessOperationCreateOrganization, accessAggregateOrganization)
	if err != nil {
		return entity.Organization{}, err
	}
	if ok {
		return s.repository.GetOrganization(ctx, applied.AggregateID)
	}
	if input.Kind == enum.OrganizationKindOwner {
		if status != enum.OrganizationStatusActive {
			return entity.Organization{}, errs.ErrPreconditionFailed
		}
		count, err := s.repository.CountActiveOwnerOrganizations(ctx)
		if err != nil {
			return entity.Organization{}, err
		}
		if count > 0 {
			return entity.Organization{}, errs.ErrAlreadyExists
		}
	}

	now := s.now(input.Meta)
	organization := entity.Organization{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Kind:                 input.Kind,
		Slug:                 helpers.NormalizeSlug(input.Slug),
		DisplayName:          strings.TrimSpace(input.DisplayName),
		ImageAssetRef:        strings.TrimSpace(input.ImageAssetRef),
		Status:               status,
		ParentOrganizationID: input.ParentOrganizationID,
	}

	event, err := s.createdEvent(
		accessEventOrganizationCreated, accessAggregateOrganization, organization.ID, now,
		payloadString(accessPayloadOrganizationID, organization.ID.String()),
		payloadString(accessPayloadKind, string(organization.Kind)),
		payloadString(accessPayloadStatus, string(organization.Status)),
		payloadVersion(organization.Version),
	)
	if err != nil {
		return entity.Organization{}, err
	}
	result, err := commandResult(input.Meta, accessOperationCreateOrganization, accessAggregateOrganization, organization.ID, now)
	if err != nil {
		return entity.Organization{}, err
	}
	if err := s.repository.CreateOrganization(ctx, organization, event, result); err != nil {
		return entity.Organization{}, err
	}
	return organization, nil
}

// CreateGroup creates a global or organization-scoped group.
func (s *Service) CreateGroup(ctx context.Context, input CreateGroupInput) (entity.Group, error) {
	if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.DisplayName) == "" {
		return entity.Group{}, errs.ErrInvalidArgument
	}
	switch input.ScopeType {
	case enum.GroupScopeGlobal:
		if input.ScopeID != nil {
			return entity.Group{}, errs.ErrInvalidArgument
		}
	case enum.GroupScopeOrganization:
		if input.ScopeID == nil {
			return entity.Group{}, errs.ErrInvalidArgument
		}
	default:
		return entity.Group{}, errs.ErrInvalidArgument
	}
	applied, ok, err := s.findCommandResult(ctx, input.Meta, accessOperationCreateGroup, accessAggregateGroup)
	if err != nil {
		return entity.Group{}, err
	}
	if ok {
		return s.repository.GetGroup(ctx, applied.AggregateID)
	}
	if input.ParentGroupID != nil {
		parent, err := s.repository.GetGroup(ctx, *input.ParentGroupID)
		if err != nil {
			return entity.Group{}, err
		}
		if parent.ScopeType != input.ScopeType || !sameUUIDPtr(parent.ScopeID, input.ScopeID) {
			return entity.Group{}, errs.ErrPreconditionFailed
		}
	}

	now := s.now(input.Meta)
	group := entity.Group{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		ScopeType:     input.ScopeType,
		ScopeID:       input.ScopeID,
		Slug:          helpers.NormalizeSlug(input.Slug),
		DisplayName:   strings.TrimSpace(input.DisplayName),
		ParentGroupID: input.ParentGroupID,
		ImageAssetRef: strings.TrimSpace(input.ImageAssetRef),
		Status:        enum.GroupStatusActive,
	}

	groupCreatedPayload := []accessEventPayloadOption{
		payloadString(accessPayloadGroupID, group.ID.String()),
		payloadString(accessPayloadScopeType, string(group.ScopeType)),
		payloadString(accessPayloadScopeID, uuidPtrString(group.ScopeID)),
		payloadVersion(group.Version),
	}
	groupCreatedEvent, err := s.createdEvent(accessEventGroupCreated, accessAggregateGroup, group.ID, now, groupCreatedPayload...)
	if err != nil {
		return entity.Group{}, err
	}
	result, err := commandResult(input.Meta, accessOperationCreateGroup, accessAggregateGroup, group.ID, now)
	if err != nil {
		return entity.Group{}, err
	}
	if err := s.repository.CreateGroup(ctx, group, groupCreatedEvent, result); err != nil {
		return entity.Group{}, err
	}
	return group, nil
}

// SetMembership creates or updates a membership edge between domain entities.
func (s *Service) SetMembership(ctx context.Context, input SetMembershipInput) (entity.Membership, error) {
	if input.SubjectID == uuid.Nil || input.TargetID == uuid.Nil {
		return entity.Membership{}, errs.ErrInvalidArgument
	}
	if err := s.validateMembershipEndpoint(ctx, input.SubjectType, input.SubjectID, input.TargetType, input.TargetID); err != nil {
		return entity.Membership{}, err
	}
	now := s.now(input.Meta)
	status := defaultMembershipStatus(input.Status)
	source := defaultMembershipSource(input.Source)
	existing, err := s.repository.FindMembership(ctx, query.MembershipIdentity{
		SubjectType: input.SubjectType, SubjectID: input.SubjectID, TargetType: input.TargetType, TargetID: input.TargetID,
	})
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.Membership{}, err
	}
	if err == nil {
		if input.Meta.ExpectedVersion != nil && *input.Meta.ExpectedVersion != existing.Version {
			return entity.Membership{}, errs.ErrConflict
		}
		membership := existing
		membership.RoleHint = strings.TrimSpace(input.RoleHint)
		membership.Status = status
		membership.Source = source
		membership.Version++
		membership.UpdatedAt = now
		eventType := accessEventMembershipUpdated
		if membership.Status == enum.MembershipStatusDisabled {
			eventType = accessEventMembershipDisabled
		}
		event, err := s.membershipEvent(eventType, membership, now, input.Meta.Reason)
		if err != nil {
			return entity.Membership{}, err
		}
		if err := s.repository.SetMembership(ctx, membership, event); err != nil {
			return entity.Membership{}, err
		}
		return membership, nil
	}
	if status == enum.MembershipStatusDisabled {
		return entity.Membership{}, errs.ErrNotFound
	}
	membership := entity.Membership{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		SubjectType: input.SubjectType,
		SubjectID:   input.SubjectID,
		TargetType:  input.TargetType,
		TargetID:    input.TargetID,
		RoleHint:    strings.TrimSpace(input.RoleHint),
		Status:      status,
		Source:      source,
	}

	event, err := s.membershipEvent(accessEventMembershipCreated, membership, now, input.Meta.Reason)
	if err != nil {
		return entity.Membership{}, err
	}
	if err := s.repository.SetMembership(ctx, membership, event); err != nil {
		return entity.Membership{}, err
	}
	return membership, nil
}

// PutAllowlistEntry creates or updates a primary admission rule.
func (s *Service) PutAllowlistEntry(ctx context.Context, input PutAllowlistEntryInput) (entity.AllowlistEntry, error) {
	normalized := strings.TrimSpace(input.Value)
	switch input.MatchType {
	case enum.AllowlistMatchEmail:
		normalized = helpers.NormalizeEmail(normalized)
	case enum.AllowlistMatchDomain:
		normalized = helpers.NormalizeDomain(normalized)
	default:
		return entity.AllowlistEntry{}, errs.ErrInvalidArgument
	}
	if normalized == "" {
		return entity.AllowlistEntry{}, errs.ErrInvalidArgument
	}
	defaultStatus, err := defaultAllowlistDefaultStatus(input.DefaultStatus)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	now := s.now(input.Meta)
	entry := entity.AllowlistEntry{
		Base:           entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		MatchType:      input.MatchType,
		Value:          normalized,
		OrganizationID: input.OrganizationID,
		DefaultStatus:  defaultStatus,
		Status:         defaultAllowlistStatus(input.Status),
	}
	eventType := accessEventAllowlistEntryCreated
	existing, err := s.repository.FindAllowlistEntry(ctx, entry.MatchType, entry.Value)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.AllowlistEntry{}, err
	}
	if err == nil {
		if sameAllowlistEntryState(existing, entry) {
			return existing, nil
		}
		if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
			return entity.AllowlistEntry{}, err
		}
		entry.Base = updateBase(existing.Base, now)
		eventType = accessEventAllowlistEntryUpdated
	}
	event, err := s.event(eventType, accessAggregateAllowlistEntry, entry.ID, value.AccessEventPayload{
		AllowlistEntryID: entry.ID.String(),
		MatchType:        string(entry.MatchType),
		OrganizationID:   uuidPtrString(entry.OrganizationID),
		Version:          entry.Version,
	}, now)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	err = s.repository.PutAllowlistEntry(ctx, entry, event)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	return entry, nil
}

// BootstrapUserFromIdentity admits or links a user after external identity login.
func (s *Service) BootstrapUserFromIdentity(ctx context.Context, input BootstrapUserFromIdentityInput) (BootstrapUserFromIdentityResult, error) {
	normalizedEmail := helpers.NormalizeEmail(input.Email)
	if strings.TrimSpace(input.Subject) == "" || normalizedEmail == "" {
		return BootstrapUserFromIdentityResult{}, errs.ErrInvalidArgument
	}

	existing, err := s.repository.GetUserByIdentity(ctx, input.Provider, input.Subject)
	if err == nil {
		return BootstrapUserFromIdentityResult{User: existing, Decision: decisionByUserStatus(existing.Status), ReasonCode: reasonIdentityFound}, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return BootstrapUserFromIdentityResult{}, err
	}

	entry, reasonCode, err := s.findAllowlistEntry(ctx, input.Email)
	if err != nil {
		return BootstrapUserFromIdentityResult{}, err
	}
	var organization *entity.Organization
	if entry.OrganizationID != nil {
		org, orgErr := s.repository.GetOrganization(ctx, *entry.OrganizationID)
		if orgErr != nil {
			return BootstrapUserFromIdentityResult{}, orgErr
		}
		organization = &org
	}
	now := s.now(input.Meta)
	existingByEmail, err := s.repository.GetUserByEmail(ctx, normalizedEmail)
	if err == nil {
		identity := entity.UserIdentity{
			ID:           s.ids.New(),
			UserID:       existingByEmail.ID,
			Provider:     input.Provider,
			Subject:      strings.TrimSpace(input.Subject),
			EmailAtLogin: normalizedEmail,
			LastLoginAt:  &now,
		}
		event, err := s.event(accessEventUserIdentityLinked, accessAggregateUser, existingByEmail.ID, value.AccessEventPayload{
			UserID:           existingByEmail.ID.String(),
			IdentityID:       identity.ID.String(),
			IdentityProvider: string(identity.Provider),
			Version:          existingByEmail.Version,
		}, now)
		if err != nil {
			return BootstrapUserFromIdentityResult{}, err
		}
		err = s.repository.LinkUserIdentity(ctx, identity, event)
		if err != nil {
			return BootstrapUserFromIdentityResult{}, err
		}
		return BootstrapUserFromIdentityResult{
			User:         existingByEmail,
			Decision:     decisionByUserStatus(existingByEmail.Status),
			ReasonCode:   reasonIdentityLinked,
			Organization: organization,
		}, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return BootstrapUserFromIdentityResult{}, err
	}
	user := entity.User{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		PrimaryEmail: normalizedEmail,
		DisplayName:  strings.TrimSpace(input.DisplayName),
		Status:       entry.DefaultStatus,
		Locale:       strings.TrimSpace(input.Locale),
	}
	identity := entity.UserIdentity{
		ID:           s.ids.New(),
		UserID:       user.ID,
		Provider:     input.Provider,
		Subject:      strings.TrimSpace(input.Subject),
		EmailAtLogin: user.PrimaryEmail,
		LastLoginAt:  &now,
	}

	event, err := s.event(accessEventUserCreated, accessAggregateUser, user.ID, value.AccessEventPayload{
		UserID:     user.ID.String(),
		Status:     string(user.Status),
		Version:    user.Version,
		IdentityID: identity.ID.String(),
	}, now)
	if err != nil {
		return BootstrapUserFromIdentityResult{}, err
	}
	err = s.repository.CreateUser(ctx, user, identity, event)
	if err != nil {
		return BootstrapUserFromIdentityResult{}, err
	}

	return BootstrapUserFromIdentityResult{
		User:         user,
		Decision:     decisionByUserStatus(user.Status),
		ReasonCode:   reasonCode,
		Organization: organization,
	}, nil
}

// PutExternalProvider creates or updates an external provider catalog entry.
func (s *Service) PutExternalProvider(ctx context.Context, input PutExternalProviderInput) (entity.ExternalProvider, error) {
	if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.DisplayName) == "" {
		return entity.ExternalProvider{}, errs.ErrInvalidArgument
	}
	now := s.now(input.Meta)
	provider := entity.ExternalProvider{
		Base: entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Slug: helpers.NormalizeSlug(input.Slug), ProviderKind: input.ProviderKind,
		DisplayName: strings.TrimSpace(input.DisplayName), IconAssetRef: strings.TrimSpace(input.IconAssetRef),
		Status: defaultExternalProviderStatus(input.Status),
	}
	eventType := accessEventExternalProviderCreated
	existing, err := s.repository.GetExternalProviderBySlug(ctx, provider.Slug)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.ExternalProvider{}, err
	}
	if err == nil {
		if sameExternalProviderState(existing, provider) {
			return existing, nil
		}
		if input.CreateOnly {
			return entity.ExternalProvider{}, errs.ErrAlreadyExists
		}
		if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
			return entity.ExternalProvider{}, err
		}
		provider.Base = updateBase(existing.Base, now)
		eventType = accessEventExternalProviderUpdated
	}
	event, err := s.event(eventType, accessAggregateExternalProvider, provider.ID, value.AccessEventPayload{
		ExternalProviderID: provider.ID.String(),
		Slug:               provider.Slug,
		Version:            provider.Version,
	}, now)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	err = s.repository.PutExternalProvider(ctx, provider, event)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	return provider, nil
}

// RegisterExternalAccount creates an external account principal.
func (s *Service) RegisterExternalAccount(ctx context.Context, input RegisterExternalAccountInput) (entity.ExternalAccount, error) {
	if input.ExternalProviderID == uuid.Nil || strings.TrimSpace(input.DisplayName) == "" {
		return entity.ExternalAccount{}, errs.ErrInvalidArgument
	}
	applied, ok, err := s.findCommandResult(ctx, input.Meta, accessOperationRegisterExternalAccount, accessAggregateExternalAccount)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	if ok {
		return s.repository.GetExternalAccount(ctx, applied.AggregateID)
	}
	if _, err := s.repository.GetExternalProvider(ctx, input.ExternalProviderID); err != nil {
		return entity.ExternalAccount{}, err
	}
	ownerScopeType, ownerScopeID, err := normalizeExternalAccountOwnerScope(input.OwnerScopeType, input.OwnerScopeID)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	now := s.now(input.Meta)
	account := entity.ExternalAccount{
		Base:               entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalProviderID: input.ExternalProviderID, AccountType: input.AccountType,
		DisplayName: strings.TrimSpace(input.DisplayName), ImageAssetRef: strings.TrimSpace(input.ImageAssetRef),
		OwnerScopeType: ownerScopeType, OwnerScopeID: ownerScopeID,
		Status: defaultExternalAccountStatus(input.Status), SecretBindingRefID: input.SecretBindingRefID,
	}
	event, err := s.createdEvent(
		accessEventExternalAccountCreated, accessAggregateExternalAccount, account.ID, now,
		payloadString(accessPayloadExternalAccountID, account.ID.String()),
		payloadString(accessPayloadExternalProviderID, account.ExternalProviderID.String()),
		payloadString(accessPayloadAccountType, string(account.AccountType)),
		payloadVersion(account.Version),
	)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	result, err := commandResult(input.Meta, accessOperationRegisterExternalAccount, accessAggregateExternalAccount, account.ID, now)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	if err := s.repository.RegisterExternalAccount(ctx, account, event, result); err != nil {
		return entity.ExternalAccount{}, err
	}
	return account, nil
}

// BindExternalAccount permits an account to be used for selected actions in a scope.
func (s *Service) BindExternalAccount(ctx context.Context, input BindExternalAccountInput) (entity.ExternalAccountBinding, error) {
	allowedActionKeys := sortedUnique(input.AllowedActionKeys)
	usageScopeID := strings.TrimSpace(input.UsageScopeID)
	if input.ExternalAccountID == uuid.Nil || len(allowedActionKeys) == 0 {
		return entity.ExternalAccountBinding{}, errs.ErrInvalidArgument
	}
	if input.Status == enum.ExternalAccountBindingStatusDisabled {
		return entity.ExternalAccountBinding{}, errs.ErrInvalidArgument
	}
	if err := validateExternalAccountUsageScope(input.UsageScopeType, usageScopeID); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	for _, actionKey := range allowedActionKeys {
		action, err := s.repository.GetAccessActionByKey(ctx, actionKey)
		if err != nil {
			return entity.ExternalAccountBinding{}, err
		}
		if action.Status != enum.AccessActionStatusActive {
			return entity.ExternalAccountBinding{}, errs.ErrPreconditionFailed
		}
	}
	if _, err := s.repository.GetExternalAccount(ctx, input.ExternalAccountID); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	now := s.now(input.Meta)
	binding := entity.ExternalAccountBinding{
		Base:              entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalAccountID: input.ExternalAccountID, UsageScopeType: input.UsageScopeType,
		UsageScopeID: usageScopeID, AllowedActionKeys: allowedActionKeys,
		Status: defaultExternalAccountBindingStatus(input.Status),
	}
	eventType := accessEventExternalAccountBindingCreated
	existing, err := s.repository.FindExternalAccountBindingByIdentity(ctx, query.ExternalAccountBindingIdentity{
		ExternalAccountID: binding.ExternalAccountID,
		UsageScope:        value.ScopeRef{Type: string(binding.UsageScopeType), ID: binding.UsageScopeID},
	})
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.ExternalAccountBinding{}, err
	}
	if err == nil {
		if sameExternalAccountBindingState(existing, binding) {
			return existing, nil
		}
		if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
			return entity.ExternalAccountBinding{}, err
		}
		binding.Base = updateBase(existing.Base, now)
		eventType = accessEventExternalAccountBindingUpdated
	}
	event, err := s.createdEvent(
		eventType, accessAggregateExternalAccountBinding, binding.ID, now,
		payloadString(accessPayloadExternalAccountBindingID, binding.ID.String()),
		payloadString(accessPayloadExternalAccountID, binding.ExternalAccountID.String()),
		payloadString(accessPayloadUsageScopeType, string(binding.UsageScopeType)),
		payloadString(accessPayloadUsageScopeID, binding.UsageScopeID),
		payloadVersion(binding.Version),
	)
	if err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	if err := s.repository.BindExternalAccount(ctx, binding, event); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	return binding, nil
}

// PutAccessAction creates or updates an access action catalog entry.
func (s *Service) PutAccessAction(ctx context.Context, input PutAccessActionInput) (entity.AccessAction, error) {
	if strings.TrimSpace(input.Key) == "" || strings.TrimSpace(input.ResourceType) == "" {
		return entity.AccessAction{}, errs.ErrInvalidArgument
	}
	now := s.now(input.Meta)
	action := entity.AccessAction{
		Base: entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Key:  strings.TrimSpace(input.Key), DisplayName: strings.TrimSpace(input.DisplayName),
		Description: strings.TrimSpace(input.Description), ResourceType: strings.TrimSpace(input.ResourceType),
		Status: defaultAccessActionStatus(input.Status),
	}
	eventType := accessEventAccessActionCreated
	existing, err := s.repository.GetAccessActionByKey(ctx, action.Key)
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.AccessAction{}, err
	}
	if err == nil {
		if sameAccessActionState(existing, action) {
			return existing, nil
		}
		if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
			return entity.AccessAction{}, err
		}
		action.Base = updateBase(existing.Base, now)
		eventType = accessEventAccessActionUpdated
	}
	event, err := s.event(eventType, accessAggregateAccessAction, action.ID, value.AccessEventPayload{
		AccessActionID: action.ID.String(),
		ActionKey:      action.Key,
		Version:        action.Version,
	}, now)
	if err != nil {
		return entity.AccessAction{}, err
	}
	err = s.repository.PutAccessAction(ctx, action, event)
	if err != nil {
		return entity.AccessAction{}, err
	}
	return action, nil
}

// PutAccessRule creates or updates a policy rule for an active action.
func (s *Service) PutAccessRule(ctx context.Context, input PutAccessRuleInput) (entity.AccessRule, error) {
	input.SubjectID = strings.TrimSpace(input.SubjectID)
	input.ActionKey = strings.TrimSpace(input.ActionKey)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.ScopeType = strings.TrimSpace(input.ScopeType)
	input.ScopeID = strings.TrimSpace(input.ScopeID)
	if input.SubjectID == "" || input.ActionKey == "" || input.ResourceType == "" {
		return entity.AccessRule{}, errs.ErrInvalidArgument
	}
	if input.Status == enum.AccessRuleStatusDisabled {
		return entity.AccessRule{}, errs.ErrInvalidArgument
	}
	if err := validateAccessRuleScope(input.ScopeType, input.ScopeID); err != nil {
		return entity.AccessRule{}, err
	}
	action, err := s.repository.GetAccessActionByKey(ctx, input.ActionKey)
	if err != nil {
		return entity.AccessRule{}, err
	}
	if action.Status != enum.AccessActionStatusActive {
		return entity.AccessRule{}, errs.ErrPreconditionFailed
	}
	if action.ResourceType != input.ResourceType {
		return entity.AccessRule{}, errs.ErrPreconditionFailed
	}
	now := s.now(input.Meta)
	rule := entity.AccessRule{
		Base:   entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Effect: input.Effect, SubjectType: input.SubjectType, SubjectID: input.SubjectID,
		ActionKey: input.ActionKey, ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		ScopeType: input.ScopeType, ScopeID: input.ScopeID, Priority: input.Priority,
		Status: defaultAccessRuleStatus(input.Status),
	}
	eventType := accessEventAccessRuleCreated
	existing, err := s.repository.FindAccessRule(ctx, query.AccessRuleIdentity{
		Effect:       rule.Effect,
		SubjectType:  rule.SubjectType,
		SubjectID:    rule.SubjectID,
		ActionKey:    rule.ActionKey,
		ResourceType: rule.ResourceType,
		ResourceID:   rule.ResourceID,
		ScopeType:    rule.ScopeType,
		ScopeID:      rule.ScopeID,
	})
	if err != nil && !errors.Is(err, errs.ErrNotFound) {
		return entity.AccessRule{}, err
	}
	if err == nil {
		if sameAccessRuleState(existing, rule) {
			return existing, nil
		}
		if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
			return entity.AccessRule{}, err
		}
		rule.Base = updateBase(existing.Base, now)
		eventType = accessEventAccessRuleUpdated
	}
	event, err := s.event(eventType, accessAggregateAccessRule, rule.ID, value.AccessEventPayload{
		AccessRuleID: rule.ID.String(),
		Effect:       string(rule.Effect),
		ActionKey:    rule.ActionKey,
		ScopeType:    rule.ScopeType,
		ScopeID:      rule.ScopeID,
		Version:      rule.Version,
	}, now)
	if err != nil {
		return entity.AccessRule{}, err
	}
	err = s.repository.PutAccessRule(ctx, rule, event)
	if err != nil {
		return entity.AccessRule{}, err
	}
	return rule, nil
}

// CheckAccess evaluates access rules for a subject, resource and scope.
func (s *Service) CheckAccess(ctx context.Context, input CheckAccessInput) (CheckAccessResult, error) {
	input = normalizeCheckAccessInput(input)
	if strings.TrimSpace(input.Subject.Type) == "" || strings.TrimSpace(input.Subject.ID) == "" ||
		strings.TrimSpace(input.ActionKey) == "" || strings.TrimSpace(input.Resource.Type) == "" {
		return CheckAccessResult{}, errs.ErrInvalidArgument
	}
	action, err := s.repository.GetAccessActionByKey(ctx, input.ActionKey)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonActionNotFound, nil)
		}
		return CheckAccessResult{}, err
	}
	if action.Status != enum.AccessActionStatusActive {
		return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonActionDisabled, nil)
	}
	if action.ResourceType != input.Resource.Type {
		return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonResourceTypeMismatch, nil)
	}
	subjects, reasonCode, err := s.resolveSubjects(ctx, input.Subject)
	if err != nil {
		return CheckAccessResult{}, err
	}
	if reasonCode == reasonSubjectBlocked {
		return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonSubjectBlocked, nil)
	}
	if reasonCode == reasonSubjectPending {
		return s.recordDecision(ctx, input, enum.AccessDecisionPending, reasonSubjectPending, nil)
	}

	rules, err := s.repository.ListAccessRules(ctx, query.AccessRuleFilter{
		Subjects: subjects, ActionKey: input.ActionKey, ResourceType: input.Resource.Type, ResourceID: input.Resource.ID, Scope: input.Scope,
	})
	if err != nil {
		return CheckAccessResult{}, err
	}
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Priority > rules[j].Priority })

	var allowRules []entity.AccessRule
	for _, rule := range rules {
		if rule.Status != enum.AccessRuleStatusActive {
			continue
		}
		if rule.Effect == enum.AccessEffectDeny {
			return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonExplicitDeny, []entity.AccessRule{rule})
		}
		if rule.Effect == enum.AccessEffectAllow {
			allowRules = append(allowRules, rule)
		}
	}
	if len(allowRules) > 0 {
		return s.recordDecision(ctx, input, enum.AccessDecisionAllow, reasonExplicitAllow, allowRules)
	}
	return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonNoMatchingRule, nil)
}

// ExplainAccess returns a previously audited access decision explanation.
func (s *Service) ExplainAccess(ctx context.Context, input ExplainAccessInput) (ExplainAccessResult, error) {
	if input.AuditID == uuid.Nil {
		return ExplainAccessResult{}, errs.ErrInvalidArgument
	}
	audit, err := s.repository.GetAccessDecisionAudit(ctx, input.AuditID)
	if err != nil {
		return ExplainAccessResult{}, err
	}
	return ExplainAccessResult{Audit: audit}, nil
}

// ResolveExternalAccountUsage returns account and secret references for allowed usage.
func (s *Service) ResolveExternalAccountUsage(ctx context.Context, input ResolveExternalAccountUsageInput) (ResolveExternalAccountUsageResult, error) {
	account, err := s.repository.GetExternalAccount(ctx, input.ExternalAccountID)
	if err != nil {
		return ResolveExternalAccountUsageResult{}, err
	}
	if account.Status != enum.ExternalAccountStatusActive || account.SecretBindingRefID == nil {
		return ResolveExternalAccountUsageResult{}, errs.ErrPreconditionFailed
	}
	binding, err := s.repository.FindExternalAccountBinding(ctx, query.ExternalAccountUsageFilter{
		ExternalAccountID: account.ID, ActionKey: input.ActionKey, UsageScope: input.UsageScope,
	})
	if err != nil {
		return ResolveExternalAccountUsageResult{}, err
	}
	if binding.Status != enum.ExternalAccountBindingStatusActive || !slices.Contains(binding.AllowedActionKeys, input.ActionKey) {
		return ResolveExternalAccountUsageResult{}, errs.ErrForbidden
	}
	secret, err := s.repository.GetSecretBindingRef(ctx, *account.SecretBindingRefID)
	if err != nil {
		return ResolveExternalAccountUsageResult{}, err
	}
	return ResolveExternalAccountUsageResult{ExternalAccount: account, SecretRef: secret, AllowedActions: binding.AllowedActionKeys}, nil
}

func normalizeCheckAccessInput(input CheckAccessInput) CheckAccessInput {
	input.Subject.Type = strings.TrimSpace(input.Subject.Type)
	input.Subject.ID = strings.TrimSpace(input.Subject.ID)
	input.ActionKey = strings.TrimSpace(input.ActionKey)
	input.Resource.Type = strings.TrimSpace(input.Resource.Type)
	input.Resource.ID = strings.TrimSpace(input.Resource.ID)
	input.Scope.Type = strings.TrimSpace(input.Scope.Type)
	input.Scope.ID = strings.TrimSpace(input.Scope.ID)
	return input
}
