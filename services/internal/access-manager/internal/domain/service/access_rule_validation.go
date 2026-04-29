package service

import "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"

func validateAccessRuleScope(scopeType string, scopeID string) error {
	switch scopeType {
	case accessRuleScopeGlobal:
		if scopeID != "" {
			return errs.ErrInvalidArgument
		}
		return nil
	case accessRuleScopeOrganization, accessRuleScopeProject, accessRuleScopeRepository:
		if scopeID == "" {
			return errs.ErrInvalidArgument
		}
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}
