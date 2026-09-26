package query

type AccessBindingFilter struct {
	Page
	SubjectKind, SubjectRef, RoleRef, ProjectRef string
	Query                                        string
	Aliases                                      []string
	IncludeRevoked                               bool
}
