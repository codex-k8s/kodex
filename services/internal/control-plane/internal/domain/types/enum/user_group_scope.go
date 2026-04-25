package enum

// UserGroupScope identifies whether a group is global or organization-scoped.
type UserGroupScope string

const (
	UserGroupScopeGlobal       UserGroupScope = "global"
	UserGroupScopeOrganization UserGroupScope = "organization"
)
