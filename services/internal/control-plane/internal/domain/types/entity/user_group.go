package entity

import enumtypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"

// UserGroup is a global or organization-scoped user group.
type UserGroup struct {
	ID             string
	OrganizationID *string
	Scope          enumtypes.UserGroupScope
	Slug           string
	Name           string
}
