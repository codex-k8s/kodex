package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/helpers"
	accessrepo "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/repository/access"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

const (
	reasonAllowlistEmail       = "allowlist_email"
	reasonAllowlistDomain      = "allowlist_domain"
	reasonAllowlistMiss        = "allowlist_miss"
	reasonExplicitAllow        = "explicit_allow"
	reasonExplicitDeny         = "explicit_deny"
	reasonNoMatchingRule       = "no_matching_rule"
	reasonSubjectBlocked       = "subject_blocked"
	schemaVersionAccessEventV1 = 1
)

type accessPayloadStringField uint8

const (
	accessPayloadOrganizationID accessPayloadStringField = iota + 1
	accessPayloadKind
	accessPayloadStatus
	accessPayloadGroupID
	accessPayloadScopeType
	accessPayloadScopeID
	accessPayloadExternalAccountID
	accessPayloadExternalProviderID
	accessPayloadAccountType
	accessPayloadExternalAccountBindingID
	accessPayloadUsageScopeType
	accessPayloadUsageScopeID
)

type accessEventPayloadOption func(*value.AccessEventPayload)

type Service struct {
	repository accessrepo.Repository
	clock      accessrepo.Clock
	ids        accessrepo.IDGenerator
}

func New(repository accessrepo.Repository, clock accessrepo.Clock, ids accessrepo.IDGenerator) *Service {
	return &Service{repository: repository, clock: clock, ids: ids}
}

func (s *Service) CreateOrganization(ctx context.Context, input CreateOrganizationInput) (entity.Organization, error) {
	if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.DisplayName) == "" {
		return entity.Organization{}, errs.ErrInvalidArgument
	}
	status := defaultOrganizationStatus(input.Status)
	if input.Kind == enum.OrganizationKindOwner {
		count, err := s.repository.CountActiveOwnerOrganizations(ctx)
		if err != nil {
			return entity.Organization{}, err
		}
		if count > 0 && status == enum.OrganizationStatusActive {
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

	if err := s.repository.CreateOrganization(ctx, organization, s.createdEvent(
		"access.organization.created", "organization", organization.ID, now,
		payloadString(accessPayloadOrganizationID, organization.ID.String()),
		payloadString(accessPayloadKind, string(organization.Kind)),
		payloadString(accessPayloadStatus, string(organization.Status)),
		payloadVersion(organization.Version),
	)); err != nil {
		return entity.Organization{}, err
	}
	return organization, nil
}

func (s *Service) CreateGroup(ctx context.Context, input CreateGroupInput) (entity.Group, error) {
	if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.DisplayName) == "" {
		return entity.Group{}, errs.ErrInvalidArgument
	}
	if input.ScopeType == enum.GroupScopeOrganization && input.ScopeID == nil {
		return entity.Group{}, errs.ErrInvalidArgument
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
	groupCreatedEvent := s.createdEvent("access.group.created", "group", group.ID, now, groupCreatedPayload...)
	if err := s.repository.CreateGroup(ctx, group, groupCreatedEvent); err != nil {
		return entity.Group{}, err
	}
	return group, nil
}

func (s *Service) SetMembership(ctx context.Context, input SetMembershipInput) (entity.Membership, error) {
	if input.SubjectID == uuid.Nil || input.TargetID == uuid.Nil {
		return entity.Membership{}, errs.ErrInvalidArgument
	}
	now := s.now(input.Meta)
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
		Status:      defaultMembershipStatus(input.Status),
		Source:      defaultMembershipSource(input.Source),
	}

	err := s.repository.SetMembership(ctx, membership, s.event("access.membership.created", "membership", membership.ID, value.AccessEventPayload{
		MembershipID: membership.ID.String(),
		SubjectType:  string(membership.SubjectType),
		SubjectID:    membership.SubjectID.String(),
		TargetType:   string(membership.TargetType),
		TargetID:     membership.TargetID.String(),
		Version:      membership.Version,
	}, now))
	if err != nil {
		return entity.Membership{}, err
	}
	return membership, nil
}

