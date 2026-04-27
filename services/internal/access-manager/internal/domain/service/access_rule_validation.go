package service

import "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"

func validateAccessRuleScope(scopeType string, scopeID string) error {
	switch scopeType {
	case "global":
		if scopeID != "" {
			return errs.ErrInvalidArgument
		}
		return nil
	case "organization", "project", "repository":
		if scopeID == "" {
			return errs.ErrInvalidArgument
		}
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}
