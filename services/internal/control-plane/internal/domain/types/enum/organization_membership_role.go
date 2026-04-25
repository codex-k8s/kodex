package enum

// OrganizationMembershipRole defines the membership role within an organization.
type OrganizationMembershipRole string

const (
	OrganizationMembershipRoleMember OrganizationMembershipRole = "member"
	OrganizationMembershipRoleAdmin  OrganizationMembershipRole = "admin"
	OrganizationMembershipRoleOwner  OrganizationMembershipRole = "owner"
)
