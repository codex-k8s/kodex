package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

const (
	defaultPendingAccessLimit = 50
	maxPendingAccessLimit     = 200
)

// SetUserStatus changes a user lifecycle status and records an auditable domain event.
func (s *Service) SetUserStatus(ctx context.Context, input SetUserStatusInput) (entity.User, error) {
	if input.UserID == uuid.Nil || !validUserStatus(input.Status) {
		return entity.User{}, errs.ErrInvalidArgument
	}
	if input.Meta.ExpectedVersion == nil {
		return entity.User{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta); err != nil {
		return entity.User{}, err
	}
	replayed, ok, err := loadAppliedCommand(ctx, s, input.Meta, accessOperationSetUserStatus, accessAggregateUser, s.repository.GetUser)
	if err != nil {
		return entity.User{}, err
	}
	if ok {
		if err := s.requireAllowedForUserStatus(ctx, input.Meta, replayed); err != nil {
			return entity.User{}, err
		}
		return replayed, nil
	}
	existing, err := s.repository.GetUser(ctx, input.UserID)
	if err != nil {
		return entity.User{}, err
	}
	if err := s.requireAllowedForUserStatus(ctx, input.Meta, existing); err != nil {
		return entity.User{}, err
	}
	if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
		return entity.User{}, err
	}
	if existing.Status == input.Status {
		return existing, nil
	}

	now := s.now(input.Meta)
	user := existing
	user.Base = updateBase(existing.Base, now)
	user.Status = input.Status
	reasonCode := strings.TrimSpace(input.Meta.Reason)
	if reasonCode == "" {
		reasonCode = reasonUserStatusChanged
	}
	event, err := s.event(accessEventUserStatusChanged, accessAggregateUser, user.ID, value.AccessEventPayload{
		UserID:     user.ID.String(),
		OldStatus:  string(existing.Status),
		NewStatus:  string(user.Status),
		ReasonCode: reasonCode,
		Version:    user.Version,
	}, now)
	if err != nil {
		return entity.User{}, err
	}
	result, err := commandResult(input.Meta, accessOperationSetUserStatus, accessAggregateUser, user.ID, now)
	if err != nil {
		return entity.User{}, err
	}
	if err := s.repository.UpdateUser(ctx, user, existing.Version, event, &result); err != nil {
		return entity.User{}, err
	}
	return user, nil
}

