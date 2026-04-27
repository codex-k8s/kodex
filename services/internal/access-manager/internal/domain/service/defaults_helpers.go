package service

import (
	"time"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

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

func defaultAllowlistDefaultStatus(status enum.UserStatus) (enum.UserStatus, error) {
	if status == "" {
		return enum.UserStatusPending, nil
	}
	if status != enum.UserStatusActive && status != enum.UserStatusPending {
		return "", errs.ErrInvalidArgument
	}
	return status, nil
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
