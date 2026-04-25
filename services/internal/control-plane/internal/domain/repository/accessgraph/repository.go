package accessgraph

import (
	"context"

	entitytypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	querytypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

type Organization = entitytypes.Organization
type UserGroup = entitytypes.UserGroup
type OrganizationMembershipView = querytypes.OrganizationMembershipView
type UserGroupMembershipView = querytypes.UserGroupMembershipView

// Repository stores and loads the foundational access membership graph.
type Repository interface {
	// EnsureBootstrapOwnerOrganizationMembership creates the canonical owner
	// organization when missing and links the bootstrap owner user to it.
	EnsureBootstrapOwnerOrganizationMembership(ctx context.Context, userID string) error
	// ListOrganizations returns organizations visible in the foundation graph.
	ListOrganizations(ctx context.Context, limit int) ([]Organization, error)
	// ListGroups returns user groups visible in the foundation graph.
	ListGroups(ctx context.Context, limit int) ([]UserGroup, error)
	// ListOrganizationMemberships returns organization memberships with user emails.
	ListOrganizationMemberships(ctx context.Context, limit int) ([]OrganizationMembershipView, error)
	// ListUserGroupMemberships returns user group memberships with user emails.
	ListUserGroupMemberships(ctx context.Context, limit int) ([]UserGroupMembershipView, error)
}