// DisableAllowlistEntry disables an allowlist rule without deleting its history.
func (s *Service) DisableAllowlistEntry(ctx context.Context, input DisableAllowlistEntryInput) (entity.AllowlistEntry, error) {
	if input.AllowlistEntryID == uuid.Nil {
		return entity.AllowlistEntry{}, errs.ErrInvalidArgument
	}
	if input.Meta.ExpectedVersion == nil {
		return entity.AllowlistEntry{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta); err != nil {
		return entity.AllowlistEntry{}, err
	}
	replayed, ok, err := loadAppliedCommand(ctx, s, input.Meta, accessOperationDisableAllowlistEntry, accessAggregateAllowlistEntry, s.repository.GetAllowlistEntry)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	if ok {
		if err := s.requireAllowedForAllowlistEntry(ctx, input.Meta, replayed); err != nil {
			return entity.AllowlistEntry{}, err
		}
		return replayed, nil
	}
	existing, err := s.repository.GetAllowlistEntry(ctx, input.AllowlistEntryID)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	if err := s.requireAllowedForAllowlistEntry(ctx, input.Meta, existing); err != nil {
		return entity.AllowlistEntry{}, err
	}
	if err := ensureExpectedVersion(input.Meta, existing.Version); err != nil {
		return entity.AllowlistEntry{}, err
	}
	if existing.Status == enum.AllowlistStatusDisabled {
		return existing, nil
	}

	now := s.now(input.Meta)
	entry := existing
	entry.Base = updateBase(existing.Base, now)
	entry.Status = enum.AllowlistStatusDisabled
	reasonCode := strings.TrimSpace(input.Meta.Reason)
	if reasonCode == "" {
		reasonCode = reasonAllowlistEntryClosed
	}
	event, err := s.event(accessEventAllowlistEntryDisabled, accessAggregateAllowlistEntry, entry.ID, value.AccessEventPayload{
		AllowlistEntryID: entry.ID.String(),
		MatchType:        string(entry.MatchType),
		OldStatus:        string(existing.Status),
		NewStatus:        string(entry.Status),
		ReasonCode:       reasonCode,
		Version:          entry.Version,
	}, now)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	result, err := commandResult(input.Meta, accessOperationDisableAllowlistEntry, accessAggregateAllowlistEntry, entry.ID, now)
	if err != nil {
		return entity.AllowlistEntry{}, err
	}
	if err := s.repository.UpdateAllowlistEntry(ctx, entry, existing.Version, event, &result); err != nil {
		return entity.AllowlistEntry{}, err
	}
	return entry, nil
}

// ListPendingAccess returns operator-visible access items that require action.
func (s *Service) ListPendingAccess(ctx context.Context, input ListPendingAccessInput) (ListPendingAccessResult, error) {
	limit, offset, err := pendingAccessPage(input.Limit, input.Cursor)
	if err != nil {
		return ListPendingAccessResult{}, err
	}
	scope, err := normalizeOptionalScope(input.Scope)
	if err != nil {
		return ListPendingAccessResult{}, err
	}
	if err := s.requireAllowed(ctx, input.Meta, accessActionListPendingAccess, value.ResourceRef{
		Type: accessResourcePendingAccess,
	}, scope); err != nil {
		return ListPendingAccessResult{}, err
	}
	items, err := s.repository.ListPendingAccess(ctx, query.PendingAccessFilter{
		Scope:  scope,
		Limit:  limit + 1,
		Offset: offset,
	})
	if err != nil {
		return ListPendingAccessResult{}, err
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = strconv.Itoa(offset + limit)
	}
	return ListPendingAccessResult{Items: items, NextCursor: nextCursor}, nil
}

func loadAppliedCommand[T any](
	ctx context.Context,
	service *Service,
	meta value.CommandMeta,
	operation string,
	aggregateType string,
	load func(context.Context, uuid.UUID) (T, error),
) (T, bool, error) {
	var zero T
	applied, ok, err := service.findCommandResult(ctx, meta, operation, aggregateType)
	if err != nil || !ok {
		return zero, ok, err
	}
	value, err := load(ctx, applied.AggregateID)
	if err != nil {
		return zero, false, err
	}
	return value, true, nil
}

func (s *Service) requireAllowedForUserStatus(ctx context.Context, meta value.CommandMeta, user entity.User) error {
	scopes, err := s.repository.ListUserAccessScopes(ctx, user.ID)
	if err != nil {
		return err
	}
	return s.requireAllowedInAnyScope(ctx, meta, accessActionSetUserStatus, value.ResourceRef{
		Type: accessResourceUser,
		ID:   user.ID.String(),
	}, scopes)
}

func (s *Service) requireAllowedForAllowlistEntry(ctx context.Context, meta value.CommandMeta, entry entity.AllowlistEntry) error {
	return s.requireAllowed(ctx, meta, accessActionDisableAllowlistEntry, value.ResourceRef{
		Type: accessResourceAllowlistEntry,
		ID:   entry.ID.String(),
	}, allowlistEntryAccessScope(entry))
}

func allowlistEntryAccessScope(entry entity.AllowlistEntry) value.ScopeRef {
	if entry.OrganizationID == nil {
		return value.ScopeRef{Type: accessRuleScopeGlobal}
	}
	return value.ScopeRef{Type: accessRuleScopeOrganization, ID: entry.OrganizationID.String()}
}

func validUserStatus(status enum.UserStatus) bool {
	switch status {
	case enum.UserStatusActive, enum.UserStatusPending, enum.UserStatusBlocked, enum.UserStatusDisabled:
		return true
	default:
		return false
	}
}

func pendingAccessPage(limit int, cursor string) (int, int, error) {
	if limit < 0 {
		return 0, 0, errs.ErrInvalidArgument
	}
	if limit == 0 {
		limit = defaultPendingAccessLimit
	}
	if limit > maxPendingAccessLimit {
		limit = maxPendingAccessLimit
	}
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return limit, 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, 0, errs.ErrInvalidArgument
	}
	return limit, offset, nil
}

func normalizeOptionalScope(scope value.ScopeRef) (value.ScopeRef, error) {
	scope.Type = strings.TrimSpace(scope.Type)
	scope.ID = strings.TrimSpace(scope.ID)
	if scope.Type == "" {
		if scope.ID != "" {
			return value.ScopeRef{}, errs.ErrInvalidArgument
		}
		return scope, nil
	}
	if scope.Type == accessRuleScopeGlobal {
		if scope.ID != "" {
			return value.ScopeRef{}, errs.ErrInvalidArgument
		}
		return scope, nil
	}
	if scope.ID == "" {
		return value.ScopeRef{}, errs.ErrInvalidArgument
	}
	return scope, nil
}
