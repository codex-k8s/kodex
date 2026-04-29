package service

import (
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func updateBase(existing entity.Base, now time.Time) entity.Base {
	return entity.Base{
		ID:        existing.ID,
		Version:   existing.Version + 1,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: now,
	}
}

func ensureExpectedVersion(meta value.CommandMeta, current int64) error {
	if meta.ExpectedVersion != nil && *meta.ExpectedVersion != current {
		return errs.ErrConflict
	}
	return nil
}

func sameAllowlistEntryState(a entity.AllowlistEntry, b entity.AllowlistEntry) bool {
	return a.MatchType == b.MatchType &&
		a.Value == b.Value &&
		sameUUIDPtr(a.OrganizationID, b.OrganizationID) &&
		a.DefaultStatus == b.DefaultStatus &&
		a.Status == b.Status
}

func sameExternalProviderState(a entity.ExternalProvider, b entity.ExternalProvider) bool {
	return catalogState{
		Key:    a.Slug,
		Kind:   string(a.ProviderKind),
		Name:   a.DisplayName,
		Extra:  a.IconAssetRef,
		Status: string(a.Status),
	} == catalogState{
		Key:    b.Slug,
		Kind:   string(b.ProviderKind),
		Name:   b.DisplayName,
		Extra:  b.IconAssetRef,
		Status: string(b.Status),
	}
}

func sameExternalAccountBindingState(a entity.ExternalAccountBinding, b entity.ExternalAccountBinding) bool {
	return a.ExternalAccountID == b.ExternalAccountID &&
		a.UsageScopeType == b.UsageScopeType &&
		a.UsageScopeID == b.UsageScopeID &&
		a.Status == b.Status &&
		slices.Equal(a.AllowedActionKeys, b.AllowedActionKeys)
}

func sameAccessActionState(a entity.AccessAction, b entity.AccessAction) bool {
	return catalogState{
		Key:    a.Key,
		Kind:   a.ResourceType,
		Name:   a.DisplayName,
		Extra:  a.Description,
		Status: string(a.Status),
	} == catalogState{
		Key:    b.Key,
		Kind:   b.ResourceType,
		Name:   b.DisplayName,
		Extra:  b.Description,
		Status: string(b.Status),
	}
}

type catalogState struct {
	Key    string
	Kind   string
	Name   string
	Extra  string
	Status string
}

func sameAccessRuleState(a entity.AccessRule, b entity.AccessRule) bool {
	return a.Effect == b.Effect &&
		a.SubjectType == b.SubjectType &&
		a.SubjectID == b.SubjectID &&
		a.ActionKey == b.ActionKey &&
		a.ResourceType == b.ResourceType &&
		a.ResourceID == b.ResourceID &&
		a.ScopeType == b.ScopeType &&
		a.ScopeID == b.ScopeID &&
		a.Priority == b.Priority &&
		a.Status == b.Status
}

func normalizeExternalAccountOwnerScope(scopeType enum.ExternalAccountScopeType, scopeID string) (enum.ExternalAccountScopeType, string, error) {
	scopeID = strings.TrimSpace(scopeID)
	if scopeType == "" {
		scopeType = enum.ExternalAccountScopeGlobal
	}
	switch scopeType {
	case enum.ExternalAccountScopeGlobal:
		if scopeID != "" {
			return "", "", errs.ErrInvalidArgument
		}
		return scopeType, "", nil
	case enum.ExternalAccountScopeOrganization,
		enum.ExternalAccountScopeProject,
		enum.ExternalAccountScopeRepository,
		enum.ExternalAccountScopeUser,
		enum.ExternalAccountScopeGroup,
		enum.ExternalAccountScopeAgent,
		enum.ExternalAccountScopeAgentRole,
		enum.ExternalAccountScopeFlow,
		enum.ExternalAccountScopePackage:
		if scopeID == "" {
			return "", "", errs.ErrInvalidArgument
		}
		return scopeType, scopeID, nil
	default:
		return "", "", errs.ErrInvalidArgument
	}
}

func validateExternalAccountUsageScope(scopeType enum.ExternalAccountScopeType, scopeID string) error {
	if strings.TrimSpace(scopeID) == "" {
		return errs.ErrInvalidArgument
	}
	switch scopeType {
	case enum.ExternalAccountScopeOrganization,
		enum.ExternalAccountScopeProject,
		enum.ExternalAccountScopeRepository,
		enum.ExternalAccountScopeUser,
		enum.ExternalAccountScopeGroup,
		enum.ExternalAccountScopeAgent,
		enum.ExternalAccountScopeAgentRole,
		enum.ExternalAccountScopeFlow,
		enum.ExternalAccountScopeStage,
		enum.ExternalAccountScopePackage:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}
