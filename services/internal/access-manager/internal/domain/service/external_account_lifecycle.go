package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/helpers"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// UpdateExternalProvider changes provider metadata or disables the provider catalog entry.
func (s *Service) UpdateExternalProvider(ctx context.Context, input UpdateExternalProviderInput) (entity.ExternalProvider, error) {
	if input.ExternalProviderID == uuid.Nil || input.Meta.ExpectedVersion == nil {
		return entity.ExternalProvider{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta); err != nil {
		return entity.ExternalProvider{}, err
	}
	if err := s.requireAllowed(ctx, input.Meta, accessActionManageExternalProvider, value.ResourceRef{
		Type: accessResourceExternalProvider,
		ID:   input.ExternalProviderID.String(),
	}, value.ScopeRef{}); err != nil {
		return entity.ExternalProvider{}, err
	}
	applied, ok, err := s.findCommandResult(ctx, input.Meta, accessOperationUpdateExternalProvider, accessAggregateExternalProvider)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	if ok {
		return s.repository.GetExternalProvider(ctx, applied.AggregateID)
	}
	existing, err := s.repository.GetExternalProvider(ctx, input.ExternalProviderID)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
		return entity.ExternalProvider{}, err
	}
	provider, err := updatedExternalProvider(existing, input)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	if sameExternalProviderState(existing, provider) {
		return existing, nil
	}

	now := s.now(input.Meta)
	provider.Base = updateBase(existing.Base, now)
	eventType := accessEventExternalProviderUpdated
	payload := value.AccessEventPayload{
		ExternalProviderID: provider.ID.String(),
		Slug:               provider.Slug,
		Version:            provider.Version,
	}
	if existing.Status != enum.ExternalProviderStatusDisabled && provider.Status == enum.ExternalProviderStatusDisabled {
		eventType = accessEventExternalProviderDisabled
		payload.ReasonCode = defaultReason(input.Meta.Reason, reasonExternalProviderOff)
	}
	event, err := s.event(eventType, accessAggregateExternalProvider, provider.ID, payload, now)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	result, err := commandResult(input.Meta, accessOperationUpdateExternalProvider, accessAggregateExternalProvider, provider.ID, now)
	if err != nil {
		return entity.ExternalProvider{}, err
	}
	if err := s.repository.UpdateExternalProvider(ctx, provider, existing.Version, event, &result); err != nil {
		return entity.ExternalProvider{}, err
	}
	return provider, nil
}

// UpdateExternalAccountStatus changes an external-account lifecycle status.
func (s *Service) UpdateExternalAccountStatus(ctx context.Context, input UpdateExternalAccountStatusInput) (entity.ExternalAccount, error) {
	if input.ExternalAccountID == uuid.Nil || !validExternalAccountStatus(input.Status) || input.Meta.ExpectedVersion == nil {
		return entity.ExternalAccount{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta); err != nil {
		return entity.ExternalAccount{}, err
	}
	if err := s.requireAllowed(ctx, input.Meta, accessActionManageExternalAccount, value.ResourceRef{
		Type: accessResourceExternalAccount,
		ID:   input.ExternalAccountID.String(),
	}, value.ScopeRef{}); err != nil {
		return entity.ExternalAccount{}, err
	}
	applied, ok, err := s.findCommandResult(ctx, input.Meta, accessOperationUpdateExternalAccountStatus, accessAggregateExternalAccount)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	if ok {
		return s.repository.GetExternalAccount(ctx, applied.AggregateID)
	}
	existing, err := s.repository.GetExternalAccount(ctx, input.ExternalAccountID)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
		return entity.ExternalAccount{}, err
	}
	if existing.Status == input.Status {
		return existing, nil
	}

	now := s.now(input.Meta)
	account := existing
	account.Base = updateBase(existing.Base, now)
	account.Status = input.Status
	event, err := s.event(accessEventExternalAccountStatusChanged, accessAggregateExternalAccount, account.ID, value.AccessEventPayload{
		ExternalAccountID: account.ID.String(),
		OldStatus:         string(existing.Status),
		NewStatus:         string(account.Status),
		ReasonCode:        defaultReason(input.Meta.Reason, reasonExternalAccountState),
		Version:           account.Version,
	}, now)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	result, err := commandResult(input.Meta, accessOperationUpdateExternalAccountStatus, accessAggregateExternalAccount, account.ID, now)
	if err != nil {
		return entity.ExternalAccount{}, err
	}
	if err := s.repository.UpdateExternalAccount(ctx, account, existing.Version, event, &result); err != nil {
		return entity.ExternalAccount{}, err
	}
	return account, nil
}

