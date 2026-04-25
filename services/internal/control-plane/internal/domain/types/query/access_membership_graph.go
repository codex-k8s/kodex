package query

import entitytypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// AccessMembershipGraphSnapshot is a typed read-model for operator inspection
// of organizations, groups and user memberships.
type AccessMembershipGraphSnapshot struct {
	Organizations           []entitytypes.Organization
	Groups                  []entitytypes.UserGroup
	OrganizationMemberships []OrganizationMembershipView
	UserGroupMemberships    []UserGroupMembershipView
}

// OrganizationMembershipView joins membership with user email for operator use.
type OrganizationMembershipView struct {
	OrganizationID string
	UserID         string
	Email          string
	Role           string
}

// UserGroupMembershipView joins group membership with user email for operator use.
type UserGroupMembershipView struct {
	GroupID string
	UserID  string
	Email   string
}