func (s *Service) PutAllowlistEntry(ctx context.Context, input PutAllowlistEntryInput) (entity.AllowlistEntry, error) {
	normalized := strings.TrimSpace(input.Value)
	switch input.MatchType {
	case enum.AllowlistMatchEmail:
		normalized = helpers.NormalizeEmail(normalized)
	case enum.AllowlistMatchDomain:
		normalized = helpers.NormalizeDomain(normalized)
	}
	if normalized == "" {
		return entity.AllowlistEntry{}, errs.ErrInvalidArgument
	}
	now := s.now(input.Meta)
	entry := entity.AllowlistEntry{
		ID:             s.ids.New(),
		MatchType:      input.MatchType,
		Value:          normalized,
		OrganizationID: input.OrganizationID,
		DefaultStatus:  defaultUserStatus(input.DefaultStatus),
		Status:         defaultAllowlistStatus(input.Status),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	err := s.repository.PutAllowlistEntry(ctx, entry, s.event("access.allowlist_entry.created", "allowlist_entry", entry.ID, value.AccessEventPayload{
		AllowlistEntryID: entry.ID.String(),
		MatchType:        string(entry.MatchType),
		OrganizationID:   uuidPtrString(entry.OrganizationID),
	}, now))
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	return entry, nil
}

func (s *Service) BootstrapUserFromIdentity(ctx context.Context, input BootstrapUserFromIdentityInput) (BootstrapUserFromIdentityResult, error) {
	if strings.TrimSpace(input.Subject) == "" || helpers.NormalizeEmail(input.Email) == "" {
		return BootstrapUserFromIdentityResult{}, errs.ErrInvalidArgument
	}

	existing, err := s.repository.GetUserByIdentity(ctx, input.Provider, input.Subject)
	if err == nil {
		return BootstrapUserFromIdentityResult{User: existing, Decision: decisionByUserStatus(existing.Status), ReasonCode: "identity_found"}, nil
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
	status := enum.UserStatusPending
	if entry.Status == enum.AllowlistStatusActive {
		status = entry.DefaultStatus
	}

	user := entity.User{
		Base: entity.Base{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		PrimaryEmail: helpers.NormalizeEmail(input.Email),
		DisplayName:  strings.TrimSpace(input.DisplayName),
		Status:       status,
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

	err = s.repository.CreateUser(ctx, user, identity, s.event("access.user.created", "user", user.ID, value.AccessEventPayload{
		UserID:     user.ID.String(),
		Status:     string(user.Status),
		Version:    user.Version,
		IdentityID: identity.ID.String(),
	}, now))
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
	err := s.repository.PutExternalProvider(ctx, provider, s.event("access.external_provider.created", "external_provider", provider.ID, value.AccessEventPayload{
		ExternalProviderID: provider.ID.String(),
		Slug:               provider.Slug,
		Version:            provider.Version,
	}, now))
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	return provider, nil
}

func (s *Service) RegisterExternalAccount(ctx context.Context, input RegisterExternalAccountInput) (entity.ExternalAccount, error) {
	if input.ExternalProviderID == uuid.Nil || strings.TrimSpace(input.DisplayName) == "" {
		return entity.ExternalAccount{}, errs.ErrInvalidArgument
	}
	if _, err := s.repository.GetExternalProvider(ctx, input.ExternalProviderID); err != nil {
		return entity.ExternalAccount{}, err
	}
	now := s.now(input.Meta)
	account := entity.ExternalAccount{
		Base:               entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalProviderID: input.ExternalProviderID, AccountType: input.AccountType,
		DisplayName: strings.TrimSpace(input.DisplayName), ImageAssetRef: strings.TrimSpace(input.ImageAssetRef),
		OwnerScopeType: input.OwnerScopeType, OwnerScopeID: strings.TrimSpace(input.OwnerScopeID),
		Status: defaultExternalAccountStatus(input.Status), SecretBindingRefID: input.SecretBindingRefID,
	}
	if err := s.repository.RegisterExternalAccount(ctx, account, s.createdEvent(
		"access.external_account.created", "external_account", account.ID, now,
		payloadString(accessPayloadExternalAccountID, account.ID.String()),
		payloadString(accessPayloadExternalProviderID, account.ExternalProviderID.String()),
		payloadString(accessPayloadAccountType, string(account.AccountType)),
		payloadVersion(account.Version),
	)); err != nil {
		return entity.ExternalAccount{}, err
	}
	return account, nil
}

func (s *Service) BindExternalAccount(ctx context.Context, input BindExternalAccountInput) (entity.ExternalAccountBinding, error) {
	allowedActionKeys := sortedUnique(input.AllowedActionKeys)
	if input.ExternalAccountID == uuid.Nil || strings.TrimSpace(input.UsageScopeID) == "" || len(allowedActionKeys) == 0 {
		return entity.ExternalAccountBinding{}, errs.ErrInvalidArgument
	}
	if _, err := s.repository.GetExternalAccount(ctx, input.ExternalAccountID); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	now := s.now(input.Meta)
	binding := entity.ExternalAccountBinding{
		ID: s.ids.New(), ExternalAccountID: input.ExternalAccountID, UsageScopeType: input.UsageScopeType,
		UsageScopeID: strings.TrimSpace(input.UsageScopeID), AllowedActionKeys: allowedActionKeys,
		Status: defaultExternalAccountBindingStatus(input.Status), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repository.BindExternalAccount(ctx, binding, s.createdEvent(
		"access.external_account_binding.created", "external_account_binding", binding.ID, now,
		payloadString(accessPayloadExternalAccountBindingID, binding.ID.String()),
		payloadString(accessPayloadExternalAccountID, binding.ExternalAccountID.String()),
		payloadString(accessPayloadUsageScopeType, string(binding.UsageScopeType)),
		payloadString(accessPayloadUsageScopeID, binding.UsageScopeID),
	)); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	return binding, nil
}

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
	err := s.repository.PutAccessAction(ctx, action, s.event("access.access_action.created", "access_action", action.ID, value.AccessEventPayload{
		AccessActionID: action.ID.String(),
		ActionKey:      action.Key,
		Version:        action.Version,
	}, now))
	if err != nil {
		return entity.AccessAction{}, err
	}
	return action, nil
}

func (s *Service) PutAccessRule(ctx context.Context, input PutAccessRuleInput) (entity.AccessRule, error) {
	if input.SubjectID == "" || input.ActionKey == "" || input.ResourceType == "" {
		return entity.AccessRule{}, errs.ErrInvalidArgument
	}
	if _, err := s.repository.GetAccessActionByKey(ctx, input.ActionKey); err != nil {
		return entity.AccessRule{}, err
	}
	now := s.now(input.Meta)
	rule := entity.AccessRule{
		Base:   entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Effect: input.Effect, SubjectType: input.SubjectType, SubjectID: input.SubjectID,
		ActionKey: input.ActionKey, ResourceType: input.ResourceType, ResourceID: input.ResourceID,
		ScopeType: input.ScopeType, ScopeID: input.ScopeID, Priority: input.Priority,
		Status: defaultAccessRuleStatus(input.Status),
	}
	err := s.repository.PutAccessRule(ctx, rule, s.event("access.access_rule.created", "access_rule", rule.ID, value.AccessEventPayload{
		AccessRuleID: rule.ID.String(),
		Effect:       string(rule.Effect),
		ActionKey:    rule.ActionKey,
		ScopeType:    rule.ScopeType,
		ScopeID:      rule.ScopeID,
		Version:      rule.Version,
	}, now))
	if err != nil {
		return entity.AccessRule{}, err
	}
	return rule, nil
}

func (s *Service) CheckAccess(ctx context.Context, input CheckAccessInput) (CheckAccessResult, error) {
	subjects, reasonCode, err := s.resolveSubjects(ctx, input.Subject)
	if err != nil {
		return CheckAccessResult{}, err
	}
	if reasonCode == reasonSubjectBlocked {
		return s.recordDecision(ctx, input, enum.AccessDecisionDeny, reasonSubjectBlocked, nil)
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

func (s *Service) findAllowlistEntry(ctx context.Context, email string) (entity.AllowlistEntry, string, error) {
	normalized := helpers.NormalizeEmail(email)
	entry, err := s.repository.FindAllowlistEntry(ctx, enum.AllowlistMatchEmail, normalized)
	if err == nil {
		return entry, reasonAllowlistEmail, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return entity.AllowlistEntry{}, "", err
	}
	domain := helpers.EmailDomain(normalized)
	if domain != "" {
		entry, err = s.repository.FindAllowlistEntry(ctx, enum.AllowlistMatchDomain, domain)
		if err == nil {
			return entry, reasonAllowlistDomain, nil
		}
		if !errors.Is(err, errs.ErrNotFound) {
			return entity.AllowlistEntry{}, "", err
		}
	}
	return entity.AllowlistEntry{}, reasonAllowlistMiss, errs.ErrUnauthorizedSubject
}

func (s *Service) resolveSubjects(ctx context.Context, subject value.SubjectRef) ([]value.SubjectRef, string, error) {
	subjects := []value.SubjectRef{subject}
	if subject.Type != string(enum.AccessSubjectUser) {
		return subjects, "", nil
	}
	userID, err := uuid.Parse(subject.ID)
	if err != nil {
		return nil, "", errs.ErrInvalidArgument
	}
	user, err := s.repository.GetUser(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if user.Status == enum.UserStatusBlocked || user.Status == enum.UserStatusDisabled {
		return subjects, reasonSubjectBlocked, nil
	}
	memberships, err := s.repository.ListMemberships(ctx, query.MembershipGraphFilter{
		Subject: value.SubjectRef{Type: string(enum.MembershipSubjectUser), ID: subject.ID}, Status: enum.MembershipStatusActive,
	})
	if err != nil {
		return nil, "", err
	}
	for _, membership := range memberships {
		switch membership.TargetType {
		case enum.MembershipTargetOrganization:
			subjects = append(subjects, value.SubjectRef{Type: string(enum.AccessSubjectOrganization), ID: membership.TargetID.String()})
		case enum.MembershipTargetGroup:
			subjects = append(subjects, value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: membership.TargetID.String()})
		}
	}
	return subjects, "", nil
}

func (s *Service) recordDecision(ctx context.Context, input CheckAccessInput, decision enum.AccessDecision, reasonCode string, rules []entity.AccessRule) (CheckAccessResult, error) {
	explanation := value.DecisionExplanation{
		Decision: string(decision), ReasonCode: reasonCode, PolicyVersion: policyVersion(rules),
		MatchedRules: ruleExplanations(rules, reasonCode),
	}
	if input.Audit {
		now := s.now(input.Meta)
		audit := entity.AccessDecisionAudit{
			ID: s.ids.New(), Subject: input.Subject, ActionKey: input.ActionKey, Resource: input.Resource,
			Decision: decision, ReasonCode: reasonCode, PolicyVersion: explanation.PolicyVersion,
			Explanation: explanation, CreatedAt: now,
		}
		var event *entity.OutboxEvent
		if decision == enum.AccessDecisionDeny {
			evt := s.event("access.access_decision.recorded", "access_decision_audit", audit.ID, value.AccessEventPayload{
				AccessDecisionAuditID: audit.ID.String(),
				SubjectType:           audit.Subject.Type,
				SubjectID:             audit.Subject.ID,
				ActionKey:             audit.ActionKey,
				Decision:              string(audit.Decision),
				ReasonCode:            audit.ReasonCode,
			}, now)
			event = &evt
		}
		if err := s.repository.RecordAccessDecision(ctx, audit, event); err != nil {
			return CheckAccessResult{}, err
		}
	}
	return CheckAccessResult{Decision: decision, ReasonCode: reasonCode, Explanation: explanation}, nil
}

func (s *Service) event(eventType string, aggregateType string, aggregateID uuid.UUID, payload value.AccessEventPayload, occurredAt time.Time) entity.OutboxEvent {
	eventID := s.ids.New()
	payload.EventID = eventID.String()
	payload.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(payload)
	return entity.OutboxEvent{
		ID: eventID, EventType: eventType, SchemaVersion: schemaVersionAccessEventV1,
		AggregateType: aggregateType, AggregateID: aggregateID, Payload: raw, OccurredAt: occurredAt,
	}
}

func (s *Service) createdEvent(
	eventType string,
	aggregateType string,
	aggregateID uuid.UUID,
	occurredAt time.Time,
	options ...accessEventPayloadOption,
) entity.OutboxEvent {
	payload := value.AccessEventPayload{}
	for _, option := range options {
		option(&payload)
	}
	return s.event(eventType, aggregateType, aggregateID, payload, occurredAt)
}

func payloadString(field accessPayloadStringField, text string) accessEventPayloadOption {
	return func(payload *value.AccessEventPayload) {
		switch field {
		case accessPayloadOrganizationID:
			payload.OrganizationID = text
		case accessPayloadKind:
			payload.Kind = text
		case accessPayloadStatus:
			payload.Status = text
		case accessPayloadGroupID:
			payload.GroupID = text
		case accessPayloadScopeType:
			payload.ScopeType = text
		case accessPayloadScopeID:
			payload.ScopeID = text
		case accessPayloadExternalAccountID:
			payload.ExternalAccountID = text
		case accessPayloadExternalProviderID:
			payload.ExternalProviderID = text
		case accessPayloadAccountType:
			payload.AccountType = text
		case accessPayloadExternalAccountBindingID:
			payload.ExternalAccountBindingID = text
		case accessPayloadUsageScopeType:
			payload.UsageScopeType = text
		case accessPayloadUsageScopeID:
			payload.UsageScopeID = text
		}
	}
}

func payloadVersion(version int64) accessEventPayloadOption {
	return func(payload *value.AccessEventPayload) {
		payload.Version = version
	}
}

func (s *Service) now(meta value.CommandMeta) time.Time {
	if !meta.OccurredAt.IsZero() {
		return meta.OccurredAt.UTC()
	}
	return s.clock.Now().UTC()
}

func defaultOrganizationStatus(status enum.OrganizationStatus) enum.OrganizationStatus {
	if status == "" {
		return enum.OrganizationStatusActive
	}
	return status
}

func defaultUserStatus(status enum.UserStatus) enum.UserStatus {
	if status == "" {
		return enum.UserStatusPending
	}
	return status
}

func defaultMembershipStatus(status enum.MembershipStatus) enum.MembershipStatus {
	if status == "" {
		return enum.MembershipStatusActive
	}
	return status
}

func defaultMembershipSource(source enum.MembershipSource) enum.MembershipSource {
	if source == "" {
		return enum.MembershipSourceManual
	}
	return source
}

func defaultAllowlistStatus(status enum.AllowlistStatus) enum.AllowlistStatus {
	if status == "" {
		return enum.AllowlistStatusActive
	}
	return status
}

func defaultExternalProviderStatus(status enum.ExternalProviderStatus) enum.ExternalProviderStatus {
	if status == "" {
		return enum.ExternalProviderStatusActive
	}
	return status
}

func defaultExternalAccountStatus(status enum.ExternalAccountStatus) enum.ExternalAccountStatus {
	if status == "" {
		return enum.ExternalAccountStatusPending
	}
	return status
}

func defaultExternalAccountBindingStatus(status enum.ExternalAccountBindingStatus) enum.ExternalAccountBindingStatus {
	if status == "" {
		return enum.ExternalAccountBindingStatusActive
	}
	return status
}

func defaultAccessActionStatus(status enum.AccessActionStatus) enum.AccessActionStatus {
	if status == "" {
		return enum.AccessActionStatusActive
	}
	return status
}

func defaultAccessRuleStatus(status enum.AccessRuleStatus) enum.AccessRuleStatus {
	if status == "" {
		return enum.AccessRuleStatusActive
	}
	return status
}

func decisionByUserStatus(status enum.UserStatus) enum.AccessDecision {
	switch status {
	case enum.UserStatusActive:
		return enum.AccessDecisionAllow
	case enum.UserStatusPending:
		return enum.AccessDecisionPending
	default:
		return enum.AccessDecisionDeny
	}
}

func sameUUIDPtr(a *uuid.UUID, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func sortedUnique(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func policyVersion(rules []entity.AccessRule) int64 {
	var version int64
	for _, rule := range rules {
		if rule.Version > version {
			version = rule.Version
		}
	}
	return version
}

func ruleExplanations(rules []entity.AccessRule, reasonCode string) []value.RuleExplanation {
	explanations := make([]value.RuleExplanation, 0, len(rules))
	for _, rule := range rules {
		explanations = append(explanations, value.RuleExplanation{
			RuleID: rule.ID, Effect: string(rule.Effect),
			Subject:   value.SubjectRef{Type: string(rule.SubjectType), ID: rule.SubjectID},
			ActionKey: rule.ActionKey, Scope: value.ScopeRef{Type: rule.ScopeType, ID: rule.ScopeID},
			Priority: rule.Priority, ReasonCode: reasonCode,
		})
	}
	return explanations
}