// DisableExternalAccountBinding disables an external-account usage binding without deleting history.
func (s *Service) DisableExternalAccountBinding(ctx context.Context, input DisableExternalAccountBindingInput) (entity.ExternalAccountBinding, error) {
	if input.ExternalAccountBindingID == uuid.Nil || input.Meta.ExpectedVersion == nil {
		return entity.ExternalAccountBinding{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	if err := s.requireAllowed(ctx, input.Meta, accessActionManageExternalAccountBinding, value.ResourceRef{
		Type: accessResourceExternalAccountBinding,
		ID:   input.ExternalAccountBindingID.String(),
	}, value.ScopeRef{}); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	applied, ok, err := s.findCommandResult(ctx, input.Meta, accessOperationDisableExternalAccountBinding, accessAggregateExternalAccountBinding)
	if err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	if ok {
		return s.repository.GetExternalAccountBinding(ctx, applied.AggregateID)
	}
	existing, err := s.repository.GetExternalAccountBinding(ctx, input.ExternalAccountBindingID)
	if err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	if existing.Status == enum.ExternalAccountBindingStatusDisabled {
		return existing, nil
	}

	now := s.now(input.Meta)
	binding := existing
	binding.Base = updateBase(existing.Base, now)
	binding.Status = enum.ExternalAccountBindingStatusDisabled
	event, err := s.event(accessEventExternalAccountBindingDisabled, accessAggregateExternalAccountBinding, binding.ID, value.AccessEventPayload{
		ExternalAccountBindingID: binding.ID.String(),
		ExternalAccountID:        binding.ExternalAccountID.String(),
		ReasonCode:               defaultReason(input.Meta.Reason, reasonExternalBindingOff),
		Version:                  binding.Version,
	}, now)
	if err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	result, err := commandResult(input.Meta, accessOperationDisableExternalAccountBinding, accessAggregateExternalAccountBinding, binding.ID, now)
	if err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	if err := s.repository.UpdateExternalAccountBinding(ctx, binding, existing.Version, event, &result); err != nil {
		return entity.ExternalAccountBinding{}, err
	}
	return binding, nil
}

func updatedExternalProvider(existing entity.ExternalProvider, input UpdateExternalProviderInput) (entity.ExternalProvider, error) {
	provider := existing
	if slug := strings.TrimSpace(input.Slug); slug != "" {
		provider.Slug = helpers.NormalizeSlug(slug)
	}
	if input.ProviderKind != "" {
		if !validExternalProviderKind(input.ProviderKind) {
			return entity.ExternalProvider{}, errs.ErrInvalidArgument
		}
		provider.ProviderKind = input.ProviderKind
	}
	if displayName := strings.TrimSpace(input.DisplayName); displayName != "" {
		provider.DisplayName = displayName
	}
	if input.IconAssetRef != "" {
		provider.IconAssetRef = strings.TrimSpace(input.IconAssetRef)
	}
	if input.Status != "" {
		if !validExternalProviderStatus(input.Status) {
			return entity.ExternalProvider{}, errs.ErrInvalidArgument
		}
		provider.Status = input.Status
	}
	if provider.Slug == "" || provider.DisplayName == "" {
		return entity.ExternalProvider{}, errs.ErrInvalidArgument
	}
	return provider, nil
}

func validExternalProviderKind(kind enum.ExternalProviderKind) bool {
	switch kind {
	case enum.ExternalProviderRepository,
		enum.ExternalProviderIdentity,
		enum.ExternalProviderModel,
		enum.ExternalProviderMessaging,
		enum.ExternalProviderPayments,
		enum.ExternalProviderOther:
		return true
	default:
		return false
	}
}

func validExternalProviderStatus(status enum.ExternalProviderStatus) bool {
	switch status {
	case enum.ExternalProviderStatusActive, enum.ExternalProviderStatusDisabled:
		return true
	default:
		return false
	}
}

func validExternalAccountStatus(status enum.ExternalAccountStatus) bool {
	switch status {
	case enum.ExternalAccountStatusActive,
		enum.ExternalAccountStatusPending,
		enum.ExternalAccountStatusNeedsReauth,
		enum.ExternalAccountStatusLimited,
		enum.ExternalAccountStatusBlocked,
		enum.ExternalAccountStatusDisabled:
		return true
	default:
		return false
	}
}

func defaultReason(reason string, fallback string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return fallback
}
