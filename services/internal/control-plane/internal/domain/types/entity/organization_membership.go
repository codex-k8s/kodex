package entity

import enumtypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"

// OrganizationMembership links a user to an organization.
type OrganizationMembership struct {
	OrganizationID string
	UserID         string
	Role           enumtypes.OrganizationMembershipRole
}
