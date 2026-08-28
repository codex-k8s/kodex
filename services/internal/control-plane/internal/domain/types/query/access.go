package query

type AccessBindingFilter struct {
	Page
	SubjectKind, SubjectRef, RoleRef, ProjectRef string
	IncludeRevoked                               bool
}
