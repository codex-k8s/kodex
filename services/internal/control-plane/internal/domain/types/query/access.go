package query

// AccessBindingFilter describes a paged lookup of access bindings by subject,
// role, project and aliases; it is selection data, not proof of read authority.
// AccessBindingFilter задаёт постраничный поиск связей доступа по субъекту,
// роли, проекту и псевдонимам; это условия отбора, а не право на чтение.
type AccessBindingFilter struct {
	Page
	SubjectKind, SubjectRef, RoleRef, ProjectRef string
	Query                                        string
	Aliases                                      []string
	IncludeRevoked                               bool
}
